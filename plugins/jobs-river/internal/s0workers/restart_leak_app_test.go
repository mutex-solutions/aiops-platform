// SPDX-License-Identifier: Apache-2.0
package s0workers

// Section 10 collectors for the in-process application of the full restart gate:
// generation ledger, goroutine/descriptor/session/pool/heap snapshots, the
// quiescence step and the per-arm accountant used by TestWorkerRestartFullGate.
// Checkpoints are only taken between cycles, after quiescence, never inside a load
// window. The evaluation rules are in restart_leak_pure_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"weak"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// labelled runs f with pprof labels naming the role and generation. Goroutines
// created inside f (pgx pool health check, River client components, the HTTP
// accept loop and its connections) inherit them, which is how a goroutine profile
// attributes them to a generation. Labels change no behaviour of the measured code.
func labelled(ctx context.Context, role string, gen int, f func(context.Context)) {
	pprof.Do(ctx, pprof.Labels("s0role", role, "s0gen", strconv.Itoa(gen)), f)
}

type gid struct {
	Role string
	Gen  int
}

type weakPool struct {
	gid
	p weak.Pointer[pgxpool.Pool]
}
type weakClient struct {
	gen, poolGen int // poolGen: generation of the worker pool the client ran on
	c            weak.Pointer[river.Client[pgx.Tx]]
}

// genLedger records the generation of every pool, client and listener the
// application creates, which ones are live, and weak references to disposed
// pools and clients (a disposed object still reachable after two GCs is retained).
type genLedger struct {
	mu          sync.Mutex
	last, live  map[string]int
	routeN      map[int]int  // listener generation -> routes registered
	handlerOn   map[int]bool // worker-client generation -> job handler installed
	pools       map[*pgxpool.Pool]gid
	weakPools   []weakPool
	weakClients []weakClient
}

func newGenLedger() *genLedger {
	return &genLedger{last: map[string]int{}, live: map[string]int{}, routeN: map[int]int{}, handlerOn: map[int]bool{}, pools: map[*pgxpool.Pool]gid{}}
}

func (g *genLedger) open(role string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.last[role]++
	g.live[role] = g.last[role]
	return g.last[role]
}

func (g *genLedger) markDisposed(role string, gen int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.live[role] == gen {
		delete(g.live, role)
	}
}

func (g *genLedger) liveMap() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]int, len(g.live))
	for k, v := range g.live {
		out[k] = v
	}
	return out
}

func (g *genLedger) poolOpened(p *pgxpool.Pool, role string, gen int) {
	g.mu.Lock()
	g.pools[p] = gid{role, gen}
	g.mu.Unlock()
}

func (g *genLedger) poolDisposed(p *pgxpool.Pool) (string, int) {
	g.mu.Lock()
	id := g.pools[p]
	delete(g.pools, p) // no strong reference to a disposed pool is kept
	g.mu.Unlock()
	g.markDisposed(id.Role, id.Gen)
	return id.Role, id.Gen
}

func (g *genLedger) keepPool(role string, gen int, p *pgxpool.Pool) {
	g.mu.Lock()
	g.weakPools = append(g.weakPools, weakPool{gid{role, gen}, weak.Make(p)})
	g.mu.Unlock()
}

func (g *genLedger) keepClient(gen int, pool *pgxpool.Pool, c *river.Client[pgx.Tx]) {
	g.mu.Lock()
	g.weakClients = append(g.weakClients, weakClient{gen, g.pools[pool].Gen, weak.Make(c)})
	g.mu.Unlock()
}

func (g *genLedger) routes(gen, n int) {
	g.mu.Lock()
	g.routeN[gen] = n
	g.mu.Unlock()
}

func (g *genLedger) handlerInstalled(gen int, on bool) {
	g.mu.Lock()
	g.handlerOn[gen] = on
	g.mu.Unlock()
}

// retained lists disposed pools and clients still reachable after two GCs and
// forgets the collected ones. A disposed worker client that ran on a pool which is
// still live is counted separately (pinned): pgconn keeps the context of the last
// query on every connection (PgConn.resultReader.ctx / multiResultReader.ctx) until
// that connection's next query, and River's job contexts carry the client, so each
// pooled connection can keep one stopped client reachable. That is bounded by the
// pool's connections and ends when the pool is closed; a client of a closed pool,
// or any disposed pool object, still reachable is a retained disposed object.
func (g *genLedger) retained() (retained []string, pinned int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	pools := g.weakPools[:0]
	for _, w := range g.weakPools {
		if w.p.Value() != nil {
			retained = append(retained, fmt.Sprintf("%s#%d", w.Role, w.Gen))
			pools = append(pools, w)
		}
	}
	g.weakPools = pools
	clients := g.weakClients[:0]
	for _, w := range g.weakClients {
		if w.c.Value() == nil {
			continue
		}
		clients = append(clients, w)
		if g.live[roleWorkerPool] == w.poolGen {
			pinned++
			continue
		}
		retained = append(retained, fmt.Sprintf("%s#%d", roleWorkerClient, w.gen))
	}
	g.weakClients = clients
	sort.Strings(retained)
	return retained, pinned
}

// ---- collection ----

func readLinks(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		l, err := os.Readlink(dir + "/" + e.Name())
		if err != nil {
			continue // closed between ReadDir and Readlink (the ReadDir descriptor itself)
		}
		out = append(out, l)
	}
	return out, nil
}

func (a *fullApp) sessions(ctx context.Context) (map[string]s10Sess, error) {
	rows, err := a.f.admin.Query(ctx, "SELECT application_name, state, coalesce(extract(epoch FROM now()-state_change),0) FROM pg_stat_activity WHERE usename=$1 AND backend_type='client backend' AND pid<>pg_backend_pid()", a.f.login)
	if err != nil {
		return nil, safeError("section 10 sessions", err)
	}
	defer rows.Close()
	out := map[string]s10Sess{}
	for rows.Next() {
		var app string
		var state *string
		var age float64
		if err := rows.Scan(&app, &state, &age); err != nil {
			return nil, safeError("section 10 sessions scan", err)
		}
		key := parseAppName(app)
		s := out[key]
		st := ""
		if state != nil {
			st = *state
		}
		switch {
		case st == "active":
			s.Active++
			if age > 2 {
				s.ActiveStale++
			}
		case st == "idle":
			s.Idle++
		case len(st) >= 19 && st[:19] == "idle in transaction":
			s.IdleInTx++
		default:
			s.Other++
		}
		out[key] = s
	}
	return out, safeError("section 10 sessions rows", rows.Err())
}

func (a *fullApp) fds() (map[string]int, int, string) {
	links, err := readLinks("/proc/self/fd")
	if err != nil {
		return nil, 0, "cannot read /proc/self/fd"
	}
	socks := map[uint64]procSock{}
	for _, f := range []string{"/proc/self/net/tcp", "/proc/self/net/tcp6"} {
		if data, err := os.ReadFile(f); err == nil {
			for k, v := range parseProcNetTCP(string(data)) {
				socks[k] = v
			}
		}
	}
	listenPort := -1
	if _, p, err := net.SplitHostPort(a.addr); err == nil {
		listenPort, _ = strconv.Atoi(p)
	}
	return fdCounts(classifyFDs(links, socks, int(a.f.base.ConnConfig.Port), listenPort)), len(links), ""
}

func poolRec(role string, gen int, p *pgxpool.Pool) s10Pool {
	st := p.Stat()
	return s10Pool{Role: role, Gen: gen, Max: int(st.MaxConns()), Acquired: int(st.AcquiredConns()), Idle: int(st.IdleConns()), Total: int(st.TotalConns()), Constructing: int(st.ConstructingConns())}
}

// s10Collect takes one snapshot. The caller has already quiesced the application.
func (a *fullApp) s10Collect(ctx context.Context, name string, cycle int, withApp bool) (*s10Snapshot, error) {
	s := &s10Snapshot{Name: name, Cycle: cycle, Live: a.gens.liveMap(), Entries: map[string]int{}, HarnessPools: map[string]int{}}
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.HeapAlloc, s.HeapObjects = ms.HeapAlloc, ms.HeapObjects

	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		return nil, err
	}
	var err error
	if s.Goroutines, err = parseGoroutineProfile(buf.String()); err != nil {
		return nil, err
	}
	s.GoroutineTotal = goroutineTotal(s.Goroutines)

	sess, err := a.sessions(ctx)
	if err != nil {
		return nil, err
	}
	s.Sessions = sess
	var fdErr string
	s.FDs, s.FDTotal, fdErr = a.fds()
	s.FDError = fdErr
	s.HarnessPools["admin"] = int(a.f.admin.Stat().TotalConns())
	s.HarnessPools["fixture-caller"] = int(a.f.callerPool.Stat().TotalConns())

	a.mu.Lock()
	cur := a.cur
	a.mu.Unlock()
	if withApp {
		if cur != nil {
			s.Pools = append(s.Pools, poolRec(roleCallerPool, a.gens.liveMap()[roleCallerPool], cur.pool))
		}
		if a.w != nil {
			s.Pools = append(s.Pools, poolRec(roleWorkerPool, a.gens.liveMap()[roleWorkerPool], a.w.pool))
		}
	}
	a.gens.mu.Lock()
	for role, gen := range a.gens.live {
		switch role {
		case roleWorkerClient, roleInsertClient:
			s.Entries["client@"+role+"#"+strconv.Itoa(gen)] = 1
		case roleListener:
			s.Entries["listener@"+role+"#"+strconv.Itoa(gen)] = 1
		}
	}
	for _, p := range s.Pools {
		s.Entries["pool@"+p.Role+"#"+strconv.Itoa(p.Gen)] = 1
	}
	for gen, n := range a.gens.routeN {
		if n > 0 {
			s.Entries["route@"+roleListener+"#"+strconv.Itoa(gen)] = n
		}
	}
	for gen, on := range a.gens.handlerOn {
		if on {
			s.Entries["job-handler@"+roleWorkerClient+"#"+strconv.Itoa(gen)] = 1
		}
	}
	a.gens.mu.Unlock()
	s.Entries["harness-delivery-tally"] = a.tallyEntries()
	s.Retained, s.PinnedClients = a.gens.retained()
	return s, nil
}

// unfinishedJobs counts this arm's jobs above floor that are not finished; a
// primary-key range read, independent of retained history.
func (a *fullApp) unfinishedJobs(ctx context.Context, floor int64) (int, error) {
	var n int
	err := a.f.admin.QueryRow(ctx, "SELECT count(*) FROM "+table(a.f.jobs, "river_job")+" WHERE id > $1 AND state IN ('available','running','retryable','scheduled','pending')", floor).Scan(&n)
	return n, safeError("section 10 unfinished jobs", err)
}

const (
	s10Settle         = 250 * time.Millisecond
	s10MaxAttempts    = 8
	s10RetainPatience = 15 * time.Second // beyond River's 10 s job timeout
)

func onlyRetention(f []s10Finding) bool {
	for _, x := range f {
		if !strings.Contains(x.Detail, "still reachable") && !strings.Contains(x.Detail, "retained_disposed_objects grew") {
			return false
		}
	}
	return len(f) > 0
}

// quiesce waits (bounded by the S4 30 s completion budget) for in-flight HTTP
// requests, held transactions/handlers, the delivery observer queue and River's
// unfinished jobs to drain, closes the load generator's idle connections and
// settles. It returns the failure description, empty when quiescent. Because it
// stops at the first quiet instant it is a bounded wait for work that exists, not a
// loop that retries until a measurement happens to pass.
func (a *fullApp) quiesce(ctx context.Context, floor int64) string {
	deadline := time.Now().Add(restartCompletionBudget)
	var why string
	for lastDB := (time.Time{}); ; {
		why = ""
		switch {
		case a.reqNow.Load() != 0 || a.inflightReq.Load() != 0:
			why = fmt.Sprintf("requests in flight: server %d, client %d", a.reqNow.Load(), a.inflightReq.Load())
		case a.heldHTTPNow.Load() != 0 || a.heldWorkerNow.Load() != 0:
			why = fmt.Sprintf("held work: http %d, worker %d", a.heldHTTPNow.Load(), a.heldWorkerNow.Load())
		case len(a.deliveries) != 0:
			why = fmt.Sprintf("observer queue not drained: %d deliveries", len(a.deliveries))
		case time.Since(lastDB) >= 200*time.Millisecond:
			lastDB = time.Now()
			n, err := a.unfinishedJobs(ctx, floor)
			if err != nil {
				why = "unfinished-job check failed: " + err.Error()
			} else if n != 0 {
				why = fmt.Sprintf("%d River jobs not finished", n)
			}
		default:
			why = "waiting"
		}
		if why == "" {
			break
		}
		if why != "waiting" && (time.Now().After(deadline) || ctx.Err() != nil) {
			return "not quiescent after " + restartCompletionBudget.String() + ": " + why
		}
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Millisecond):
		}
	}
	a.httpClient.CloseIdleConnections()
	select {
	case <-ctx.Done():
	case <-time.After(s10Settle):
	}
	return ""
}

// warmPools opens every connection of both application pools once and returns them
// (harness only; before the warmed baseline and before cycle 1). A pgx connection
// carries about 0.2 MB of statement cache, and the restart burst grows the pools to
// their maximum, so a baseline taken with 4 or 5 lazily opened connections would be
// compared with checkpoints at 8 and read that as heap growth. River may hold a few
// worker-pool connections itself; an acquire that cannot be served within 2 s ends the
// warm-up of that pool.
func (a *fullApp) warmPools(ctx context.Context) {
	a.mu.Lock()
	cur := a.cur
	a.mu.Unlock()
	for _, p := range []*pgxpool.Pool{cur.pool, a.w.pool} {
		var held []*pgxpool.Conn
		for i := 0; i < fullPoolConns; i++ {
			c, cancel := context.WithTimeout(ctx, 2*time.Second)
			conn, err := p.Acquire(c)
			cancel()
			if err != nil {
				break
			}
			held = append(held, conn)
		}
		for _, c := range held {
			c.Release()
		}
	}
}

// warmTraffic offers 100/s for d through the same request path as a cycle; its IDs
// and ledger are discarded. It is the warm-up that precedes the warmed baseline.
func (a *fullApp) warmTraffic(ctx context.Context, prefix string, d time.Duration) {
	l := newLedger()
	var wg sync.WaitGroup
	runOpenLoop(ctx, time.Now().UTC().Add(100*time.Millisecond), fullInterval, max(1, int(d/fullInterval)), func() time.Time { return time.Now().UTC() }, timerWait, func(i int, scheduled, started time.Time) {
		key := fmt.Sprintf("%s%05d", prefix, i)
		l.register(key, i, scheduled, started)
		wg.Add(1)
		go func() { defer wg.Done(); a.doRequest(ctx, l, key, scheduled) }()
	})
	wg.Wait()
}

// ---- per-arm accountant ----

type s10DeclaredReport struct {
	Owners           []string               `json:"goroutine_owners"`
	PerGeneration    map[string][]declEntry `json:"background_goroutines_per_live_generation"`
	HarnessApp       []declEntry            `json:"background_goroutines_of_the_application_itself"`
	SessionAllowance map[string]int         `json:"server_sessions_beyond_pool_total"`
	EntriesPerLive   map[string]int         `json:"entries_per_live_generation_example"`
	Note             string                 `json:"note"`
}

func s10DeclaredNow() s10DeclaredReport {
	ex := map[string]int{roleCallerPool: 1, roleWorkerPool: 1, roleWorkerClient: 1, roleInsertClient: 1, roleListener: 1}
	return s10DeclaredReport{Owners: s10Owners, PerGeneration: s10Declared, HarnessApp: s10DeclaredHarness, SessionAllowance: s10SessionAllowance, EntriesPerLive: s10DeclaredEntries(ex),
		Note: "declared before the first measurement; a goroutine group or entry outside this set, or with another count, is a failure, never an addition"}
}

type s10ArmReport struct {
	Arm        string                   `json:"arm"`
	Label      string                   `json:"label"`
	Planned    int                      `json:"cycles_planned"`
	Required   []int                    `json:"required_checkpoint_cycles"`
	Scheduled  []int                    `json:"scheduled_checkpoint_cycles"`
	Taken      []int                    `json:"taken_checkpoint_cycles"`
	Declared   s10DeclaredReport        `json:"declared_background_set"`
	Harness    *s10Snapshot             `json:"harness_baseline_before_application,omitempty"`
	Baseline   *s10Snapshot             `json:"warmed_baseline,omitempty"`
	Checkpoint []*s10Snapshot           `json:"checkpoints"`
	Teardown   *s10Snapshot             `json:"teardown,omitempty"`
	Series     map[string]s10SeriesStat `json:"series,omitempty"`
	Failed     []string                 `json:"failed_bounds"`
	CostMs     map[string]float64       `json:"checkpoint_cost_ms,omitempty"`
}

type s10Report struct {
	Label string          `json:"label"`
	Rule  []string        `json:"rules"`
	Arms  []*s10ArmReport `json:"arms"`
}

var s10Rules = []string{
	"Warmed baseline: after the application is up and warm-up traffic (at least 3 s at 100/s, the configured warm-up otherwise) has run, drained, quiesced and two GCs.",
	"Checkpoints: after cycle 0 (no restart yet; a control that measurement itself is stable) and after cycles 10, 20, 30 of a 30-cycle arm, 0 and 10 of the combined arm; plus the last cycle of a reduced arm when it is not one of those, so a state after a restart is always evaluated.",
	"Every checkpoint is taken between cycles after drain, quiescence of worker and insert clients (River jobs finished, no request or held transaction in flight, delivery observer queue empty, idle HTTP connections closed), a 250 ms settle, then two GCs.",
	"A violating snapshot is re-taken after 250 ms up to 8 times (transient closing sockets, River's own 5 ms fetch poll); a persistent leak fails on the last attempt, a clean one never waits.",
	"Labels: 'passed' only for a frozen-scale run with every required checkpoint and the teardown check; 'partially evaluated (reduced)' for any reduced run (cycles, windows, race instrumentation, GOMAXPROCS); 'not evaluated' when no baseline was taken or the report has no section 10 field.",
}

type s10Arm struct {
	a                   *fullApp
	t                   *testing.T
	rep                 *s10ArmReport
	frozen, frozenScale bool
	floor               int64
}

func newS10Arm(t *testing.T, a *fullApp, arm string, planned int, frozenScale bool) *s10Arm {
	frozen := frozenCyclesPerKind
	if arm == "combined" {
		frozen = frozenCombinedCycles
	}
	x := &s10Arm{a: a, t: t, frozenScale: frozenScale, rep: &s10ArmReport{Arm: arm, Planned: planned, Required: s10RequiredCycles(frozen), Scheduled: s10CheckpointCycles(planned),
		Declared: s10DeclaredNow(), Failed: []string{}, CostMs: map[string]float64{}}}
	x.rep.Label = s10NotEvaluated
	return x
}

func (x *s10Arm) frozenCycles() int {
	if x.rep.Arm == "combined" {
		return frozenCombinedCycles
	}
	return frozenCyclesPerKind
}

func (x *s10Arm) status() s10ArmStatus {
	return s10ArmStatus{BaselineTaken: x.rep.Baseline != nil, Planned: x.rep.Planned, Frozen: x.frozenCycles(), FrozenScale: x.frozenScale, Taken: x.rep.Taken, TeardownDone: x.rep.Teardown != nil, Failed: len(x.rep.Failed) > 0}
}

func (x *s10Arm) relabel() { x.rep.Label = s10ArmLabel(x.status()) }

func (x *s10Arm) fail(name string, b boundResult) {
	x.rep.Failed = append(x.rep.Failed, name+": "+b.Name)
	x.t.Errorf("%s section 10 bound %s missed at %s (limit %s, observed %s)", x.rep.Arm, b.Name, name, b.Limit, b.Observed)
}

// take quiesces and takes a snapshot, re-taking a violating one up to
// s10MaxAttempts times, and returns it with its findings.
func (x *s10Arm) take(ctx context.Context, name string, cycle int, mode string) (*s10Snapshot, []s10Finding) {
	t0 := time.Now()
	why := x.a.quiesce(ctx, x.floor)
	if floor, err := x.a.idFloor(ctx); err == nil && why == "" {
		x.floor = floor
	}
	x.a.resetTally()
	var snap *s10Snapshot
	var findings, first []s10Finding
	for attempt := 1; ; attempt++ {
		var err error
		snap, err = x.a.s10Collect(ctx, name, cycle, mode != "teardown")
		if err != nil {
			snap = &s10Snapshot{Name: name, Cycle: cycle}
			findings = []s10Finding{{bQuiescence, "section 10 collection failed: " + err.Error()}}
			break
		}
		snap.Attempts, snap.Quiescence = attempt, why
		findings = s10Evaluate(mode, x.rep.Harness, x.rep.Baseline, snap, x.priorCheckpoints(snap))
		if attempt == 1 {
			first = findings
		}
		if len(findings) == 0 || ctx.Err() != nil {
			break
		}
		// A disposed object is also kept alive for a while by library timers and
		// contexts (River's job timeout is 10 s). A finding that is only that is
		// re-taken until s10RetainPatience; every other finding stops at s10MaxAttempts.
		if attempt >= s10MaxAttempts && !(onlyRetention(findings) && time.Since(t0) < s10RetainPatience) {
			break
		}
		time.Sleep(s10Settle)
	}
	snap.Attempts = max(snap.Attempts, 1)
	if snap.Attempts > 1 { // what was still settling on the first sample, for the record
		for i, f := range first {
			if i == 6 {
				break
			}
			snap.FirstAttempt = append(snap.FirstAttempt, f.Bound+": "+f.Detail[:min(len(f.Detail), 300)])
		}
	}
	snap.CostMs = float64(time.Since(t0)) / float64(time.Millisecond)
	snap.Findings = findings
	x.rep.CostMs[name] = snap.CostMs
	return snap, findings
}

func (x *s10Arm) priorCheckpoints(cur *s10Snapshot) []*s10Snapshot {
	var out []*s10Snapshot
	for _, c := range x.rep.Checkpoint {
		if c.Cycle < cur.Cycle {
			out = append(out, c)
		}
	}
	return out
}

func printS10(tag string, v any) {
	line, _ := json.Marshal(v)
	fmt.Printf("S0WORKERS_RESTART_FULL_S10_%s %s\n", tag, line)
}

// harness takes the pre-application harness baseline (fixture up, application not yet started).
func (x *s10Arm) harness(ctx context.Context) {
	var prev *s10Snapshot
	for i := 0; i < s10MaxAttempts; i++ {
		time.Sleep(s10Settle)
		s, err := x.a.s10Collect(ctx, "harness_baseline", 0, false)
		if err != nil {
			x.t.Fatalf("section 10 harness baseline failed: %v", err)
		}
		// stable once two consecutive snapshots agree on goroutines and descriptors
		if prev != nil && prev.GoroutineTotal == s.GoroutineTotal && prev.FDTotal == s.FDTotal {
			x.rep.Harness = s
			return
		}
		prev = s
	}
	x.rep.Harness = prev
}

// baseline takes the warmed baseline (application up, warm traffic run) and checks
// it against the declared background set.
func (x *s10Arm) baseline(ctx context.Context) {
	printS10("DECLARED", map[string]any{"arm": x.rep.Arm, "declared": x.rep.Declared, "rules": s10Rules})
	snap, findings := x.take(ctx, "warmed_baseline", 0, "baseline")
	x.rep.Baseline = snap
	printS10("BASELINE", map[string]any{"arm": x.rep.Arm, "snapshot": snap})
	for _, b := range s10Bounds(findings, s10BaselineBounds) {
		if !b.Pass {
			x.fail("warmed_baseline", b)
		}
	}
	x.relabel()
}

// checkpoint takes a checkpoint after the given cycle and returns the bounds to add to its cycle line.
func (x *s10Arm) checkpoint(ctx context.Context, cycle int) (*s10Snapshot, []boundResult) {
	name := fmt.Sprintf("cycle_%d", cycle)
	snap, findings := x.take(ctx, name, cycle, "checkpoint")
	x.rep.Checkpoint = append(x.rep.Checkpoint, snap)
	x.rep.Taken = append(x.rep.Taken, cycle)
	bounds := s10Bounds(findings, s10CheckpointBounds)
	for _, b := range bounds {
		if !b.Pass {
			x.fail(name, b)
		}
	}
	if cycle == 0 {
		printS10("CHECKPOINT", map[string]any{"arm": x.rep.Arm, "snapshot": snap, "bounds": bounds})
	}
	x.rep.Series = s10Series(x.rep.Checkpoint)
	x.relabel()
	return snap, bounds
}

// teardown stops the application and checks that counts return to the harness baseline.
func (x *s10Arm) teardown() {
	x.a.teardown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	snap, findings := x.take(ctx, "teardown", -1, "teardown")
	x.rep.Teardown = snap
	bounds := s10Bounds(findings, s10TeardownBounds)
	printS10("TEARDOWN", map[string]any{"arm": x.rep.Arm, "snapshot": snap, "bounds": bounds})
	for _, b := range bounds {
		if !b.Pass {
			x.fail("teardown", b)
		}
	}
	x.relabel()
}
