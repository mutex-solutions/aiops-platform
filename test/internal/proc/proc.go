// SPDX-License-Identifier: Apache-2.0

// Package proc starts the child processes of the test suites so that a child can
// never outlive the test process, whatever ends the test process.
//
// Three layers, each covering what the one before cannot:
//   - Kill (from t.Cleanup) kills the child's process group and waits for it (a test that ends
//     normally or fails);
//   - exec.Cmd.Cancel bound to the test's context kills the group, and WaitDelay
//     bounds the wait (a test that times out);
//   - on Linux, Pdeathsig SIGKILL makes the kernel kill the child when the thread
//     that started it dies (the test process killed from outside, before any Go
//     code ran).
package proc

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// WaitDelay bounds how long Wait waits for the child's output pipes after the child
// is killed.
const WaitDelay = 5 * time.Second

// Child is a started process. Its Wait runs in the goroutine that started it.
type Child struct {
	Cmd  *exec.Cmd
	done chan struct{}
	err  error
}

// Command is exec.CommandContext with the group, Cancel and WaitDelay settings.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = WaitDelay
	setAttr(cmd)
	return cmd
}

// Start starts cmd (made by Command). The caller must call Kill, usually from
// t.Cleanup.
//
// Pdeathsig refers to the OS thread that forked the child, not to the process: when
// that thread exits, the kernel signals the child. A Go goroutine moves between
// threads, so the fork is done from a dedicated goroutine that locks its thread and
// does not return until the child has been reaped. The same goroutine runs Wait, so
// the thread lives exactly as long as the child does; a locked goroutine that
// returned earlier would end its thread and, with it, the child.
func Start(cmd *exec.Cmd) (*Child, error) {
	c := &Child{Cmd: cmd, done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(c.done)
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		c.err = cmd.Wait()
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return c, nil
}

// Done is closed when the child has ended and been reaped.
func (c *Child) Done() <-chan struct{} { return c.done }

// Err is the result of Wait. Valid after Done.
func (c *Child) Err() error { return c.err }

// Kill kills the child's whole process group and waits until it is reaped.
func (c *Child) Kill() {
	select {
	case <-c.done:
	default:
		_ = killGroup(c.Cmd)
		<-c.done
	}
}
