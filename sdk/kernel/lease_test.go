// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

// leaseFixture mounts an owner whose cleanup records how many leases (and leased
// bodies) were open when it ran, for the "no inactive-owner body" checks.
type leaseFixture struct {
	ctx     context.Context
	r       Runtime
	id      uint64
	c       *Handle
	cleaned atomic.Bool
	inBody  atomic.Int64 // Leased bodies currently running.
	openAt  atomic.Int64 // Leases plus bodies observed open when cleanup ran.
	bodyAt  atomic.Int64 // Leased bodies alone, observed when cleanup ran.
}

func mountLeaseFixture(t testing.TB, makeRuntime func() Runtime) *leaseFixture {
	t.Helper()
	f := &leaseFixture{}
	f.ctx, f.r = effectRuntime(t, makeRuntime)
	contexts := make(chan *Handle, 1)
	f.id = effectMount(t, f.ctx, f.r, Spec{ID: "lease-owner", Apply: func(c *Handle) error {
		contexts <- c
		return c.Defer("probe", func(context.Context) error {
			f.bodyAt.Store(f.inBody.Load())
			f.openAt.Store(f.inBody.Load() + c.lease.Load())
			f.cleaned.Store(true)
			return nil
		})
	}})
	f.c = effectWait(t, f.ctx, contexts)
	return f
}

func (f *leaseFixture) dispose(t testing.TB) {
	t.Helper()
	if err := f.r.Dispose(f.ctx, f.id); err != nil {
		t.Error(err)
	}
}

func (f *leaseFixture) await(t testing.TB) {
	t.Helper()
	if err := effectAwait(t, f.ctx, f.r, f.id); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseRefusedAfterUnloadBegins(t *testing.T) {
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			f := mountLeaseFixture(t, makeRuntime)
			if !f.c.Enter() {
				t.Fatal("active owner refused a lease")
			}
			f.dispose(t)
			<-f.c.Std().Done() // live is cleared before the context is canceled.
			for range 100 {
				if f.c.Enter() {
					t.Fatal("unloading owner granted a lease")
				}
			}
			if got := f.c.lease.Load(); got != 1 {
				t.Fatalf("refused Enter leaked or stole a lease: count = %d, want 1", got)
			}
			if f.cleaned.Load() {
				t.Fatal("cleanup ran while a lease was open")
			}
			f.c.Leave()
			f.await(t)
			if !f.cleaned.Load() || f.c.lease.Load() != 0 {
				t.Fatalf("drain incomplete: cleaned=%v leases=%d", f.cleaned.Load(), f.c.lease.Load())
			}
		})
	}
}

// Many goroutines churn leases, half of them releasing from a different
// goroutine than the one that entered (so Leave lands on another shard), while
// the owner unloads. Cleanup must run only after every granted lease returned,
// and no leased body may start after cleanup.
func TestLeaseDrainWaitsForLastLeaveUnderChurn(t *testing.T) {
	const workers, rounds = 32, 20
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			for round := range rounds {
				f := mountLeaseFixture(t, makeRuntime)
				var wg sync.WaitGroup
				var granted atomic.Int64
				var violation atomic.Value
				churning := make(chan struct{})
				var churnOnce sync.Once
				for w := range workers {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for f.c.Enter() {
							f.inBody.Add(1)
							if f.cleaned.Load() {
								violation.CompareAndSwap(nil, "leased body ran after owner cleanup")
							}
							if granted.Add(1) == workers*4 {
								churnOnce.Do(func() { close(churning) })
							}
							release := func() { f.inBody.Add(-1); f.c.Leave() }
							if w%2 == 0 {
								done := make(chan struct{})
								go func() { release(); close(done) }()
								<-done
							} else {
								release()
							}
						}
					}()
				}
				effectWait(t, f.ctx, churning)
				f.dispose(t)
				f.await(t)
				wg.Wait()
				if v := violation.Load(); v != nil {
					t.Fatalf("round %d: %v", round, v)
				}
				// Not openAt: the lease sum read at cleanup can include the +1 of a
				// refused Enter that is still in flight, because one of the 32
				// workers passed its first live check before the unload and was
				// descheduled until now. That transient is rolled back by the same
				// Enter and cannot be told from an open lease, so only the bodies
				// are checked here; the exact lease count is checked below, after
				// every worker has returned.
				if got := f.bodyAt.Load(); got != 0 {
					t.Fatalf("round %d: cleanup ran with %d leased bodies running", round, got)
				}
				if got := f.c.lease.Load(); got != 0 {
					t.Fatalf("round %d: lease count = %d after quiescence", round, got)
				}
			}
		})
	}
}

// The last Leave races the start of unload. AwaitFiber must always finish (no
// lost wake-up, no hang until the test timeout) and cleanup must not precede it.
func TestLeaseNoLostWakeupWhenLastLeaveRacesUnload(t *testing.T) {
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			for i := range 300 {
				f := mountLeaseFixture(t, makeRuntime)
				if !f.c.Enter() {
					t.Fatalf("iteration %d: owner refused a lease", i)
				}
				f.inBody.Add(1)
				start := make(chan struct{})
				var wg sync.WaitGroup
				wg.Add(2)
				go func() { defer wg.Done(); <-start; f.inBody.Add(-1); f.c.Leave() }()
				go func() { defer wg.Done(); <-start; f.dispose(t) }()
				close(start)
				wg.Wait()
				f.await(t)
				if !f.cleaned.Load() || f.openAt.Load() != 0 || f.c.lease.Load() != 0 {
					t.Fatalf("iteration %d: cleaned=%v openAtCleanup=%d leases=%d", i, f.cleaned.Load(), f.openAt.Load(), f.c.lease.Load())
				}
			}
		})
	}
}

func TestLeaseCounterSumsAcrossShards(t *testing.T) {
	var l leaseCount
	var want int64
	for i := range l.shards {
		l.shards[i].n.Add(int64(i) - 3) // A Leave may hit another shard: negatives are fine.
		want += int64(i) - 3
	}
	if got := l.Load(); got != want {
		t.Fatalf("sum = %d, want %d", got, want)
	}
}

// Every Enter and every Leave writes one lease shard, so under contention the
// path costs the moves of shard lines between cores and nothing else (ADR-034,
// 2026-11-18). That holds only while each shard counter has its 64-byte line to
// itself and no other handle field, read on the path (live, leave) or not, sits on
// a shard's line. The allocator may place the handle at any 8-byte offset of a
// line, so every one is tried. A field added to handle, such as act, cannot break
// it unless it lands inside the padding.
func TestLeaseShardsOwnTheirCacheLines(t *testing.T) {
	checkShardLines(t, reflect.TypeFor[handle]())
}

// A zero-size field, such as a leading `_ struct{}` marker, occupies no line and
// must not make the check run past its end.
func TestShardLineCheckTakesZeroSizeFields(t *testing.T) {
	h := reflect.TypeFor[handle]()
	fields := []reflect.StructField{{Name: "_", PkgPath: h.PkgPath(), Type: reflect.TypeFor[struct{}]()}}
	for i := range h.NumField() {
		fields = append(fields, h.Field(i))
	}
	checkShardLines(t, reflect.StructOf(fields))
}

func checkShardLines(t *testing.T, h reflect.Type) {
	t.Helper()
	const line = 64
	lease, _ := h.FieldByName("lease")
	shards, _ := reflect.TypeFor[leaseCount]().FieldByName("shards")
	n, _ := reflect.TypeFor[leaseShard]().FieldByName("n")
	stride := reflect.TypeFor[leaseShard]().Size()
	for base := uintptr(0); base < line; base += 8 {
		shardOn := make(map[uintptr]uintptr) // Line -> shard index.
		for i := range uintptr(leaseShards) {
			at := base + lease.Offset + shards.Offset + i*stride + n.Offset
			if first, last := at/line, (at+n.Type.Size()-1)/line; first != last {
				t.Fatalf("base %d: shard %d straddles lines %d and %d", base, i, first, last)
			}
			if j, ok := shardOn[at/line]; ok {
				t.Fatalf("base %d: shards %d and %d share line %d", base, j, i, at/line)
			}
			shardOn[at/line] = i
		}
		for i := range h.NumField() {
			f := h.Field(i)
			if f.Name == "lease" || f.Type.Size() == 0 { // A zero-size field has no last byte.
				continue
			}
			for l := (base + f.Offset) / line; l <= (base+f.Offset+f.Type.Size()-1)/line; l++ {
				if j, ok := shardOn[l]; ok {
					t.Errorf("base %d: field %s shares line %d with shard %d", base, f.Name, l, j)
				}
			}
		}
	}
}

func TestEnterLeaveAllocatesNothing(t *testing.T) {
	for name, makeRuntime := range effectCandidates() {
		t.Run(name, func(t *testing.T) {
			f := mountLeaseFixture(t, makeRuntime)
			if allocs := testing.AllocsPerRun(1000, func() {
				if !f.c.Enter() {
					t.Error("active owner refused a lease")
				}
				f.c.Leave()
			}); allocs != 0 {
				t.Fatalf("Enter+Leave allocs = %v, want 0", allocs)
			}
			f.dispose(t)
			f.await(t)
			if allocs := testing.AllocsPerRun(1000, func() {
				if f.c.Enter() {
					t.Error("disposed owner granted a lease")
				}
			}); allocs != 0 {
				t.Fatalf("refused Enter allocs = %v, want 0", allocs)
			}
		})
	}
}
