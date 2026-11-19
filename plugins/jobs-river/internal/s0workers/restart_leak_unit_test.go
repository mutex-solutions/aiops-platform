// SPDX-License-Identifier: Apache-2.0
package s0workers

// Database-free tests of the section 10 accounting: goroutine owner classification
// from profile text, descriptor classification, slope and heap arithmetic, the
// baseline-equality evaluator and the evaluated / reduced / not-evaluated labels.

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

const sp = "\t0x1\t"

func block(count int, labels string, frames ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d @ 0x1 0x2\n", count)
	if labels != "" {
		b.WriteString("# labels: " + labels + "\n")
	}
	for _, f := range frames {
		b.WriteString("#" + sp + f + "+0x10\tfile.go:1\n")
	}
	return b.String()
}

const (
	lPool1   = `{"s0gen":"1","s0role":"pool-caller"}`
	lPool2   = `{"s0gen":"2","s0role":"pool-caller"}`
	lClient3 = `{"s0gen":"3","s0role":"river-worker"}`
	lListen1 = `{"s0gen":"1","s0role":"http-server"}`
)

// fixtureProfile is the shape of a goroutine profile of a warmed application, reduced.
func fixtureProfile(extra ...string) string {
	blocks := []string{
		block(1, "", "runtime.gopark", "runtime.main"),
		block(1, "", "testing.(*T).Run", "testing.tRunner"),
		block(2, "", "github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck", "github.com/jackc/pgx/v5/pgxpool.NewWithConfig.func5"),
		block(1, lPool1, "github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck", "github.com/jackc/pgx/v5/pgxpool.NewWithConfig.func5"),
		block(1, lClient3, "github.com/riverqueue/river/internal/maintenance.(*JobCleaner).Start.func1", "github.com/riverqueue/river/rivershared/util/timeutil.(*TickerWithInitialTick).runLoop"),
		block(2, lClient3, "github.com/riverqueue/river/rivershared/util/timeutil.(*TickerWithInitialTick).runLoop"),
		block(1, lClient3, "github.com/riverqueue/river/internal/notifier.(*Notifier).waitOnce.func3", "sync.(*WaitGroup).Go.func1"),
		block(1, lListen1, "net/http.(*Server).Serve", "github.com/mutex-solutions/aiops-platform/plugins/jobs-river/internal/s0workers.(*fullApp).startListener.func1.1"),
		block(3, lListen1, "net/http.(*conn).serve"),
		block(2, "", "net/http.(*persistConn).readLoop"),
		block(1, "", "github.com/mutex-solutions/aiops-platform/plugins/jobs-river/internal/s0workers.(*fullApp).once", "github.com/mutex-solutions/aiops-platform/plugins/jobs-river/internal/s0workers.(*fullApp).doRequest"),
		block(1, "", "github.com/mutex-solutions/aiops-platform/plugins/jobs-river/internal/s0workers.(*fullApp).drain"),
	}
	blocks = append(blocks, extra...)
	n := 0
	for _, b := range blocks {
		var c int
		fmt.Sscanf(b, "%d @", &c)
		n += c
	}
	return fmt.Sprintf("goroutine profile: total %d\n%s", n, strings.Join(blocks, "\n"))
}

func groupMap(t *testing.T, text string) map[gKey]int {
	t.Helper()
	g, err := parseGoroutineProfile(text)
	if err != nil {
		t.Fatal(err)
	}
	m := map[gKey]int{}
	for _, x := range g {
		m[x.key()] += x.Count
	}
	return m
}

func TestParseGoroutineProfileOwners(t *testing.T) {
	m := groupMap(t, fixtureProfile())
	want := map[gKey]int{
		{ownerRuntime, "", 0, "runtime.main"}:                                                                                  1,
		{ownerHarness, "", 0, "testing.tRunner"}:                                                                               1,
		{ownerHarness, "", 0, "s0workers.(*fullApp).drain"}:                                                                    1,
		{ownerPgxPool, "", 0, "pgx/pgxpool.NewWithConfig.func5"}:                                                               2,
		{ownerPgxPool, roleCallerPool, 1, "pgx/pgxpool.NewWithConfig.func5"}:                                                   1,
		{ownerRiver, roleWorkerClient, 3, "river/rivershared/util/timeutil.(*TickerWithInitialTick).runLoop"}:                  3,
		{ownerRiver, roleWorkerClient, 3, "sync.(*WaitGroup).Go.func1 via river/internal/notifier.(*Notifier).waitOnce.func3"}: 1,
		{ownerHTTPServer, roleListener, 1, "s0workers.(*fullApp).startListener.func1.1"}:                                       1,
		{ownerHTTPServer, roleListener, 1, "net/http.(*conn).serve"}:                                                           3,
		{ownerLoad, "", 0, "net/http.(*persistConn).readLoop"}:                                                                 2,
		{ownerLoad, "", 0, "s0workers.(*fullApp).doRequest"}:                                                                   1,
	}
	// runtime.main via testing.tRunner in the first block: the entry decides, runtime.main is the entry.
	for k, n := range want {
		if m[k] != n {
			t.Errorf("group %+v: got %d want %d", k, m[k], n)
		}
	}
	for k := range m {
		if k.Owner == ownerUnclassified {
			t.Errorf("unexpected unclassified group %+v", k)
		}
	}
}

func TestParseGoroutineProfileUnclassifiedAndMalformed(t *testing.T) {
	m := groupMap(t, fixtureProfile(block(1, "", "main.leakedWorker", "main.startLeak")))
	if m[gKey{ownerUnclassified, "", 0, "main.startLeak"}] != 1 {
		t.Fatalf("an unknown goroutine must be its own unclassified group: %+v", m)
	}
	if _, err := parseGoroutineProfile("goroutine profile: total 9\n" + block(1, "", "runtime.main")); err == nil {
		t.Fatal("a profile whose groups do not sum to the header must be rejected")
	}
	if _, err := parseGoroutineProfile("goroutine profile: total 1\n" + block(1, `{bad`, "runtime.main")); err == nil {
		t.Fatal("bad labels must be rejected")
	}
}

func TestGoroutineDiffNamesEveryDeviation(t *testing.T) {
	a := gKey{ownerRiver, roleWorkerClient, 0, "river.(*producer).fetchPollLoop"}
	b := gKey{ownerRiver, roleWorkerClient, 0, "river.(*producer).heartbeatLogLoop"}
	base := map[gKey]int{a: 1, b: 1}
	// Same total (2): one worker disappeared, another appeared. Neither cancels the other.
	got := map[gKey]int{a: 2}
	d := diffGoroutines(base, got)
	if len(d) != 2 {
		t.Fatalf("expected two findings, got %v", d)
	}
	joined := strings.Join(d, "\n")
	if !strings.Contains(joined, "heartbeatLogLoop: expected 1, observed 0 (group missing)") || !strings.Contains(joined, "fetchPollLoop: expected 1, observed 2 (leak)") {
		t.Fatalf("findings must name both the missing worker and the leaked one: %v", d)
	}
	// Different owner with the same total: owner totals alone would match.
	other := gKey{ownerLoad, "", 0, "net/http.(*persistConn).readLoop"}
	if d := diffGoroutines(map[gKey]int{a: 1}, map[gKey]int{other: 1}); len(d) != 2 {
		t.Fatalf("a worker replaced by an unrelated one must be two findings: %v", d)
	}
	if d := diffGoroutines(base, map[gKey]int{a: 1, b: 1}); len(d) != 0 {
		t.Fatalf("equal sets must pass: %v", d)
	}
}

func TestNormalizeGoroutinesFlagsDisposedGenerations(t *testing.T) {
	groups, err := parseGoroutineProfile(fixtureProfile(block(1, lPool2, "github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck", "github.com/jackc/pgx/v5/pgxpool.NewWithConfig.func5")))
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]int{roleCallerPool: 2, roleWorkerClient: 3, roleListener: 1}
	norm, disposed := normalizeGoroutines(groups, live)
	if len(disposed) != 1 || !strings.Contains(disposed[0], "pool-caller#1 (disposed) owns 1 goroutine") {
		t.Fatalf("generation 1 of the pool is disposed and owns a goroutine: %v", disposed)
	}
	if norm[gKey{ownerPgxPool, roleCallerPool, 0, "pgx/pgxpool.NewWithConfig.func5"}] != 1 {
		t.Fatalf("the live generation must normalize to generation 0: %v", norm)
	}
}

func TestDeclaredBackgroundSetMatchesFixtureShape(t *testing.T) {
	// The declared set is fixed in code; the owner list is closed.
	if len(s10Owners) != 6 {
		t.Fatal("the owner set is fixed")
	}
	n := 0
	for _, e := range s10Declared[roleWorkerClient] {
		n += e.Count
		if e.Owner != ownerRiver {
			t.Fatalf("river component declared under %s", e.Owner)
		}
	}
	if n != 31 {
		t.Fatalf("declared worker-client goroutines = %d; the set is part of the criteria interpretation and must not drift silently", n)
	}
	for role, es := range s10Declared {
		for _, e := range es {
			if e.Owner == ownerUnclassified || e.Count <= 0 {
				t.Fatalf("%s declares %+v", role, e)
			}
		}
	}
}

const procNetFixture = ` sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:C350 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0 100 0 0
   1: 0100007F:C350 0100007F:9C40 01 00000000:00000000 00:00000000 00000000  1000        0 112 1 0 100 0 0
   2: 0100007F:9C40 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1000        0 113 1 0 100 0 0
   3: 0100007F:A001 0100007F:D8BB 01 00000000:00000000 00:00000000 00000000  1000        0 114 1 0 100 0 0
   4: 0100007F:A002 0100007F:D8BB 08 00000000:00000000 00:00000000 00000000  1000        0 115 1 0 100 0 0
   5: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 116 1 0 100 0 0
   6: 0100007F:B000 0100007F:1234 01 00000000:00000000 00:00000000 00000000  1000        0 117 1 0 100 0 0
`

func TestClassifyFDs(t *testing.T) {
	// listener port 0xC350=50000, PostgreSQL port 0xD8BB=55483, an unrelated listener 0x1F90=8080.
	socks := parseProcNetTCP(procNetFixture)
	if len(socks) != 7 || !socks[111].Listen || socks[112].Remote != 0x9C40 {
		t.Fatalf("proc net parse: %+v", socks)
	}
	links := []string{"socket:[111]", "socket:[112]", "socket:[113]", "socket:[114]", "socket:[115]", "socket:[116]", "socket:[117]", "socket:[999]",
		"anon_inode:[eventpoll]", "pipe:[5]", "pipe:[6]", "/tmp/log", "anon_inode:[eventfd]", "weird"}
	got := fdCounts(classifyFDs(links, socks, 0xD8BB, 0xC350))
	want := map[string]int{"listener-socket": 1, "http-server-conn": 1, "http-client-conn": 1, "pg-socket": 2, "listen-socket-unexpected": 1, "tcp-other": 1, "socket-other": 1,
		"epoll": 1, "pipe": 2, "file": 1, "anon-inode": 1, "other": 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("class %s: got %d want %d (all: %v)", k, got[k], n, got)
		}
	}
	// A CLOSE_WAIT (state 08) PostgreSQL socket still counts as a PostgreSQL socket: an unclosed connection stays visible.
	if got["pg-socket"] != 2 {
		t.Fatal("a half-closed PostgreSQL socket must still be counted")
	}
}

func TestSlope(t *testing.T) {
	x := []float64{0, 10, 20, 30}
	for _, c := range []struct {
		name string
		y    []float64
		want float64
	}{
		{"flat", []float64{40, 40, 40, 40}, 0},
		{"one per cycle", []float64{40, 50, 60, 70}, 1},
		{"shrinking", []float64{70, 60, 50, 40}, -1},
		{"noisy", []float64{0, 1, 0, 1}, 0.02},
	} {
		if got := slope(x, c.y); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: slope %v want %v", c.name, got, c.want)
		}
	}
	if slope([]float64{5}, []float64{7}) != 0 || slope([]float64{3, 3}, []float64{1, 2}) != 0 {
		t.Fatal("degenerate series have slope 0")
	}
}

func TestHeapTolerance(t *testing.T) {
	const mib = 1 << 20
	for _, c := range []struct {
		base, tol uint64
	}{{0, mib}, {2 * mib, mib}, {20 * mib, mib}, {21 * mib, 21 * mib / 20}, {100 * mib, 5 * mib}} {
		if got := heapTolerance(c.base); got != c.tol {
			t.Errorf("tolerance(%d) = %d want %d", c.base, got, c.tol)
		}
	}
	base := uint64(2 * mib)
	if heapExceeds(base, base+mib) {
		t.Error("exactly the tolerance is allowed")
	}
	if !heapExceeds(base, base+mib+1) {
		t.Error("one byte over the tolerance must exceed")
	}
	if heapExceeds(base, 1) {
		t.Error("below the baseline never exceeds")
	}
	big := uint64(100 * mib)
	if heapExceeds(big, big+5*mib) || !heapExceeds(big, big+5*mib+1) {
		t.Error("5% boundary of a 100 MiB baseline")
	}
}

func TestAppNames(t *testing.T) {
	if parseAppName(s10AppName(roleCallerPool, 7)) != "pool-caller#7" || parseAppName("s0workers") != roleFixture || parseAppName("psql") != "unattributed" || parseAppName("s0wk-x") != "unattributed" {
		t.Fatal("application_name attribution")
	}
	if p, r, g, ok := splitOwner("pool@pool-worker#4"); !ok || p != "pool" || r != "pool-worker" || g != 4 {
		t.Fatal("splitOwner")
	}
	if _, _, _, ok := splitOwner("harness-delivery-tally"); ok {
		t.Fatal("keys without a generation")
	}
}

// ---- evaluator ----

func liveGens() map[string]int {
	return map[string]int{roleCallerPool: 1, roleWorkerPool: 1, roleWorkerClient: 1, roleInsertClient: 1, roleListener: 1}
}

func appGoroutines(live map[string]int) []gGroup {
	var out []gGroup
	for role, es := range s10Declared {
		for _, e := range es {
			out = append(out, gGroup{e.Owner, role, live[role], e.Entry, e.Count})
		}
	}
	return out
}

func harnessSnap() *s10Snapshot {
	return &s10Snapshot{Name: "harness", Live: map[string]int{},
		Goroutines: []gGroup{{ownerHarness, "", 0, "testing.tRunner", 2}, {ownerRuntime, "", 0, "runtime.main", 1}, {ownerPgxPool, "", 0, "pgx/pgxpool.NewWithConfig.func5", 2}},
		FDs:        map[string]int{"pipe": 2, "file": 1, "epoll": 1, "pg-socket": 2}, FDTotal: 6, HarnessPools: map[string]int{"admin": 1, "fixture-caller": 1},
		Sessions: map[string]s10Sess{roleFixture: {Idle: 1}}, Entries: map[string]int{}, HeapAlloc: 4 << 20, Pools: nil}
}

// appSnap is a clean warmed application: both pools at the given sizes.
func appSnap(cycle int, gen int, callerTotal, workerTotal int) *s10Snapshot {
	live := map[string]int{}
	for k := range liveGens() {
		live[k] = gen
	}
	h := harnessSnap()
	g := append([]gGroup{}, h.Goroutines...)
	g = append(g, appGoroutines(live)...)
	g = append(g, gGroup{ownerHarness, "", 0, "s0workers.(*fullApp).drain", 1})
	callerID, workerID := fmt.Sprintf("pool-caller#%d", gen), fmt.Sprintf("pool-worker#%d", gen)
	s := &s10Snapshot{Name: fmt.Sprintf("cycle_%d", cycle), Cycle: cycle, Live: live, Goroutines: g,
		FDs: map[string]int{"pipe": 2, "file": 1, "epoll": 1, "listener-socket": 1, "pg-socket": 2 + callerTotal + workerTotal + 1}, HarnessPools: map[string]int{"admin": 1, "fixture-caller": 1},
		Pools:    []s10Pool{{Role: roleCallerPool, Gen: gen, Max: 8, Idle: callerTotal, Total: callerTotal}, {Role: roleWorkerPool, Gen: gen, Max: 8, Idle: workerTotal, Total: workerTotal}},
		Sessions: map[string]s10Sess{roleFixture: {Idle: 1}, callerID: {Idle: callerTotal}, workerID: {Idle: workerTotal + 1}},
		Entries:  map[string]int{}, HeapAlloc: 5 << 20, HeapObjects: 1000}
	for k, v := range s10DeclaredEntries(live) {
		s.Entries[k] = v
	}
	s.Entries["harness-delivery-tally"] = 0
	return s
}

func TestEvaluateCleanBaselineAndCheckpoints(t *testing.T) {
	h := harnessSnap()
	base := appSnap(0, 1, 6, 5)
	if f := s10Evaluate("baseline", h, nil, base, nil); len(f) != 0 {
		t.Fatalf("clean baseline: %v", f)
	}
	// A later generation with another pool size is clean: generations normalize and pool size is demand driven.
	cp := appSnap(10, 4, 8, 7)
	if f := s10Evaluate("checkpoint", h, base, cp, []*s10Snapshot{appSnap(0, 1, 6, 5)}); len(f) != 0 {
		t.Fatalf("clean checkpoint: %v", f)
	}
	td := &s10Snapshot{Name: "teardown", Cycle: -1, Live: map[string]int{}, Goroutines: append([]gGroup{}, h.Goroutines...), FDs: map[string]int{"pipe": 2, "file": 1, "epoll": 1, "pg-socket": 2}, HarnessPools: h.HarnessPools,
		Sessions: map[string]s10Sess{roleFixture: {Idle: 1}}, Entries: map[string]int{"harness-delivery-tally": 0}}
	if f := s10Evaluate("teardown", h, base, td, nil); len(f) != 0 {
		t.Fatalf("clean teardown: %v", f)
	}
}

func hasFinding(f []s10Finding, bound, sub string) bool {
	for _, x := range f {
		if x.Bound == bound && strings.Contains(x.Detail, sub) {
			return true
		}
	}
	return false
}

func TestEvaluateCatchesEachLeakClass(t *testing.T) {
	h := harnessSnap()
	base := appSnap(0, 1, 6, 5)
	check := func(name string, mutate func(*s10Snapshot), bound, sub string) {
		t.Run(name, func(t *testing.T) {
			cp := appSnap(10, 3, 6, 5)
			mutate(cp)
			f := s10Evaluate("checkpoint", h, base, cp, nil)
			if !hasFinding(f, bound, sub) {
				t.Fatalf("expected %s containing %q, got %v", bound, sub, f)
			}
		})
	}
	check("goroutine parked in a named function", func(s *s10Snapshot) {
		s.Goroutines = append(s.Goroutines, gGroup{ownerUnclassified, "", 0, "s0workers.leakedParkedWorker", 1})
	}, bGoroutines, "s0workers.leakedParkedWorker")
	check("goroutine of a disposed pool generation", func(s *s10Snapshot) {
		s.Goroutines = append(s.Goroutines, gGroup{ownerPgxPool, roleCallerPool, 2, "pgx/pgxpool.NewWithConfig.func5", 1})
	}, bDisposed, "pool-caller#2 (disposed) owns 1 goroutine")
	check("connection acquired and never released in the live pool", func(s *s10Snapshot) {
		s.Pools[0].Acquired, s.Pools[0].Idle = 1, s.Pools[0].Idle-1
	}, bConns, "pool-caller#3 acquired connections: expected 0, observed 1")
	check("session of an old generation survives", func(s *s10Snapshot) {
		s.Sessions["pool-caller#2"] = s10Sess{Idle: 1}
		s.FDs["pg-socket"]++
	}, bDisposed, "pool-caller#2 (disposed) owns 1 server session")
	check("session idle in transaction", func(s *s10Snapshot) {
		ss := s.Sessions["pool-caller#3"]
		ss.Idle--
		ss.IdleInTx++
		s.Sessions["pool-caller#3"] = ss
	}, bOpenTx, "pool-caller#3 has 1 session(s) idle in transaction")
	check("extra listener", func(s *s10Snapshot) {
		s.FDs["listener-socket"]++
		s.Entries["listener@http-server#2"] = 1
		s.Goroutines = append(s.Goroutines, gGroup{ownerHTTPServer, roleListener, 2, "s0workers.(*fullApp).startListener.func1.1", 1})
	}, bFDs, "listener-socket: expected 1, observed 2")
	check("extra listener leaves a disposed-generation entry and goroutine", func(s *s10Snapshot) {
		s.Entries["listener@http-server#2"] = 1
		s.Goroutines = append(s.Goroutines, gGroup{ownerHTTPServer, roleListener, 2, "s0workers.(*fullApp).startListener.func1.1", 1})
	}, bDisposed, "http-server#2 (disposed)")
	check("leaked socket", func(s *s10Snapshot) { s.FDs["tcp-other"] = 1 }, bFDs, "tcp-other")
	check("registry entry left", func(s *s10Snapshot) { s.Entries["route@http-server#9"] = 1 }, bDisposed, "http-server#9 (disposed) owns 1 of route")
	check("heap over tolerance", func(s *s10Snapshot) { s.HeapAlloc = base.HeapAlloc + heapTolerance(base.HeapAlloc) + 1 }, bHeap, "exceeds warmed baseline")
	check("retained disposed object", func(s *s10Snapshot) { s.Retained = []string{"pool-worker#2"} }, bDisposed, "pool-worker#2 (disposed) object still reachable")
	check("more pinned stopped clients than connections can pin", func(s *s10Snapshot) { s.PinnedClients = 6 }, bDisposed, "6 disposed River clients still reachable, more than the 5 connections")
	check("PostgreSQL socket without a session", func(s *s10Snapshot) { s.FDs["pg-socket"]++ }, bConns, "PostgreSQL sockets")
	check("session count differs from pool total", func(s *s10Snapshot) {
		s.Sessions["pool-worker#3"] = s10Sess{Idle: 9}
		s.FDs["pg-socket"] += 3
	}, bConns, "pool-worker#3 server sessions")
	check("growth against the previous checkpoint", func(s *s10Snapshot) {
		s.Goroutines = append(s.Goroutines, gGroup{ownerRiver, roleWorkerClient, 3, "river.(*producer).fetchPollLoop", 1})
	}, bGrowth, "goroutines:total grew")
}

func TestPinnedClientsWithinPoolConnectionsAreNotALeak(t *testing.T) {
	h := harnessSnap()
	base := appSnap(0, 1, 6, 5)
	cp := appSnap(10, 2, 6, 5)
	cp.PinnedClients = 5 // every worker-pool connection pins one stopped client through its last query context
	if f := s10Evaluate("checkpoint", h, base, cp, nil); len(f) != 0 {
		t.Fatalf("bounded pgconn context retention must not fail the checkpoint: %v", f)
	}
}

func TestEvaluateHeapWithinToleranceDoesNotExcuseGoroutines(t *testing.T) {
	h := harnessSnap()
	base := appSnap(0, 1, 6, 5)
	cp := appSnap(10, 2, 6, 5)
	cp.HeapAlloc = base.HeapAlloc // heap is fine
	cp.Goroutines = append(cp.Goroutines, gGroup{ownerRiver, roleWorkerClient, 2, "river.(*producer).fetchPollLoop", 1})
	f := s10Evaluate("checkpoint", h, base, cp, nil)
	if !hasFinding(f, bGoroutines, "fetchPollLoop") || hasFinding(f, bHeap, "") {
		t.Fatalf("a goroutine leak fails with a clean heap: %v", f)
	}
}

func TestEvaluateOneDisappearingWorkerDoesNotCancelALeak(t *testing.T) {
	h := harnessSnap()
	base := appSnap(0, 1, 6, 5)
	cp := appSnap(10, 2, 6, 5)
	var kept []gGroup
	removed := false
	for _, g := range cp.Goroutines {
		if g.Entry == "river.(*producer).heartbeatLogLoop" && !removed {
			removed = true // an unrelated worker disappears
			continue
		}
		kept = append(kept, g)
	}
	kept = append(kept, gGroup{ownerHarness, "", 0, "s0workers.leakedParkedWorker", 1}) // and another one leaks: same total
	cp.Goroutines = kept
	if goroutineTotal(cp.Goroutines) != goroutineTotal(base.Goroutines) {
		t.Fatal("test setup: totals must be equal")
	}
	f := s10Evaluate("checkpoint", h, base, cp, nil)
	if !hasFinding(f, bGoroutines, "heartbeatLogLoop: expected 1, observed 0 (group missing)") || !hasFinding(f, bGoroutines, "leakedParkedWorker: expected 0, observed 1") {
		t.Fatalf("both deviations must be reported: %v", f)
	}
}

func TestEvaluateBaselineAgainstDeclaredSetAndTeardown(t *testing.T) {
	h := harnessSnap()
	base := appSnap(0, 1, 6, 5)
	base.Goroutines = append(base.Goroutines, gGroup{ownerUnclassified, "", 0, "vendor.mystery", 1})
	f := s10Evaluate("baseline", h, nil, base, nil)
	if !hasFinding(f, bDeclared, "unclassified goroutine group (not pre-declared): 1 at vendor.mystery") {
		t.Fatalf("an unclassified owner at the baseline is a failure, not an addition: %v", f)
	}
	td := &s10Snapshot{Name: "teardown", Cycle: -1, Live: map[string]int{}, Goroutines: append([]gGroup{{ownerRiver, roleWorkerClient, 1, "river.(*producer).fetchPollLoop", 1}}, h.Goroutines...),
		FDs: map[string]int{"pipe": 2, "file": 1, "epoll": 1, "pg-socket": 2, "tcp-other": 1}, HarnessPools: h.HarnessPools, Sessions: map[string]s10Sess{roleFixture: {Idle: 1}}, Entries: map[string]int{}}
	f = s10Evaluate("teardown", h, appSnap(0, 1, 6, 5), td, nil)
	if !hasFinding(f, bDisposed, "river-worker#1 (disposed) owns 1 goroutine") || !hasFinding(f, bTeardownFD, "exceed the harness baseline") {
		t.Fatalf("teardown with a goroutine and an extra descriptor: %v", f)
	}
}

func TestSeriesSlopeAndGrowth(t *testing.T) {
	var cps []*s10Snapshot
	for i, c := range []int{0, 10, 20, 30} {
		s := appSnap(c, i+1, 6, 5)
		s.HeapAlloc = uint64(5<<20 + 1000*c)
		cps = append(cps, s)
	}
	st := s10Series(cps)
	if got := st["heap_alloc_bytes"].Slope; math.Abs(got-1000) > 1e-6 {
		t.Fatalf("heap slope %v", got)
	}
	if st["goroutines:total"].Slope != 0 || st["goroutines:total"].Grew || !st["goroutines:total"].Exact {
		t.Fatalf("flat goroutine series: %+v", st["goroutines:total"])
	}
	cps[3].Goroutines = append(cps[3].Goroutines, gGroup{ownerRiver, roleWorkerClient, 4, "river.(*producer).fetchPollLoop", 1})
	st = s10Series(cps)
	if !st["goroutines:total"].Grew || st["goroutines:total"].Slope <= 0 {
		t.Fatalf("growing goroutine series must be flagged: %+v", st["goroutines:total"])
	}
	if st["pool_total_conns:pool-caller"].Exact {
		t.Fatal("pool size is demand driven, not an exact series")
	}
}

func TestCheckpointSchedule(t *testing.T) {
	for _, c := range []struct {
		planned int
		want    []int
	}{{30, []int{0, 10, 20, 30}}, {10, []int{0, 10}}, {1, []int{0, 1}}, {5, []int{0, 5}}, {12, []int{0, 10, 12}}, {25, []int{0, 10, 20, 25}}} {
		if got := s10CheckpointCycles(c.planned); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("planned %d: %v want %v", c.planned, got, c.want)
		}
	}
	if fmt.Sprint(s10RequiredCycles(30)) != "[0 10 20 30]" || fmt.Sprint(s10RequiredCycles(10)) != "[0 10]" {
		t.Fatal("required checkpoints")
	}
}

func TestSection10Labels(t *testing.T) {
	full := s10ArmStatus{BaselineTaken: true, Planned: 30, Frozen: 30, FrozenScale: true, Taken: []int{0, 10, 20, 30}, TeardownDone: true}
	comb := s10ArmStatus{BaselineTaken: true, Planned: 10, Frozen: 10, FrozenScale: true, Taken: []int{0, 10}, TeardownDone: true}
	for _, c := range []struct {
		name string
		st   s10ArmStatus
		want string
	}{
		{"frozen scale, everything taken", full, s10Passed},
		{"combined arm frozen, 0 and 10", comb, s10Passed},
		{"missing checkpoint 20", func() s10ArmStatus { s := full; s.Taken = []int{0, 10, 30}; return s }(), s10Incomplete},
		{"missing teardown", func() s10ArmStatus { s := full; s.TeardownDone = false; return s }(), s10Incomplete},
		{"CI reduced: 1 cycle", s10ArmStatus{BaselineTaken: true, Planned: 1, Frozen: 30, Taken: []int{0, 1}, TeardownDone: true}, s10Reduced},
		{"reduced but all four checkpoints", func() s10ArmStatus { s := full; s.FrozenScale = false; return s }(), s10Reduced},
		{"fewer cycles than frozen", func() s10ArmStatus { s := full; s.Planned = 20; s.Taken = []int{0, 10, 20}; return s }(), s10Reduced},
		{"no baseline", s10ArmStatus{Planned: 30, Frozen: 30}, s10NotEvaluated},
		{"failed bound", func() s10ArmStatus { s := full; s.Failed = true; return s }(), s10Failed},
	} {
		if got := s10ArmLabel(c.st); got != c.want {
			t.Errorf("%s: %q want %q", c.name, got, c.want)
		}
	}
	if got := s10OverallLabel([]string{s10Passed, s10Passed, s10Passed, s10Passed}, 4); got != s10Passed {
		t.Errorf("all arms passed: %q", got)
	}
	if got := s10OverallLabel([]string{s10Passed, s10Passed, s10Passed}, 4); got != s10Incomplete {
		t.Errorf("a missing arm cannot pass: %q", got)
	}
	if got := s10OverallLabel([]string{s10Passed, s10Reduced, s10Passed, s10Passed}, 4); got != s10Reduced {
		t.Errorf("one reduced arm: %q", got)
	}
	if got := s10OverallLabel([]string{s10Passed, s10Failed}, 4); got != s10Failed {
		t.Errorf("a failed arm: %q", got)
	}
	if got := s10OverallLabel(nil, 4); got != s10NotEvaluated {
		t.Errorf("no arms: %q", got)
	}
	if got := s10OverallLabel([]string{s10NotEvaluated, s10NotEvaluated}, 4); got != s10NotEvaluated {
		t.Errorf("nothing evaluated: %q", got)
	}
}

func TestOlderReportsAreNotEvaluated(t *testing.T) {
	old := []byte(`{"classification":"PASS at frozen scale","status":"PASS at frozen scale: 100/100 cycles","cycles":[{"goroutines_after_cycle":79,"open_fds_after_cycle":55}]}`)
	if got := s10LabelFromReport(old); got != s10NotEvaluated {
		t.Fatalf("a report without section 10 fields cannot claim it: %q", got)
	}
	if got := s10LabelFromReport([]byte(`{"section10_label":"partially evaluated (reduced)"}`)); got != s10Reduced {
		t.Fatal(got)
	}
	if got := s10LabelFromReport([]byte(`not json`)); got != s10NotEvaluated {
		t.Fatal(got)
	}
}

// Regression: the delivery tally kept one map entry per delivered job for the whole
// arm (450,000 at frozen scale); section 10's heap bound reads that as unbounded
// growth. It is now cleared between cycles; counts of redeliveries stay cumulative.
func TestDeliveryTallyIsBoundedAndStillCountsRedeliveries(t *testing.T) {
	a := newFullApp(nil, 0)
	a.drainOn = true
	go a.drain()
	defer func() { close(a.stopDrain); <-a.drainDone }()
	send := func(ids ...int64) {
		for _, id := range ids {
			a.deliveries <- delivery{stage: "handled", jobID: id, inserted: true}
		}
		for len(a.deliveries) != 0 {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(10 * time.Millisecond)
	}
	send(1, 2, 3, 2) // job 2 delivered twice inside one cycle
	if re, _ := a.tally(); re != 1 || a.tallyEntries() != 3 {
		t.Fatalf("redeliveries=%d entries=%d", re, a.tallyEntries())
	}
	if n := a.resetTally(); n != 3 || a.tallyEntries() != 0 {
		t.Fatalf("reset must drop every per-job entry (dropped %d, left %d)", n, a.tallyEntries())
	}
	if re, _ := a.tally(); re != 1 {
		t.Fatal("the cumulative redelivery count must survive the reset")
	}
	for cycle := 0; cycle < 50; cycle++ {
		ids := make([]int64, 1000)
		for i := range ids {
			ids[i] = int64(cycle*1000 + i + 10)
		}
		send(ids...)
		a.resetTally()
	}
	if a.tallyEntries() != 0 {
		t.Fatal("entries must not accumulate across cycles")
	}
}
