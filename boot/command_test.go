// SPDX-License-Identifier: Apache-2.0

package boot

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The codes of RunOptions.Command, which aiopsctl sets (ADR-061 decision 13; ADR-059
// decision 29): a stop that an ExitCommand request began returns its code, 0, 1 or 2,
// when the tree disposed in time with no leak, and ExitLeak otherwise; a signal or the
// end of ctx returns ExitInterrupted; where boot would return ExitUsage it returns
// ExitInternal. So a launcher code of 0 or 2 is always a command's.

// commandExit is a row that requests the end of the command with code as it mounts.
func commandExit(code int) ModuleEntry {
	return toy(toySpec{id: "t/command", needs: []kernel.AnyKey{launch.AppExitKey}, apply: func(c *kernel.Context, _ any) error {
		launch.AppExitKey.Of(c).RequestExit(launch.ExitRequest{Reason: launch.ExitCommand, Detail: "the command returned", Code: code})
		return nil
	}})
}

func commandFixture(t *testing.T, code int, extra ...ModuleEntry) *fixture {
	bundle := happyBundle + "    - {id: command, plugin: t/command, roles: [api]}\n"
	for _, e := range extra {
		bundle += fmt.Sprintf("    - {id: %s, plugin: %s, roles: [api]}\n", strings.TrimPrefix(e.ID, "t/"), e.ID)
	}
	f := newFixture(t, bundle, append([]ModuleEntry{commandExit(code)}, extra...)...)
	bindLaunch(f, "t/command")
	f.opt.Command = true
	return f
}

func TestCommandReturnsTheCodeOfTheCommand(t *testing.T) {
	for _, code := range []int{ExitOK, ExitRefused, ExitUsage} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			f := commandFixture(t, code)
			if got := f.run(t.Context()); got != code {
				t.Fatalf("exit %d, want %d; stderr:\n%s", got, code, f.errb.String())
			}
			if s := f.lastStatus(); s.Exit != code || s.Phase != PhaseStopped {
				t.Fatalf("final status %+v", s)
			}
		})
	}
}

// Any other code with the reason ExitCommand is a launcher failure, and so is the reason
// in a launcher that runs no command: the codes of aiops do not change.
func TestCommandRefusesACodeOutsideTheThree(t *testing.T) {
	f := commandFixture(t, ExitRequired)
	if got := f.run(t.Context()); got != ExitInternal || !strings.Contains(f.errb.String(), "command: the command returned") {
		t.Fatalf("exit %d; stderr:\n%s", got, f.errb.String())
	}
	f = commandFixture(t, ExitOK)
	f.opt.Command = false
	if got := f.run(t.Context()); got != ExitInternal {
		t.Fatalf("exit %d without Command; stderr:\n%s", got, f.errb.String())
	}
}

// A command that returned and then leaked at shutdown is not a clean stop.
func TestCommandThatLeaksAtShutdown(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	f := commandFixture(t, ExitUsage, slowDispose(make(chan struct{}), release))
	f.opt.Limits.DisposeDeadline = 30 * time.Millisecond
	f.opt.Limits.ShutdownBound = 300 * time.Millisecond
	if got := f.run(t.Context()); got != ExitLeak || !strings.Contains(f.errb.String(), "shutdown did not finish cleanly") {
		t.Fatalf("exit %d; stderr:\n%s", got, f.errb.String())
	}
}

// A signal or the end of ctx is an interruption, after boot or during it, whatever the
// shutdown leaves: no command code is read.
func TestCommandInterrupted(t *testing.T) {
	t.Run("signal", func(t *testing.T) {
		f := newFixture(t, happyBundle)
		f.opt.Command = true
		done := f.start(t.Context())
		f.waitReady(t, done)
		if got := stopAndWait(t, f, done); got != ExitInterrupted || !strings.Contains(f.errb.String(), "aiops: exit 8: signal") {
			t.Fatalf("exit %d; stderr:\n%s", got, f.errb.String())
		}
	})
	t.Run("context", func(t *testing.T) {
		f := newFixture(t, happyBundle)
		f.opt.Command = true
		ctx, cancel := context.WithCancel(t.Context())
		done := f.start(ctx)
		f.waitReady(t, done)
		cancel()
		if got := <-done; got != ExitInterrupted {
			t.Fatalf("exit %d; stderr:\n%s", got, f.errb.String())
		}
	})
	t.Run("during boot", func(t *testing.T) {
		entered := make(chan struct{})
		slow := toy(toySpec{id: "t/slowboot", apply: func(c *kernel.Context, _ any) error {
			close(entered)
			<-c.Std().Done()
			return c.Std().Err()
		}})
		f := newFixture(t, happyBundle+"    - {id: slow, plugin: t/slowboot, roles: [api]}\n", slow)
		f.opt.Command = true
		done := f.start(t.Context())
		<-entered
		if got := stopAndWait(t, f, done); got != ExitInterrupted {
			t.Fatalf("exit %d; stderr:\n%s", got, f.errb.String())
		}
	})
	t.Run("leak", func(t *testing.T) {
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		f := newFixture(t, happyBundle+"    - {id: slow, plugin: t/slow, roles: [api]}\n", slowDispose(make(chan struct{}), release))
		f.opt.Command = true
		f.opt.Limits.DisposeDeadline = 30 * time.Millisecond
		f.opt.Limits.ShutdownBound = 300 * time.Millisecond
		done := f.start(t.Context())
		f.waitReady(t, done)
		if got := stopAndWait(t, f, done); got != ExitInterrupted {
			t.Fatalf("exit %d; stderr:\n%s", got, f.errb.String())
		}
	})
}

// Under Command a usage error of the launcher is its failure: 2 is the command's.
func TestCommandUsageOfTheLauncherIsInternal(t *testing.T) {
	f := newFixture(t, happyBundle)
	f.opt.Command, f.opt.Role = true, "root"
	if got := f.run(t.Context()); got != ExitInternal || !strings.Contains(f.errb.String(), `role "root"`) {
		t.Fatalf("exit %d; stderr:\n%s", got, f.errb.String())
	}
}
