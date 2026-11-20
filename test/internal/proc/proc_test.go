// SPDX-License-Identifier: Apache-2.0

package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const modeEnv = "PROC_TEST_MODE"

// TestMain re-executes the test binary in two helper modes: "parent" starts a
// grandchild through Start, records its pid and blocks; "sleep" just blocks.
func TestMain(m *testing.M) {
	switch os.Getenv(modeEnv) {
	case "parent":
		cmd := Command(context.Background(), os.Args[0])
		cmd.Env = append(os.Environ(), modeEnv+"=sleep")
		if _, err := Start(cmd); err != nil {
			os.Exit(2)
		}
		f := os.Getenv("PROC_TEST_PIDFILE")
		_ = os.WriteFile(f+".tmp", []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
		_ = os.Rename(f+".tmp", f)
		select {}
	case "sleep":
		select {}
	}
	os.Exit(m.Run())
}

// gone is true when the pid no longer exists or is a zombie awaiting a reaper.
func gone(pid int) bool {
	if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
		return true
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(data)
	return strings.HasPrefix(s[strings.LastIndexByte(s, ')')+2:], "Z")
}

// waitFor polls ok every few milliseconds until a deadline.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !ok() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("timed out: %s", what)
		}
	}
}

// TestChildDiesWithTestProcess kills a process that holds a grandchild started
// through Start with SIGKILL, so that no cleanup runs, and polls until the
// grandchild is gone.
func TestChildDiesWithTestProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Pdeathsig is Linux only")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	parent := exec.Command(os.Args[0])
	parent.Env = append(os.Environ(), modeEnv+"=parent", "PROC_TEST_PIDFILE="+pidFile)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Process.Kill(); _ = parent.Wait() })
	var pid int
	waitFor(t, "grandchild pid", func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(data))
		return err == nil
	})
	if gone(pid) {
		t.Fatal("grandchild is not running before the kill")
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()
	waitFor(t, "grandchild still running after its parent was killed", func() bool { return gone(pid) })
}

// TestContextReapsGroup: a cancelled context kills the child's group, including a
// grandchild of the shell.
func TestContextReapsGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process groups are Linux only")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := Command(ctx, "sh", "-c", "sleep 300 & echo $!; wait")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(cmd)
	if err == nil {
		t.Cleanup(c.Kill)
	}
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	n, _ := out.Read(buf)
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-c.Done()
	waitFor(t, "sleep still running after cancel", func() bool { return gone(pid) })
}
