// SPDX-License-Identifier: Apache-2.0

package reloadsocket

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/reload-socket/internal/socket"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	reloadfake "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// short are the bounds of most tests: the fixed ones but a quick notice, so that a
// client that closed its end frees its place at once.
var short = bounds{conns: maxConns, read: readWait, write: writeWait, notice: 20 * time.Millisecond}

// world is a kernel root with the launch and reload fakes and a directory of mode
// 0700 for the socket.
type world struct {
	t      *testing.T
	root   *kernel.Root
	launch *launchfake.Launcher
	rl     *reloadfake.Reload
	dir    string
}

func newWorld(t *testing.T, opts ...kerneltest.Option) *world {
	t.Helper()
	w := &world{t: t, root: kerneltest.New(t, opts...), dir: privateDir(t)}
	var lp, rp kernel.Plugin
	w.launch, lp = launchfake.New()
	w.rl, rp = reloadfake.New()
	kerneltest.Mount(w.root, lp, rp)
	return w
}

// shortTemp is a new directory under the system's temporary directory that the test
// removes. Its name holds no test name, so a socket path under it stays within the 107
// bytes of Linux where TMPDIR is long, as the builder sets it under its scratch directory
// when the release build runs the tests of each first-party plugin (ADR-060 decision 50).
func shortTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "rs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// privateDir is a new directory of mode 0700 under the test's temporary directory.
func privateDir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(shortTemp(t), "run")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

// mount mounts the row with bounds b and config cfg and returns its fiber and the
// error of its activation.
func (w *world) mount(b bounds, cfg map[string]any) (*kernel.Fiber, error) {
	w.t.Helper()
	f := w.root.Plugin(newPlugin(b), cfg)
	return f, f.Await(w.t.Context())
}

// serve mounts the row on w.dir and fails the test unless it is ACTIVE.
func (w *world) serve(b bounds) *kernel.Fiber {
	w.t.Helper()
	f, err := w.mount(b, map[string]any{"dir": w.dir})
	if st, _ := f.State(w.t.Context()); err != nil || st != kernel.Active {
		w.t.Fatalf("row is %s: %v", st, err)
	}
	return f
}

// fails mounts the row with cfg and returns why it failed.
func (w *world) fails(cfg map[string]any) string {
	w.t.Helper()
	f, err := w.mount(short, cfg)
	if st, _ := f.State(w.t.Context()); err == nil || st != kernel.Failed {
		w.t.Fatalf("row is %s (%v), want FAILED", st, err)
	}
	return err.Error()
}

// client is one connection to the socket.
type client struct {
	t    *testing.T
	conn *net.UnixConn
	r    *bufio.Reader
}

func (w *world) dial() *client {
	w.t.Helper()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: filepath.Join(w.dir, reload.SocketFile), Net: "unix"})
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = conn.Close() })
	return &client{t: w.t, conn: conn, r: bufio.NewReader(conn)}
}

func (c *client) send(line string) *client {
	c.t.Helper()
	if _, err := c.conn.Write([]byte(line)); err != nil {
		c.t.Fatal(err)
	}
	return c
}

func (c *client) reply() reload.Reply {
	c.t.Helper()
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("reply: %q, %v", line, err)
	}
	r, err := reload.DecodeReply(line)
	if err != nil || r.V != reload.WireVersion {
		c.t.Fatalf("reply %q: %+v, %v", line, r, err)
	}
	return r
}

// result reads a reply line that holds a result of status want.
func (c *client) result(want reload.Status) reload.WireResult {
	c.t.Helper()
	r := c.reply()
	if r.Result == nil || r.Result.Status != want {
		c.t.Fatalf("reply %+v (error %+v), want a result %s", r.Result, r.Error, want)
	}
	return *r.Result
}

// refused reads an error line of cause, which is the last: the row closes.
func (c *client) refused(cause, text string) {
	c.t.Helper()
	r := c.reply()
	if r.Error == nil || !slices.Equal(r.Error.Causes, []string{cause}) || !strings.Contains(r.Error.Text, text) {
		c.t.Fatalf("reply %+v (result %+v), want the error %s with %q", r.Error, r.Result, cause, text)
	}
	c.closed()
}

// closed reads the end of the connection.
func (c *client) closed() {
	c.t.Helper()
	if line, err := c.r.ReadBytes('\n'); !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
		c.t.Fatalf("after the last line: %q, %v; want the end of the connection", line, err)
	}
}

const submitEnable = `{"v":1,"op":"submit","command":"plugin enable"}` + "\n"

func await(id reload.ID) string {
	return `{"v":1,"op":"await","id":` + strings.TrimSpace(string(reload.Line(id))) + "}\n"
}

// held submits an operation to the fake that stays queued until Finish.
func (w *world) held() reload.ID {
	w.t.Helper()
	w.rl.Next = &reload.Result{Status: reload.Queued}
	id, err := w.rl.Submit(w.t.Context(), reload.Op{Source: "aiopsctl plugin disable"})
	if err != nil {
		w.t.Fatal(err)
	}
	w.rl.Next = nil
	return id
}

func TestSubmitAnswersTheResultAsItStandsThenTheFinalOne(t *testing.T) {
	w := newWorld(t)
	w.serve(short)
	w.rl.Next = &reload.Result{Status: reload.Queued}
	c := w.dial().send(submitEnable)
	first := c.result(reload.Queued)
	if first.ID != 1 || first.Source != "aiopsctl plugin enable" {
		t.Fatalf("first line %+v", first)
	}
	if subs := w.rl.Submissions(); len(subs) != 1 || subs[0].Source != "aiopsctl plugin enable" {
		t.Fatalf("submissions %v", subs)
	}
	w.rl.Finish(first.ID, reload.Result{Status: reload.Passed, Stage: reload.StagePublish, Generation: 3})
	if last := c.result(reload.Passed); last.ID != 1 || last.Generation != 3 {
		t.Fatalf("final line %+v", last)
	}
	c.closed()

	// A submission whose result is final at once answers it once.
	w.rl.Next = nil
	c = w.dial().send(`{"v":1,"op":"submit","command":"plugin remove"}` + "\n")
	if r := c.result(reload.Passed); r.ID != 2 || r.Source != "aiopsctl plugin remove" {
		t.Fatalf("line %+v", r)
	}
	c.closed()
}

func TestARefusedSubmissionAnswersItsCause(t *testing.T) {
	w := newWorld(t)
	w.serve(short)
	w.rl.SubmitErr = reload.ErrQueueFull
	w.dial().send(submitEnable).refused(reload.CauseQueueFull, "queue is full")
}

func TestAwaitOfAFinalIDAnswersOneLine(t *testing.T) {
	w := newWorld(t)
	w.serve(short)
	id, _ := w.rl.Submit(t.Context(), reload.Op{Source: "aiopsctl plugin install"})
	c := w.dial().send(await(id))
	if r := c.result(reload.Passed); r.ID != id || r.Source != "aiopsctl plugin install" {
		t.Fatalf("line %+v", r)
	}
	c.closed()
}

func TestAwaitOfAnUnknownIDAnswersUnknown(t *testing.T) {
	w := newWorld(t)
	w.serve(short)
	w.dial().send(await(99)).refused(reload.CauseUnknown, "unknown operation")
}

func TestEachMalformedRequestGetsItsRefusal(t *testing.T) {
	w := newWorld(t)
	w.serve(short)
	long := `{"v":1,"op":"submit","command":"plugin enable","pad":"` + strings.Repeat("x", reload.MaxRequest) + `"}` + "\n"
	for _, c := range []struct{ name, line, cause, text string }{
		{"over 4,096 bytes", long, reload.CauseRequest, "at most 4096 bytes"},
		{"an unknown op", `{"v":1,"op":"purge"}` + "\n", reload.CauseRequest, `unknown op "purge"; version 1 serves submit and await`},
		{"an unknown command", `{"v":1,"op":"submit","command":"plugin purge"}` + "\n", reload.CauseRequest, `unknown command "plugin purge"; version 1 serves plugin enable, plugin disable, plugin remove, plugin install`},
		{"an await with no id", `{"v":1,"op":"await"}` + "\n", reload.CauseRequest, "await names the id"},
		{"not JSON", "enable extra\n", reload.CauseRequest, "one JSON object"},
		{"version 2", `{"v":2,"op":"submit","command":"plugin enable"}` + "\n", reload.CauseVersion, "version 2 is not served; this row serves version 1"},
		{"no newline before the end", `{"v":1,"op":"submit","command":"plugin enable"}`, reload.CauseRequest, "one line ended by a newline"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl := w.dial().send(c.line)
			if !strings.HasSuffix(c.line, "\n") {
				_ = cl.conn.CloseWrite()
			}
			cl.refused(c.cause, c.text)
		})
	}
	if subs := w.rl.Submissions(); len(subs) != 0 {
		t.Errorf("a refused request submitted %v", subs)
	}
}

// A request of exactly 4,096 bytes with its newline is read.
func TestARequestOf4096BytesIsServed(t *testing.T) {
	w := newWorld(t)
	w.serve(short)
	head := `{"v":1,"op":"submit","command":"plugin enable","pad":"`
	line := head + strings.Repeat("x", reload.MaxRequest-len(head)-3) + `"}` + "\n"
	if len(line) != reload.MaxRequest {
		t.Fatalf("line of %d bytes", len(line))
	}
	w.dial().send(line).result(reload.Passed)
}

func TestAClientThatSendsNothingIsRefusedAtTheReadBound(t *testing.T) {
	w := newWorld(t)
	b := short
	b.read = 50 * time.Millisecond
	w.serve(b)
	w.dial().refused(reload.CauseRequest, "no request within 50ms of the accept")
}

// The ninth open connection gets busy, and a client that closes its end while it
// awaits frees its place within the notice bound; its operation goes on in reload.
func TestTheNinthConnectionIsBusyUntilAClientCloses(t *testing.T) {
	w := newWorld(t)
	w.serve(short)
	id := w.held()
	var open []*client
	for range maxConns {
		c := w.dial().send(await(id))
		c.result(reload.Queued)
		open = append(open, c)
	}
	w.dial().refused(reload.CauseBusy, "8 connections are open")
	_ = open[0].conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c := w.dial()
		_, _ = c.conn.Write([]byte(await(id))) // a busy refusal may close the connection first
		r := c.reply()
		if r.Result != nil && r.Result.Status == reload.Queued {
			open[0] = c
			break
		}
		if !r.Error.Has(reload.CauseBusy) || time.Now().After(deadline) {
			t.Fatalf("a place was not freed: %+v %+v", r.Result, r.Error)
		}
	}
	w.rl.Finish(id, reload.Result{Status: reload.Passed})
	for _, c := range open {
		c.result(reload.Passed)
		c.closed()
	}
}

func TestTheDirectoryMustBeMode0700AndOwnedByTheProcess(t *testing.T) {
	w := newWorld(t)
	if err := os.Chmod(w.dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if got := w.fails(map[string]any{"dir": w.dir}); !strings.Contains(got, w.dir+" must be owned by uid") || !strings.Contains(got, "with mode 0750") {
		t.Errorf("mode 0750: %s", got)
	}
	target := privateDir(t)
	link := filepath.Join(shortTemp(t), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := w.fails(map[string]any{"dir": link}); !strings.Contains(got, link+" is a symlink") {
		t.Errorf("symlink: %s", got)
	}
	file := filepath.Join(shortTemp(t), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := w.fails(map[string]any{"dir": file}); !strings.Contains(got, file+" is not a directory") {
		t.Errorf("file: %s", got)
	}
	if got := w.fails(map[string]any{"dir": filepath.Join(target, "missing")}); !strings.Contains(got, "no such file or directory") {
		t.Errorf("missing: %s", got)
	}
}

func TestALockHeldElsewhereFailsTheRow(t *testing.T) {
	w := newWorld(t)
	lock, err := os.OpenFile(filepath.Join(w.dir, reload.LockFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if got := w.fails(map[string]any{"dir": w.dir}); !strings.Contains(got, "another process serves "+w.dir) {
		t.Errorf("lock held: %s", got)
	}
	if _, err := os.Lstat(filepath.Join(w.dir, reload.SocketFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a row that does not hold the lock made a socket: %v", err)
	}
}

// With no dir, the row serves <home>/run, and a lock held there names the home.
func TestTheDefaultDirectoryIsRunUnderTheHome(t *testing.T) {
	w := newWorld(t)
	if got := w.fails(nil); !strings.Contains(got, "no dir in the config and no home") {
		t.Errorf("no home: %s", got)
	}
	home := filepath.Dir(w.dir)
	w.launch.Flags = map[string]string{"home": home}
	f, err := w.mount(short, nil)
	if st, _ := f.State(t.Context()); err != nil || st != kernel.Active {
		t.Fatalf("row is %s: %v", st, err)
	}
	w.dial().send(submitEnable).result(reload.Passed)
	if got := w.fails(nil); !strings.Contains(got, "another process serves "+home+":") {
		t.Errorf("second row: %s", got)
	}
}

// aiopsctl never listens (ADR-062 decision 2): a row that a layer enables in the
// process of an aiopsctl command, whose launcher alone fills the working directory of
// cmdline, is ACTIVE with no socket and no lock, so a server that starts meanwhile
// serves its own, and the command submits to that server.
func TestAiopsctlNeverListens(t *testing.T) {
	w := newWorld(t)
	w.launch.Flags, w.launch.WorkDir = map[string]string{"home": filepath.Dir(w.dir)}, "/work"
	f, err := w.mount(short, nil)
	if st, _ := f.State(t.Context()); err != nil || st != kernel.Active {
		t.Fatalf("row is %s: %v", st, err)
	}
	for _, name := range []string{"reload.sock", "reload.lock"} {
		if _, err := os.Stat(filepath.Join(w.dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the row in an aiopsctl process made %s: %v", name, err)
		}
	}
}

func TestASocketLeftBehindIsReplaced(t *testing.T) {
	w := newWorld(t)
	sock := filepath.Join(w.dir, reload.SocketFile)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	_ = ln.Close()
	if fi, err := os.Lstat(sock); err != nil || fi.Mode()&fs.ModeSocket == 0 {
		t.Fatalf("no socket left behind: %v", err)
	}
	w.serve(short)
	w.dial().send(submitEnable).result(reload.Passed)
}

func TestASocketPathOver107BytesFailsTheRow(t *testing.T) {
	w := newWorld(t)
	base := shortTemp(t)
	at := func(n int) string { // a directory of mode 0700 whose socket path is n bytes
		d := filepath.Join(base, strings.Repeat("d", n-len(base)-2-len(reload.SocketFile)))
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		return d
	}
	long := at(108)
	if got := w.fails(map[string]any{"dir": long}); !strings.Contains(got, "is 108 bytes; Linux holds at most 107") {
		t.Errorf("108 bytes: %s", got)
	}
	w.dir = at(107)
	w.serve(short)
	w.dial().send(submitEnable).result(reload.Passed)
}

// The socket has mode 0600; after dispose it is gone, the lock is free and the
// disposer reported no error.
func TestTheSocketIsMode0600AndDisposeLeavesNothingHeld(t *testing.T) {
	w := newWorld(t)
	f := w.serve(short)
	sock := filepath.Join(w.dir, reload.SocketFile)
	fi, err := os.Lstat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&fs.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket %v; want a socket of mode 0600", fi.Mode())
	}
	if err := f.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if st, _ := f.State(t.Context()); st != kernel.Disposed {
		t.Fatalf("row is %s, want DISPOSED", st)
	}
	if _, err := os.Lstat(sock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket after dispose: %v", err)
	}
	lock, err := os.OpenFile(filepath.Join(w.dir, reload.LockFile), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Errorf("lock after dispose: %v", err)
	}
}

// An unload while one connection awaits an operation that is not final and another
// has sent nothing ends within 1 s, under a drain deadline of 1 s: every blocking call
// of a connection ends with the activation's context.
func TestAnUnloadWaitsForNoClient(t *testing.T) {
	w := newWorld(t, kerneltest.WithDeadlines(10*time.Second, time.Second, 10*time.Second))
	f := w.serve(bounds{conns: maxConns, read: readWait, write: writeWait, notice: notice})
	awaiting := w.dial().send(await(w.held()))
	awaiting.result(reload.Queued)
	silent := w.dial()
	start := time.Now()
	if err := f.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if took := time.Since(start); took >= time.Second {
		t.Fatalf("the unload took %s", took)
	}
	if st, _ := f.State(t.Context()); st != kernel.Disposed {
		t.Fatalf("row is %s, want DISPOSED", st)
	}
	awaiting.closed()
	silent.closed()
}

func TestOnlyThePeerOfTheProcessUIDIsServed(t *testing.T) {
	for _, c := range []struct {
		peer, own int
		want      bool
	}{
		{1000, 1000, true},
		{0, 1000, false},     // root is refused
		{65534, 1000, false}, // a peer with no mapping reads as the overflow uid
		{1001, 1000, false},
		{0, 0, true},
	} {
		if got := servedPeer(c.peer, c.own); got != c.want {
			t.Errorf("servedPeer(%d, %d) = %v", c.peer, c.own, got)
		}
	}
}

// The real path reads the peer's uid with SO_PEERCRED: the test's own user.
func TestThePeerUIDIsTheTestsOwnUser(t *testing.T) {
	sock := filepath.Join(privateDir(t), "peer.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	conn, err := ln.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if uid, err := socket.PeerUID(conn); err != nil || uid != os.Geteuid() {
		t.Fatalf("PeerUID = %d, %v; want %d", uid, err, os.Geteuid())
	}
}
