// SPDX-License-Identifier: Apache-2.0

package readiness_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/mutex-solutions/aiops-platform/plugins/readiness"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// levels records the level of each "not ready" line of the readiness row.
type levels struct {
	mu  *sync.Mutex
	got *[]slog.Level
}

func (h levels) Enabled(context.Context, slog.Level) bool { return true }
func (h levels) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "readiness: not ready, the required set is not ACTIVE" {
		h.mu.Lock()
		*h.got = append(*h.got, r.Level)
		h.mu.Unlock()
	}
	return nil
}
func (h levels) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h levels) WithGroup(string) slog.Handler      { return h }

// A process is not ready while it boots, which is no fault: the row says so at the
// level info until the required set was ACTIVE once, as on every aiopsctl command, and
// warns of a drop after that.
func TestNotReadyWarnsOnlyAfterReady(t *testing.T) {
	var mu sync.Mutex
	var got []slog.Level
	w := newWorld(t, kerneltest.WithConfig(func(c *kernel.Config) { c.LogHandler = levels{&mu, &got} }))
	f := w.root.Plugin(readiness.New(), nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.settle()
	w.rows(kernel.Active, kernel.Pending) // still booting
	w.settle()
	w.rows(kernel.Active, kernel.Active)
	w.settle()
	w.wantReady(true)
	w.rows(kernel.Active, kernel.Failed) // a drop
	w.settle()
	w.wantReady(false)
	mu.Lock()
	defer mu.Unlock()
	if len(got) < 2 || slices.Contains(got[:len(got)-1], slog.LevelWarn) || got[len(got)-1] != slog.LevelWarn {
		t.Errorf("the levels of the not-ready lines: %v; want info until ready, then warn", got)
	}
}

// The launcher ends the latch when the process stops, a command of aiopsctl among
// them, and its rows then stop: that is no fault, so the row says so at the level info,
// however many rows it sees stop.
func TestNotReadyAtAStopIsNoWarning(t *testing.T) {
	var mu sync.Mutex
	var got []slog.Level
	w := newWorld(t, kerneltest.WithConfig(func(c *kernel.Config) { c.LogHandler = levels{&mu, &got} }))
	f := w.root.Plugin(readiness.New(), nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	w.rows(kernel.Active, kernel.Active)
	w.settle()
	w.wantReady(true)
	w.launch.End() // as boot's stop ends the latch
	w.rows(kernel.Active, kernel.Unloading)
	w.settle()
	w.rows(kernel.Unloading, kernel.Disposed)
	w.settle()
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 || slices.Contains(got, slog.LevelWarn) {
		t.Errorf("the levels of the not-ready lines at a stop: %v; want info only", got)
	}
}
