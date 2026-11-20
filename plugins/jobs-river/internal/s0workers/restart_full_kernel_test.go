// SPDX-License-Identifier: Apache-2.0
package s0workers

// The kernel-owned variant of the full restart gate (S0WORKERS_RESTART_OWNER=kernel).
// docs/design/s0-spike-criteria.md section 6, last paragraph: "After the final kernel
// exists, rerun this probe with resources owned by its real effects and actual
// lease/quiesce behavior; standalone-library restart is not sufficient final
// acceptance."
//
// Three kernel plugins, each a fiber whose Apply creates its resource inside
// Context.Effect (the disposer closes it) and provides a typed key:
//
//	s0restart/pool      both pgx pools (caller, trusted jobs)          key pool
//	s0restart/client    insert and worker River clients; Needs pool    key client
//	s0restart/listener  the loopback HTTP server; Needs pool, client   key listener
//
// A restart is Root.Batch(UpdateOp(fiber)...) followed by Root.Settle. Dependants
// unload and come back by availability alone; the harness never orders anything.
// HTTP handlers and River jobs hold Context.Enter leases on the fibers of the
// resources they use for the whole request or job, so quiesce and the drain deadline
// are what in-flight work meets. Everything the harness starts in the background
// goes through Context.Go. No resource is created or closed outside a kernel effect.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

const (
	resOwnerLibrary = "library"
	resOwnerKernel  = "kernel"
)

// resourceOwnerFromEnv reads S0WORKERS_RESTART_OWNER: "library" (default) or "kernel".
func resourceOwnerFromEnv(get func(string) string) (string, error) {
	switch v := get("S0WORKERS_RESTART_OWNER"); v {
	case "", resOwnerLibrary:
		return resOwnerLibrary, nil
	case resOwnerKernel:
		return resOwnerKernel, nil
	default:
		return "", fmt.Errorf("S0WORKERS_RESTART_OWNER must be %q or %q", resOwnerLibrary, resOwnerKernel)
	}
}

// kernelCfg is fixture data, not a kernel default (the kernel holds none). The three
// deadlines are each well under the gate's own 5 s recovery budget, so a stage that
// overruns is reported by the kernel (a blocked fiber and a deadline event) before the
// recovery bound is lost; a restart is judged by the gate's bound, not by these.
// Held work lasts 200 ms, so a drain that needs more than a second is a finding.
// EventWindow holds many whole restarts (a combined restart writes under 100 events),
// so the harness can read one restart's events after Settle without truncation.
var kernelCfg = kernel.Config{
	LoadDeadline:    2 * time.Second, // Apply: connect, SELECT 1, River start; well below the 5 s budget
	DrainDeadline:   2 * time.Second, // leases and tracked goroutines: 200 ms holds plus a request in flight
	DisposeDeadline: 2 * time.Second, // pool Close, StopAndCancel, Shutdown of one fiber's whole effect walk
	ArchiveLimit:    16,              // fibers are never disposed before the final Close
	EventWindow:     8192,
	ObserverQueue:   1024, // no observer is attached; the dispatcher still exists
	OwnerInbox:      256,
	TerminalLimit:   16,
}

func kernelConfigReport() map[string]any {
	c := kernelCfg
	return map[string]any{
		"load_deadline_ms": c.LoadDeadline.Milliseconds(), "drain_deadline_ms": c.DrainDeadline.Milliseconds(), "dispose_deadline_ms": c.DisposeDeadline.Milliseconds(),
		"archive_limit": c.ArchiveLimit, "event_window": c.EventWindow, "observer_queue": c.ObserverQueue, "owner_inbox": c.OwnerInbox, "terminal_limit": c.TerminalLimit,
		"constructor": "kernel.New", "bindings": "none (all keys open)",
	}
}

// The service values are small handles, never the resources: the resources live in
// kernelApp tables that the disposers empty, so section 10 can attribute each one to
// its generation. (The kernel itself no longer keeps a provided value after its
// provision ends; the handles predate that fix and are kept for attribution.)
type (
	kPoolH     struct{ tok uint64 }
	kClientH   struct{ tok uint64 }
	kListenerH struct {
		Addr  string
		Epoch int64
	}
)

var (
	kPoolKey     = kernel.NewKey[*kPoolH]("s0restart/pool", 1)
	kClientKey   = kernel.NewKey[*kClientH]("s0restart/client", 1)
	kListenerKey = kernel.NewKey[*kListenerH]("s0restart/listener", 1)
)

// kLease is a fiber activation's lease counter around Context.Enter: the harness's
// own record of how many leases are open on it, of how many were refused while it
// quiesced, and of how many a disposer found still open (it must find none: the kernel
// awaits leases before it runs any disposer).
type kLease struct {
	name       string
	c          *kernel.Context
	open       atomic.Int64
	refused    atomic.Int64
	disposeSaw atomic.Int64 // sum of open leases seen by disposers (must stay 0)
	disposedAt atomic.Int64 // unix nanoseconds of the first disposer
}

func (l *kLease) enter() (func(), bool) {
	leave, ok := l.c.Enter()
	if !ok {
		l.refused.Add(1)
		return nil, false
	}
	l.open.Add(1)
	return func() { l.open.Add(-1); leave() }, true
}

// disposing is called first thing by every disposer of the activation.
func (l *kLease) disposing() {
	l.disposedAt.CompareAndSwap(0, time.Now().UnixNano())
	if n := l.open.Load(); n != 0 {
		l.disposeSaw.Add(n)
	}
}

// enterAll takes the leases in order and leaves them in reverse order; a refusal
// leaves those already taken.
func enterAll(ls ...*kLease) (func(), bool) {
	lefts := make([]func(), 0, len(ls))
	for _, l := range ls {
		leave, ok := l.enter()
		if !ok {
			for i := len(lefts) - 1; i >= 0; i-- {
				lefts[i]()
			}
			return nil, false
		}
		lefts = append(lefts, leave)
	}
	return func() {
		for i := len(lefts) - 1; i >= 0; i-- {
			lefts[i]()
		}
	}, true
}

type kPoolRes struct {
	caller, worker *pgxpool.Pool
	lease          *kLease
}

type kClientRes struct {
	g     *callerGen // insert client, its pool and the fixture copy bound to them
	w     *workerGen
	pool  *kPoolRes
	lease *kLease
}

var kRoles = []string{"pool", "client", "listener"}

func kPluginID(role string) string { return "s0restart/" + role }

// kTracked are the Context.Go labels each fiber starts (and the ledger must show
// finished before its first disposer runs).
var kTracked = map[string][]string{
	"pool":     nil,
	"client":   {"delivery-drain"},
	"listener": {"http-serve", "http-quiesce"},
}

type kernelApp struct {
	a    *fullApp
	ctx  context.Context
	root *kernel.Root
	fib  map[string]*kernel.Fiber

	mu                     sync.Mutex
	nextTok                uint64
	pools                  map[uint64]*kPoolRes
	clients                map[uint64]*kClientRes
	curPool                atomic.Pointer[kPoolRes]
	curClient, curListener atomic.Pointer[kLease]
	last                   *kernelCycle
}

func newKernelApp(a *fullApp) *kernelApp {
	return &kernelApp{a: a, fib: map[string]*kernel.Fiber{}, pools: map[uint64]*kPoolRes{}, clients: map[uint64]*kClientRes{}}
}

func (k *kernelApp) livePools() (caller, worker *pgxpool.Pool) {
	if r := k.curPool.Load(); r != nil {
		return r.caller, r.worker
	}
	return nil, nil
}

func (k *kernelApp) currentLeases() map[string]*kLease {
	out := map[string]*kLease{"client": k.curClient.Load(), "listener": k.curListener.Load()}
	if r := k.curPool.Load(); r != nil {
		out["pool"] = r.lease
	}
	return out
}

// openLeases is the number of kernel leases currently open on the live activations.
func (k *kernelApp) openLeases() int64 {
	var n int64
	for _, l := range k.currentLeases() {
		if l != nil {
			n += l.open.Load()
		}
	}
	return n
}

func (k *kernelApp) putPool(r *kPoolRes) *kPoolH {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.nextTok++
	k.pools[k.nextTok] = r
	return &kPoolH{k.nextTok}
}

func (k *kernelApp) poolRes(h *kPoolH) *kPoolRes {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.pools[h.tok]
}

func (k *kernelApp) putClient(r *kClientRes) *kClientH {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.nextTok++
	k.clients[k.nextTok] = r
	return &kClientH{k.nextTok}
}

func (k *kernelApp) clientRes(h *kClientH) *kClientRes {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.clients[h.tok]
}

func (k *kernelApp) drop(tok uint64) {
	k.mu.Lock()
	delete(k.pools, tok)
	delete(k.clients, tok)
	k.mu.Unlock()
}

// ---- the three plugins ----

func (k *kernelApp) poolPlugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: kPluginID("pool"), Provides: []kernel.AnyKey{kPoolKey},
		Apply: func(c *kernel.Context, _ any) error {
			a := k.a
			res := &kPoolRes{lease: &kLease{name: "pool", c: c}}
			if err := c.Effect("caller-pool", func() (kernel.Disposer, error) {
				p, err := a.newCallerPool(k.ctx)
				if err != nil {
					return nil, err
				}
				res.caller = p
				return func(context.Context) error { res.lease.disposing(); a.closePool(p); return nil }, nil
			}); err != nil {
				return err
			}
			if err := c.Effect("worker-pool", func() (kernel.Disposer, error) {
				p, err := a.newWorkerPool(k.ctx)
				if err != nil {
					return nil, err
				}
				res.worker = p
				return func(context.Context) error { res.lease.disposing(); a.closePool(p); return nil }, nil
			}); err != nil {
				return err
			}
			h := k.putPool(res)
			k.curPool.Store(res)
			c.Defer("pool-table", func(context.Context) error { k.drop(h.tok); k.curPool.CompareAndSwap(res, nil); return nil })
			return kPoolKey.Provide(c, h)
		}})
}

func (k *kernelApp) clientPlugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: kPluginID("client"), Inject: []kernel.Dep{kernel.Need(kPoolKey)}, Provides: []kernel.AnyKey{kClientKey},
		Apply: func(c *kernel.Context, _ any) error {
			a := k.a
			pr := k.poolRes(kPoolKey.Of(c))
			if pr == nil {
				return errors.New("pool provision has no resources")
			}
			res := &kClientRes{pool: pr, lease: &kLease{name: "client", c: c}}
			if err := c.Effect("insert-client", func() (kernel.Disposer, error) {
				g, err := a.makeCaller(pr.caller)
				if err != nil {
					return nil, err
				}
				res.g = g
				return func(context.Context) error {
					res.lease.disposing()
					a.gens.markDisposed(roleInsertClient, g.insGen)
					return nil
				}, nil
			}); err != nil {
				return err
			}
			if err := c.Effect("worker-client", func() (kernel.Disposer, error) {
				w, err := a.makeWorkerWith(pr.worker, nil, k.workerHandler(res))
				if err != nil {
					return nil, err
				}
				res.w = w
				return func(context.Context) error { res.lease.disposing(); a.stopWorkerGen(w); return nil }, nil
			}); err != nil {
				return err
			}
			// The delivery observer the library variant runs as a harness goroutine.
			labelled(context.Background(), roleWorkerClient, res.w.gen, func(context.Context) {
				c.Go("delivery-drain", func(ctx context.Context) error { a.drainUntil(ctx.Done()); return nil })
			})
			h := k.putClient(res)
			k.curClient.Store(res.lease)
			c.Defer("client-table", func(context.Context) error { k.drop(h.tok); k.curClient.CompareAndSwap(res.lease, nil); return nil })
			return kClientKey.Provide(c, h)
		}})
}

func (k *kernelApp) listenerPlugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: kPluginID("listener"), Inject: []kernel.Dep{kernel.Need(kPoolKey), kernel.Need(kClientKey)}, Provides: []kernel.AnyKey{kListenerKey},
		Apply: func(c *kernel.Context, _ any) error {
			a := k.a
			cr := k.clientRes(kClientKey.Of(c))
			if cr == nil || k.poolRes(kPoolKey.Of(c)) != cr.pool {
				return errors.New("client provision has no resources, or runs on another pool")
			}
			ls := &kLease{name: "listener", c: c}
			var (
				srv    *http.Server
				ln     net.Listener
				gen    int
				served atomic.Bool
				done   = make(chan struct{})
			)
			a.mu.Lock()
			a.epoch++ // published only by this activation: an ACK stamped with it was served by the recreated resources
			epoch := a.epoch
			a.mu.Unlock()
			if err := c.Effect("listener", func() (kernel.Disposer, error) {
				addr := a.addr
				if addr == "" {
					addr = "127.0.0.1:0"
				}
				l, err := net.Listen("tcp", addr)
				if err != nil {
					return nil, safeError("listen", err)
				}
				if a.addr == "" {
					a.addr = l.Addr().String() // never serialized; written once, reused on every recreation
				}
				ln = l
				mux := http.NewServeMux()
				mux.HandleFunc("/enqueue", k.serveEnqueue(ls, cr, epoch))
				gen = a.gens.open(roleListener)
				a.gens.routes(gen, 1)
				srv = &http.Server{Handler: mux, ReadHeaderTimeout: restartRecoveryBudget}
				a.liveListeners.Add(1)
				return func(dctx context.Context) error {
					ls.disposing()
					a.gens.markDisposed(roleListener, gen)
					_ = srv.Close() // idempotent: the quiesce goroutine has already shut the server down
					_ = ln.Close()
					if served.Load() {
						select {
						case <-done:
						case <-dctx.Done():
							a.orphans.Add(1)
							return nil
						}
					}
					a.liveListeners.Add(-1)
					a.gens.routes(gen, 0)
					return nil
				}, nil
			}); err != nil {
				return err
			}
			labelled(context.Background(), roleListener, gen, func(context.Context) {
				c.Go("http-serve", func(context.Context) error {
					served.Store(true)
					defer close(done)
					if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
						return safeError("serve", err)
					}
					return nil
				})
				// Quiesce stops accepting at once and finishes the requests in flight; the
				// handlers' leases are what the kernel's drain then waits for.
				c.Go("http-quiesce", func(ctx context.Context) error {
					<-ctx.Done()
					sctx, cancel := context.WithTimeout(context.Background(), kernelCfg.DrainDeadline)
					defer cancel()
					if err := srv.Shutdown(sctx); err != nil {
						a.orphans.Add(1)
						_ = srv.Close()
					}
					return nil
				})
			})
			k.curListener.Store(ls)
			c.Defer("listener-table", func(context.Context) error { k.curListener.CompareAndSwap(ls, nil); return nil })
			return kListenerKey.Provide(c, &kListenerH{Addr: a.addr, Epoch: epoch})
		}})
}

// serveEnqueue is the HTTP handler of one listener activation. It holds a lease on the
// listener, on the client fiber (the insert client) and on the pool fiber (the caller
// pool) for the whole request, hold included. A refused lease is the kernel saying the
// resource is quiescing: 503, never counted as accepted work.
func (k *kernelApp) serveEnqueue(ls *kLease, cr *kClientRes, epoch int64) http.HandlerFunc {
	a := k.a
	return func(w http.ResponseWriter, r *http.Request) {
		leave, ok := enterAll(ls, cr.lease, cr.pool.lease)
		if !ok {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer leave()
		a.reqNow.Add(1)
		defer a.reqNow.Add(-1)
		key := r.URL.Query().Get("id")
		if key == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reply, err := a.insert(r, cr.g, key)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fullReply{restartReply: reply, Gen: epoch}) // 2xx only after COMMIT
	}
}

// workerHandler is the River job handler of one client activation: a lease on the
// client fiber and on the pool fiber for the whole job, hold included. A refused lease
// snoozes the job (no attempt consumed), as the library variant's closed gate does.
func (k *kernelApp) workerHandler(cr *kClientRes) func(*dispatcher) handlerFunc {
	a := k.a
	return func(d *dispatcher) handlerFunc {
		return func(ctx context.Context, row deliveryRow) error {
			leave, ok := enterAll(cr.lease, cr.pool.lease)
			if !ok {
				return river.JobSnooze(10 * time.Millisecond)
			}
			defer leave()
			a.reqNow.Add(1)
			defer a.reqNow.Add(-1)
			a.holdIfActive(ctx, &a.heldWorker, &a.heldWorkerNow, nil)
			return cr.g.fx.recordingHandler(d)(ctx, row)
		}
	}
}

// ---- lifecycle ----

func (k *kernelApp) start(ctx context.Context) error {
	k.ctx = ctx
	root, err := kernel.New(kernelCfg)
	if err != nil {
		return fmt.Errorf("kernel.New: %w", err)
	}
	k.root = root
	k.fib["pool"] = root.Plugin(k.poolPlugin(), nil)
	k.fib["client"] = root.Plugin(k.clientPlugin(), nil)
	k.fib["listener"] = root.Plugin(k.listenerPlugin(), nil)
	if err := root.Settle(ctx); err != nil {
		return fmt.Errorf("kernel settle after mount: %w", err)
	}
	snaps, err := root.Snapshots(ctx)
	if err != nil {
		return fmt.Errorf("kernel snapshots after mount: %w", err)
	}
	for _, s := range snaps {
		for _, role := range kRoles {
			if s.Plugin == kPluginID(role) && s.State != kernel.Active {
				return fmt.Errorf("kernel fiber %s is %s after mount: %s", s.Plugin, s.State, s.Error)
			}
		}
	}
	return nil
}

// close is the final Root.Close; after it no kernel goroutine may remain (the
// teardown snapshot of section 10 fails the arm if one does).
func (k *kernelApp) close() error {
	if k.root == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := k.root.Close(ctx); err != nil {
		return fmt.Errorf("kernel Close: %w", err)
	}
	return nil
}

// kernelExpectation is what a restart of kind must do: the fibers it requests, and the
// fibers that must restart as a consequence (the requested ones and their dependants).
func kernelExpectation(kind string) (explicit, expected map[string]bool, err error) {
	set := func(roles ...string) map[string]bool {
		m := map[string]bool{}
		for _, r := range roles {
			m[r] = true
		}
		return m
	}
	switch kind {
	case "pool":
		return set("pool"), set("pool", "client", "listener"), nil
	case "client":
		return set("client"), set("client", "listener"), nil
	case "listener":
		return set("listener"), set("listener"), nil
	case "combined":
		return set(kRoles...), set(kRoles...), nil
	}
	return nil, nil, errors.New("unknown restart kind")
}

// restart is a restart in place through the kernel: one Root.Batch of UpdateOps, then
// Root.Settle. The dependants of a requested fiber are not named anywhere here.
func (k *kernelApp) restart(ctx context.Context, kind string) (fullRestart, error) {
	explicit, expected, err := kernelExpectation(kind)
	if err != nil {
		return fullRestart{}, err
	}
	in := kernelCheckIn{Kind: kind}
	leases := k.currentLeases()
	for role, l := range leases { // the in-flight leases the restart request meets
		if l != nil {
			in.openAtRequest(role, l.open.Load())
		}
	}
	pre, err := k.root.Inspect(ctx)
	if err != nil {
		return fullRestart{}, fmt.Errorf("kernel inspect before restart: %w", err)
	}
	in.Watermark = pre.Sequence
	revBefore := map[uint64]uint64{}
	for _, f := range pre.Fibers {
		revBefore[f.ID] = f.Revision
	}
	k.a.mu.Lock()
	epoch0 := k.a.epoch
	k.a.mu.Unlock()

	var ops []kernel.BatchOp
	for _, role := range kRoles {
		if explicit[role] {
			ops = append(ops, kernel.UpdateOp(k.fib[role]))
		}
	}
	rs := fullRestart{Requested: time.Now().UTC()}
	if _, err := k.root.Batch(ctx, ops...); err != nil {
		return rs, fmt.Errorf("kernel batch: %w", err)
	}
	in.SettleErr = k.root.Settle(ctx)
	rs.ResourceReady = time.Now().UTC()
	k.a.mu.Lock()
	rs.Epoch = k.a.epoch
	k.a.mu.Unlock()

	in.Requested = rs.Requested
	for _, role := range kRoles {
		fi := kernelFiberIn{Role: role, Plugin: kPluginID(role), ID: k.fib[role].ID(), Explicit: explicit[role], Expected: expected[role], Tracked: kTracked[role], OpenAtRequest: in.open[role]}
		fi.RevBefore = revBefore[fi.ID]
		if l := leases[role]; l != nil {
			fi.Refused, fi.DisposerSaw, fi.OpenAfter = l.refused.Load(), l.disposeSaw.Load(), l.open.Load()
			if at := l.disposedAt.Load(); at != 0 {
				fi.DisposerAt = time.Unix(0, at)
			}
		}
		in.Fibers = append(in.Fibers, fi)
	}
	post, perr := k.root.Inspect(ctx)
	in.Snaps, in.Provisions = post.Fibers, post.Provisions
	in.InspectErr = perr
	for i := range in.Fibers {
		for _, s := range post.Fibers {
			if s.ID == in.Fibers[i].ID {
				in.Fibers[i].RevAfter = s.Revision
			}
		}
	}
	in.Events, in.EventsErr = k.root.Events(ctx)
	k.last = checkKernelRecord(in)
	if rs.Epoch <= epoch0 {
		return rs, errors.New("kernel restart produced no new listener epoch")
	}
	return rs, nil
}

// cycleRecord is the record of the restart of the cycle that just ran.
func (k *kernelApp) cycleRecord() *kernelCycle {
	if k.last == nil {
		return &kernelCycle{Problems: map[string][]string{kbSettle: {"no restart was recorded"}}}
	}
	return k.last
}

// ---- the kernel's own record of a restart ----

const (
	kbSettle = "kernel_settle_returned_nil"
	kbLeaked = "kernel_no_fiber_leaked_blocked_or_failed"
	kbSet    = "kernel_restart_set_by_availability_alone"
	kbOrder  = "kernel_ledger_quiesce_then_drain_then_dispose"
	kbHeld   = "kernel_leases_open_at_restart_request"
	kbWaited = "kernel_unload_waited_for_open_leases_within_drain_deadline"
)

var kernelBoundNames = []string{kbSettle, kbLeaked, kbSet, kbOrder, kbHeld, kbWaited}

var kernelLimits = map[string]string{
	kbSettle: "Root.Settle returns nil after the restart (no fiber blocked past a deadline)",
	kbLeaked: "no fiber of the kernel is Leaked, carries a diagnostic, or is not ACTIVE; the three provisions are active",
	kbSet:    "exactly the requested fibers and their dependants went UNLOADING and ACTIVE again (the requested ones with a new revision; a dependant keeps its revision, the kernel counts only updates), no other fiber did; the harness orders nothing",
	kbOrder:  "ledger: UNLOADING, then every tracked goroutine's go:done, then the first dispose-effect, then ACTIVE, for each restarted fiber; no deadline event",
	kbHeld:   ">=1 kernel lease open on every requested fiber at the restart request",
	kbWaited: "no disposer ran while a lease was open; no lease open after Settle; no drain deadline hit",
}

type kernelFiberIn struct {
	Role, Plugin                      string
	ID                                uint64
	Explicit, Expected                bool
	Tracked                           []string
	RevBefore, RevAfter               uint64
	OpenAtRequest, Refused, OpenAfter int64
	DisposerSaw                       int64
	DisposerAt                        time.Time
}

type kernelCheckIn struct {
	Kind       string
	Requested  time.Time
	Watermark  uint64
	Fibers     []kernelFiberIn
	Events     []kernel.LedgerEvent
	EventsErr  error
	Snaps      []kernel.FiberSnapshot
	Provisions []kernel.Provision
	InspectErr error
	SettleErr  error
	open       map[string]int64
}

func (in *kernelCheckIn) openAtRequest(role string, n int64) {
	if in.open == nil {
		in.open = map[string]int64{}
	}
	in.open[role] = n
}

type kernelFiberRec struct {
	Fiber                string   `json:"fiber"`
	ID                   uint64   `json:"fiber_id"`
	Explicit             bool     `json:"restart_requested"`
	Expected             bool     `json:"restart_expected"`
	RevisionBefore       uint64   `json:"revision_before"`
	RevisionAfter        uint64   `json:"revision_after"`
	LeasesOpenAtRequest  int64    `json:"leases_open_at_restart_request"`
	LeasesRefused        int64    `json:"leases_refused_in_this_activation"`
	DisposerSawOpen      int64    `json:"open_leases_seen_by_disposers"`
	LeasesOpenAfter      int64    `json:"leases_open_after_settle"`
	RequestToDisposerMs  float64  `json:"request_to_first_disposer_ms"`
	UnloadingSeq         uint64   `json:"ledger_unloading_seq"`
	TrackedDone          []string `json:"ledger_go_done_before_first_dispose"`
	FirstDisposeSeq      uint64   `json:"ledger_first_dispose_effect_seq"`
	ActiveSeq            uint64   `json:"ledger_active_again_seq"`
	LedgerEventsOfFiber  int      `json:"ledger_events_after_request"`
	LedgerDeadlineEvents int      `json:"ledger_deadline_events"`
}

// kernelCycle is what the kernel's ledger and snapshots say about one restart.
type kernelCycle struct {
	Kind        string              `json:"restart_kind"`
	WatermarkAt uint64              `json:"ledger_watermark_before_request"`
	Events      int                 `json:"ledger_events_after_request"`
	Fibers      []kernelFiberRec    `json:"fibers"`
	SettleError string              `json:"settle_error,omitempty"`
	Problems    map[string][]string `json:"problems,omitempty"`
}

// checkKernelRecord judges one restart from the kernel's own record (Events, Inspect)
// and the harness's lease counters. The ledger has no lease events and no
// timestamps: that a drain waited for a lease is shown by the order of its events
// (UNLOADING, tracked goroutines done, first dispose-effect, ACTIVE) together with the
// disposers' own count of open leases, which must be 0 although leases were open at the
// request.
func checkKernelRecord(in kernelCheckIn) *kernelCycle {
	c := &kernelCycle{Kind: in.Kind, WatermarkAt: in.Watermark, Problems: map[string][]string{}}
	add := func(bound, f string, args ...any) {
		c.Problems[bound] = append(c.Problems[bound], fmt.Sprintf(f, args...))
	}
	if in.SettleErr != nil {
		c.SettleError = in.SettleErr.Error()
		add(kbSettle, "Settle: %s", in.SettleErr)
	}
	if in.InspectErr != nil {
		add(kbLeaked, "Inspect: %s", in.InspectErr)
	}
	if in.EventsErr != nil && !errors.Is(in.EventsErr, kernel.ErrEventsTruncated) {
		add(kbOrder, "Events: %s", in.EventsErr)
	}
	if len(in.Events) == 0 || in.Events[0].Sequence > in.Watermark+1 {
		add(kbOrder, "the event window no longer holds the restart (oldest retained sequence is past the watermark %d)", in.Watermark)
	}
	var after []kernel.LedgerEvent
	for _, e := range in.Events {
		if e.Sequence > in.Watermark {
			after = append(after, e)
		}
	}
	c.Events = len(after)
	for _, e := range after {
		if e.Kind == "deadline" {
			add(kbWaited, "%s hit its %s deadline (ledger sequence %d)", e.Plugin, e.Label, e.Sequence)
		}
	}
	for _, fi := range in.Fibers {
		r := kernelFiberRec{Fiber: fi.Role, ID: fi.ID, Explicit: fi.Explicit, Expected: fi.Expected, RevisionBefore: fi.RevBefore, RevisionAfter: fi.RevAfter,
			LeasesOpenAtRequest: fi.OpenAtRequest, LeasesRefused: fi.Refused, DisposerSawOpen: fi.DisposerSaw, LeasesOpenAfter: fi.OpenAfter}
		if !fi.DisposerAt.IsZero() && !in.Requested.IsZero() {
			r.RequestToDisposerMs = ms(fi.DisposerAt.Sub(in.Requested))
		}
		var doneAt = map[string]uint64{}
		for _, e := range after {
			if e.Fiber != fi.ID {
				continue
			}
			r.LedgerEventsOfFiber++
			switch {
			case e.Kind == "state" && e.State == kernel.Unloading && r.UnloadingSeq == 0:
				r.UnloadingSeq = e.Sequence
			case e.Kind == "dispose-effect" && r.UnloadingSeq != 0 && r.FirstDisposeSeq == 0:
				r.FirstDisposeSeq = e.Sequence
			case e.Kind == "state" && e.State == kernel.Active && r.UnloadingSeq != 0:
				r.ActiveSeq = e.Sequence
			case e.Kind == "go:done" && doneAt[e.Label] == 0:
				doneAt[e.Label] = e.Sequence
			case e.Kind == "deadline":
				r.LedgerDeadlineEvents++
			}
		}
		for _, label := range fi.Tracked {
			if s := doneAt[label]; s != 0 && r.FirstDisposeSeq != 0 && s < r.FirstDisposeSeq {
				r.TrackedDone = append(r.TrackedDone, label)
			}
		}
		switch {
		case fi.Expected:
			if r.UnloadingSeq == 0 || r.FirstDisposeSeq == 0 || r.ActiveSeq == 0 {
				add(kbOrder, "%s: ledger lacks UNLOADING (%d), a dispose-effect (%d) or ACTIVE again (%d)", fi.Role, r.UnloadingSeq, r.FirstDisposeSeq, r.ActiveSeq)
			} else if !(r.UnloadingSeq < r.FirstDisposeSeq && r.FirstDisposeSeq < r.ActiveSeq) {
				add(kbOrder, "%s: ledger order UNLOADING %d, first dispose-effect %d, ACTIVE %d", fi.Role, r.UnloadingSeq, r.FirstDisposeSeq, r.ActiveSeq)
			}
			if len(r.TrackedDone) != len(fi.Tracked) {
				add(kbOrder, "%s: tracked goroutines %v, finished before the first dispose-effect %v", fi.Role, fi.Tracked, r.TrackedDone)
			}
			if fi.Explicit && fi.RevAfter <= fi.RevBefore {
				add(kbSet, "%s was updated and its revision did not move: %d -> %d", fi.Role, fi.RevBefore, fi.RevAfter)
			}
			if fi.DisposerSaw != 0 {
				add(kbWaited, "%s: a disposer ran with %d lease(s) still open", fi.Role, fi.DisposerSaw)
			}
			if fi.OpenAfter != 0 {
				add(kbWaited, "%s: %d lease(s) of the old activation still open after Settle", fi.Role, fi.OpenAfter)
			}
		default:
			if fi.RevAfter != fi.RevBefore || r.UnloadingSeq != 0 {
				add(kbSet, "%s restarted although nothing it depends on, and not itself, was restarted: revision %d -> %d, UNLOADING at ledger sequence %d", fi.Role, fi.RevBefore, fi.RevAfter, r.UnloadingSeq)
			}
		}
		if fi.Explicit && fi.OpenAtRequest < 1 {
			add(kbHeld, "%s: no kernel lease was open on it at the restart request", fi.Role)
		}
		c.Fibers = append(c.Fibers, r)
	}
	active := 0
	for _, s := range in.Snaps {
		switch {
		case s.Leaked:
			add(kbLeaked, "fiber %d %s is marked leaked", s.ID, s.Plugin)
		case s.Error != "":
			add(kbLeaked, "fiber %d %s carries a diagnostic: %s", s.ID, s.Plugin, s.Error)
		}
		for _, role := range kRoles {
			if s.Plugin == kPluginID(role) && (s.State != kernel.Active || !s.Alive) {
				add(kbLeaked, "fiber %s is %s after Settle", s.Plugin, s.State)
			}
		}
	}
	for _, p := range in.Provisions {
		for _, role := range kRoles {
			if p.Plugin == kPluginID(role) && p.Active {
				active++
			}
		}
	}
	if in.InspectErr == nil && active != len(kRoles) {
		add(kbLeaked, "%d of %d provisions of the three fibers are active after Settle", active, len(kRoles))
	}
	if len(c.Problems) == 0 {
		c.Problems = nil
	}
	return c
}

// bounds turns the record into named bounds appended to the cycle's.
func (c *kernelCycle) bounds() []boundResult {
	out := make([]boundResult, 0, len(kernelBoundNames))
	for _, n := range kernelBoundNames {
		obs := "0 findings"
		if p := c.Problems[n]; len(p) > 0 {
			obs = strings.Join(p, "; ")
			if len(obs) > 1500 {
				obs = obs[:1500] + "..."
			}
		}
		out = append(out, boundResult{Name: n, Limit: kernelLimits[n], Observed: obs, Pass: len(c.Problems[n]) == 0})
	}
	return out
}

// ---- section 10: the kernel's declared owners ----

const (
	kEntryOwnerLoop  = "kernel.(*owner).run"
	kEntryDispatcher = "kernel.(*dispatcher).run"
	kEntryTracked    = "kernel.(*handle).goTracked.func1" // every Context.Go goroutine
)

// s10KernelSet is the declared set of the kernel variant: the library set, with the
// accept loop of the listener and the delivery observer now tracked goroutines of their
// fibers, plus the kernel's own two goroutines (owner loop, observer dispatcher), which
// exist while the application does. After the final Root.Close none may remain.
func s10KernelSet() *s10Set {
	per := map[string][]declEntry{}
	for role, es := range s10Declared {
		per[role] = slices.Clone(es)
	}
	// http-serve (the accept loop) and http-quiesce (stops accepting when the fiber quiesces)
	per[roleListener] = []declEntry{{ownerKernelRT, kEntryTracked, 2}}
	// delivery-drain: the delivery observer, tracked by the client fiber
	per[roleWorkerClient] = append(per[roleWorkerClient], declEntry{ownerKernelRT, kEntryTracked, 1})
	return &s10Set{
		Owner:   resOwnerKernel,
		Owners:  append(slices.Clone(s10Owners), ownerKernelRT),
		PerGen:  per,
		Harness: []declEntry{{ownerKernelRT, kEntryOwnerLoop, 1}, {ownerKernelRT, kEntryDispatcher, 1}},
	}
}

// ---- report text ----

var kernelInterpretations = []string{
	"Resource owner: kernel (S0WORKERS_RESTART_OWNER=kernel). The pgx pools, the River clients and the HTTP listener are created inside Context.Effect by three fibers (pool, client, listener) built by kernel.New and mounted on the root; the disposers close them; each provides a typed key and the dependants Need it. This is the criteria's section 6 'rerun with resources owned by the final kernel's real effects and actual lease/quiesce behaviour'.",
	"A restart is Root.Batch(UpdateOp(fiber)...) then Root.Settle, never a manual close and recreate: pool restarts the pool fiber, client the client fiber, listener the listener fiber, combined all three in one Batch. Dependants unload and return by availability alone (a Need of a provision that is withdrawn); the harness names no order. Resource recreation time is the Settle return; recovery is judged by the unchanged readiness check.",
	"Leases: every HTTP handler holds Context.Enter leases on the listener, client and pool fibers, every River job on the client and pool fibers, for the whole request or job; the 200 ms hold is taken inside them, so the restart request meets open leases. A refused lease is a 503 (HTTP) or a snooze (job). The kernel's record (Inspect, Events) is read after every Settle: UNLOADING then tracked goroutines done then dispose-effect then ACTIVE, no deadline event, no fiber leaked or carrying a diagnostic, Settle nil, and no disposer ever ran with a lease open.",
	"The kernel's Config values (three deadlines, five capacities) are fixture data, printed in the report config as kernel_config; each deadline is below the gate's 5 s recovery budget.",
}

func fullInterpretationsFor(owner string) []string {
	if owner != resOwnerKernel {
		return fullInterpretations
	}
	return append(slices.Clone(fullInterpretations), kernelInterpretations...)
}

func fullLimitsFor(owner string) []string {
	if owner != resOwnerKernel {
		return fullLimits
	}
	out := []string{
		"Loopback synthetic HTTP in one process; no production ingress, proxy or TLS. Resources are owned by three kernel fibers of a kernel.New root with no binding maps and open keys; no other plugin, hook, contribution or profile composition is exercised.",
		"The kernel's event ledger carries no lease events and no timestamps: that a drain waited for open leases is shown by the ledger order of UNLOADING, tracked goroutines done, first dispose-effect, ACTIVE, plus the harness's own lease counters read by the disposers (none may find an open lease).",
		"Service values are handles into harness tables: the kernel keeps provision values in its event ledger until EventWindow events pass, which would retain a disposed pool or client.",
	}
	for _, l := range fullLimits {
		if strings.HasPrefix(l, "Loopback synthetic HTTP") || strings.HasPrefix(l, "S10 resource/leak accounting") {
			continue
		}
		out = append(out, l)
	}
	return append(out, "S10 resource/leak accounting is enforced for what exists in this probe (see section10 and restart-README.md 'Section 10 requirements'); the kernel's own goroutines (owner loop, observer dispatcher, tracked goroutines) are declared owners, and the kernel's fibers, provisions and effect ledger are judged per restart by the kernel_* bounds. goroutines_after_cycle and open_fds_after_cycle remain unbounded raw observations.")
}
