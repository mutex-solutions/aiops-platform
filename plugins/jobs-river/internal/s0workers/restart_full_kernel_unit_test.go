// SPDX-License-Identifier: Apache-2.0
package s0workers

// Database-free tests of the kernel-owned variant: owner selection, the declared
// kernel goroutine owners (classification, baseline, teardown) and the judgement of
// the kernel's own record of a restart.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func TestResourceOwnerFromEnv(t *testing.T) {
	for _, c := range []struct {
		in, want string
		bad      bool
	}{{"", resOwnerLibrary, false}, {"library", resOwnerLibrary, false}, {"kernel", resOwnerKernel, false}, {"Kernel", "", true}, {"both", "", true}} {
		got, err := resourceOwnerFromEnv(func(string) string { return c.in })
		if (err != nil) != c.bad || got != c.want {
			t.Fatalf("%q: got %q, %v", c.in, got, err)
		}
	}
}

func useKernelSet(t *testing.T) {
	t.Helper()
	s10Active = s10KernelSet()
	t.Cleanup(func() { s10Active = nil })
}

const kPath = "github.com/mutex-solutions/aiops-platform/sdk/kernel"

func TestKernelGoroutinesAreClassifiedByTheirCreator(t *testing.T) {
	text := "goroutine profile: total 4\n" + strings.Join([]string{
		block(1, "", kPath+".(*owner).run"),
		block(1, "", kPath+".(*dispatcher).loop", kPath+".(*dispatcher).run"),
		// a Context.Go goroutine parked in net/http: the creator is the kernel's tracked-work wrapper
		block(1, lListen1, "net/http.(*Server).Serve", "github.com/mutex-solutions/aiops-platform/plugins/jobs-river/internal/s0workers.(*kernelApp).listenerPlugin.func1.4", kPath+".runTracked", kPath+".(*handle).goTracked.func1"),
		block(1, lClient3, "github.com/mutex-solutions/aiops-platform/plugins/jobs-river/internal/s0workers.(*fullApp).drainUntil", kPath+".runTracked", kPath+".(*handle).goTracked.func1"),
	}, "\n")
	m := groupMap(t, text)
	for _, want := range []gKey{
		{ownerKernelRT, "", 0, kEntryOwnerLoop},
		{ownerKernelRT, "", 0, kEntryDispatcher},
		{ownerKernelRT, roleListener, 1, kEntryTracked},
		{ownerKernelRT, roleWorkerClient, 3, kEntryTracked},
	} {
		if m[want] != 1 {
			t.Errorf("missing %+v in %v", want, m)
		}
	}
}

// kernelAppSnap is a clean warmed kernel-owned application snapshot.
func kernelAppSnap(cycle, gen int) *s10Snapshot {
	s := appSnap(cycle, gen, 6, 5)
	var g []gGroup
	for _, x := range s.Goroutines { // the library's harness-side drain and accept loop do not exist here
		if x.Entry == "s0workers.(*fullApp).drain" || x.Entry == "s0workers.(*fullApp).startListener.func1.1" {
			continue
		}
		g = append(g, x)
	}
	g = append(g, gGroup{ownerKernelRT, "", 0, kEntryOwnerLoop, 1}, gGroup{ownerKernelRT, "", 0, kEntryDispatcher, 1},
		gGroup{ownerKernelRT, roleListener, gen, kEntryTracked, 2}, gGroup{ownerKernelRT, roleWorkerClient, gen, kEntryTracked, 1})
	s.Goroutines = g
	return s
}

func TestKernelDeclaredSetBaselineCheckpointTeardown(t *testing.T) {
	useKernelSet(t)
	h := harnessSnap()
	base := kernelAppSnap(0, 1)
	if f := s10Evaluate("baseline", h, nil, base, nil); len(f) != 0 {
		t.Fatalf("clean kernel baseline: %v", f)
	}
	if f := s10Evaluate("checkpoint", h, base, kernelAppSnap(10, 4), []*s10Snapshot{kernelAppSnap(0, 1)}); len(f) != 0 {
		t.Fatalf("clean kernel checkpoint: %v", f)
	}
	td := &s10Snapshot{Name: "teardown", Cycle: -1, Live: map[string]int{}, Goroutines: append([]gGroup{}, h.Goroutines...), FDs: map[string]int{"pipe": 2, "file": 1, "epoll": 1, "pg-socket": 2}, HarnessPools: h.HarnessPools,
		Sessions: map[string]s10Sess{roleFixture: {Idle: 1}}, Entries: map[string]int{"harness-delivery-tally": 0}}
	if f := s10Evaluate("teardown", h, base, td, nil); len(f) != 0 {
		t.Fatalf("clean kernel teardown: %v", f)
	}
	// A kernel goroutine left after the final Root.Close is a teardown finding, named.
	td.Goroutines = append(td.Goroutines, gGroup{ownerKernelRT, "", 0, kEntryOwnerLoop, 1})
	f := s10Evaluate("teardown", h, base, td, nil)
	if !hasFinding(f, bTeardown, "kernel goroutine remains after Root.Close: 1 at "+kEntryOwnerLoop) {
		t.Fatalf("a remaining kernel goroutine must fail teardown: %v", f)
	}
}

func TestKernelDeclaredSetCatchesTrackedGoroutineLeaks(t *testing.T) {
	useKernelSet(t)
	h := harnessSnap()
	base := kernelAppSnap(0, 1)
	// the drain goroutine of the client fiber did not end with an old generation
	cp := kernelAppSnap(10, 4)
	cp.Goroutines = append(cp.Goroutines, gGroup{ownerKernelRT, roleWorkerClient, 3, kEntryTracked, 1})
	if f := s10Evaluate("checkpoint", h, base, cp, nil); !hasFinding(f, bDisposed, "river-worker#3 (disposed) owns 1 goroutine(s) of kernel "+kEntryTracked) {
		t.Fatalf("a tracked goroutine of a disposed generation: %v", f)
	}
	// a third tracked goroutine in the live listener generation
	cp = kernelAppSnap(10, 4)
	cp.Goroutines = append(cp.Goroutines, gGroup{ownerKernelRT, roleListener, 4, kEntryTracked, 1})
	if f := s10Evaluate("checkpoint", h, base, cp, nil); !hasFinding(f, bGoroutines, "role=http-server entry="+kEntryTracked) {
		t.Fatalf("an extra tracked goroutine: %v", f)
	}
	// the library set never declares a kernel goroutine
	s10Active = nil
	if f := s10Evaluate("baseline", h, nil, kernelAppSnap(0, 1), nil); len(f) == 0 {
		t.Fatal("kernel goroutines are not part of the library variant's declared set")
	}
}

// ---- the kernel's own record of a restart ----

func ev(seq, fiber uint64, kind string, state kernel.State, label string) kernel.LedgerEvent {
	return kernel.LedgerEvent{Sequence: seq, Fiber: fiber, Plugin: "p", Kind: kind, State: state, Label: label}
}

// cleanRestartIn is a pool restart: pool 1, client 2, listener 3 all restart; the
// dependants unload first, the provider disposes after them.
func cleanRestartIn() kernelCheckIn {
	var evs []kernel.LedgerEvent
	seq := uint64(10)
	add := func(fiber uint64, kind string, state kernel.State, label string) {
		seq++
		evs = append(evs, ev(seq, fiber, kind, state, label))
	}
	add(1, "state", kernel.Unloading, "")
	add(2, "state", kernel.Unloading, "")
	add(3, "state", kernel.Unloading, "")
	add(3, "go:done", "", "http-serve")
	add(3, "go:done", "", "http-quiesce")
	add(3, "dispose-effect", "", "provide:s0restart/listener@1")
	add(2, "go:done", "", "delivery-drain")
	add(2, "dispose-effect", "", "provide:s0restart/client@1")
	add(1, "dispose-effect", "", "provide:s0restart/pool@1")
	add(1, "state", kernel.Active, "")
	add(2, "state", kernel.Active, "")
	add(3, "state", kernel.Active, "")
	in := kernelCheckIn{Kind: "pool", Requested: time.Unix(100, 0), Watermark: 10, Events: evs, EventsErr: kernel.ErrEventsTruncated}
	for i, role := range kRoles {
		in.Fibers = append(in.Fibers, kernelFiberIn{Role: role, Plugin: kPluginID(role), ID: uint64(i + 1), Explicit: role == "pool", Expected: true, Tracked: kTracked[role],
			RevBefore: 1, RevAfter: 2, OpenAtRequest: 2, DisposerAt: time.Unix(100, 150e6)})
		in.Snaps = append(in.Snaps, kernel.FiberSnapshot{ID: uint64(i + 1), Plugin: kPluginID(role), State: kernel.Active, Alive: true})
		in.Provisions = append(in.Provisions, kernel.Provision{Plugin: kPluginID(role), Active: true})
	}
	return in
}

func TestKernelRecordCleanRestart(t *testing.T) {
	c := checkKernelRecord(cleanRestartIn())
	if len(c.Problems) != 0 {
		t.Fatalf("clean restart: %v", c.Problems)
	}
	for _, b := range c.bounds() {
		if !b.Pass {
			t.Fatalf("bound %s: %s", b.Name, b.Observed)
		}
	}
	if len(c.bounds()) != len(kernelBoundNames) || c.Fibers[0].RequestToDisposerMs != 150 || len(c.Fibers[2].TrackedDone) != 2 {
		t.Fatalf("record: %+v", c)
	}
}

func TestKernelRecordCatchesEachDeviation(t *testing.T) {
	for _, tc := range []struct {
		name, bound, want string
		mutate            func(*kernelCheckIn)
	}{
		{"settle error", kbSettle, "Settle: boom", func(in *kernelCheckIn) { in.SettleErr = errors.New("boom") }},
		{"leaked fiber", kbLeaked, "fiber 2 s0restart/client is marked leaked", func(in *kernelCheckIn) { in.Snaps[1].Leaked = true }},
		{"diagnostic", kbLeaked, "carries a diagnostic: stuck", func(in *kernelCheckIn) { in.Snaps[0].Error = "stuck" }},
		{"not active", kbLeaked, "s0restart/listener is UNLOADING", func(in *kernelCheckIn) { in.Snaps[2].State = kernel.Unloading }},
		{"provision withdrawn", kbLeaked, "2 of 3 provisions", func(in *kernelCheckIn) { in.Provisions[0].Active = false }},
		{"dispose before unloading", kbOrder, "ledger lacks UNLOADING (40), a dispose-effect (0)", func(in *kernelCheckIn) {
			e := in.Events[0] // the pool's UNLOADING comes after its dispose-effect
			e.Sequence = 40
			in.Events, in.Watermark = append(in.Events[1:], e), 11
		}},
		{"tracked not done", kbOrder, "tracked goroutines [http-serve http-quiesce], finished before the first dispose-effect []", func(in *kernelCheckIn) {
			in.Events[3].Sequence, in.Events[4].Sequence = 90, 91 // done only after the dispose
		}},
		{"window lost", kbOrder, "event window no longer holds", func(in *kernelCheckIn) { in.Events = in.Events[3:] }},
		{"update without a new revision", kbSet, "pool was updated and its revision did not move", func(in *kernelCheckIn) { in.Fibers[0].RevAfter = 1 }},
		{"dependant never unloaded", kbOrder, "listener: ledger lacks UNLOADING (0)", func(in *kernelCheckIn) {
			in.Events = slices.DeleteFunc(in.Events, func(e kernel.LedgerEvent) bool { return e.Fiber == 3 })
		}},
		{"unexpected restart", kbSet, "restarted although nothing it depends on", func(in *kernelCheckIn) { in.Fibers[1].Expected, in.Fibers[1].Explicit = false, false }},
		{"no lease open", kbHeld, "pool: no kernel lease was open", func(in *kernelCheckIn) { in.Fibers[0].OpenAtRequest = 0 }},
		{"disposer saw a lease", kbWaited, "client: a disposer ran with 1 lease(s) still open", func(in *kernelCheckIn) { in.Fibers[1].DisposerSaw = 1 }},
		{"lease open after settle", kbWaited, "still open after Settle", func(in *kernelCheckIn) { in.Fibers[0].OpenAfter = 1 }},
		{"drain deadline", kbWaited, "hit its drain deadline", func(in *kernelCheckIn) { in.Events = append(in.Events, ev(99, 2, "deadline", "", "drain")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := cleanRestartIn()
			tc.mutate(&in)
			c := checkKernelRecord(in)
			found := false
			for _, p := range c.Problems[tc.bound] {
				found = found || strings.Contains(p, tc.want)
			}
			if !found {
				t.Fatalf("expected %s to name %q, got %v", tc.bound, tc.want, c.Problems)
			}
			for _, b := range c.bounds() {
				if b.Name == tc.bound && b.Pass {
					t.Fatal("the bound must fail")
				}
			}
		})
	}
}

func TestKernelRecordClientRestartLeavesPoolAlone(t *testing.T) {
	in := cleanRestartIn()
	in.Kind = "client"
	// only the client and the listener restart; the pool fiber has no events and keeps its revision
	var evs []kernel.LedgerEvent
	for _, e := range in.Events {
		if e.Fiber != 1 {
			evs = append(evs, e)
		}
	}
	in.Events, in.Watermark = evs, 11
	in.Fibers[0].Expected, in.Fibers[0].Explicit, in.Fibers[0].RevAfter = false, false, 1
	in.Fibers[1].Explicit = true
	if c := checkKernelRecord(in); len(c.Problems) != 0 {
		t.Fatalf("client restart: %v", c.Problems)
	}
	in.Fibers[0].RevAfter = 2 // the pool restarted though only the client was requested
	if c := checkKernelRecord(in); len(c.Problems[kbSet]) == 0 {
		t.Fatal("a restart of the provider that nobody requested must be reported")
	}
	in.Fibers[0].RevAfter = 1
	in.Events = append(in.Events, ev(60, 1, "state", kernel.Unloading, "")) // or a provider that unloaded without a revision
	if c := checkKernelRecord(in); len(c.Problems[kbSet]) == 0 {
		t.Fatal("an unrequested unload of the provider must be reported")
	}
}

func TestKernelExpectation(t *testing.T) {
	for kind, want := range map[string][2]string{"pool": {"pool", "client listener pool"}, "client": {"client", "client listener"}, "listener": {"listener", "listener"}, "combined": {"client listener pool", "client listener pool"}} {
		e, x, err := kernelExpectation(kind)
		if err != nil || fmt.Sprint(sortedKeys(e)) != fmt.Sprint(strings.Fields(want[0])) || fmt.Sprint(sortedKeys(x)) != fmt.Sprint(strings.Fields(want[1])) {
			t.Errorf("%s: %v %v %v", kind, sortedKeys(e), sortedKeys(x), err)
		}
	}
	if _, _, err := kernelExpectation("other"); err == nil {
		t.Fatal("unknown kind")
	}
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestKernelLeaseCountersAndEnterAll(t *testing.T) {
	// A lease on an ended context is refused and counted; the counter never goes negative.
	var l kLease
	l.disposing()
	if l.disposedAt.Load() == 0 || l.disposeSaw.Load() != 0 {
		t.Fatal("a disposer with no open lease records nothing")
	}
	l.open.Store(2)
	l.disposing()
	if l.disposeSaw.Load() != 2 {
		t.Fatal("a disposer that finds open leases records them")
	}
}

func TestKernelConfigIsCompleteFixtureData(t *testing.T) {
	c := kernelCfg
	if c.LoadDeadline <= 0 || c.DrainDeadline <= 0 || c.DisposeDeadline <= 0 || c.ArchiveLimit <= 0 || c.EventWindow <= 0 || c.ObserverQueue <= 0 || c.OwnerInbox <= 0 || c.TerminalLimit <= 0 {
		t.Fatalf("kernel.New refuses a zero number: %+v", c)
	}
	for _, d := range []time.Duration{c.LoadDeadline, c.DrainDeadline, c.DisposeDeadline} {
		if d >= restartRecoveryBudget {
			t.Fatalf("deadline %s must stay below the gate's %s recovery budget", d, restartRecoveryBudget)
		}
	}
	r, err := kernel.New(kernelCfg)
	if err != nil {
		t.Fatalf("kernel.New: %v", err)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := kernelConfigReport()["constructor"]; got != "kernel.New" {
		t.Fatal(got)
	}
}

func TestOwnerSelectsLimitsAndInterpretations(t *testing.T) {
	if len(fullLimitsFor(resOwnerLibrary)) != len(fullLimits) || len(fullInterpretationsFor(resOwnerLibrary)) != len(fullInterpretations) {
		t.Fatal("the library variant's report text is unchanged")
	}
	for _, l := range fullLimitsFor(resOwnerKernel) {
		if strings.Contains(l, "no kernel leases") {
			t.Fatalf("the kernel variant must not claim it has no kernel: %s", l)
		}
	}
}
