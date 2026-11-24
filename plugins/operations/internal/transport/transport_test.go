// SPDX-License-Identifier: Apache-2.0

package transport_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/operations/internal/transport"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
)

// turn is what the scripted server does with one connection: it reads the request,
// writes lines and closes. busyFirst refuses with busy before it reads anything.
type turn struct {
	lines []string
	busy  bool
}

// server speaks version 1 on <home>/run/reload.sock and scripts its lines.
type server struct {
	t        *testing.T
	home     string
	requests chan reload.Request
}

// newHome is a home whose status.json says that profile dev runs.
func newHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	status(t, home, `{"phase":"running","profile":"dev","ready":true}`)
	return home
}

func status(t *testing.T, home, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "run", "status.json"), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// serve answers one connection per turn, in order, and records each request.
func serve(t *testing.T, home string, turns ...turn) *server {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(home, "run", "reload.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	s := &server{t: t, home: home, requests: make(chan reload.Request, len(turns))}
	done := make(chan struct{})
	t.Cleanup(func() { _ = ln.Close(); <-done })
	go func() {
		defer close(done)
		for _, tu := range turns {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if !tu.busy {
				line, _ := bufio.NewReader(conn).ReadBytes('\n')
				req, err := reload.DecodeRequest(line)
				if err != nil {
					t.Errorf("request %q: %v", line, err)
				}
				s.requests <- req
			}
			for _, l := range tu.lines {
				_, _ = conn.Write([]byte(l))
			}
			_ = conn.Close()
		}
	}()
	return s
}

// sent returns the requests the server read, as "submit <command>" or "await <id>".
func (s *server) sent() []string {
	var out []string
	for {
		select {
		case r := <-s.requests:
			if r.Op == reload.OpAwait {
				out = append(out, "await "+strings.TrimSpace(string(reload.Line(r.ID))))
			} else {
				out = append(out, r.Op+" "+r.Command)
			}
		default:
			return out
		}
	}
}

func result(id reload.ID, st reload.Status, causes ...string) string {
	w := reload.WireResult{ID: id, Source: reload.SourcePrefix + reload.CommandEnable, Status: st}
	if len(causes) > 0 {
		w.Error = reload.NewError("reload: "+strings.Join(causes, ", "), causes...)
	}
	return string(reload.Line(reload.Reply{V: reload.WireVersion, Result: &w}))
}

func refusal(cause string) string {
	return string(reload.Line(reload.Reply{V: reload.WireVersion, Error: reload.NewError("refused: "+cause, cause)}))
}

func submit(t *testing.T, home string) transport.Outcome {
	t.Helper()
	o, err := transport.Submit(t.Context(), transport.Request{Home: home, Profile: "dev", Command: reload.CommandEnable,
		ConnectWait: 300 * time.Millisecond, Wait: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func check(t *testing.T, o transport.Outcome, kind transport.Kind, restore bool, ids []reload.ID, last reload.Status) {
	t.Helper()
	if o.Kind != kind || o.Restore() != restore || !slices.Equal(o.IDs, ids) || o.Last != last {
		t.Fatalf("outcome %s restore=%v ids=%v last=%q (result %+v, error %+v); want %s restore=%v ids=%v last=%q",
			o.Kind, o.Restore(), o.IDs, o.Last, o.Result, o.Error, kind, restore, ids, last)
	}
}

func TestEachFinalResultDecides(t *testing.T) {
	for _, c := range []struct {
		status  reload.Status
		causes  []string
		restore bool
	}{
		{reload.Passed, nil, false},
		{reload.Rejected, []string{reload.CauseDenied}, true},
		{reload.RolledBack, []string{reload.CauseVerdict}, true},
		{reload.RollbackFailed, []string{reload.CauseVerdict, reload.CauseRollback, reload.CauseStateUnknown}, true},
		{reload.Abandoned, []string{reload.CauseAbandoned, reload.CauseStateUnknown}, true}, // restore, and say to restart
	} {
		t.Run(string(c.status), func(t *testing.T) {
			home := newHome(t)
			s := serve(t, home, turn{lines: []string{result(4, reload.Queued), result(4, c.status, c.causes...)}})
			o := submit(t, home)
			check(t, o, transport.Final, c.restore, []reload.ID{4}, c.status)
			var causes []string
			if o.Result.Error != nil {
				causes = o.Result.Error.Causes
			}
			if o.Result.Status != c.status || !slices.Equal(causes, c.causes) {
				t.Errorf("result %+v", o.Result)
			}
			if got := s.sent(); !slices.Equal(got, []string{"submit plugin enable"}) {
				t.Errorf("requests %v", got)
			}
		})
	}
}

func TestEachRefusalBeforeAnIDRestores(t *testing.T) {
	for _, cause := range []string{reload.CausePeer, reload.CauseBusy, reload.CauseRequest, reload.CauseVersion,
		reload.CauseQueueFull, reload.CauseClosed, reload.CauseNested, reload.CauseStateUnknown} {
		t.Run(cause, func(t *testing.T) {
			home := newHome(t)
			serve(t, home, turn{lines: []string{refusal(cause)}})
			o := submit(t, home)
			check(t, o, transport.Refused, true, nil, "")
			if !o.Error.Has(cause) {
				t.Errorf("error %+v", o.Error)
			}
		})
	}
}

// A busy refusal before the request is read, as the row gives the ninth connection,
// still reaches the client.
func TestABusyRefusalBeforeTheRequestRestores(t *testing.T) {
	home := newHome(t)
	serve(t, home, turn{busy: true, lines: []string{refusal(reload.CauseBusy)}})
	check(t, submit(t, home), transport.Refused, true, nil, "")
}

// An end of file before the first reply line submits once more, and the second
// result decides.
func TestAnEndBeforeTheFirstLineSubmitsOnceMore(t *testing.T) {
	home := newHome(t)
	s := serve(t, home, turn{}, turn{lines: []string{result(2, reload.Queued), result(2, reload.Passed)}})
	check(t, submit(t, home), transport.Final, false, []reload.ID{2}, reload.Passed)
	if got := s.sent(); !slices.Equal(got, []string{"submit plugin enable", "submit plugin enable"}) {
		t.Errorf("requests %v", got)
	}
}

// An end of file after the id awaits that id, and a busy await connects again.
func TestAnEndAfterTheIDAwaitsIt(t *testing.T) {
	home := newHome(t)
	s := serve(t, home,
		turn{lines: []string{result(7, reload.Queued)}},
		turn{busy: true, lines: []string{refusal(reload.CauseBusy)}},
		turn{lines: []string{result(7, reload.Running), result(7, reload.RolledBack, reload.CauseVerdict)}})
	check(t, submit(t, home), transport.Final, true, []reload.ID{7}, reload.RolledBack)
	if got := s.sent(); !slices.Equal(got, []string{"submit plugin enable", "await 7"}) {
		t.Errorf("requests %v", got)
	}
}

// unknown on that await submits once more.
func TestUnknownOnTheAwaitSubmitsOnceMore(t *testing.T) {
	home := newHome(t)
	s := serve(t, home,
		turn{lines: []string{result(7, reload.Queued)}},
		turn{lines: []string{refusal(reload.CauseUnknown)}},
		turn{lines: []string{result(1, reload.Passed)}})
	check(t, submit(t, home), transport.Final, false, []reload.ID{7, 1}, reload.Passed)
	if got := s.sent(); !slices.Equal(got, []string{"submit plugin enable", "await 7", "submit plugin enable"}) {
		t.Errorf("requests %v", got)
	}
}

// abandoned without state-unknown submits once more, and a second such result
// restores the snapshot: neither generation touched the tree.
func TestAbandonedTwiceRestores(t *testing.T) {
	home := newHome(t)
	s := serve(t, home,
		turn{lines: []string{result(3, reload.Queued), result(3, reload.Abandoned, reload.CauseAbandoned)}},
		turn{lines: []string{result(4, reload.Abandoned, reload.CauseAbandoned)}})
	check(t, submit(t, home), transport.NoFinal, true, []reload.ID{3, 4}, reload.Abandoned)
	if got := s.sent(); !slices.Equal(got, []string{"submit plugin enable", "submit plugin enable"}) {
		t.Errorf("requests %v", got)
	}
}

// A second outcome with no final result after an end of file keeps the files, with
// both ids: the verdict is unknown.
func TestNoFinalResultOnTheSecondSubmissionKeepsTheFiles(t *testing.T) {
	home := newHome(t)
	serve(t, home,
		turn{lines: []string{result(3, reload.Queued)}},
		turn{lines: []string{refusal(reload.CauseUnknown)}},
		turn{lines: []string{result(9, reload.Running)}},
		turn{lines: []string{refusal(reload.CauseUnknown)}})
	check(t, submit(t, home), transport.NoFinal, false, []reload.ID{3, 9}, reload.Running)

	// An end before the first line, then an abandoned operation: the first may have
	// run, so the files stay.
	home = newHome(t)
	serve(t, home, turn{}, turn{lines: []string{result(5, reload.Abandoned, reload.CauseAbandoned)}})
	check(t, submit(t, home), transport.NoFinal, false, []reload.ID{5}, reload.Abandoned)
}

func TestNoProcessOnTheProfileKeepsTheFiles(t *testing.T) {
	for name, st := range map[string]string{
		"absent":        "",
		"stopped":       `{"phase":"stopped","profile":"dev"}`,
		"other profile": `{"phase":"running","profile":"cli"}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := newHome(t)
			if st == "" {
				if err := os.Remove(filepath.Join(home, "run", "status.json")); err != nil {
					t.Fatal(err)
				}
			} else {
				status(t, home, st)
			}
			check(t, submit(t, home), transport.NotRunning, false, nil, "")
		})
	}
}

// A process runs the profile and nothing answers within the connect window.
func TestNothingAnswersIsRestartRequired(t *testing.T) {
	home := newHome(t)
	o := submit(t, home)
	check(t, o, transport.RestartRequired, false, nil, "")
	if o.Socket != filepath.Join(home, "run", "reload.sock") {
		t.Errorf("socket %s", o.Socket)
	}
	// A status file that does not read names no phase: a process may run.
	status(t, home, "{")
	check(t, submit(t, home), transport.RestartRequired, false, nil, "")
}

// --wait elapsed after the operation was accepted: the files stay, with the id and
// the last status.
func TestWaitElapsedKeepsTheFiles(t *testing.T) {
	home := newHome(t)
	release := make(chan struct{})
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(home, "run", "reload.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { close(release); _ = ln.Close(); <-done })
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(result(6, reload.Running)))
		<-release // holds the connection open and writes nothing more
		_ = conn.Close()
	}()
	o, err := transport.Submit(t.Context(), transport.Request{Home: home, Profile: "dev", Command: reload.CommandEnable,
		ConnectWait: time.Second, Wait: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	check(t, o, transport.Unknown, false, []reload.ID{6}, reload.Running)
}

// The end of ctx ends the wait with the outcome so far.
func TestTheEndOfTheContextEndsTheWait(t *testing.T) {
	home := newHome(t)
	ctx, cancel := context.WithCancel(t.Context())
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(home, "run", "reload.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { _ = ln.Close(); <-done })
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		_, _ = conn.Write([]byte(result(8, reload.Queued)))
		cancel()
		_, _ = conn.Read(make([]byte, 1)) // until the client closes
	}()
	o, err := transport.Submit(ctx, transport.Request{Home: home, Profile: "dev", Command: reload.CommandEnable})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	check(t, o, transport.Unknown, false, []reload.ID{8}, reload.Queued)
}

// A line of another form, or of another version, ends the wait: the verdict is
// unknown, and no second submission follows.
func TestALineOfAnotherFormKeepsTheFiles(t *testing.T) {
	for name, line := range map[string]string{
		"not JSON":  "passed\n",
		"version 2": `{"v":2,"result":{"id":3,"status":"passed"}}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			home := newHome(t)
			s := serve(t, home, turn{lines: []string{result(3, reload.Queued), line}})
			o := submit(t, home)
			check(t, o, transport.Unknown, false, []reload.ID{3}, reload.Queued)
			if o.Error == nil || !strings.Contains(o.Error.Text, "another form") {
				t.Errorf("error %+v", o.Error)
			}
			if got := s.sent(); !slices.Equal(got, []string{"submit plugin enable"}) {
				t.Errorf("requests %v", got)
			}
		})
	}
}

func TestARequestItCannotSendIsAnError(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("h", 120))
	for _, r := range []transport.Request{
		{Home: long, Profile: "dev", Command: reload.CommandEnable},
		{Profile: "dev", Command: reload.CommandEnable},
		{Home: t.TempDir(), Command: reload.CommandEnable},
	} {
		if _, err := transport.Submit(t.Context(), r); err == nil {
			t.Errorf("Submit(%+v) = nil error", r)
		}
	}
	if _, err := transport.Submit(t.Context(), transport.Request{Home: long, Profile: "dev"}); err == nil || !strings.Contains(err.Error(), "Linux holds at most 107") {
		t.Errorf("long path: %v", err)
	}
}
