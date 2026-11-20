// SPDX-License-Identifier: Apache-2.0
package s0workers

// The in-process application under the full restart gate: a loopback HTTP
// listener, two pgx pools (caller and trusted-jobs, the fixture's two roles), an
// insert client and a worker client. Each can be stopped and recreated in the
// same process. Harness boundary kept explicit: riverpgxv5 binds a concrete
// pool, so recreating a pool necessarily recreates the River clients bound to it
// (S6: "may recreate a library object if required by that library").

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

var fullScope = callScope{"tenant-a", "restart-full"}

type fullReply struct {
	restartReply
	Gen int64 `json:"epoch"`
}

// callerGen is the ingress side: caller pool plus the insert-only River client
// bound to it. workerGen is the consumer side: trusted-jobs pool, dispatcher and
// the started River client.
type callerGen struct {
	pool     *pgxpool.Pool
	fx       *fixture // per-generation copy bound to this pool/inserter
	inflight sync.WaitGroup
	insGen   int // generation of the insert client (section 10 accounting)
}
type workerGen struct {
	pool         *pgxpool.Pool
	d            *dispatcher
	client       *river.Client[pgx.Tx]
	stopLifetime context.CancelFunc
	gen          int // generation of the worker client (section 10 accounting)
}

type fullApp struct {
	f          *fixture
	addr       string
	httpClient *http.Client

	hold                       atomic.Bool
	heldHTTP, heldWorker       atomic.Int64 // cumulative
	heldHTTPNow, heldWorkerNow atomic.Int64
	orphans                    atomic.Int64
	clientSeq                  atomic.Int64
	inflightReq, maxInflight   atomic.Int64
	livePools, liveClients     atomic.Int64
	liveListeners              atomic.Int64
	gens                       *genLedger    // section 10: generation of every pool, client and listener
	retention                  time.Duration // River cleaner retention; 0 = deletion off
	httpHoldSlots              chan struct{}
	gate                       *capGate // structural S4 cap on concurrent requests

	// The dispatcher's delivery channel must always be drained.
	deliveries    chan delivery
	tallyMu       sync.Mutex
	handled       map[int64]int
	redelivered   int
	insertedFalse int
	stopDrain     chan struct{}
	drainDone     chan struct{}

	mu        sync.Mutex
	cur       *callerGen
	open      bool
	epoch     int64
	srv       *http.Server
	srvGen    int
	tornDown  sync.Once
	drainOn   bool
	reqNow    atomic.Int64 // admitted requests/handlers not yet finished (section 10 quiescence)
	serveDone chan struct{}
	w         *workerGen // owned by the single controller goroutine

	// kern is non-nil in the kernel-owned variant (S0WORKERS_RESTART_OWNER=kernel):
	// every pool, River client and listener is then created by an effect of a kernel
	// fiber, and cur, w, srv and the drain goroutine above are unused.
	kern    *kernelApp
	tearErr error
}

func newFullApp(f *fixture, retention time.Duration) *fullApp {
	a := &fullApp{gens: newGenLedger(), retention: retention, f: f, deliveries: make(chan delivery, 1024), handled: map[int64]int{}, stopDrain: make(chan struct{}), drainDone: make(chan struct{}), httpHoldSlots: make(chan struct{}, fullPoolConns/2), gate: newCapGate(fullMaxConcurrent)}
	a.httpClient = &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 64, IdleConnTimeout: 30 * time.Second}}
	return a
}

// drain empties the dispatcher's delivery channel for the life of the application.
// It starts in start (not newFullApp) so a section 10 harness baseline can be taken
// before any goroutine of the application exists.
func (a *fullApp) drain() {
	defer close(a.drainDone)
	for {
		select {
		case d := <-a.deliveries:
			a.tallyDelivery(d)
		case <-a.stopDrain:
			return
		}
	}
}

// tallyDelivery counts one delivery of the dispatcher's observer channel.
func (a *fullApp) tallyDelivery(d delivery) {
	if d.stage != "handled" {
		return
	}
	a.tallyMu.Lock()
	a.handled[d.jobID]++
	if a.handled[d.jobID] > 1 {
		a.redelivered++
	}
	if !d.inserted {
		a.insertedFalse++
	}
	a.tallyMu.Unlock()
}

// drainUntil is the kernel variant's delivery observer loop: a goroutine tracked by
// the client fiber (Context.Go) that ends when its Std is cancelled.
func (a *fullApp) drainUntil(done <-chan struct{}) {
	for {
		select {
		case d := <-a.deliveries:
			a.tallyDelivery(d)
		case <-done:
			return
		}
	}
}

// resetTally drops the per-job delivery counts of finished cycles. They only exist
// to detect a handler redelivery of the same job; the map grew by one entry per
// delivered job for the whole arm (about 450,000 entries at frozen scale), which
// section 10's retained-heap bound correctly reads as unbounded growth. It is
// called between cycles, after the cycle's own tally was read and its jobs drained.
func (a *fullApp) resetTally() int {
	a.tallyMu.Lock()
	defer a.tallyMu.Unlock()
	n := len(a.handled)
	a.handled = map[int64]int{}
	return n
}

func (a *fullApp) tallyEntries() int {
	a.tallyMu.Lock()
	defer a.tallyMu.Unlock()
	return len(a.handled)
}

func (a *fullApp) tally() (redelivered, insertedFalse int) {
	a.tallyMu.Lock()
	defer a.tallyMu.Unlock()
	return a.redelivered, a.insertedFalse
}

// S6 controlled hold: a handler/transaction that starts while the hold flag is
// raised stays in flight for exactly fullHold, so the restart request meets it.
// Held HTTP transactions are capped at half the caller pool (slots) so the
// consumer stays fed: all eight connections held would starve every worker handler.
func (a *fullApp) holdIfActive(ctx context.Context, total, now *atomic.Int64, slots chan struct{}) {
	if !a.hold.Load() {
		return
	}
	if slots != nil {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			return
		}
	}
	total.Add(1)
	now.Add(1)
	defer now.Add(-1)
	t := time.NewTimer(fullHold)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

func (a *fullApp) newCallerPool(ctx context.Context) (*pgxpool.Pool, error) {
	cc := a.f.base.Copy()
	cc.MaxConns = fullPoolConns
	gen := a.gens.open(roleCallerPool)
	cc.ConnConfig.RuntimeParams["application_name"] = s10AppName(roleCallerPool, gen) // harness-only: attributes the server session to its generation
	var p *pgxpool.Pool
	var err error
	labelled(ctx, roleCallerPool, gen, func(ctx context.Context) { p, err = pgxpool.NewWithConfig(ctx, cc) })
	if err != nil {
		return nil, safeError("full caller pool", err)
	}
	a.gens.poolOpened(p, roleCallerPool, gen)
	var one int
	if err := p.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		p.Close()
		return nil, safeError("full caller pool ready", err)
	}
	a.livePools.Add(1)
	return p, nil
}

func (a *fullApp) newWorkerPool(ctx context.Context) (*pgxpool.Pool, error) {
	wc := a.f.base.Copy()
	wc.MaxConns = fullPoolConns
	wc.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE "+ident(a.f.jobs))
		return safeError("trusted jobs session", err)
	}
	gen := a.gens.open(roleWorkerPool)
	wc.ConnConfig.RuntimeParams["application_name"] = s10AppName(roleWorkerPool, gen) // harness-only
	var p *pgxpool.Pool
	var err error
	labelled(ctx, roleWorkerPool, gen, func(ctx context.Context) { p, err = pgxpool.NewWithConfig(ctx, wc) })
	if err != nil {
		return nil, safeError("full worker pool", err)
	}
	a.gens.poolOpened(p, roleWorkerPool, gen)
	var user string
	if err = p.QueryRow(ctx, "SELECT current_user").Scan(&user); err != nil || user != a.f.jobs {
		p.Close()
		return nil, errors.New("full worker pool is not the trusted jobs role")
	}
	a.livePools.Add(1)
	return p, nil
}

func (a *fullApp) makeCaller(pool *pgxpool.Pool) (*callerGen, error) {
	fx := *a.f
	fx.callerPool = pool
	ins, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Schema: a.f.jobs, Logger: quiet})
	if err != nil {
		return nil, safeError("full insert client", err)
	}
	fx.inserter = ins
	return &callerGen{pool: pool, fx: &fx, insGen: a.gens.open(roleInsertClient)}, nil
}

// The handler resolves the CURRENT caller generation per call; while the caller
// side is closed for a restart it snoozes (no attempt consumed).
func (a *fullApp) workerHandler(d *dispatcher) handlerFunc {
	return func(ctx context.Context, row deliveryRow) error {
		a.holdIfActive(ctx, &a.heldWorker, &a.heldWorkerNow, nil)
		g, _ := a.acquire()
		if g == nil {
			return river.JobSnooze(10 * time.Millisecond)
		}
		defer a.finish(g)
		return g.fx.recordingHandler(d)(ctx, row)
	}
}

// riverRetention maps the probe parameter to River's option: -1 disables deletion.
// The cleaner itself runs only in the client that holds leadership.
func riverRetention(d time.Duration) time.Duration {
	if d <= 0 {
		return -1
	}
	return d
}

// makeWorker builds and STARTS a worker client. d is reused for client-only recreation.
func (a *fullApp) makeWorker(pool *pgxpool.Pool, d *dispatcher) (*workerGen, error) {
	return a.makeWorkerWith(pool, d, a.workerHandler)
}

// makeWorkerWith is makeWorker with the job handler factory chosen by the resource
// owner: the library variant's gate, or the kernel variant's leases.
func (a *fullApp) makeWorkerWith(pool *pgxpool.Pool, d *dispatcher, handler func(*dispatcher) handlerFunc) (*workerGen, error) {
	if d == nil {
		d = &dispatcher{pool: pool, schema: a.f.jobs, snooze: 10 * time.Millisecond, deliveries: a.deliveries}
	}
	if err := d.registry.install(handler(d)); err != nil {
		return nil, err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, d) // once per NEW client
	gen := a.gens.open(roleWorkerClient)
	a.gens.handlerInstalled(gen, true)
	var client *river.Client[pgx.Tx]
	var err error
	cfg := &river.Config{
		Schema: a.f.jobs, ID: "s0wk-" + a.f.id + "-full-" + strconv.FormatInt(a.clientSeq.Add(1), 10), Logger: quiet, Workers: workers,
		Queues:        map[string]river.QueueConfig{queue: {MaxWorkers: fullWorkers}},
		FetchCooldown: time.Millisecond, FetchPollInterval: 5 * time.Millisecond,
		JobTimeout: 10 * time.Second, SoftStopTimeout: 2 * time.Second,
		RescueStuckJobsAfter: time.Minute, TestOnly: true,
		CancelledJobRetentionPeriod: riverRetention(a.retention), CompletedJobRetentionPeriod: riverRetention(a.retention), DiscardedJobRetentionPeriod: riverRetention(a.retention),
		ReindexerIndexNames: []string{},
	}
	life, cancel := context.WithCancel(context.Background())
	labelled(life, roleWorkerClient, gen, func(ctx context.Context) { // goroutines inherit the generation label
		if client, err = river.NewClient(riverpgxv5.New(pool), cfg); err != nil {
			return
		}
		err = client.Start(life)
	})
	if err != nil {
		cancel()
		return nil, safeError("full worker client", err)
	}
	a.liveClients.Add(1)
	return &workerGen{pool: pool, d: d, client: client, stopLifetime: cancel, gen: gen}, nil
}

// openCaller publishes the generation with a NEW epoch under one lock: an ACK
// stamped with it was served only by the recreated resources.
func (a *fullApp) openCaller(g *callerGen) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cur, a.open = g, true
	a.epoch++
	return a.epoch
}

func (a *fullApp) acquire() (*callerGen, int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.open {
		return nil, 0
	}
	a.cur.inflight.Add(1)
	a.reqNow.Add(1)
	return a.cur, a.epoch
}

func (a *fullApp) finish(g *callerGen) {
	g.inflight.Done()
	a.reqNow.Add(-1)
}

// closeCaller makes new requests see 503 / handlers snooze and drains admitted ones.
func (a *fullApp) closeCaller() *callerGen {
	a.mu.Lock()
	a.open = false
	g := a.cur
	a.mu.Unlock()
	if g == nil {
		return nil
	}
	done := make(chan struct{})
	go func() { g.inflight.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(restartRecoveryBudget):
		a.orphans.Add(1)
	}
	return g
}

func (a *fullApp) stopWorker() {
	a.stopWorkerGen(a.w)
}

func (a *fullApp) stopWorkerGen(w *workerGen) {
	if w == nil {
		return
	}
	a.gens.markDisposed(roleWorkerClient, w.gen)
	ctx, cancel := context.WithTimeout(context.Background(), restartRecoveryBudget)
	defer cancel()
	if err := w.d.registry.revoke(ctx); err != nil {
		a.orphans.Add(1)
	}
	if err := w.client.StopAndCancel(ctx); err != nil {
		a.orphans.Add(1)
	}
	w.stopLifetime()
	a.liveClients.Add(-1)
	a.gens.handlerInstalled(w.gen, w.d.registry.live.Load() != nil)
	a.gens.keepClient(w.gen, w.pool, w.client)
}

func (a *fullApp) closePool(p *pgxpool.Pool) {
	role, gen := a.gens.poolDisposed(p)
	defer a.gens.keepPool(role, gen, p)
	done := make(chan struct{})
	go func() { p.Close(); close(done) }() // Close blocks until every connection is returned
	select {
	case <-done:
		a.livePools.Add(-1)
	case <-time.After(restartRecoveryBudget):
		a.orphans.Add(1)
	}
}

func (a *fullApp) startListener() error {
	addr := a.addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return safeError("listen", err)
	}
	if a.addr == "" {
		a.addr = ln.Addr().String() // never serialized; written once, reused on every recreation
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/enqueue", a.serveEnqueue)
	gen := a.gens.open(roleListener)
	a.gens.routes(gen, 1)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: restartRecoveryBudget}
	done := make(chan struct{})
	a.mu.Lock()
	a.srv, a.serveDone = srv, done
	a.mu.Unlock()
	a.liveListeners.Add(1)
	a.mu.Lock()
	a.srvGen = gen
	a.mu.Unlock()
	labelled(context.Background(), roleListener, gen, func(context.Context) { go func() { _ = srv.Serve(ln); close(done) }() })
	return nil
}

func (a *fullApp) stopListener() {
	a.mu.Lock()
	srv, done, gen := a.srv, a.serveDone, a.srvGen
	a.srv = nil
	a.mu.Unlock()
	if srv == nil {
		return
	}
	a.gens.markDisposed(roleListener, gen)
	ctx, cancel := context.WithTimeout(context.Background(), restartRecoveryBudget)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		a.orphans.Add(1)
		_ = srv.Close()
	}
	select {
	case <-done:
		a.liveListeners.Add(-1)
		a.gens.routes(gen, 0)
	case <-time.After(restartRecoveryBudget):
		a.orphans.Add(1)
	}
}

func (a *fullApp) serveEnqueue(w http.ResponseWriter, r *http.Request) {
	g, epoch := a.acquire()
	if g == nil {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable) // never counted as accepted work
		return
	}
	defer a.finish(g)
	key := r.URL.Query().Get("id")
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	reply, err := a.insert(r, g, key)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(fullReply{restartReply: reply, Gen: epoch}) // 2xx only after COMMIT
}

// Same transaction/role ordering as enqueue; the held transaction is the S6
// "acquired transaction" held for fullHold before commit.
func (a *fullApp) insert(r *http.Request, g *callerGen, key string) (restartReply, error) {
	reply := restartReply{Key: key}
	ctx := r.Context()
	tx, err := g.fx.beginCaller(ctx, fullScope)
	if err != nil {
		return reply, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+ident(g.fx.jobs)); err != nil {
		return reply, safeError("full role", err)
	}
	res, insertErr := g.fx.inserter.InsertTx(ctx, tx, workerArgs{Tenant: hostileTenant, IdempotencyID: key}, &river.InsertOpts{Queue: queue, UniqueOpts: river.UniqueOpts{ByArgs: true}})
	_, restoreErr := tx.Exec(ctx, "SET LOCAL ROLE "+ident(g.fx.caller))
	if insertErr != nil {
		return reply, safeError("full insert", insertErr)
	}
	if restoreErr != nil {
		return reply, safeError("full restore", restoreErr)
	}
	reply.JobID, reply.Duplicate = res.Job.ID, res.UniqueSkippedAsDuplicate
	a.holdIfActive(ctx, &a.heldHTTP, &a.heldHTTPNow, a.httpHoldSlots)
	if err := ctx.Err(); err != nil {
		return reply, safeError("full held transaction", err)
	}
	return reply, safeError("full commit", tx.Commit(ctx))
}

type fullRestart struct {
	Requested, ResourceReady time.Time
	Epoch                    int64 // epoch served only by the recreated resources
}

// ownerName is the resource owner recorded in every report and cycle line.
func (a *fullApp) ownerName() string {
	if a.kern != nil {
		return resOwnerKernel
	}
	return resOwnerLibrary
}

// appPools returns the live caller and worker pools (nil when absent).
func (a *fullApp) appPools() (caller, worker *pgxpool.Pool) {
	if a.kern != nil {
		return a.kern.livePools()
	}
	a.mu.Lock()
	cur := a.cur
	a.mu.Unlock()
	if cur != nil {
		caller = cur.pool
	}
	if a.w != nil {
		worker = a.w.pool
	}
	return caller, worker
}

func (a *fullApp) teardownErr() error { return a.tearErr }

func (a *fullApp) start(ctx context.Context) error {
	if a.kern != nil {
		return a.kern.start(ctx)
	}
	a.drainOn = true
	go a.drain()
	cp, err := a.newCallerPool(ctx)
	if err != nil {
		return err
	}
	wp, err := a.newWorkerPool(ctx)
	if err != nil {
		return err
	}
	g, err := a.makeCaller(cp)
	if err != nil {
		return err
	}
	if a.w, err = a.makeWorker(wp, nil); err != nil {
		return err
	}
	a.openCaller(g)
	return a.startListener()
}

// recreate stops the caller side and the worker client, optionally the pgx
// pools, then rebuilds them. S6: the gate stays closed (503 for HTTP) until every
// affected consumer is back; the listener is untouched.
func (a *fullApp) recreate(ctx context.Context, newPools bool) (int64, error) {
	old := a.closeCaller()
	a.gens.markDisposed(roleInsertClient, old.insGen)
	a.stopWorker()
	callerPool, workerPool, d := old.pool, a.w.pool, a.w.d
	if newPools {
		a.closePool(callerPool)
		a.closePool(workerPool)
		var err error
		if callerPool, err = a.newCallerPool(ctx); err != nil {
			return 0, err
		}
		if workerPool, err = a.newWorkerPool(ctx); err != nil {
			return 0, err
		}
		d = nil // the dispatcher holds the pool; a new pool means a new dispatcher
	}
	w, err := a.makeWorker(workerPool, d)
	if err != nil {
		return 0, err
	}
	a.w = w
	g, err := a.makeCaller(callerPool)
	if err != nil {
		return 0, err
	}
	return a.openCaller(g), nil
}

// restart performs one controlled recreation inside this process; it never
// replaces the process. pool=pgx pools (and the River clients bound to them),
// client=both River clients on the same pools, listener=HTTP listener on the
// same loopback address, combined=all three.
func (a *fullApp) restart(ctx context.Context, kind string) (fullRestart, error) {
	if a.kern != nil {
		return a.kern.restart(ctx, kind)
	}
	rs := fullRestart{Requested: time.Now().UTC()}
	var err error
	switch kind {
	case "pool":
		rs.Epoch, err = a.recreate(ctx, true)
	case "client":
		rs.Epoch, err = a.recreate(ctx, false)
	case "listener":
		a.stopListener()
		a.mu.Lock()
		a.epoch++
		rs.Epoch = a.epoch
		a.mu.Unlock()
		err = a.startListener()
	case "combined":
		a.stopListener()
		if rs.Epoch, err = a.recreate(ctx, true); err == nil {
			err = a.startListener()
		}
	default:
		err = errors.New("unknown restart kind")
	}
	rs.ResourceReady = time.Now().UTC()
	return rs, err
}

func (a *fullApp) teardown() {
	a.tornDown.Do(a.teardownOnce)
}

func (a *fullApp) teardownOnce() {
	if a.kern != nil {
		a.tearErr = a.kern.close()
		a.httpClient.CloseIdleConnections()
		return
	}
	a.stopListener()
	if g := a.closeCaller(); g != nil {
		a.gens.markDisposed(roleInsertClient, g.insGen)
		a.closePool(g.pool)
	}
	a.stopWorker()
	if a.w != nil {
		a.closePool(a.w.pool)
	}
	a.httpClient.CloseIdleConnections()
	if a.drainOn {
		close(a.stopDrain)
		<-a.drainDone
	}
	a.mu.Lock()
	a.cur, a.srv = nil, nil // the application keeps no reference to a disposed generation
	a.mu.Unlock()
	a.w = nil
}

// ---- client side ----

func (a *fullApp) once(ctx context.Context, key string, attempt int) (fullACK, string) {
	if err := a.gate.acquire(ctx); err != nil {
		return fullACK{}, "concurrency_cap_wait_timeout"
	}
	defer a.gate.release()
	// maxInflight stays an independent observation of what the gate allowed.
	n := a.inflightReq.Add(1)
	defer a.inflightReq.Add(-1)
	for {
		m := a.maxInflight.Load()
		if n <= m || a.maxInflight.CompareAndSwap(m, n) {
			break
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+a.addr+"/enqueue?id="+key, nil)
	if err != nil {
		return fullACK{}, "request_build"
	}
	res, err := a.httpClient.Do(req)
	if err != nil {
		return fullACK{}, classifyTransport(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		if res.StatusCode == http.StatusServiceUnavailable {
			return fullACK{}, "http_503"
		}
		return fullACK{}, "http_" + strconv.Itoa(res.StatusCode)
	}
	var reply fullReply
	if err := json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&reply); err != nil {
		return fullACK{}, "decode_failed"
	}
	observed := time.Now().UTC() // the client observer owns the ACK timestamp
	if reply.Key != key {
		return fullACK{}, "wrong_key"
	}
	return fullACK{restartACK: restartACK{JobID: reply.JobID, Observed: observed, Duplicate: reply.Duplicate}, Gen: reply.Gen, Attempt: attempt}, ""
}

// doRequest: same-ID retries are allowed (S6) only inside the 5 s operation
// timeout measured from SCHEDULED arrival (S4). Every attempt is recorded.
func (a *fullApp) doRequest(ctx context.Context, l *opLedger, key string, scheduled time.Time) {
	deadline := scheduled.Add(fullOpTimeout)
	octx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for attempt := 1; ; attempt++ {
		l.attempt(key)
		ack, class := a.once(octx, key, attempt)
		if class == "" {
			l.ack(key, ack)
			return
		}
		l.fail(key, class)
		if time.Now().Add(fullRetryBackoff).After(deadline) || timerWait(octx, time.Now().Add(fullRetryBackoff)) != nil {
			l.timedOut(key)
			return
		}
	}
}
