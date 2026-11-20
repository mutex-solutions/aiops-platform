// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests pin the oracle order of the S0 differential corpus (D09, D14):
// a dependant is refreshed before the service event, a consumer whose
// availability check is false never leaves PENDING, and notify records a
// service event after refreshing dependants.

func orderCandidates() map[string]func() Runtime {
	return map[string]func() Runtime{"reconciler": NewRecordingReconciler}
}

func isServiceEvent(e ledgerEvent, key string) bool {
	return (e.Kind == "service" || e.Kind == "service:publish") && e.Key == key
}

func firstEvent(events []ledgerEvent, match func(ledgerEvent) bool) int {
	for i, e := range events {
		if match(e) {
			return i
		}
	}
	return -1
}

func countEvents(events []ledgerEvent, match func(ledgerEvent) bool) int {
	n := 0
	for _, e := range events {
		if match(e) {
			n++
		}
	}
	return n
}

func stateEvent(name string, to State) func(ledgerEvent) bool {
	return func(e ledgerEvent) bool { return e.Kind == "state" && e.Name == name && e.State == to }
}

// D09: the false check never moves the consumer to LOADING, and Notify records
// the second service event after the consumer moved.
func TestDeniedCheckKeepsConsumerPendingAndNotifyRecordsService(t *testing.T) {
	for name, makeRuntime := range orderCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			defer closeRuntime(ctx, r)
			var allow, applied atomic.Int64
			if _, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"svc"}, Apply: func(c *Handle) error { return c.Provide("svc", "v") }}); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Mount(ctx, Spec{ID: "consumer", Needs: []string{"svc"},
				Check: func(*Handle) bool { return allow.Load() == 1 },
				Apply: func(*Handle) error { applied.Add(1); return nil }}); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			events, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if n := countEvents(events, func(e ledgerEvent) bool { return e.Kind == "state" && e.Name == "consumer" }); n != 0 || applied.Load() != 0 {
				t.Fatalf("consumer with a false check changed state %d times, applied %d", n, applied.Load())
			}
			if n := countEvents(events, func(e ledgerEvent) bool { return isServiceEvent(e, "svc") }); n != 1 {
				t.Fatalf("service events before notify = %d, want 1", n)
			}
			allow.Store(1)
			if err := r.(Notifier).Notify(ctx, "svc"); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			events, err = r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			loading := firstEvent(events, stateEvent("consumer", Loading))
			var notified []int
			for i, e := range events {
				if isServiceEvent(e, "svc") {
					notified = append(notified, i)
				}
			}
			if len(notified) != 2 || loading < 0 || notified[1] < loading {
				t.Fatalf("want the notify service event after consumer LOADING: loading=%d service=%v", loading, notified)
			}
			if e := events[notified[1]]; e.UID == 0 {
				t.Fatalf("notify event does not carry the provision: %+v", e)
			}
		})
	}
}

// D14: the dependant leaves PENDING before the provider's publication, even
// though the check runs off the owner and takes as long as it needs.
func TestDependantLoadingPrecedesServiceEvent(t *testing.T) {
	for name, makeRuntime := range orderCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			gate, entered := make(chan struct{}), make(chan struct{})
			var once, release sync.Once
			// Deferred last-in first-out: the gate opens before Close, even on failure.
			defer closeRuntime(ctx, r)
			defer release.Do(func() { close(gate) })
			// The consumer is mounted first, so the provider's activation is what
			// refreshes it, whatever the scheduling of the two mounts.
			if _, err := r.Mount(ctx, Spec{ID: "consumer", Needs: []string{"svc"}, Check: func(*Handle) bool {
				once.Do(func() {
					close(entered)
					<-gate
				})
				return true
			}}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"svc"}, Apply: func(c *Handle) error { return c.Provide("svc", "v") }}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("check never ran")
			}
			// The check is in flight: the dependant is still PENDING and the
			// publication is not recorded yet.
			events, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if i := firstEvent(events, func(e ledgerEvent) bool { return isServiceEvent(e, "svc") || stateEvent("consumer", Loading)(e) }); i >= 0 {
				t.Fatalf("event %+v recorded while the dependant check was in flight", events[i])
			}
			release.Do(func() { close(gate) })
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			events, err = r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			loading := firstEvent(events, stateEvent("consumer", Loading))
			published := firstEvent(events, func(e ledgerEvent) bool { return isServiceEvent(e, "svc") })
			if loading < 0 || published < 0 || loading > published {
				t.Fatalf("consumer LOADING at %d must precede the service event at %d", loading, published)
			}
		})
	}
}

// D09: a dependant without a check starts loading in the same owner step that
// holds the provider's service event for another dependant's check. Its load
// must not record anything before that event: LOADING, service event, then the
// load's own records. The slow dependant's check calls back into the runtime
// (the owner is free to answer) so the loader goroutine can run meanwhile.
func TestServiceEventPrecedesDependantLoadRecords(t *testing.T) {
	for name, makeRuntime := range orderCandidates() {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			defer closeRuntime(ctx, r)
			noop := func(context.Context) error { return nil }
			if _, err := r.Mount(ctx, Spec{ID: "fast", Needs: []string{"svc"}, Apply: func(c *Handle) error { return c.Defer("fast-load", noop) }}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Mount(ctx, Spec{ID: "slow", Needs: []string{"svc"}, Check: func(*Handle) bool {
				// Owner round trips: the fast loader has been launched by now.
				for range 8 {
					if _, err := r.Events(ctx); err != nil {
						return false
					}
				}
				return true
			}}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Mount(ctx, Spec{ID: "provider", Provides: []string{"svc"}, Apply: func(c *Handle) error { return c.Provide("svc", "v") }}); err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			events, err := r.Events(ctx)
			if err != nil {
				t.Fatal(err)
			}
			loading := firstEvent(events, stateEvent("fast", Loading))
			service := firstEvent(events, func(e ledgerEvent) bool { return isServiceEvent(e, "svc") })
			record := firstEvent(events, func(e ledgerEvent) bool { return e.Name == "fast" && strings.HasPrefix(e.Kind, "effect") })
			if loading < 0 || service < 0 || record < 0 || !(loading < service && service < record) {
				t.Fatalf("want LOADING < service event < load record: loading=%d service=%d record=%d\n%+v", loading, service, record, events)
			}
		})
	}
}
