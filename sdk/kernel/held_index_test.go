// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// indexCandidates build each candidate with complete event history: the
// ordering checks below read the ledger.
func indexCandidates() map[string]func() Runtime {
	return map[string]func() Runtime{"reconciler": NewRecordingReconciler}
}

// TestHeldServiceIndexConsistency drives randomised interleavings of provide
// (provider restarts), check completion, dispose and update, and checks the
// held-service-event / pending-load bookkeeping: every load starts at most once
// per activation, none starts before the service event for the provision it
// binds is in the ledger, every consumer that can be ACTIVE becomes so (no load
// is left pending), and no index entry survives quiescence.
func TestHeldServiceIndexConsistency(t *testing.T) {
	for name := range indexCandidates() {
		for seed := int64(1); seed <= 12; seed++ {
			t.Run(fmt.Sprintf("%s/seed%d", name, seed), func(t *testing.T) {
				runIndexScenario(t, seed)
			})
		}
	}
}

type indexScenario struct {
	mu         sync.Mutex
	violations []string
	applied    map[*Handle]int
}

func (s *indexScenario) fail(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.violations = append(s.violations, fmt.Sprintf(format, args...))
}

func runIndexScenario(t *testing.T, seed int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rng := rand.New(rand.NewSource(seed))
	// Same construction as newCore, keeping the owner so the test can read its
	// indexes after the owner goroutine has exited.
	cfg := testConfig()
	transport := &core{cfg: cfg, inbox: make(chan message, cfg.ownerInbox), wake: make(chan struct{}, 1), done: make(chan struct{})}
	owner := newOwner(transport)
	go owner.run()
	var r Runtime = &rt{core: transport}
	notifier := r.(Notifier)
	scenario := &indexScenario{applied: make(map[*Handle]int)}
	var provisions atomic.Int64

	const providers, consumers = 4, 10
	keys := make([]string, providers)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
	}
	providerIDs := make([]uint64, providers)
	for i, key := range keys {
		id, err := r.Mount(ctx, Spec{ID: "p" + key, Provides: []string{key}, Apply: func(c *Handle) error {
			return c.Provide(key, fmt.Sprintf("%s#%d", key, provisions.Add(1)))
		}})
		if err != nil {
			t.Fatal(err)
		}
		providerIDs[i] = id
	}
	type consumer struct {
		id    uint64
		needs []string
		gate  chan struct{} // Nil: no availability check.
		once  sync.Once
	}
	cs := make([]*consumer, consumers)
	for i := range cs {
		c := &consumer{}
		for _, k := range rng.Perm(providers)[:1+rng.Intn(2)] {
			c.needs = append(c.needs, keys[k])
		}
		spec := Spec{ID: fmt.Sprintf("c%d", i), Needs: c.needs}
		if rng.Intn(2) == 0 {
			c.gate = make(chan struct{})
			spec.Check = func(ctx *Handle) bool {
				select {
				case <-c.gate:
					return true
				case <-ctx.Std().Done():
					return false
				}
			}
		}
		spec.Apply = func(ac *Handle) error {
			scenario.mu.Lock()
			scenario.applied[ac]++
			count := scenario.applied[ac]
			scenario.mu.Unlock()
			if count != 1 {
				scenario.fail("%s: Apply ran %d times for one activation", spec.ID, count)
			}
			// No load may start before the service event of each bound provision.
			events, err := r.Events(ctx)
			if err != nil {
				scenario.fail("%s: Events: %v", spec.ID, err)
				return nil
			}
			for _, key := range c.needs {
				value, err := ac.Read(key)
				if err != nil {
					scenario.fail("%s: Read(%s): %v", spec.ID, key, err)
					continue
				}
				if firstEvent(events, func(e ledgerEvent) bool { return isServiceEvent(e, key) && e.State == Active }) < 0 {
					scenario.fail("%s: load started before the service event for %s=%v", spec.ID, key, value)
				}
			}
			return nil
		}
		id, err := r.Mount(ctx, spec)
		if err != nil {
			t.Fatal(err)
		}
		c.id, cs[i] = id, c
	}

	dead := make(map[uint64]bool)
	for step := 0; step < 160; step++ {
		switch op := rng.Intn(10); {
		case op < 3: // Provider restart: a new provision, held service event, refreshed dependants.
			_ = r.Update(ctx, providerIDs[rng.Intn(providers)])
		case op < 6: // Check completion for one consumer.
			if c := cs[rng.Intn(consumers)]; c.gate != nil {
				c.once.Do(func() { close(c.gate) })
			}
		case op < 7:
			c := cs[rng.Intn(consumers)]
			if !dead[c.id] {
				dead[c.id] = true
				_ = r.Dispose(ctx, c.id)
			}
		case op < 8:
			_ = r.Update(ctx, cs[rng.Intn(consumers)].id)
		case op < 9:
			_ = notifier.Notify(ctx, keys[rng.Intn(providers)])
		default:
			time.Sleep(time.Duration(rng.Intn(300)) * time.Microsecond)
		}
	}
	for _, c := range cs {
		if c.gate != nil {
			c.once.Do(func() { close(c.gate) })
		}
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatalf("Settle: %v (a pending load was never started, or a check never completed)", err)
	}
	snapshots, err := r.Snapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state := make(map[uint64]State)
	for _, s := range snapshots {
		state[s.ID] = s.State
	}
	for _, c := range cs {
		if dead[c.id] {
			if state[c.id] != Disposed {
				t.Errorf("disposed consumer %d is %s", c.id, state[c.id])
			}
		} else if state[c.id] != Active {
			t.Errorf("live consumer %d (needs %v) is %s after Settle, want ACTIVE: a pending load was lost", c.id, c.needs, state[c.id])
		}
	}
	scenario.mu.Lock()
	for _, v := range scenario.violations {
		t.Error(v)
	}
	scenario.mu.Unlock()
	if err := closeRuntime(ctx, r); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// After Close every owner has exited, so the indexes can be read directly.
	if len(owner.loadsByKey) != 0 || len(owner.heldSvc) != 0 || len(owner.pendingSvc) != 0 || len(owner.notifying) != 0 {
		t.Errorf("index entries survive quiescence: loads=%d held=%d pending=%d notifying=%d", len(owner.loadsByKey), len(owner.heldSvc), len(owner.pendingSvc), len(owner.notifying))
	}
}

// A load that needs two keys, each with a held service event, starts only after
// both events are out, and exactly once.
func TestLoadNeedingTwoHeldKeysStartsAfterBoth(t *testing.T) {
	for name, build := range indexCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			r := build()
			defer closeRuntime(ctx, r)
			gate := make(chan struct{})
			slowCheck := func(*Handle) bool { <-gate; return true }
			for _, key := range []string{"a", "b"} {
				// A checked dependant of each key holds that key's service event until
				// its availability check has completed.
				if _, err := r.Mount(ctx, Spec{ID: "dep-" + key, Needs: []string{key}, Check: slowCheck}); err != nil {
					t.Fatal(err)
				}
				if _, err := r.Mount(ctx, Spec{ID: "prov-" + key, Provides: []string{key}, Apply: func(c *Handle) error { return c.Provide(key, key+"-v") }}); err != nil {
					t.Fatal(err)
				}
			}
			var loads atomic.Int64
			if _, err := r.Mount(ctx, Spec{ID: "both", Needs: []string{"a", "b"}, Apply: func(c *Handle) error {
				loads.Add(1)
				events, err := r.Events(ctx)
				if err != nil {
					return err
				}
				for _, key := range []string{"a", "b"} {
					if firstEvent(events, func(e ledgerEvent) bool { return isServiceEvent(e, key) && e.State == Active }) < 0 {
						return fmt.Errorf("load started before the service event of %s", key)
					}
				}
				return nil
			}}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			if loads.Load() != 0 {
				t.Fatal("load started while its service events were still held")
			}
			close(gate)
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if loads.Load() != 1 {
				t.Fatalf("load ran %d times, want exactly once", loads.Load())
			}
			events, _ := r.Events(ctx)
			for _, key := range []string{"a", "b"} {
				if i := firstEvent(events, func(e ledgerEvent) bool { return isServiceEvent(e, key) && e.State == Active }); i < 0 {
					t.Fatalf("service event for %s missing: %+v", key, events)
				}
			}
		})
	}
}
