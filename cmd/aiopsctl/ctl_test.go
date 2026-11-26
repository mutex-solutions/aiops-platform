// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// installable returns the environment of a home that trusts the test distribution, whose
// dev and cli profiles compose with the test distribution's required set, and the
// layout of the echo pack signed by an author whom the home trusts for acme/*. The home
// is under the system's temporary directory, so its socket path stays under the 107
// bytes of Linux.
func installable(t *testing.T) (env map[string]string, layout string) {
	t.Helper()
	home, err := os.MkdirTemp("", "ctl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	trusted.Write(t, home, testdist.Signed)
	work := t.TempDir()
	err = writeFiles(home, map[string]string{
		"profiles/dev/aiops.patch.yml": "- insert:\n    - {id: spine, plugin: aiops-test/toy-spine, roles: [api], config: {workers: 2}}\n" +
			"    - {id: store, plugin: aiops-test/toy-provider, roles: [api]}\n    - {id: dispatch, plugin: aiops/example-dispatch, roles: [api]}\n",
		"profiles/cli/profile.yaml": "bundles: [aiops-core, aiops-cli, toys]\n",
		"bundles/toys/bundle.yaml":  "- insert:\n    - {id: spine, plugin: aiops-test/toy-spine, config: {workers: 2}}\n    - {id: store, plugin: aiops-test/toy-provider}\n",
	})
	if err == nil {
		err = writeFiles(work, echoPack)
	}
	if err != nil {
		t.Fatal(err)
	}
	env = map[string]string{"PWD": work, "AIOPS_HOME": home, "AIOPS_KEY_PASSWORD": "test"}
	for _, args := range [][]string{{"key", "generate", "author"}, {"trust", "add", "author.pub", "--ids", "acme/*"},
		{"plugin", "pack", "--out", "out"}, {"plugin", "sign", "--key", "author.key", "--out", "out"}} {
		if r := doTest(t, env, standIns, args...); r.code != 0 {
			t.Fatalf("%q: exit %d\n%s", args, r.code, r.errOut)
		}
	}
	return env, filepath.Join(work, "out")
}

// stalled makes run/status.json of home say that a process runs dev, and serves its
// socket as a process whose generation never ends: it takes each request and answers
// nothing. Each request is sent on the channel it returns.
func stalled(t *testing.T, home string) <-chan struct{} {
	t.Helper()
	run := filepath.Join(home, reload.RunDir)
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, reload.StatusFile), reload.Line(reload.ProcessStatus{Phase: "running", Profile: "dev"}), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(run, reload.SocketFile), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	sent := make(chan struct{}, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			if _, err := bufio.NewReader(c).ReadBytes('\n'); err == nil {
				select {
				case sent <- struct{}{}:
				default:
				}
			}
		}
	}()
	return sent
}

// homeTree returns every regular file of home by slash path with its content, but
// write.lock and run/, which the process and the lock own.
func homeTree(t *testing.T, home string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(home, p)
		switch {
		case err != nil:
			return err
		case d.IsDir() && rel == reload.RunDir:
			return filepath.SkipDir
		case d.IsDir() || rel == "write.lock":
			return nil
		}
		b, err := os.ReadFile(p)
		out[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// An install, as a process of its own, that a kill stops after its renames, while it
// waits for the final result of the process that runs dev, leaves its snapshot, and the
// next writer of the home restores the home as it was before the install and says so;
// one that an interrupt stops there restores the home itself and exits 1 (ADR-061
// decisions 17 and 31). The install copies the shipped dev into the home, writes the
// artifact's files, the bundle list and plugins.lock, and unpacks the content first.
func TestAnInstallKilledOrInterruptedIsRestored(t *testing.T) {
	for _, killed := range []bool{true, false} {
		t.Run(map[bool]string{true: "killed", false: "interrupted"}[killed], func(t *testing.T) {
			env, layout := installable(t)
			home := env["AIOPS_HOME"]
			sent := stalled(t, home)
			before := homeTree(t, home)
			var errOut bytes.Buffer
			cmd := ctl(t, env, "plugin", "install", layout, "--profile", "dev")
			cmd.Stderr = &errOut
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-sent:
			case <-time.After(2 * time.Minute):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("the install submitted nothing:\n%s", errOut.String())
			}
			during := homeTree(t, home)
			if _, ok := during["profiles/dev/profile.yaml"]; !ok || !slices.ContainsFunc(slices.Collect(maps.Keys(during)), func(p string) bool { return strings.HasPrefix(p, "snapshot/") }) {
				t.Fatalf("the install submitted before its renames: %v", slices.Sorted(maps.Keys(during)))
			}
			if killed {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				r := ctlRun(t, env, "profile", "sanitize", "dev")
				if r.code != 0 || !strings.Contains(r.errOut, "restored") {
					t.Errorf("the next writer, profile sanitize: exit %d; stderr:\n%s", r.code, r.errOut)
				}
			} else {
				if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
					t.Fatal(err)
				}
				var ee *exec.ExitError
				if err := cmd.Wait(); !errors.As(err, &ee) || ee.ExitCode() != 1 || !strings.Contains(errOut.String(), "interrupted") {
					t.Errorf("the interrupted install: %v; want exit 1, interrupted\n%s", err, errOut.String())
				}
			}
			if after := homeTree(t, home); !maps.Equal(after, before) {
				t.Errorf("the home after the %s install holds %v; want it as before, %v", map[bool]string{true: "killed", false: "interrupted"}[killed],
					slices.Sorted(maps.Keys(after)), slices.Sorted(maps.Keys(before)))
			}
		})
	}
}
