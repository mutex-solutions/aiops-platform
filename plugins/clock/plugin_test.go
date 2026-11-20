// SPDX-License-Identifier: Apache-2.0

package clock_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/clock"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// holder is a consumer that keeps its context and the clock it was given.
type holder struct {
	c   *kernel.Context
	clk def.Clock
}

func (h *holder) plugin(id string) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: id, Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { h.c, h.clk = c, def.Key.Of(c); return nil }})
}

func TestNowIsTheSystemTime(t *testing.T) {
	root := kerneltest.New(t)
	kerneltest.Mount(root, clock.Plugin)
	var h holder
	kerneltest.Mount(root, h.plugin("aiops/consumer"))
	before := time.Now()
	got := h.clk.Now()
	if got.Before(before) || time.Since(got) < 0 {
		t.Errorf("Now = %v, outside [%v, now]", got, before)
	}
}

func TestZeroTimerFiresAtOnce(t *testing.T) {
	root := kerneltest.New(t)
	var h holder
	kerneltest.Mount(root, clock.Plugin, h.plugin("aiops/consumer"))
	tm, err := h.clk.After(h.c, 0)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-tm.C():
	case <-t.Context().Done():
		t.Fatal("a zero timer did not fire")
	}
	if tm.Stop() {
		t.Error("Stop reported a fired timer as pending")
	}
}

func TestTimersStopWithTheirFiber(t *testing.T) {
	root := kerneltest.New(t)
	var gone, kept holder
	kerneltest.Mount(root, clock.Plugin)
	f := root.Plugin(gone.plugin("aiops/gone"), nil)
	kerneltest.Mount(root, kept.plugin("aiops/kept"))
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	stopped, err := gone.clk.After(gone.c, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := kept.clk.After(kept.c, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	labels, _ := f.Effects(t.Context())
	if len(labels) != 1 || labels[0] != "clock timer" {
		t.Errorf("effects of the caller = %v, want the one timer", labels)
	}
	// The stop is recorded by the scope's setup on the fiber's lane; a fiber that
	// quiesced before it ran would leave its timer to run out (see After).
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := f.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if stopped.Stop() {
		t.Error("the timer of a disposed fiber was still pending")
	}
	if !pending.Stop() {
		t.Error("the timer of a live fiber was stopped")
	}
	if _, err := gone.clk.After(gone.c, time.Hour); !errors.Is(err, kernel.ErrInactiveEffect) {
		t.Errorf("After on a disposed fiber = %v, want ErrInactiveEffect", err)
	}
}

// A timer that fires or is stopped leaves its fiber's effect ledger, so a fiber that
// starts one timer after another holds only those still pending.
func TestFiredAndStoppedTimersLeaveTheLedger(t *testing.T) {
	root := kerneltest.New(t)
	var h holder
	kerneltest.Mount(root, clock.Plugin)
	f := root.Plugin(h.plugin("aiops/consumer"), nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		tm, err := h.clk.After(h.c, 0)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-tm.C():
		case <-t.Context().Done():
			t.Fatal("a zero timer did not fire")
		}
	}
	stopped, err := h.clk.After(h.c, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped.Stop() {
		t.Fatal("Stop reported a pending timer as fired")
	}
	if _, err := h.clk.After(h.c, time.Hour); err != nil {
		t.Fatal(err)
	}
	// The kernel withdraws an effect on the fiber's lane; Settle joins that work.
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if labels, _ := f.Effects(t.Context()); !slices.Equal(labels, []string{"clock timer"}) {
		t.Errorf("effects = %d %v, want the one pending timer", len(labels), labels[:min(len(labels), 3)])
	}
}
