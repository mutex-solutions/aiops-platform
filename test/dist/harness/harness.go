// SPDX-License-Identifier: Apache-2.0

// Package harness is what the suites that drive the built test binary share
// (test/failure-matrix, test/recomposition): it builds test/dist/cmd/aiops-test once,
// starts it on a fresh home, reads the two files it writes (the status of the
// launcher and the state of test-state), submits a recomposition through the control
// file of test-submit, and stops and reaps it. It is test code: no shipped
// distribution links it.
//
// Nothing here sleeps to synchronise. A wait is a bounded poll of those files, and it
// ends at once when the process exits, because what a dead process wrote cannot
// change.
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/internal/serve"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/internal/proc"
)

// PollInterval is the period of every poll of the process's files.
const PollInterval = 10 * time.Millisecond

// SkipMessage is printed when a suite that runs the binary is skipped under -short.
const SkipMessage = "SKIPPED under -short: this suite builds and runs the test binary; CI runs it in the s1-suites job"

var (
	buildOnce sync.Once
	buildDir  string
	buildBin  string
	buildErr  error
)

// distDir is test/dist, found from this file so that a test runs from any directory.
func distDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(file))
}

// PatchFile returns the absolute path of a file of test/dist/patches.
func PatchFile(name string) string { return filepath.Join(distDir(), "patches", name) }

// Binary builds the test binary once per test process and returns its path. A test
// that needs it under -short is skipped, loudly. Call Cleanup from TestMain.
func Binary(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Log(SkipMessage)
		t.Skip(SkipMessage)
	}
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "aiops-test-bin-")
		if buildErr != nil {
			return
		}
		buildBin = filepath.Join(buildDir, "aiops-test")
		cmd := exec.Command("go", "build", "-o", buildBin, ".")
		cmd.Dir = filepath.Join(distDir(), "cmd", "aiops-test")
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildBin
}

// Cleanup removes the built binary.
func Cleanup() {
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
}

// Limits are the kernel deadlines of the tests: a fault that passes one is seen
// within seconds. They are the limits of test/dist/limits.yaml, written out so that a
// suite names them (the binary would use them anyway; --limits makes it explicit).
const Limits = "loadDeadline: 1s\ndrainDeadline: 1s\ndisposeDeadline: 1s\nshutdownBound: 10s\nleakGrace: 500ms\n"

// Options is how a process is started.
type Options struct {
	Profile string          // default "smoke"
	Role    string          // default: not passed
	Patches []string        // --patch files, in order
	Env     []string        // extra environment variables, NAME=value
	Limits  string          // content of the --limits file; default Limits
	Context context.Context // ends the process when done (a test timeout); default Background
}

// syncBuf is the combined output of the process.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// Proc is one running (or ended) test binary.
type Proc struct {
	Home   string
	cmd    *exec.Cmd
	child  *proc.Child
	out    *syncBuf
	exited chan struct{}
	code   int
}

// Start runs the binary with a fresh home. The caller must call Stop or Kill; a test
// registers it with t.Cleanup.
func Start(bin string, o Options) (*Proc, error) {
	home, err := os.MkdirTemp("", "aiops-test-home-")
	if err != nil {
		return nil, err
	}
	lim := o.Limits
	if lim == "" {
		lim = Limits
	}
	limFile := filepath.Join(home, "limits.yaml")
	if err := os.WriteFile(limFile, []byte(lim), 0o644); err != nil {
		return nil, err
	}
	profile := o.Profile
	if profile == "" {
		profile = "smoke"
	}
	args := []string{"--profile", profile, "--home", home, "--limits", limFile}
	if o.Role != "" {
		args = append(args, "--role", o.Role)
	}
	for _, p := range o.Patches {
		args = append(args, "--patch", p)
	}
	p := &Proc{Home: home, out: &syncBuf{}, exited: make(chan struct{})}
	ctx := o.Context
	if ctx == nil {
		ctx = context.Background()
	}
	p.cmd = proc.Command(ctx, bin, args...)
	p.cmd.Env = append(os.Environ(), o.Env...)
	p.cmd.Stdout, p.cmd.Stderr = p.out, p.out
	// proc starts it so that it dies with this process (see package proc).
	child, err := proc.Start(p.cmd)
	if err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	p.child = child
	go func() {
		<-child.Done()
		p.code = p.cmd.ProcessState.ExitCode()
		close(p.exited)
	}()
	return p, nil
}

// Output is everything the process wrote to stdout and stderr, in order.
func (p *Proc) Output() string { return p.out.String() }

// Exited is closed when the process has ended and been reaped.
func (p *Proc) Exited() <-chan struct{} { return p.exited }

// Code is the exit code, -1 for a process ended by a signal. Valid after Exited.
func (p *Proc) Code() int { return p.code }

// RunDir is the runtime directory of the home.
func (p *Proc) RunDir() string { return filepath.Join(p.Home, "run") }

// Stop asks for a clean stop with SIGTERM and returns the exit code; a process that
// does not end within the bound is killed and the code is -1.
func (p *Proc) Stop(bound time.Duration) int {
	select {
	case <-p.exited:
	default:
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.exited:
		case <-time.After(bound):
			p.Kill()
			return -1
		}
	}
	return p.code
}

// Kill ends the process at once and reaps it.
func (p *Proc) Kill() {
	select {
	case <-p.exited:
	default:
		p.child.Kill()
		<-p.exited
	}
}

// Release ends the process if it still runs and removes the home.
func (p *Proc) Release() {
	p.Kill()
	_ = os.RemoveAll(p.Home)
}

// Snapshot is what the process's files said at one poll.
type Snapshot struct {
	Status      serve.Status
	HaveStatus  bool
	State       teststate.State
	HaveState   bool
	Exited      bool
	Code        int
	Output      string
	SubmitError string // the error of the last submission, from the result file
}

func (p *Proc) read(name string, v any) bool {
	data, err := os.ReadFile(filepath.Join(p.RunDir(), name))
	return err == nil && json.Unmarshal(data, v) == nil
}

// Snap reads the files now. When the process has ended, the files are read after it
// ended, so a snapshot with Exited holds the last word.
func (p *Proc) Snap() Snapshot {
	var s Snapshot
	select {
	case <-p.exited:
		s.Exited, s.Code = true, p.code
	default:
	}
	// The output is read first: the launcher drops readiness before it says why, so a
	// status read after the output is never older than what the output tells.
	s.Output = p.Output()
	s.HaveStatus = p.read(serve.StatusFile, &s.Status)
	s.HaveState = p.read(teststate.File, &s.State)
	var res struct {
		Error string `json:"error"`
	}
	if p.read("submit.result.json", &res) {
		s.SubmitError = res.Error
	}
	return s
}

// Until polls until ok holds for a snapshot and returns it. It gives up at the
// deadline, and at once when the process has ended and ok does not hold for the last
// word. The bool is whether ok held.
func (p *Proc) Until(limit time.Duration, ok func(Snapshot) bool) (Snapshot, bool) {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	tick := time.NewTicker(PollInterval)
	defer tick.Stop()
	for {
		s := p.Snap()
		if ok(s) {
			return s, true
		}
		if s.Exited {
			return s, false
		}
		select {
		case <-tick.C:
		case <-p.exited:
		case <-deadline.C:
			return p.Snap(), false
		}
	}
}

// Submit writes the control file of test-submit atomically; each distinct marker is
// one submission.
func (p *Proc) Submit(marker string) error {
	control := filepath.Join(p.RunDir(), "submit.json")
	if err := os.WriteFile(control+".tmp", []byte(marker), 0o644); err != nil {
		return err
	}
	return os.Rename(control+".tmp", control)
}

// Touch creates an empty file in the runtime directory: the trigger of a fault.
func (p *Proc) Touch(name string) error {
	return os.WriteFile(filepath.Join(p.RunDir(), name), nil, 0o644)
}

// RowState is the state of row id in the snapshot, "" when the state file does not
// hold it or it has no fiber.
func (s Snapshot) RowState(id string) string {
	r, _ := s.Row(id)
	return r.State
}

// Row returns row id of the state file, false when it does not hold it.
func (s Snapshot) Row(id string) (teststate.Row, bool) {
	for _, r := range s.State.Rows {
		if r.ID == id {
			return r, true
		}
	}
	return teststate.Row{}, false
}

// Running reports whether the launcher says it finished booting and is serving.
func (s Snapshot) Running() bool { return s.HaveStatus && s.Status.Phase == "running" && !s.Exited }

// OpDone reports whether reload finished operation id, in the state file.
func (s Snapshot) OpDone(id uint64) bool {
	op := s.State.Reload
	return op != nil && op.ID >= id && op.Status != "queued" && op.Status != "running"
}

// ReadJSON reads a JSON file of the runtime directory into v, false when it is absent
// or not whole yet.
func (p *Proc) ReadJSON(name string, v any) bool { return p.read(name, v) }
