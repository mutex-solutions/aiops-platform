// SPDX-License-Identifier: Apache-2.0

package concurrency

// Hazard tests G1 to G4 (graph-lock suspicions, run on both cores where the
// scenario applies) and B1 (burst admission). See hazards_test.go for the
// conventions and the shared helpers.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// ---------------------------------------------------------------------------
// G1: kernel code inside the plugin's recover boundary.
//
// The kernel cannot be modified to panic, so this enumerates plugin-controlled
// inputs to every plugin-facing call (nil, empty, duplicate, undeclared, nil map
// and func values, a value whose Error and String methods panic, a zero-value
// Configuration, ...) and checks that none of them panics inside kernel code:
// the plugin's Apply must complete, the fibers must be ACTIVE with no error, the
// graph must stay usable afterwards (a held lock or half-written state would
// stall or corrupt the follow-up calls) and Close must be clean. On the
// reconciler a kernel panic would kill the process (the owner has no recover),
// so surviving the scenario is the check there.

type panicValue struct{}

func (panicValue) String() string { panic("String called by the kernel") }
func (panicValue) Error() string  { panic("Error called by the kernel") }

func TestHazardG1PluginInputsNeverPanicInKernel(t *testing.T) {
	for core, c := range hazardCores {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := hazardCores[core].make(t)
				var mu sync.Mutex
				got := map[string]error{}
				record := func(name string, err error) {
					mu.Lock()
					got[name] = err
					mu.Unlock()
				}
				id, err := r.Mount(context.Background(), Spec{
					ID:       "plugin",
					Provides: []string{"k_nil", "k_map", "k_func", "k_str", "k_err"},
					Apply: func(c *Context) error {
						record("provide empty key", c.Provide("", 1))
						record("provide undeclared", c.Provide("undeclared", 1))
						record("provide nil value", c.Provide("k_nil", nil))
						record("provide nil map value", c.Provide("k_map", map[string]int(nil)))
						record("provide func value", c.Provide("k_func", func() {}))
						record("provide panicking String", c.Provide("k_str", panicValue{}))
						record("provide panicking Error", c.Provide("k_err", error(panicValue{})))
						record("provide duplicate", c.Provide("k_nil", 2))
						record("defer nil disposer", c.Defer("d", nil))
						record("effect empty label", c.Effect("", nil))
						record("effect nil setup", c.Effect("e", nil))
						record("effect nil disposer", c.Effect("e", func() (Disposer, error) { return nil, nil }))
						record("effect setup error with disposer", c.Effect("e", func() (Disposer, error) {
							return func(context.Context) error { return nil }, errors.New("setup failed")
						}))
						_, err := c.MountChild(Spec{ID: "dup", Needs: []string{"a", "a"}})
						record("child duplicate needs", err)
						_, err = c.MountChild(Spec{ID: "both", Apply: func(*Context) error { return nil }, ApplyEffect: func(*Context) (Disposer, error) { return nil, nil }})
						record("child Apply and ApplyEffect", err)
						_, err = c.MountChild(Spec{ID: "zero-config", Config: &Configuration{}})
						record("child zero Configuration", err)
						_, err = c.MountChild(Spec{})
						record("child zero Spec", err)
						record("go empty", c.Go("", nil))
						_, err = c.RequestEffectScope("", nil)
						record("scope invalid", err)
						return nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := r.Settle(ctx); err != nil {
					t.Fatalf("Settle: %v", err)
				}
				mu.Lock()
				defer mu.Unlock()
				rejected := []string{"provide empty key", "provide undeclared", "provide duplicate", "defer nil disposer", "effect empty label",
					"effect nil setup", "effect setup error with disposer", "child duplicate needs", "child Apply and ApplyEffect", "go empty", "scope invalid"}
				accepted := []string{"provide nil value", "provide nil map value", "provide func value", "provide panicking String",
					"provide panicking Error", "effect nil disposer", "child zero Configuration", "child zero Spec"}
				for _, name := range rejected {
					if e, ok := got[name]; !ok || e == nil {
						t.Errorf("%s: err = %v (recorded %v), want an error", name, e, ok)
					}
				}
				for _, name := range accepted {
					if e, ok := got[name]; !ok || e != nil {
						t.Errorf("%s: err = %v (recorded %v), want nil", name, e, ok)
					}
				}
				snaps, err := r.Snapshots(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if len(snaps) != 3 {
					t.Fatalf("want the plugin and its 2 children, got %d fibers", len(snaps))
				}
				for _, s := range snaps {
					if s.State != Active || s.Error != "" {
						t.Errorf("fiber %d %q: state=%s error=%q, want ACTIVE and no error (an Apply panic would show here)", s.ID, s.Name, s.State, s.Error)
					}
				}
				if in, ok := r.(Inspector); ok {
					if _, err := in.Inspect(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := r.Events(ctx); err != nil {
					t.Fatal(err)
				}
				// The graph (and, on the graph-lock, g.mu) is still usable.
				if _, err := r.Mount(ctx, Spec{ID: "after"}); err != nil {
					t.Fatalf("Mount after the hostile inputs: %v", err)
				}
				if err := r.Dispose(ctx, id); err != nil {
					t.Fatal(err)
				}
				if err := r.Settle(ctx); err != nil {
					t.Fatalf("Settle after Dispose: %v", err)
				}
				if err := closeRuntime(ctx, r); err != nil {
					t.Fatalf("Close: %v", err)
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// G2: unbounded waits for dependants and children.

type g2Obs struct {
	grand, mid, di uint64
	atMinute       map[uint64]Snapshot
	deadlineByID   map[uint64][]string // deadline events per fiber
	settleErr      error
	closeErr       error
	closingMid     bool // observer dispatcher closed while the cleanup was still hung
	closingAfter   bool // ... and after the hung cleanup finally returned
	finalSettle    error
}

func g2Scenario(t *testing.T, core int) g2Obs {
	var obs g2Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardCores[core].make(t)
		gate := make(chan struct{})
		var once sync.Once
		open := func() { once.Do(func() { close(gate) }) }
		t.Cleanup(open)
		ctx := context.Background()
		mount := func(s Spec) uint64 {
			id, err := r.Mount(ctx, s)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		obs.grand = mount(Spec{ID: "grandparent", Provides: []string{"a"}, Apply: func(c *Context) error { return c.Provide("a", 1) }})
		obs.mid = mount(Spec{ID: "provider", Needs: []string{"a"}, Provides: []string{"b"}, Apply: func(c *Context) error { return c.Provide("b", 2) }})
		obs.di = mount(Spec{ID: "dependant", Needs: []string{"b"}, Apply: func(c *Context) error {
			// A cleanup that ignores its context and hangs.
			return c.Defer("hang", func(context.Context) error { <-gate; return nil })
		}})
		if err := r.Settle(ctx); err != nil {
			t.Fatal(err)
		}
		if err := r.Dispose(ctx, obs.grand); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Minute)
		snaps, err := r.Snapshots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		obs.atMinute = map[uint64]Snapshot{}
		for _, s := range snaps {
			obs.atMinute[s.ID] = s
		}
		obs.deadlineByID = map[uint64][]string{}
		events, err := r.Events(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if strings.HasPrefix(e.Kind, "deadline") {
				obs.deadlineByID[e.Fiber] = append(obs.deadlineByID[e.Fiber], e.Kind+"/"+e.Key)
			}
		}
		bounded := func() (context.Context, context.CancelFunc) { return context.WithTimeout(ctx, 10*time.Second) }
		sctx, cancel := bounded()
		obs.settleErr = r.Settle(sctx)
		cancel()
		cctx, cancel := bounded()
		obs.closeErr = r.Close(cctx)
		cancel()
		status := func() bool { return r.(interface{ ObserverStatus() ObserverStatus }).ObserverStatus().Closing }
		obs.closingMid = status()
		open()
		synctest.Wait()
		fctx, cancel := bounded()
		obs.finalSettle = r.Settle(fctx)
		cancel()
		synctest.Wait()
		obs.closingAfter = status()
	})
	return obs
}

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func g2Describe(o g2Obs) string {
	s := "after 1 minute: "
	for _, id := range []uint64{o.grand, o.mid, o.di} {
		sn := o.atMinute[id]
		s += fmt.Sprintf("%s={%s error=%q deadline events=%v} ", sn.Name, sn.State, sn.Error, o.deadlineByID[id])
	}
	return s + fmt.Sprintf("Settle(10s)=%q Close(10s)=%q observers closed while hung=%v, after release=%v final Settle=%v",
		errText(o.settleErr), errText(o.closeErr), o.closingMid, o.closingAfter, o.finalSettle)
}

func TestHazardG2UnboundedDependantWaits(t *testing.T) {
	for core, c := range hazardCores {
		t.Run(c.name, func(t *testing.T) {
			o := g2Scenario(t, core)
			observed := g2Describe(o)
			t.Log(observed)
			hung := o.atMinute[o.di]
			if hung.Error == "" && len(o.deadlineByID[o.di]) == 0 {
				t.Fatalf("scenario broken: the hanging fiber has no diagnostic itself: %s", observed)
			}
			names := func(err error) bool {
				return err != nil && (strings.Contains(err.Error(), "dependant") || strings.Contains(err.Error(), fmt.Sprintf("fiber %d", o.di)))
			}
			t.Run("provider-diagnostic", func(t *testing.T) {
				// Every waiter held up by the hung cleanup should say so.
				mid, grand := o.atMinute[o.mid], o.atMinute[o.grand]
				silent := (mid.Error == "" && len(o.deadlineByID[o.mid]) == 0) || (grand.Error == "" && len(o.deadlineByID[o.grand]) == 0)
				knownHazard(t, silent, "a provider (and its ancestors) waiting for a dependant whose cleanup hangs has no deadline of its own and reports nothing", observed)
			})
			t.Run("settle-names-culprit", func(t *testing.T) {
				knownHazard(t, !names(o.settleErr), "Settle blocks until the caller's context expires and returns only the context error, without naming the stuck fiber", observed)
			})
			t.Run("close-names-culprit", func(t *testing.T) {
				knownHazard(t, !names(o.closeErr), "Close returns only the context error, without naming the stuck fiber", observed)
			})
			t.Run("observers-closed-after-recovery", func(t *testing.T) {
				// Once the hung cleanup returns and the runtime is empty, a failed
				// Close must not leave the observer dispatcher open.
				knownHazard(t, !o.closingAfter, "after a failed Close the observer dispatcher stays open until the caller calls Close again", observed)
			})
		})
	}
}

// ---------------------------------------------------------------------------
// G3: wake-all per event.

// wakeCtx counts how often a core looks at its context. On the graph-lock
// AwaitFiber and Settle call Done once per wait-loop iteration (one initial and
// one per wake-up) and AwaitFiber also calls Err once per iteration, so the
// counters are an exact wake-up count that needs no change to the core.
type wakeCtx struct {
	context.Context
	done, err atomic.Int64
}

func (c *wakeCtx) Done() <-chan struct{} { c.done.Add(1); return c.Context.Done() }
func (c *wakeCtx) Err() error            { c.err.Add(1); return c.Context.Err() }

type g3Obs struct {
	doneCalls int64 // Done calls by all waiters, including the one initial iteration each
	errCalls  int64
}

func g3Count(t *testing.T, core int, kind string, waiters, events int) g3Obs {
	var obs g3Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardCores[core].make(t)
		gate := make(chan struct{})
		var once sync.Once
		t.Cleanup(func() { once.Do(func() { close(gate) }) })
		id, err := r.Mount(context.Background(), Spec{ID: "slow", Apply: func(*Context) error { <-gate; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		parent, cancel := context.WithCancel(context.Background())
		counters := make([]*wakeCtx, waiters)
		var wg sync.WaitGroup
		for i := range counters {
			counters[i] = &wakeCtx{Context: parent}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if kind == "AwaitFiber" {
					_ = r.(Waiter).AwaitFiber(counters[i], id)
				} else {
					_ = r.Settle(counters[i])
				}
			}()
		}
		synctest.Wait() // every waiter is parked
		recorder := r.(Recorder)
		for i := 0; i < events; i++ {
			if err := recorder.Record(context.Background(), "tick", map[string]any{"i": i}); err != nil {
				t.Fatal(err)
			}
			synctest.Wait() // every woken waiter has looked and parked again
		}
		for _, c := range counters {
			obs.doneCalls += c.done.Load()
			obs.errCalls += c.err.Load()
		}
		cancel()
		wg.Wait()
		once.Do(func() { close(gate) })
	})
	return obs
}

func TestHazardG3WakeAllPerEvent(t *testing.T) {
	const events = 20
	for core, c := range hazardCores {
		for _, kind := range []string{"AwaitFiber", "Settle"} {
			t.Run(c.name+"/"+kind, func(t *testing.T) {
				var observed string
				sizes := []int{10, 100}
				if hazardRepro() {
					sizes = append(sizes, 1000) // 0.2 to 0.4 s per case under -race
				}
				for _, w := range sizes {
					o := g3Count(t, core, kind, w, events)
					line := fmt.Sprintf("W=%d E=%d: Done calls=%d (W*(E+1)=%d) Err calls=%d", w, events, o.doneCalls, w*(events+1), o.errCalls)
					t.Log(line)
					observed += line + "; "
					if c.name == "graph-lock" {
						// Exact and deterministic: a waiter looks at its context once
						// when it parks and the unrelated events wake nobody, so W
						// waiters cost W calls, whatever E is (it was W*(E+1)).
						if got, want := o.doneCalls, int64(w); got != want {
							t.Fatalf("graph-lock %s W=%d: Done calls = %d, want exactly W = %d (unrelated events must not wake waiters)", kind, w, got, want)
						}
					}
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// G4: test knobs read without the lock.

// TestHazardG4KnobsAreWrittenBeforeAnyGoroutine checks both halves of the
// argument that the unlocked knob reads (awaitLeases, runUnload) cannot race:
// no production code assigns a knob after construction, and the tests assign
// them only straight after construction, before the first goroutine of the
// runtime exists (the go statement orders that write before every read). The
// lifecycle below reads every knob site from many goroutines and is meant to be
// run under -race.
var knobAssign = regexp.MustCompile(`knobs(\.\w+)?(\s*,\s*[\w.]+)*\s*=[^=]`)

func TestHazardG4KnobsAreWrittenBeforeAnyGoroutine(t *testing.T) {
	src, err := os.ReadFile("graphlock.go")
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(src), "\n") {
		code, _, _ := strings.Cut(line, "//")
		if knobAssign.MatchString(code) {
			t.Errorf("graphlock.go:%d assigns a knob outside construction: %s", i+1, strings.TrimSpace(line))
		}
	}
	synctest.Test(t, func(t *testing.T) {
		g := graphTestRuntime(t).(*GraphLock)
		g.knobs.load, g.knobs.cleanup, g.knobs.leaseDrain = time.Hour, time.Hour, time.Hour
		ctx := context.Background()
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				key := fmt.Sprintf("k%d", i)
				provider, err := g.Mount(ctx, Spec{ID: "p", Provides: []string{key}, Apply: func(c *Context) error {
					_ = c.Defer("d", func(context.Context) error { return nil })
					_ = c.Effect("e", func() (Disposer, error) { return func(context.Context) error { return nil }, errors.New("x") })
					_, _ = c.RequestEffectScope("s", func(*EffectScope) error { return nil })
					if c.Enter() {
						defer c.Leave()
					}
					return c.Provide(key, i)
				}})
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = g.Mount(ctx, Spec{ID: "c", Needs: []string{key}, Check: func(*Context) bool { return true }})
				_ = g.Dispose(ctx, provider)
			}()
		}
		wg.Wait()
		if err := g.Settle(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

// ---------------------------------------------------------------------------
// B1: command admission under a burst.

type b1Obs struct {
	accepted, ctxErr, other int
	present                 int
	orphans                 int // fibers present whose id no caller holds
	heldButMissing          int
}

// b1Burst submits total mounts from workers goroutines. Odd commands carry a
// context cancelled before the call; even commands carry one cancelled right
// after the core's last check of it (lateCancelCtx), so the counts do not
// depend on timing.
func b1Burst(t *testing.T, core, total, workers int) b1Obs {
	var obs b1Obs
	synctest.Test(t, func(t *testing.T) {
		r := hazardCores[core].make(t)
		var next atomic.Int64
		var mu sync.Mutex
		held := map[uint64]bool{}
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					i := int(next.Add(1)) - 1
					if i >= total {
						return
					}
					var ctx context.Context
					if i%2 == 1 {
						c, cancel := context.WithCancel(context.Background())
						cancel()
						ctx = c
					} else {
						ctx = newLateCancelCtx(2, synctest.Wait)
					}
					id, err := r.Mount(ctx, Spec{ID: fmt.Sprintf("b1-%d", i)})
					mu.Lock()
					switch {
					case err == nil:
						obs.accepted++
						held[id] = true
					case errors.Is(err, context.Canceled):
						obs.ctxErr++
					default:
						obs.other++
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		synctest.Wait()
		snaps, err := r.Snapshots(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		obs.present = len(snaps)
		present := map[uint64]bool{}
		for _, s := range snaps {
			present[s.ID] = true
			if !held[s.ID] {
				obs.orphans++
			}
		}
		for id := range held {
			if !present[id] {
				obs.heldButMissing++
			}
		}
	})
	return obs
}

func TestHazardB1BurstAdmission(t *testing.T) {
	total := 1000
	if hazardRepro() {
		total = 10000
	}
	for core, c := range hazardCores {
		t.Run(c.name, func(t *testing.T) {
			o := b1Burst(t, core, total, 64)
			observed := fmt.Sprintf("%d mounts from 64 goroutines: accepted=%d context-error=%d other=%d fibers present=%d fibers whose id no caller holds=%d held ids missing=%d",
				total, o.accepted, o.ctxErr, o.other, o.present, o.orphans, o.heldButMissing)
			t.Log(observed)
			if o.accepted+o.ctxErr+o.other != total || o.other != 0 || o.heldButMissing != 0 {
				t.Fatalf("inconsistent counts: %s", observed)
			}
			half := total / 2
			if c.name == "graph-lock" {
				// Even commands commit under their only ctx check and return ids.
				if o.accepted != half || o.ctxErr != half || o.present != half || o.orphans != 0 {
					t.Fatalf("graph-lock: %s", observed)
				}
				return
			}
			knownHazard(t, o.orphans != 0, "reconciler commands cancelled after the owner dequeued them commit fibers whose ids are never returned", observed)
		})
	}
}
