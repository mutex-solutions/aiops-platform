// SPDX-License-Identifier: Apache-2.0
package s0workers

// Section 10 (resource and leak accounting) of docs/design/s0-spike-criteria.md
// applied to the full restart gate. This file is the database-free part:
// goroutine-profile parsing and owner classification, file-descriptor
// classification, regression slope, heap tolerance, the snapshot model and the
// evaluator that turns snapshots into named bounds. The collectors that read the
// live process and the database are in restart_leak_app_test.go.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ---- goroutine owners (fixed, pre-declared set) ----

const (
	ownerRuntime      = "go-runtime"
	ownerHTTPServer   = "net/http-server"
	ownerPgxPool      = "pgx-pool"
	ownerRiver        = "river-client"
	ownerHarness      = "harness-coordinator"
	ownerLoad         = "load-generator"
	ownerUnclassified = "unclassified"
)

// s10Owners is the closed set of owners. Anything else is "unclassified", which
// is a failure unless pre-declared (it is never declared).
var s10Owners = []string{ownerRuntime, ownerHTTPServer, ownerPgxPool, ownerRiver, ownerHarness, ownerLoad}

// Generation roles. A role is the kind of resource a generation counter belongs
// to; the live generation of each role is known to the harness.
const (
	roleCallerPool   = "pool-caller"
	roleWorkerPool   = "pool-worker"
	roleWorkerClient = "river-worker"
	roleInsertClient = "river-insert"
	roleListener     = "http-server"
	roleFixture      = "harness-fixture" // the fixture's own pool (not a generation)
)

type gKey struct {
	Owner, Role string
	Gen         int
	Entry       string
}

// gGroup is a set of goroutines with the same owner, role/generation (pprof
// labels set by the harness when it creates a generation) and entry function.
type gGroup struct {
	Owner string `json:"owner"`
	Role  string `json:"role,omitempty"`
	Gen   int    `json:"gen,omitempty"`
	Entry string `json:"entry"`
	Count int    `json:"count"`
}

func (g gGroup) key() gKey { return gKey{g.Owner, g.Role, g.Gen, g.Entry} }

var fnShort = strings.NewReplacer(
	"github.com/mutex-solutions/aiops-platform/plugins/jobs-river/internal/s0workers", "s0workers",
	"github.com/riverqueue/river", "river",
	"github.com/jackc/pgx/v5", "pgx",
	"github.com/jackc/puddle/v2", "puddle",
)

// loadFuncs are the harness functions that generate or observe load.
var loadFuncs = []string{"(*fullApp).runCycle", "(*fullApp).doRequest", "(*fullApp).once", "runOpenLoop", "(*fullApp).scanner", "(*fullApp).warmTraffic"}

func shortFn(fn string) string {
	if i := strings.LastIndex(fn, "+0x"); i >= 0 {
		fn = fn[:i]
	}
	return fnShort.Replace(fn)
}

func ownerOfFrame(fn string) string {
	switch {
	case strings.HasPrefix(fn, "pgx/"), strings.HasPrefix(fn, "puddle."):
		return ownerPgxPool
	case strings.HasPrefix(fn, "river."), strings.HasPrefix(fn, "river/"):
		return ownerRiver
	case strings.HasPrefix(fn, "net/http.(*persistConn)"), strings.HasPrefix(fn, "net/http.(*Transport)"):
		return ownerLoad
	case strings.HasPrefix(fn, "net/http.(*Server)"), strings.HasPrefix(fn, "net/http.(*conn)"):
		return ownerHTTPServer
	case strings.HasPrefix(fn, "s0workers.(*fullApp).startListener"):
		return ownerHTTPServer // the accept loop (srv.Serve) started by the listener generation
	case strings.HasPrefix(fn, "s0workers."):
		for _, l := range loadFuncs {
			if strings.Contains(fn, l) {
				return ownerLoad
			}
		}
		return ownerHarness
	case strings.HasPrefix(fn, "testing."), strings.HasPrefix(fn, "main.main"), strings.HasPrefix(fn, "runtime/pprof."):
		return ownerHarness
	}
	return ""
}

// classifyFrames classifies one stack, frames listed top first as in a goroutine
// profile. The owner is that of the first frame, walking from the goroutine's
// entry function upward, that belongs to a known owner package: the creator, not
// whatever the goroutine happens to be blocked in. The key is the entry function,
// plus that frame when the entry is a generic wrapper (sync.WaitGroup.Go).
func classifyFrames(frames []string) (owner, key string) {
	if len(frames) == 0 {
		return ownerUnclassified, "(empty stack)"
	}
	short := make([]string, len(frames))
	for i, f := range frames {
		short[i] = shortFn(f)
	}
	entry := short[len(short)-1]
	for i := len(short) - 1; i >= 0; i-- {
		if o := ownerOfFrame(short[i]); o != "" {
			if i == len(short)-1 {
				return o, entry
			}
			return o, entry + " via " + short[i]
		}
	}
	if strings.HasPrefix(entry, "runtime.") || strings.HasPrefix(entry, "runtime/") || strings.HasPrefix(entry, "os/signal.") {
		return ownerRuntime, entry
	}
	return ownerUnclassified, entry
}

// parseGoroutineProfile parses runtime/pprof "goroutine" profile text at debug=1
// ("N @ pcs", optional "# labels: {...}", then "#\t0xpc\tfunc+0xoff\tfile:line").
// Labels s0role/s0gen are set by the harness on every goroutine tree it creates.
func parseGoroutineProfile(text string) ([]gGroup, error) {
	merged := map[gKey]int{}
	total, sum := -1, 0
	for _, block := range strings.Split(text, "\n\n") {
		block = strings.Trim(block, "\n")
		if block == "" {
			continue
		}
		var count int
		var role string
		var gen int
		var frames []string
		for i, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "goroutine profile: total "):
				n, err := strconv.Atoi(strings.TrimPrefix(line, "goroutine profile: total "))
				if err != nil {
					return nil, fmt.Errorf("goroutine profile header: %w", err)
				}
				total = n
			case i == 0 || (count == 0 && strings.Contains(line, " @ ")):
				n, err := strconv.Atoi(strings.Fields(line)[0])
				if err != nil {
					return nil, fmt.Errorf("goroutine profile block header %q", line)
				}
				count = n
			case strings.HasPrefix(line, "# labels: "):
				var labels map[string]string
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "# labels: ")), &labels); err != nil {
					return nil, fmt.Errorf("goroutine profile labels: %w", err)
				}
				role = labels["s0role"]
				gen, _ = strconv.Atoi(labels["s0gen"])
			case strings.HasPrefix(line, "#"):
				f := strings.Fields(strings.TrimPrefix(line, "#"))
				if len(f) >= 2 {
					frames = append(frames, f[1])
				}
			}
		}
		if count == 0 {
			continue // header-only block
		}
		owner, entry := classifyFrames(frames)
		merged[gKey{owner, role, gen, entry}] += count
		sum += count
	}
	if total >= 0 && sum != total {
		return nil, fmt.Errorf("goroutine profile malformed: groups sum to %d, header says %d", sum, total)
	}
	out := make([]gGroup, 0, len(merged))
	for k, n := range merged {
		out = append(out, gGroup{k.Owner, k.Role, k.Gen, k.Entry, n})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Owner != b.Owner {
			return a.Owner < b.Owner
		}
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		if a.Gen != b.Gen {
			return a.Gen < b.Gen
		}
		return a.Entry < b.Entry
	})
	return out, nil
}

// normalizeGoroutines maps the live generation of each role to generation 0
// ("current") so snapshots of different generations compare. A group of a role
// whose generation is not the live one belongs to a disposed generation: it is
// reported as a finding and is not part of the returned map.
func normalizeGoroutines(groups []gGroup, live map[string]int) (map[gKey]int, []string) {
	out := map[gKey]int{}
	var disposed []string
	for _, g := range groups {
		if g.Role != "" && live[g.Role] != g.Gen {
			disposed = append(disposed, fmt.Sprintf("%s#%d (disposed) owns %d goroutine(s) of %s %s", g.Role, g.Gen, g.Count, g.Owner, g.Entry))
			continue
		}
		k := g.key()
		if g.Role != "" {
			k.Gen = 0
		}
		out[k] += g.Count
	}
	return out, disposed
}

// diffGoroutines compares per owner/role/entry. Both directions are findings: a
// missing group never cancels an extra one because every group is its own line.
func diffGoroutines(want, got map[gKey]int) []string {
	keys := map[gKey]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	var out []string
	for k := range keys {
		w, g := want[k], got[k]
		if w == g {
			continue
		}
		what := "count differs"
		switch {
		case w == 0:
			what = "unexpected group (not in the declared/baseline set)"
		case g == 0:
			what = "group missing"
		case g > w:
			what = "leak"
		}
		role := k.Role
		if role == "" {
			role = "-"
		}
		out = append(out, fmt.Sprintf("%s role=%s entry=%s: expected %d, observed %d (%s)", k.Owner, role, k.Entry, w, g, what))
	}
	sort.Strings(out)
	return out
}

func goroutineTotal(groups []gGroup) int {
	n := 0
	for _, g := range groups {
		n += g.Count
	}
	return n
}

func goroutinesByOwner(m map[gKey]int) map[string]int {
	out := map[string]int{}
	for k, n := range m {
		role := k.Role
		if role == "" {
			role = "-"
		}
		out["goroutines:"+k.Owner+"/"+role] += n
	}
	return out
}

// ---- background set declared BEFORE any measurement ----

type declEntry struct {
	Owner string `json:"owner"`
	Entry string `json:"entry"`
	Count int    `json:"count"`
}

// s10Declared is the fixed library/application background set of each live
// generation, enumerated here and printed before the first measurement. A
// goroutine group that is not listed (or listed with another count) is a failure
// at the warmed baseline and at every later checkpoint; the list is never
// extended after a leak is found.
//
// The River list is the background set of one started worker client (v0.51.2, one
// queue, elected leader, cleaner on) as observed on an idle warmed instance; the
// insert-only client starts nothing. TickerWithInitialTick.runLoop is six tickers
// with identical stacks (cleaner, rescuer, scheduler, queue cleaner, periodic
// enqueuer, producer status): they can only be counted together.
var s10Declared = map[string][]declEntry{
	roleCallerPool: {{ownerPgxPool, "pgx/pgxpool.NewWithConfig.func5", 1}}, // backgroundHealthCheck
	roleWorkerPool: {{ownerPgxPool, "pgx/pgxpool.NewWithConfig.func5", 1}},
	roleListener:   {{ownerHTTPServer, "s0workers.(*fullApp).startListener.func1.1", 1}}, // srv.Serve accept loop; connection goroutines must be 0 at quiescence
	roleWorkerClient: {
		{ownerRiver, "river.(*Client[...]).Start.func3", 1},
		{ownerRiver, "river.(*Client[...]).logStatsLoop.func1", 1},
		{ownerRiver, "river.(*producer).StartWorkContext.func4", 1},
		{ownerRiver, "river.(*producer).fetchPollLoop", 1},
		{ownerRiver, "river.(*producer).heartbeatLogLoop", 1},
		{ownerRiver, "river.(*producer).pollForJobCancellations", 1},
		{ownerRiver, "river.(*producer).pollForSettingChanges", 1},
		{ownerRiver, "river.(*producer).reportProducerStatusLoop", 1},
		{ownerRiver, "river.(*producer).reportQueueStatusLoop", 1},
		{ownerRiver, "river.(*subscriptionManager).Start.func1", 1},
		{ownerRiver, "river/internal/jobcompleter.(*BatchCompleter).Start.func1", 1},
		{ownerRiver, "river/internal/leadership.(*Elector).Start.func2", 1},
		{ownerRiver, "river/internal/leadership.(*subscriptionRelay).run", 1},
		{ownerRiver, "river/internal/maintenance.(*JobCleaner).Start.func1", 1},
		{ownerRiver, "river/internal/maintenance.(*JobRescuer).Start.func1", 1},
		{ownerRiver, "river/internal/maintenance.(*JobScheduler).Start.func1", 1},
		{ownerRiver, "river/internal/maintenance.(*PeriodicJobEnqueuer).Start.func2", 1},
		{ownerRiver, "river/internal/maintenance.(*PeriodicJobEnqueuer).periodicJobKeepAliveAndReapPeriodically.func1", 1},
		{ownerRiver, "river/internal/maintenance.(*QueueCleaner).Start.func1", 1},
		{ownerRiver, "river/internal/maintenance.(*QueueMaintainer).Start.func1", 1},
		{ownerRiver, "river/internal/maintenance.(*QueueMaintainerLeader).Start.func1", 1},
		{ownerRiver, "river/internal/maintenance.(*Reindexer).Start.func1", 1},
		{ownerRiver, "river/internal/notifier.(*Notifier).Start.func1", 1},
		{ownerRiver, "river/internal/notifier.(*Notifier).waitOnce.func3", 1},
		{ownerRiver, "river/rivershared/util/timeutil.(*TickerWithInitialTick).runLoop", 6},
		{ownerRiver, "sync.(*WaitGroup).Go.func1 via river/internal/notifier.(*Notifier).Start.func1.3", 1},
	},
}

// s10DeclaredHarness is added to the pre-application harness baseline while the
// application is up (unlabelled goroutines the application itself owns).
var s10DeclaredHarness = []declEntry{{ownerHarness, "s0workers.(*fullApp).drain", 1}}

// s10SessionAllowance is the number of server sessions a pool generation owns
// beyond the connections the pool itself counts (River's LISTEN connection is
// hijacked from the worker pool).
var s10SessionAllowance = map[string]int{roleWorkerPool: 1}

func expectedGoroutines(harness map[gKey]int, live map[string]int) map[gKey]int {
	out := map[gKey]int{}
	for k, n := range harness {
		out[k] = n
	}
	if len(live) > 0 {
		for _, e := range s10DeclaredHarness {
			out[gKey{e.Owner, "", 0, e.Entry}] += e.Count
		}
	}
	for role := range live {
		for _, e := range s10Declared[role] {
			out[gKey{e.Owner, role, 0, e.Entry}] += e.Count
		}
	}
	return out
}

// ---- file descriptors ----

type procSock struct {
	Local, Remote int
	Listen        bool
}

// parseProcNetTCP parses /proc/net/tcp or tcp6 into inode -> endpoints.
func parseProcNetTCP(text string) map[uint64]procSock {
	out := map[uint64]procSock{}
	port := func(addr string) int {
		i := strings.LastIndex(addr, ":")
		if i < 0 {
			return -1
		}
		p, err := strconv.ParseUint(addr[i+1:], 16, 32)
		if err != nil {
			return -1
		}
		return int(p)
	}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[0] == "sl" {
			continue
		}
		inode, err := strconv.ParseUint(f[9], 10, 64)
		if err != nil || inode == 0 {
			continue
		}
		out[inode] = procSock{Local: port(f[1]), Remote: port(f[2]), Listen: f[3] == "0A"}
	}
	return out
}

type fdEntry struct {
	Class     string
	LocalPort int
}

// classifyFDs classifies /proc/self/fd link targets. Sockets are classified from
// the TCP tables: the application listener, accepted/dialled HTTP connections on
// that listener, connections to PostgreSQL, any other listening socket.
func classifyFDs(links []string, socks map[uint64]procSock, pgPort, listenPort int) []fdEntry {
	out := make([]fdEntry, 0, len(links))
	for _, l := range links {
		switch {
		case strings.HasPrefix(l, "socket:["):
			inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]"), 10, 64)
			s, ok := socks[inode]
			switch {
			case err != nil || !ok:
				out = append(out, fdEntry{Class: "socket-other"})
			case s.Listen && s.Local == listenPort:
				out = append(out, fdEntry{Class: "listener-socket", LocalPort: s.Local})
			case s.Listen:
				out = append(out, fdEntry{Class: "listen-socket-unexpected", LocalPort: s.Local})
			case s.Remote == pgPort:
				out = append(out, fdEntry{Class: "pg-socket", LocalPort: s.Local})
			case s.Local == listenPort:
				out = append(out, fdEntry{Class: "http-server-conn", LocalPort: s.Local})
			case s.Remote == listenPort:
				out = append(out, fdEntry{Class: "http-client-conn", LocalPort: s.Local})
			default:
				out = append(out, fdEntry{Class: "tcp-other", LocalPort: s.Local})
			}
		case l == "anon_inode:[eventpoll]":
			out = append(out, fdEntry{Class: "epoll"})
		case strings.HasPrefix(l, "pipe:["):
			out = append(out, fdEntry{Class: "pipe"})
		case strings.HasPrefix(l, "anon_inode:"):
			out = append(out, fdEntry{Class: "anon-inode"})
		case strings.HasPrefix(l, "/"):
			out = append(out, fdEntry{Class: "file"})
		default:
			out = append(out, fdEntry{Class: "other"})
		}
	}
	return out
}

// fdCounts counts descriptors per class. PostgreSQL sockets are NOT attributed to
// a pool generation: the server sees the client port after any NAT/proxy in front
// of it (docker-proxy in the local fixture), so a port join would be wrong. The
// per-generation evidence is the server session by application_name; sockets are
// tied to it in aggregate (sockets == sessions of the fixture login + admin pool).
func fdCounts(entries []fdEntry) map[string]int {
	out := map[string]int{}
	for _, e := range entries {
		out[e.Class]++
	}
	return out
}

// ---- application names (generation attribution on the server) ----

func s10AppName(role string, gen int) string { return fmt.Sprintf("s0wk-%s-%d", role, gen) }

// parseAppName returns the session owner key: "role#gen" for a generation,
// "harness-fixture" for the fixture's own pool, "unattributed" otherwise.
func parseAppName(name string) string {
	if name == "s0workers" {
		return roleFixture
	}
	if rest, ok := strings.CutPrefix(name, "s0wk-"); ok {
		if i := strings.LastIndex(rest, "-"); i > 0 {
			if g, err := strconv.Atoi(rest[i+1:]); err == nil {
				return rest[:i] + "#" + strconv.Itoa(g)
			}
		}
	}
	return "unattributed"
}

// splitOwner splits "name@role#gen" into the name part and (role, gen). ok is
// false for keys that carry no generation.
func splitOwner(key string) (prefix, role string, gen int, ok bool) {
	at := strings.LastIndex(key, "@")
	if at < 0 {
		return key, "", 0, false
	}
	prefix, suffix := key[:at], key[at+1:]
	h := strings.LastIndex(suffix, "#")
	if h < 0 {
		return prefix, suffix, 0, false
	}
	g, err := strconv.Atoi(suffix[h+1:])
	if err != nil {
		return prefix, suffix, 0, false
	}
	return prefix, suffix[:h], g, true
}

// ---- numbers ----

// slope is the least-squares slope of ys over xs; 0 for fewer than two distinct x.
func slope(xs, ys []float64) float64 {
	n := float64(len(xs))
	if len(xs) < 2 || len(xs) != len(ys) {
		return 0
	}
	var sx, sy, sxx, sxy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

const heapFloor = 1 << 20

// heapTolerance is max(1 MiB, 5% of the warmed baseline), rounded down.
func heapTolerance(base uint64) uint64 { return max(heapFloor, base*5/100) }

// heapExceeds is true when cur is more than the tolerance above the baseline.
func heapExceeds(base, cur uint64) bool { return cur > base && cur-base > heapTolerance(base) }

// ---- snapshots ----

type s10Pool struct {
	Role         string `json:"role"`
	Gen          int    `json:"gen"`
	Max          int    `json:"max"`
	Acquired     int    `json:"acquired"`
	Idle         int    `json:"idle"`
	Total        int    `json:"total"`
	Constructing int    `json:"constructing"`
}

type s10Sess struct {
	Active      int `json:"active"`
	ActiveStale int `json:"active_over_2s"`
	Idle        int `json:"idle"`
	IdleInTx    int `json:"idle_in_transaction"`
	Other       int `json:"other"`
}

func (s s10Sess) total() int { return s.Active + s.Idle + s.IdleInTx + s.Other }

type s10Snapshot struct {
	Name           string             `json:"name"`
	Cycle          int                `json:"cycle"`
	Live           map[string]int     `json:"live_generations"`
	Goroutines     []gGroup           `json:"goroutines"`
	GoroutineTotal int                `json:"goroutines_total"`
	FDs            map[string]int     `json:"fds_by_class"`
	FDTotal        int                `json:"fds_total"`
	FDError        string             `json:"fd_error,omitempty"`
	Pools          []s10Pool          `json:"pools"`
	HarnessPools   map[string]int     `json:"harness_pool_total_conns"`
	Sessions       map[string]s10Sess `json:"server_sessions_by_owner"`
	Entries        map[string]int     `json:"entries"`
	Retained       []string           `json:"retained_disposed_objects"`
	PinnedClients  int                `json:"disposed_clients_pinned_by_live_pool_connections"`
	HeapAlloc      uint64             `json:"heap_alloc_bytes"`
	HeapObjects    uint64             `json:"heap_objects"`
	Quiescence     string             `json:"quiescence_failure,omitempty"`
	Attempts       int                `json:"attempts"`
	FirstAttempt   []string           `json:"first_attempt_findings,omitempty"`
	CostMs         float64            `json:"cost_ms"`
	Findings       []s10Finding       `json:"findings,omitempty"`
}

// ---- evaluation ----

const (
	bQuiescence = "s10_quiescence_before_measurement"
	bGoroutines = "s10_goroutines_equal_warmed_baseline_per_owner"
	bDisposed   = "s10_disposed_generations_own_zero"
	bConns      = "s10_connections_accounted_per_owner_generation"
	bOpenTx     = "s10_no_open_transaction_or_stuck_statement"
	bFDs        = "s10_fds_equal_warmed_baseline"
	bEntries    = "s10_entries_equal_warmed_baseline"
	bHeap       = "s10_retained_heap_within_tolerance"
	bGrowth     = "s10_no_cycle_to_cycle_growth"
	bDeclared   = "s10_warmed_baseline_matches_declared_background_set"
	bTeardown   = "s10_teardown_returns_to_harness_baseline"
	bTeardownFD = "s10_teardown_no_fd_growth"
)

var s10BaselineBounds = []string{bQuiescence, bDeclared, bDisposed, bConns, bOpenTx}
var s10CheckpointBounds = []string{bQuiescence, bGoroutines, bDisposed, bConns, bOpenTx, bFDs, bEntries, bHeap, bGrowth}
var s10TeardownBounds = []string{bQuiescence, bTeardown, bTeardownFD, bDisposed, bConns, bOpenTx}

type s10Finding struct {
	Bound  string `json:"bound"`
	Detail string `json:"detail"`
}

func isPG(k string) bool { return k == "pg-socket" }

// normMap replaces the live generation of each role in "name@role#gen" keys by
// "#cur"; entries of other generations are returned as disposed findings.
func normMap(m map[string]int, live map[string]int) (map[string]int, []string) {
	out := map[string]int{}
	var disposed []string
	for k, n := range m {
		prefix, role, gen, ok := splitOwner(k)
		if !ok {
			out[k] += n
			continue
		}
		if live[role] != gen {
			if n > 0 {
				disposed = append(disposed, fmt.Sprintf("%s (disposed) owns %d of %s", role+"#"+strconv.Itoa(gen), n, prefix))
			}
			continue
		}
		out[prefix+"@"+role+"#cur"] += n
	}
	return out, disposed
}

func diffCounts(label string, want, got map[string]int) []string {
	var out []string
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	for k := range keys {
		if want[k] != got[k] {
			out = append(out, fmt.Sprintf("%s %s: expected %d, observed %d", label, k, want[k], got[k]))
		}
	}
	sort.Strings(out)
	return out
}

// s10DeclaredEntries is the declared registry/entry set of a live generation.
func s10DeclaredEntries(live map[string]int) map[string]int {
	out := map[string]int{}
	add := func(name, role string) {
		if g, ok := live[role]; ok {
			out[name+"@"+role+"#"+strconv.Itoa(g)] = 1
		}
	}
	add("pool", roleCallerPool)
	add("pool", roleWorkerPool)
	add("client", roleWorkerClient)
	add("job-handler", roleWorkerClient)
	add("client", roleInsertClient)
	add("listener", roleListener)
	add("route", roleListener)
	return out
}

func nonPG(fds map[string]int) (map[string]int, int) {
	out, total := map[string]int{}, 0
	for k, n := range fds {
		if !isPG(k) {
			out[k] = n
			total += n
		}
	}
	return out, total
}

// exactSeries are the countable resources that must equal the warmed baseline
// and never grow. Pool idle/total connections and the PostgreSQL sockets and
// sessions they own are demand driven: they are accounted by exact relations in
// s10Evaluate and reported as informational series, never compared to the
// baseline value.
func exactSeries(s *s10Snapshot) map[string]float64 {
	out := map[string]float64{}
	norm, _ := normalizeGoroutines(s.Goroutines, s.Live)
	for k, n := range goroutinesByOwner(norm) {
		out[k] = float64(n)
	}
	out["goroutines:total"] = float64(goroutineTotal(s.Goroutines))
	fds, total := nonPG(s.FDs)
	for k, n := range fds {
		out["fd:"+k] = float64(n)
	}
	out["fd:non-pg-total"] = float64(total)
	ent, _ := normMap(s.Entries, s.Live)
	for k, n := range ent {
		out["entries:"+k] = float64(n)
	}
	var acq, cons, itx, stale, retained int
	for _, p := range s.Pools {
		acq += p.Acquired
		cons += p.Constructing
	}
	for _, ss := range s.Sessions {
		itx += ss.IdleInTx
		stale += ss.ActiveStale
	}
	retained = len(s.Retained)
	out["pool:acquired"], out["pool:constructing"] = float64(acq), float64(cons)
	out["sessions:idle_in_transaction"], out["sessions:active_over_2s"] = float64(itx), float64(stale)
	out["retained_disposed_objects"] = float64(retained)
	return out
}

// infoSeries are reported with their slope but not bounded by equality.
func infoSeries(s *s10Snapshot) map[string]float64 {
	out := map[string]float64{"fd:total": float64(s.FDTotal), "heap_alloc_bytes": float64(s.HeapAlloc), "heap_objects": float64(s.HeapObjects), "disposed_clients_pinned_by_pool_connections": float64(s.PinnedClients)}
	pg := map[string]int{}
	for k, n := range s.FDs {
		if isPG(k) {
			pg[k] = n
		}
	}
	pgn, _ := normMap(pg, s.Live)
	for k, n := range pgn {
		out["fd:"+k] = float64(n)
	}
	for _, p := range s.Pools {
		out[fmt.Sprintf("pool_total_conns:%s", p.Role)] = float64(p.Total)
		out[fmt.Sprintf("pool_idle_conns:%s", p.Role)] = float64(p.Idle)
	}
	for k, ss := range s.Sessions {
		name := k
		if _, role, gen, ok := splitOwner("x@" + k); ok {
			if s.Live[role] != gen {
				continue
			}
			name = role + "#cur"
		}
		out["sessions_total:"+name] = float64(ss.total())
	}
	return out
}

// s10Evaluate checks one snapshot. mode is "baseline" (against the declared
// set and the harness baseline), "checkpoint" (against the warmed baseline) or
// "teardown" (against the harness baseline; nothing of the application may remain).
func s10Evaluate(mode string, harness, base, cur *s10Snapshot, prior []*s10Snapshot) []s10Finding {
	var out []s10Finding
	add := func(bound string, details []string) {
		for _, d := range details {
			out = append(out, s10Finding{bound, d})
		}
	}
	if cur.Quiescence != "" {
		add(bQuiescence, []string{cur.Quiescence})
	}
	if cur.FDError != "" {
		add(bFDs, []string{"file descriptor accounting unavailable: " + cur.FDError})
	}

	// Goroutines: per owner/role/entry against declared set (+harness) or baseline.
	gnorm, gdisposed := normalizeGoroutines(cur.Goroutines, cur.Live)
	add(bDisposed, gdisposed)
	var unclassified []string
	for k, n := range gnorm {
		if k.Owner == ownerUnclassified {
			unclassified = append(unclassified, fmt.Sprintf("unclassified goroutine group (not pre-declared): %d at %s", n, k.Entry))
		}
	}
	sort.Strings(unclassified)
	gbound := bGoroutines
	var want map[gKey]int
	switch mode {
	case "baseline":
		hn, _ := normalizeGoroutines(harness.Goroutines, nil)
		want, gbound = expectedGoroutines(hn, cur.Live), bDeclared
	case "teardown":
		hn, _ := normalizeGoroutines(harness.Goroutines, nil)
		want, gbound = expectedGoroutines(hn, nil), bTeardown
	default:
		want, _ = normalizeGoroutines(base.Goroutines, base.Live)
	}
	add(gbound, unclassified)
	add(gbound, diffGoroutines(want, gnorm))

	// Disposed generations own zero of everything else, by key.
	for k, n := range cur.FDs {
		if _, role, gen, ok := splitOwner(k); ok && cur.Live[role] != gen && n > 0 {
			add(bDisposed, []string{fmt.Sprintf("%s#%d (disposed) owns %d file descriptor(s) %s", role, gen, n, k)})
		}
	}
	_, edisposed := normMap(cur.Entries, cur.Live)
	add(bDisposed, edisposed)
	for k, ss := range cur.Sessions {
		if _, role, gen, ok := splitOwner("x@" + k); ok && cur.Live[role] != gen && ss.total() > 0 {
			add(bDisposed, []string{fmt.Sprintf("%s (disposed) owns %d server session(s) of the fixture login (%d idle in transaction)", k, ss.total(), ss.IdleInTx)})
		}
	}
	for _, r := range cur.Retained {
		add(bDisposed, []string{r + " (disposed) object still reachable after two GCs"})
	}
	// Each pooled connection can pin at most one stopped client through the last
	// query context it kept (pgconn); more pinned clients than connections is a leak.
	pinCap := 0
	for _, p := range cur.Pools {
		if p.Role == roleWorkerPool {
			pinCap = p.Total
		}
	}
	if cur.PinnedClients > pinCap {
		add(bDisposed, []string{fmt.Sprintf("%d disposed River clients still reachable, more than the %d connections of the live worker pool that can each pin one through their last query context", cur.PinnedClients, pinCap)})
	}

	// Connections: acquired/constructing equal baseline, sessions and sockets
	// account for every pool connection (+ declared allowance), never above max.
	baseAcq, baseCons := map[string]int{}, map[string]int{}
	if base != nil {
		for _, p := range base.Pools {
			baseAcq[p.Role], baseCons[p.Role] = p.Acquired, p.Constructing
		}
	}
	for _, p := range cur.Pools {
		id := fmt.Sprintf("%s#%d", p.Role, p.Gen)
		if p.Acquired != baseAcq[p.Role] {
			add(bConns, []string{fmt.Sprintf("%s acquired connections: expected %d, observed %d", id, baseAcq[p.Role], p.Acquired)})
		}
		if p.Constructing != baseCons[p.Role] {
			add(bConns, []string{fmt.Sprintf("%s constructing connections: expected %d, observed %d", id, baseCons[p.Role], p.Constructing)})
		}
		if p.Total > p.Max {
			add(bConns, []string{fmt.Sprintf("%s total connections %d above max %d", id, p.Total, p.Max)})
		}
		wantSess := p.Total + s10SessionAllowance[p.Role]
		if got := cur.Sessions[id].total(); got != wantSess {
			add(bConns, []string{fmt.Sprintf("%s server sessions: expected %d (pool total %d + allowance %d), observed %d", id, wantSess, p.Total, s10SessionAllowance[p.Role], got)})
		}
	}
	if got, want := cur.Sessions[roleFixture].total(), cur.HarnessPools["fixture-caller"]; got != want {
		add(bConns, []string{fmt.Sprintf("harness fixture pool server sessions: expected %d, observed %d", want, got)})
	}
	allSessions := 0
	for _, ss := range cur.Sessions {
		allSessions += ss.total()
	}
	if got, want := cur.FDs["pg-socket"], allSessions+cur.HarnessPools["admin"]; got != want {
		add(bConns, []string{fmt.Sprintf("PostgreSQL sockets %d != server sessions of the fixture login %d + harness admin pool connections %d", got, allSessions, cur.HarnessPools["admin"])})
	}
	if n := cur.Sessions["unattributed"].total(); n != 0 {
		add(bConns, []string{fmt.Sprintf("%d server session(s) of the fixture login with an unknown application_name", n)})
	}

	// Open transactions and stuck statements, any owner.
	keys := make([]string, 0, len(cur.Sessions))
	for k := range cur.Sessions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ss := cur.Sessions[k]
		if ss.IdleInTx > 0 {
			add(bOpenTx, []string{fmt.Sprintf("%s has %d session(s) idle in transaction", k, ss.IdleInTx)})
		}
		if ss.ActiveStale > 0 {
			add(bOpenTx, []string{fmt.Sprintf("%s has %d statement(s) active for over 2 s at quiescence", k, ss.ActiveStale)})
		}
	}

	// File descriptors other than demand-driven PostgreSQL sockets.
	curFD, curTotal := nonPG(cur.FDs)
	hFD, hTotal := nonPG(harness.FDs)
	switch mode {
	case "baseline":
		wantFD := map[string]int{}
		for k, n := range hFD {
			wantFD[k] = n
		}
		wantFD["listener-socket"]++
		add(bDeclared, diffCounts("fd class", wantFD, curFD))
	case "teardown":
		add(bTeardown, diffCounts("fd class", hFD, curFD))
		if curTotal > hTotal {
			add(bTeardownFD, []string{fmt.Sprintf("non-PostgreSQL file descriptors %d exceed the harness baseline %d", curTotal, hTotal)})
		}
	default:
		bFD, _ := nonPG(base.FDs)
		add(bFDs, diffCounts("fd class", bFD, curFD))
	}

	// Entries.
	enorm, _ := normMap(cur.Entries, cur.Live)
	switch mode {
	case "baseline":
		wantE := map[string]int{}
		for k, n := range s10DeclaredEntries(cur.Live) {
			prefix, role, _, _ := splitOwner(k)
			wantE[prefix+"@"+role+"#cur"] = n
		}
		add(bDeclared, diffCounts("entry", wantE, enorm))
	case "teardown":
		add(bTeardown, diffCounts("entry", map[string]int{}, enorm))
	default:
		bn, _ := normMap(base.Entries, base.Live)
		add(bEntries, diffCounts("entry", bn, enorm))
	}

	if mode == "checkpoint" {
		if heapExceeds(base.HeapAlloc, cur.HeapAlloc) {
			add(bHeap, []string{fmt.Sprintf("retained heap %d B exceeds warmed baseline %d B by more than %d B (max(1 MiB, 5%%))", cur.HeapAlloc, base.HeapAlloc, heapTolerance(base.HeapAlloc))})
		}
		// Growth in any exact series, against the previous checkpoint and the baseline.
		prev := base
		if len(prior) > 0 {
			prev = prior[len(prior)-1]
		}
		cs, ps, bs := exactSeries(cur), exactSeries(prev), exactSeries(base)
		names := make([]string, 0, len(cs))
		for k := range cs {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			if cs[k] > ps[k] || cs[k] > bs[k] {
				add(bGrowth, []string{fmt.Sprintf("%s grew: baseline %.0f, previous checkpoint %.0f, now %.0f", k, bs[k], ps[k], cs[k])})
			}
		}
	}
	return out
}

// s10Bounds turns findings into one named bound per name (limit/observed/pass).
func s10Bounds(findings []s10Finding, names []string) []boundResult {
	out := make([]boundResult, 0, len(names))
	for _, n := range names {
		var d []string
		for _, f := range findings {
			if f.Bound == n {
				d = append(d, f.Detail)
			}
		}
		obs := "0 findings"
		if len(d) > 0 {
			obs = strings.Join(d, "; ")
			if len(obs) > 1500 {
				obs = obs[:1500] + "..."
			}
		}
		out = append(out, boundResult{Name: n, Limit: s10Limits[n], Observed: obs, Pass: len(d) == 0})
	}
	return out
}

var s10Limits = map[string]string{
	bQuiescence: "in-flight work drained and worker/insert clients quiescent before measuring",
	bGoroutines: "per owner/role/entry: exactly the warmed baseline, nothing unclassified",
	bDisposed:   "disposed generations own exactly 0 goroutines, sockets, sessions, entries, retained objects",
	bConns:      "acquired and constructing equal baseline; sessions per generation == pool total + declared allowance <= max; sockets == sessions + admin pool",
	bOpenTx:     "0 sessions idle in transaction, 0 statements active over 2 s",
	bFDs:        "every non-PostgreSQL descriptor class equals the warmed baseline",
	bEntries:    "registry/route/listener/job-handler/client/pool entries equal the warmed baseline",
	bHeap:       "heap after two GCs <= warmed baseline + max(1 MiB, 5%)",
	bGrowth:     "no exact series above the previous checkpoint or the baseline",
	bDeclared:   "baseline goroutines/descriptors/entries equal the background set declared before measurement",
	bTeardown:   "after full teardown goroutines, descriptors and entries equal the pre-application harness baseline",
	bTeardownFD: "after full teardown no more non-PostgreSQL descriptors than the harness baseline",
}

// ---- checkpoint schedule, series and labels ----

// s10CheckpointCycles are the cycles after which a checkpoint is taken: the
// cycles 0/10/20/30 that exist in an arm of `planned` cycles, plus the last cycle
// when it is not one of them (so a reduced arm still evaluates a state after a
// restart).
func s10CheckpointCycles(planned int) []int {
	out := []int{0}
	for _, c := range []int{10, 20, 30} {
		if c <= planned {
			out = append(out, c)
		}
	}
	if planned > 0 && out[len(out)-1] != planned {
		out = append(out, planned)
	}
	return out
}

// s10RequiredCycles are the checkpoints section 10 requires of an arm whose
// frozen length is `frozen` cycles (30 for pool/client/listener, 10 combined).
func s10RequiredCycles(frozen int) []int {
	out := []int{0}
	for _, c := range []int{10, 20, 30} {
		if c <= frozen {
			out = append(out, c)
		}
	}
	return out
}

type s10SeriesStat struct {
	Cycles []int     `json:"cycles"`
	Values []float64 `json:"values"`
	Slope  float64   `json:"slope_per_cycle"`
	Exact  bool      `json:"must_not_grow"`
	Grew   bool      `json:"grew"`
}

// s10Series builds the checkpoint series with a least-squares slope each.
func s10Series(cps []*s10Snapshot) map[string]s10SeriesStat {
	out := map[string]s10SeriesStat{}
	names := map[string]bool{}
	ex := make([]map[string]float64, len(cps))
	in := make([]map[string]float64, len(cps))
	for i, c := range cps {
		ex[i], in[i] = exactSeries(c), infoSeries(c)
		for k := range ex[i] {
			names[k] = true
		}
		for k := range in[i] {
			names[k] = true
		}
	}
	for k := range names {
		st := s10SeriesStat{}
		var xs []float64
		for i, c := range cps {
			v, isExact := ex[i][k]
			if !isExact {
				v = in[i][k]
			}
			st.Exact = st.Exact || isExact
			st.Cycles = append(st.Cycles, c.Cycle)
			st.Values = append(st.Values, v)
			xs = append(xs, float64(c.Cycle))
			if len(st.Values) > 1 && v > st.Values[len(st.Values)-2] {
				st.Grew = st.Grew || isExact
			}
		}
		st.Slope = slope(xs, st.Values)
		out[k] = st
	}
	return out
}

const (
	s10NotEvaluated = "not evaluated"
	s10Reduced      = "partially evaluated (reduced)"
	s10Incomplete   = "partially evaluated (incomplete)"
	s10Failed       = "FAILED"
	s10Passed       = "passed"
)

type s10ArmStatus struct {
	BaselineTaken bool
	Planned       int   // cycles planned in this arm
	Frozen        int   // frozen cycles of this arm (30, or 10 for combined)
	FrozenScale   bool  // the whole run is a frozen-scale run (full windows, no race, GOMAXPROCS=8)
	Taken         []int // checkpoint cycles taken
	TeardownDone  bool
	Failed        bool
}

// s10ArmLabel never reports "passed" for a reduced run, and reports "not
// evaluated" when no baseline was taken.
func s10ArmLabel(st s10ArmStatus) string {
	switch {
	case !st.BaselineTaken:
		return s10NotEvaluated
	case st.Failed:
		return s10Failed
	}
	have := map[int]bool{}
	for _, c := range st.Taken {
		have[c] = true
	}
	complete := st.TeardownDone
	for _, c := range s10RequiredCycles(st.Frozen) {
		complete = complete && have[c]
	}
	switch {
	case complete && st.FrozenScale && st.Planned == st.Frozen:
		return s10Passed
	case st.FrozenScale && st.Planned == st.Frozen:
		return s10Incomplete
	}
	return s10Reduced
}

// s10OverallLabel combines the four arms. Missing arms count as not evaluated.
func s10OverallLabel(labels []string, wantArms int) string {
	rank := map[string]int{s10Passed: 0, s10NotEvaluated: 1, s10Incomplete: 2, s10Reduced: 3, s10Failed: 4}
	evaluated, worst := 0, s10Passed
	for _, l := range labels {
		if l != s10NotEvaluated {
			evaluated++
		}
		if rank[l] > rank[worst] {
			worst = l
		}
	}
	switch {
	case worst == s10Failed || worst == s10Reduced:
		return worst
	case evaluated == 0:
		return s10NotEvaluated
	case len(labels) < wantArms || worst != s10Passed:
		return s10Incomplete
	}
	return s10Passed
}

// s10LabelFromReport reads the section 10 label from a final report line; a
// report without the field (older binary) is "not evaluated" and can never be
// cited as passing section 10.
func s10LabelFromReport(line []byte) string {
	var r struct {
		Label string `json:"section10_label"`
	}
	if err := json.Unmarshal(line, &r); err != nil || r.Label == "" {
		return s10NotEvaluated
	}
	return r.Label
}
