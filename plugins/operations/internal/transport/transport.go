// SPDX-License-Identifier: Apache-2.0

// Package transport is the client of the socket of the row reload-socket, internal to
// the operations plugin (ADR-062 decision 16): from the read of <home>/run/status.json
// to the second submission. The library's Install, Enable, Disable and Remove call
// Submit while they hold the write lock and the snapshot of 08:374, and do with the
// files what Outcome.Restore says (ADR-062 decision 11). It speaks wire format
// version 1 of sdk/def/reload/v0 and dials with net.DialUnix and the network unix
// only, the named exception of the dial rule (decision 14).
package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"

	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
)

// The defaults of --connect-wait and --wait (ADR-062 decisions 9 and 10), and how
// often a dial is tried again within the connect window. The default window is the
// load, drain and dispose deadlines of reload and reload-socket at the defaults of
// ADR-059 decision 30; a site whose --limits raises them raises the flag.
const (
	DefaultConnectWait = 60 * time.Second
	DefaultWait        = 10 * time.Minute
	retryEvery         = 100 * time.Millisecond
)

// Request is one submission of a command.
type Request struct {
	Home    string // $AIOPS_HOME, from which the paths derive; the row's dir is never read
	Profile string // the profile the command changed
	Command string // reload.CommandEnable, CommandDisable, CommandRemove or CommandInstall
	// ConnectWait bounds each connection, retried every 100 ms; Wait bounds the
	// whole wait for the final result. Zero takes the default.
	ConnectWait, Wait time.Duration
}

// Kind is the row of the outcome table of ADR-062 decision 11 that applies.
type Kind string

// The kinds.
const (
	// Final: a final result, in Result. With status abandoned it holds state-unknown,
	// since one without it is submitted once more.
	Final Kind = "final"
	// Refused: an error line before an id existed, in Error: peer, busy, request,
	// version, or a refusal of Submit.
	Refused Kind = "refused"
	// NotRunning: status.json is absent, says stopped or names another profile; the
	// change applies when the process starts.
	NotRunning Kind = "not-running"
	// RestartRequired: a process runs the profile and the socket did not answer
	// within the connect window; the change applies at the next start.
	RestartRequired Kind = "restart-required"
	// NoFinal: the second submission ended with no final result either.
	NoFinal Kind = "no-final"
	// Unknown: Wait elapsed, or the connection was lost beyond the connect window,
	// after a request was sent.
	Unknown Kind = "unknown"
)

// Outcome is what a submission came to.
type Outcome struct {
	Kind   Kind
	Result *reload.WireResult // Final
	Error  *reload.WireError  // Refused
	Socket string             // the socket dialled
	IDs    []reload.ID        // every operation id the row answered, oldest first
	Last   reload.Status      // the last status the row answered, "" for none
	// untouched: NoFinal where each operation was abandoned without state-unknown, so
	// no generation touched the tree.
	untouched bool
}

// Restore reports whether the command restores its snapshot, the Files column of the
// table of ADR-062 decision 11: after a refusal before an id, a final result that
// refuses or fails the generation, and a second operation abandoned before its
// generation touched the tree. Otherwise the files are kept: they apply at the next
// start, the tree runs them, or the verdict is unknown.
func (o Outcome) Restore() bool {
	switch o.Kind {
	case Refused:
		return true
	case Final:
		return o.Result.Status != reload.Passed
	case NoFinal:
		return o.untouched
	}
	return false
}

// Submit submits r.Command for r.Profile to the process that runs it on r.Home and
// waits for the outcome (ADR-062 decisions 9 and 10). An end of the connection after
// an id awaits that id on a new connection; an end before the first line, unknown on
// an await, and a result abandoned without state-unknown submit once more, at most
// once. It returns an error for a request it cannot send and, with the outcome so
// far, when ctx ends.
func Submit(ctx context.Context, r Request) (Outcome, error) {
	if r.Home == "" || r.Profile == "" {
		return Outcome{}, errors.New("transport: a submission names its home and profile")
	}
	sock := filepath.Join(r.Home, reload.RunDir, reload.SocketFile)
	if len(sock) > reload.MaxSocketPath {
		return Outcome{}, fmt.Errorf("transport: the socket path %s is %d bytes; Linux holds at most %d", sock, len(sock), reload.MaxSocketPath)
	}
	if !running(r.Home, r.Profile) {
		return Outcome{Kind: NotRunning, Socket: sock}, nil
	}
	if r.ConnectWait <= 0 {
		r.ConnectWait = DefaultConnectWait
	}
	if r.Wait <= 0 {
		r.Wait = DefaultWait
	}
	s := &session{r: r, o: Outcome{Socket: sock, untouched: true}, until: time.Now().Add(r.Wait)}
	return s.run(ctx)
}

// running reports whether status.json says that a process runs profile on home. A
// file that does not read as JSON names no phase, so it counts as running.
func running(home, profile string) bool {
	b, err := os.ReadFile(filepath.Join(home, reload.RunDir, reload.StatusFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	var st reload.ProcessStatus
	if err != nil || json.Unmarshal(b, &st) != nil {
		return true
	}
	return st.Phase != reload.PhaseStopped && st.Profile == profile
}

// session is one submission and its second, if any.
type session struct {
	r      Request
	o      Outcome
	until  time.Time // the end of Wait
	id     reload.ID // the operation awaited, 0 before the row answered one
	second bool      // the second submission was made
	sent   bool      // a request reached a connection
}

func (s *session) run(ctx context.Context) (Outcome, error) {
	for again := false; ; again = true {
		conn, err := s.dial(ctx, again)
		switch {
		case ctx.Err() != nil:
			return s.end(Unknown), ctx.Err()
		case err != nil && !s.sent:
			return s.end(RestartRequired), nil
		case err != nil:
			return s.end(Unknown), nil
		}
		k, again := s.exchange(ctx, conn)
		_ = conn.Close()
		switch {
		case ctx.Err() != nil:
			return s.end(Unknown), ctx.Err()
		case !again:
			return s.end(k), nil
		}
	}
}

// end returns the outcome of kind k.
func (s *session) end(k Kind) Outcome {
	s.o.Kind = k
	return s.o
}

// dial connects within the connect window, trying again every 100 ms while the
// socket is absent or refuses. A connection made again, after the row ended or
// refused the last one, waits 100 ms first.
func (s *session) dial(ctx context.Context, pause bool) (*net.UnixConn, error) {
	addr := &net.UnixAddr{Name: s.o.Socket, Net: "unix"}
	end := time.Now().Add(s.r.ConnectWait)
	for {
		if pause {
			t := time.NewTimer(retryEvery)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			}
		}
		conn, err := net.DialUnix("unix", nil, addr)
		if err == nil || !time.Now().Before(end) {
			return conn, err
		}
		pause = true
	}
}

// exchange sends one request on conn and reads its lines. It returns the kind of the
// outcome, or again when the session goes on with a new connection.
func (s *session) exchange(ctx context.Context, conn *net.UnixConn) (k Kind, again bool) {
	req := reload.Request{V: reload.WireVersion, Op: reload.OpSubmit, Command: s.r.Command}
	if s.id != 0 {
		req = reload.Request{V: reload.WireVersion, Op: reload.OpAwait, ID: s.id}
	}
	_ = conn.SetWriteDeadline(s.until)
	// A refusal may close the connection before the request is written; its line is
	// still read below.
	if _, err := conn.Write(reload.Line(req)); err == nil {
		s.sent = true
	}
	rd := bufio.NewReader(conn)
	for {
		line, err := s.line(ctx, conn, rd)
		if err != nil {
			return s.lost(err)
		}
		reply, err := reload.DecodeReply(line)
		if err != nil || reply.V != reload.WireVersion {
			s.o.Error = reload.NewError(fmt.Sprintf("transport: a reply line of another form: %q", line))
			return Unknown, false
		}
		if e := reply.Error; e != nil {
			return s.refused(e, req.Op)
		}
		r := reply.Result
		if s.id != r.ID {
			s.id, s.o.IDs = r.ID, append(s.o.IDs, r.ID)
		}
		s.o.Last = r.Status
		if !r.Status.Done() {
			continue
		}
		if r.Status == reload.Abandoned && !r.Error.Has(reload.CauseStateUnknown) {
			// The generation never touched the tree: its submitter submits again
			// (ADR-059 decision 48).
			return s.resubmit(true)
		}
		s.o.Result = r
		return Final, false
	}
}

// line reads one line within Wait, waking every 100 ms to see whether ctx ended.
func (s *session) line(ctx context.Context, conn net.Conn, rd *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		tick := time.Now().Add(retryEvery)
		if tick.After(s.until) {
			tick = s.until
		}
		_ = conn.SetReadDeadline(tick)
		b, err := rd.ReadBytes('\n')
		line = append(line, b...)
		var ne net.Error
		if err == nil || !errors.As(err, &ne) || !ne.Timeout() || !time.Now().Before(s.until) {
			return line, err
		}
		if ctx.Err() != nil {
			return line, ctx.Err()
		}
	}
}

// refused handles an error line, which is the last of its connection.
func (s *session) refused(e *reload.WireError, op string) (Kind, bool) {
	switch {
	case op == reload.OpSubmit:
		s.o.Error = e
		return Refused, false
	case e.Has(reload.CauseUnknown): // the process was replaced or the history evicted the id
		return s.resubmit(false)
	case e.Has(reload.CauseBusy) && time.Now().Before(s.until):
		return "", true // the socket refuses: await again on a new connection
	}
	s.o.Error = e
	return Unknown, false
}

// lost handles the end of a connection before a final line: an end of file means that
// the row restarted (ADR-062 decision 10).
func (s *session) lost(err error) (Kind, bool) {
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return Unknown, false // Wait elapsed
	case s.id != 0:
		return "", true // await the id on a new connection
	}
	return s.resubmit(false) // no line came: submit once more
}

// resubmit submits once more, at most once per command. untouched says that the
// operation that ended was abandoned before its generation touched the tree.
func (s *session) resubmit(untouched bool) (Kind, bool) {
	s.o.untouched = s.o.untouched && untouched
	if s.second {
		return NoFinal, false
	}
	s.second, s.id = true, 0
	return "", true
}
