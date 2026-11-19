// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// Section 9, cold mount on the acknowledgement path. Nothing here is a product
// tenant cache or a database: the acknowledgement path is SIMULATED on the real
// candidates, and the criteria's own wording (items, body, pinned dependencies,
// churn, scope labels) is reproduced exactly.
//
// A request is one acknowledgement: for every synthetic item it takes the
// activation lease of a pinned dependency (Enter), resolves its typed provision
// (composition.Resolve), leaves, and hashes the item; the request then appends
// one commit record to an in-memory ledger and only then is acknowledged. Latency
// starts at the SCHEDULED arrival (open loop), includes queueing for the 32-request
// cap, and ends at the acknowledgement. Arms:
//
//	A   every request cold-mounts a fresh four-fiber scope inline, before commit
//	B0  requests use only the pinned 97 shared fibers; the churn scheduler runs but starts no mounts
//	B1  as B0, while the churn scheduler mounts 50 fresh four-fiber trees per second
//	    (scope labels round-robin over 500, at most eight in flight) and disposes
//	    each after 100 ms of idle time

// ItemBytes is the size of one synthetic item (an interpretation: the criteria fix
// the item count and the 1 MiB body, not an item size).
const ItemBytes = 256

type ColdArm string

const (
	ArmA  ColdArm = "A"
	ArmB0 ColdArm = "B0"
	ArmB1 ColdArm = "B1"
)

type BodySize struct {
	Name  string `json:"name"`
	Items int    `json:"items"`
}

// ColdPlan carries every count of the cold-mount run. FrozenColdPlan is the only
// acceptance plan; DryColdPlan is reduced and can never pass.
type ColdPlan struct {
	Dry               bool          `json:"dry"`
	Repetitions       int           `json:"repetitions"`
	Warmup            time.Duration `json:"warmupNS"`
	Window            time.Duration `json:"windowNS"`
	Drain             time.Duration `json:"drainNS"`
	RequestsPerSecond int           `json:"requestsPerSecond"`
	RequestCap        int           `json:"requestCap"`
	ChurnPerSecond    int           `json:"churnPerSecond"`
	ChurnCap          int           `json:"churnCap"`
	IdleBeforeDispose time.Duration `json:"idleBeforeDisposeNS"`
	RequestTimeout    time.Duration `json:"requestTimeoutNS"`
	Labels            int           `json:"scopeLabels"`
	TreeWidth         int           `json:"treeWidth"`
	Bodies            []BodySize    `json:"bodies"`
}

// Frozen thresholds of section 9.
const (
	AckP99Items     = 250 * time.Millisecond // arm B, 100-item body.
	AckP99MiB       = time.Second            // arm B, 1 MiB body.
	ChurnDeltaLimit = 5 * time.Millisecond   // B1 p99 over B0 p99, per matched pair and aggregate.
	SuccessFloor    = 0.999                  // section 4: each normal steady-state arm.
	ColdMinimumReps = 10
	coldMiBItems    = (1 << 20) / ItemBytes
)

func FrozenColdPlan() ColdPlan {
	return ColdPlan{Repetitions: ColdMinimumReps, Warmup: 30 * time.Second, Window: 120 * time.Second, Drain: 30 * time.Second,
		RequestsPerSecond: 100, RequestCap: 32, ChurnPerSecond: 50, ChurnCap: 8, IdleBeforeDispose: 100 * time.Millisecond,
		RequestTimeout: 5 * time.Second, Labels: 500, TreeWidth: 4,
		Bodies: []BodySize{{Name: "100-items", Items: 100}, {Name: "1MiB", Items: coldMiBItems}}}
}

// DryColdPlan keeps every rate and cap but shrinks repetitions and durations.
func DryColdPlan() ColdPlan {
	p := FrozenColdPlan()
	p.Dry, p.Repetitions, p.Warmup, p.Window, p.Drain = true, 2, 500*time.Millisecond, 3*time.Second, 10*time.Second
	return p
}

func (p ColdPlan) Frozen() bool {
	f := FrozenColdPlan()
	return !p.Dry && p.Repetitions == f.Repetitions && p.Warmup == f.Warmup && p.Window == f.Window && p.Drain == f.Drain &&
		p.RequestsPerSecond == f.RequestsPerSecond && p.RequestCap == f.RequestCap && p.ChurnPerSecond == f.ChurnPerSecond &&
		p.ChurnCap == f.ChurnCap && p.IdleBeforeDispose == f.IdleBeforeDispose && p.RequestTimeout == f.RequestTimeout &&
		p.Labels == f.Labels && p.TreeWidth == f.TreeWidth && slices.Equal(p.Bodies, f.Bodies)
}

func (p ColdPlan) validate() error {
	if p.Repetitions < 1 || p.Warmup < 0 || p.Window <= 0 || p.Drain <= 0 || p.RequestsPerSecond < 1 || p.RequestCap < 1 ||
		p.ChurnPerSecond < 1 || p.ChurnCap < 1 || p.RequestTimeout <= 0 || p.Labels < 1 || p.TreeWidth < 2 || len(p.Bodies) == 0 {
		return errors.New("cold-mount plan has a non-positive count")
	}
	for _, b := range p.Bodies {
		if b.Items < 1 || b.Name == "" {
			return errors.New("cold-mount plan has an unusable body")
		}
	}
	return nil
}

func (p ColdPlan) arrivals(perSecond int) int {
	return int((p.Warmup + p.Window) / (time.Second / time.Duration(perSecond)))
}

// ScheduledRequests / ScheduledChurn are exact: 100/s for 150 s is 15,000 request
// arrivals and 50/s for 150 s is 7,500 churn arrivals, warm-up included.
func (p ColdPlan) ScheduledRequests() int { return p.arrivals(p.RequestsPerSecond) }
func (p ColdPlan) ScheduledChurn() int    { return p.arrivals(p.ChurnPerSecond) }

// MeasuredRequests are the arrivals scheduled inside the measured window only.
func (p ColdPlan) MeasuredRequests() int {
	return p.ScheduledRequests() - int(p.Warmup/(time.Second/time.Duration(p.RequestsPerSecond)))
}

// AckRecord is one scheduled request arrival. Every arrival has a record,
// including overflow, error and timeout; none is removed from a denominator.
type AckRecord struct {
	Seq         int    `json:"seq"`
	Warmup      bool   `json:"warmup"`
	Label       int    `json:"label"`
	ScheduledNS int64  `json:"scheduledNS"` // offsets from the run start.
	StartedNS   int64  `json:"startedNS"`
	EndedNS     int64  `json:"endedNS"`
	LatencyNS   int64  `json:"latencyNS"` // ended - SCHEDULED; an unserved overflow carries the request timeout.
	Outcome     string `json:"outcome"`   // ok | overflow | timeout | unavailable503 | error
	Status      int    `json:"status"`
	Committed   bool   `json:"committed"`
	ColdEnsures int    `json:"coldEnsures"`
	MountNS     int64  `json:"mountNS,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ChurnRecord is one scheduled background mount arrival (every arrival is kept).
type ChurnRecord struct {
	Seq           int    `json:"seq"`
	Warmup        bool   `json:"warmup"`
	Label         int    `json:"label"`
	ScheduledNS   int64  `json:"scheduledNS"`
	StartedNS     int64  `json:"startedNS"`
	MountedNS     int64  `json:"mountDoneNS"`
	DisposeAtNS   int64  `json:"disposeStartNS"`
	DisposedNS    int64  `json:"disposeDoneNS"`
	MountLatency  int64  `json:"mountLatencyNS"`
	Outcome       string `json:"outcome"` // ok | idle(B0) | overflow | late | collision | error
	Error         string `json:"error,omitempty"`
	StartLagNS    int64  `json:"startLagNS"`
	Late          bool   `json:"late"` // started more than one churn interval after its scheduled instant.
	MountedOK     bool   `json:"mounted"`
	DisposedOK    bool   `json:"disposed"`
	SchedulerOnly bool   `json:"schedulerOnly"`
}

// ColdRun is the complete raw record of one fresh-root arm run.
type ColdRun struct {
	Backend      string        `json:"backend"`
	Body         BodySize      `json:"body"`
	Arm          ColdArm       `json:"arm"`
	Repetition   int           `json:"repetition"`
	StartedUTC   time.Time     `json:"startedUTC"`
	EndedUTC     time.Time     `json:"endedUTC"`
	Acks         []AckRecord   `json:"acks"`
	Churn        []ChurnRecord `json:"churn"`
	Ledger       int           `json:"ledgerCommits"`
	ColdEnsures  int64         `json:"coldEnsureCalls"`
	Outstanding  int           `json:"outstandingAfterDrain"`
	DrainNS      int64         `json:"drainNS"`
	Errors       []string      `json:"errors,omitempty"`
	HostBefore   HostSample    `json:"hostBefore"`
	HostAfter    HostSample    `json:"hostAfter"`
	Host         HostDelta     `json:"hostDelta"`
	GoroutinesOK bool          `json:"goroutineBaselineRestored"`
}

// Commit is one ledger record: an acknowledgement exists only after its commit.
type Commit struct {
	Seq    int
	Label  int
	Value  int
	Digest [sha256.Size]byte
}

// AckResult is the outcome of one acknowledgement attempt.
type AckResult struct {
	Status     int           // 200 committed and acknowledged; 503 cold lookup timed out.
	RetryAfter time.Duration // set with 503 or a snooze.
	Snoozed    bool          // the simulated job was snoozed instead of acknowledged.
	Committed  bool
	MountNS    int64
}

// coldProbe is the simulated acknowledgement path over one fresh real root.
type coldProbe struct {
	r         *RealCandidateRuntime
	plan      ColdPlan
	body      []byte
	items     int
	pinned    []*co.Context // the shared consumers: mounted for the whole run, never churned.
	sharedKey cm.Key[int]
	localKey  cm.Key[int]
	busy      []atomic.Bool // one live incarnation per scope label.
	ensures   atomic.Int64  // cold-ensure (mount) calls made by acknowledgement paths.
	mu        sync.Mutex
	ledger    map[int]Commit
}

func newColdProbe(ctx context.Context, backend Backend, plan ColdPlan, body BodySize) (*coldProbe, error) {
	f, err := TreeFixture(plan.Labels, plan.TreeWidth)
	if err != nil {
		return nil, err
	}
	r, err := NewRealCandidateRuntime(backend, f)
	if err != nil {
		return nil, err
	}
	p := &coldProbe{r: r, plan: plan, items: body.Items, busy: make([]atomic.Bool, plan.Labels), ledger: make(map[int]Commit),
		sharedKey: r.keys["shared"], localKey: r.keys["value"], body: make([]byte, body.Items*ItemBytes)}
	for i := range p.body {
		p.body[i] = byte(i*31 + i>>8)
	}
	fail := func(err error) (*coldProbe, error) {
		cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
		defer cancel()
		return nil, errors.Join(err, p.teardown(cleanup, &RunResult{}))
	}
	if err := r.Mount(ctx, 0, f.Shared); err != nil {
		return fail(err)
	}
	if err := r.Runtime.Settle(ctx); err != nil {
		return fail(err)
	}
	if err := inspectActive(ctx, r, &RunResult{}, 0, f.Shared); err != nil {
		return fail(err)
	}
	r.mu.Lock()
	for i := 1; i < f.Shared; i++ {
		p.pinned = append(p.pinned, r.live[i].composed)
	}
	r.mu.Unlock()
	if slices.Contains(p.pinned, nil) {
		return fail(errors.New("a pinned shared consumer has no activation"))
	}
	return p, nil
}

// teardown disposes everything this probe may own, then verifies the ledger is
// empty and closes the root. Failures are returned, never discarded.
func (p *coldProbe) teardown(ctx context.Context, scratch *RunResult) error {
	var errs []error
	if p.r.Runtime != nil {
		if err := p.r.RequestDisposal(ctx, 0, len(p.r.Fixture.Nodes)); err != nil {
			errs = append(errs, fmt.Errorf("final disposal request: %w", err))
		}
		if err := p.r.Runtime.Settle(ctx); err != nil {
			errs = append(errs, fmt.Errorf("final settle: %w", err))
		}
		if err := inspectDisposed(ctx, p.r, scratch); err != nil {
			errs = append(errs, err)
		}
		if err := p.r.Runtime.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("root Close: %w", err))
		}
	}
	p.r.Clear()
	return errors.Join(errs...)
}

func (p *coldProbe) commit(c Commit) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, dup := p.ledger[c.Seq]; dup {
		return fmt.Errorf("duplicate commit for request %d", c.Seq)
	}
	p.ledger[c.Seq] = c
	return nil
}

// process is the shared simulated body: per item one lease, one typed resolve and
// one hash; then the commit. check validates the resolved value, which a request
// must read only from its own scope's contribution.
func (p *coldProbe) process(ctx context.Context, seq, label int, c *co.Context, key cm.Key[int], want int) (Commit, error) {
	h := sha256.New()
	for i := 0; i < p.items; i++ {
		if i&63 == 0 {
			if err := ctx.Err(); err != nil {
				return Commit{}, err // never commit after the deadline.
			}
		}
		if !c.Enter() {
			return Commit{}, cc.ErrInactive
		}
		v, err := co.Resolve(c, key)
		c.Leave()
		if err != nil || v != want {
			return Commit{}, fmt.Errorf("item %d resolved %d (want %d): %w", i, v, want, err)
		}
		h.Write(p.body[i*ItemBytes : (i+1)*ItemBytes])
	}
	var out Commit
	out.Seq, out.Label, out.Value = seq, label, want
	copy(out.Digest[:], h.Sum(nil))
	return out, p.commit(out)
}

// AckPinned is arms B0/B1: the pinned dependencies only; no cold ensure, no mount.
func (p *coldProbe) AckPinned(ctx context.Context, seq int) (AckResult, error) {
	if _, err := p.process(ctx, seq, -1, p.pinned[seq%len(p.pinned)], p.sharedKey, 0); err != nil {
		return AckResult{}, err
	}
	return AckResult{Status: 200, Committed: true}, nil
}

// AckCold is arm A and the cold-timeout fixture: one fresh scope per first-time
// request, mounted inline BEFORE the commit. budget > 0 bounds the cold ensure;
// when it expires the request answers 503 with Retry-After (job=false) or is
// snoozed (job=true). It never commits and never reads another scope's contribution.
func (p *coldProbe) AckCold(ctx context.Context, seq int, budget time.Duration, job bool) (AckResult, error) {
	label := seq % p.plan.Labels
	if !p.busy[label].CompareAndSwap(false, true) {
		return AckResult{}, fmt.Errorf("scope label %d still has a live incarnation", label)
	}
	p.ensures.Add(1)
	ensure := ctx
	if budget > 0 {
		var cancel context.CancelFunc
		ensure, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	mountStart := time.Now()
	err := p.r.MountTreeAwait(ensure, label)
	mount := time.Since(mountStart).Nanoseconds()
	if err != nil {
		p.release(label)
		if budget > 0 && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			if job {
				return AckResult{Snoozed: true, RetryAfter: time.Second, MountNS: mount}, nil
			}
			return AckResult{Status: 503, RetryAfter: time.Second, MountNS: mount}, nil
		}
		return AckResult{MountNS: mount}, err
	}
	f := p.r.Fixture
	start := f.Shared + label*f.TreeWidth
	p.r.mu.Lock()
	consumer := p.r.live[start+1].composed
	p.r.mu.Unlock()
	if consumer == nil {
		p.release(label)
		return AckResult{MountNS: mount}, fmt.Errorf("label %d has no consumer activation", label)
	}
	_, err = p.process(ctx, seq, label, consumer, p.localKey, p.r.providers[RawKey{Name: "value", Realm: label}])
	if err != nil {
		p.release(label)
		return AckResult{MountNS: mount}, err
	}
	return AckResult{Status: 200, Committed: true, MountNS: mount}, nil
}

// release disposes the tree of a label (idempotent for fibers that never mounted
// or are archived) and frees the label. It uses a fresh bounded context so a
// cancelled request cannot leave a half-mounted tree behind.
func (p *coldProbe) release(label int) {
	cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
	defer cancel()
	f := p.r.Fixture
	start := f.Shared + label*f.TreeWidth
	_ = p.r.RequestDisposal(cleanup, start, start+f.TreeWidth)
	for i := start; i < start+f.TreeWidth; i++ {
		if id := p.r.IDs[i]; id != 0 {
			_ = p.r.Runtime.(cc.Waiter).AwaitFiber(cleanup, id)
		}
	}
	p.busy[label].Store(false)
}

// disposeTree is the normal churn disposal: one native BatchDispose of the live
// tree, joined per target. It returns once every fiber of the tree is retired.
func (p *coldProbe) disposeTree(ctx context.Context, label int) error {
	f := p.r.Fixture
	start := f.Shared + label*f.TreeWidth
	if err := p.r.BatchDisposal(ctx, nodeIndexes(start, start+f.TreeWidth)); err != nil {
		return err
	}
	waiter := p.r.Runtime.(cc.Waiter)
	var errs []error
	for i := start; i < start+f.TreeWidth; i++ {
		errs = append(errs, waiter.AwaitFiber(ctx, p.r.IDs[i]))
	}
	p.busy[label].Store(false)
	return errors.Join(errs...)
}

// schedule dispatches n open-loop arrivals at fixed intervals from start. fn runs
// on the dispatcher goroutine at (or after) each scheduled instant and must not
// block; the dispatcher never slows down, so lateness shows up in the records.
func schedule(ctx context.Context, start time.Time, interval time.Duration, n int, fn func(i int, scheduled, actual time.Time)) {
	for i := 0; i < n; i++ {
		at := start.Add(time.Duration(i) * interval)
		if wait := time.Until(at); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-t.C:
			case <-ctx.Done():
				t.Stop()
				return
			}
		}
		fn(i, at, time.Now())
	}
}

// RunColdArm runs one fresh-root arm: warm-up plus measured window, then a
// bounded drain, teardown and goroutine accounting. Failures are recorded in
// the ColdRun, never returned in place of the record.
func RunColdArm(ctx context.Context, backend Backend, plan ColdPlan, body BodySize, arm ColdArm, rep int) (run ColdRun) {
	run = ColdRun{Backend: backend.Name, Body: body, Arm: arm, Repetition: rep, StartedUTC: time.Now().UTC(), HostBefore: ReadHost()}
	defer func() {
		run.EndedUTC, run.HostAfter = time.Now().UTC(), ReadHost()
		run.Host = hostDelta(fmt.Sprintf("%s/%s/%s/rep-%02d", backend.Name, body.Name, arm, rep), run.HostBefore, run.HostAfter)
	}()
	baseline := checkpoint(nil, true)
	p, err := newColdProbe(ctx, backend, plan, body)
	if err != nil {
		run.Errors = append(run.Errors, "fixture: "+err.Error())
		return run
	}
	var (
		acksMu, churnMu sync.Mutex
		wg              sync.WaitGroup
		inflight        atomic.Int64
		churnInflight   atomic.Int64
	)
	reqInterval := time.Second / time.Duration(plan.RequestsPerSecond)
	churnInterval := time.Second / time.Duration(plan.ChurnPerSecond)
	totalReq, totalChurn := plan.ScheduledRequests(), plan.ScheduledChurn()
	run.Acks, run.Churn = make([]AckRecord, totalReq), make([]ChurnRecord, totalChurn)
	t0 := time.Now().Add(50 * time.Millisecond)
	off := func(t time.Time) int64 { return t.Sub(t0).Nanoseconds() }
	var drainStart atomic.Int64

	handle := func(i int, scheduled, actual time.Time) {
		rec := AckRecord{Seq: i, Warmup: scheduled.Sub(t0) < plan.Warmup, Label: i % plan.Labels, ScheduledNS: off(scheduled), StartedNS: off(actual)}
		if inflight.Add(1) > int64(plan.RequestCap) {
			inflight.Add(-1)
			// Request cap overflow: the arrival is recorded, never queued or dropped silently.
			rec.Outcome, rec.EndedNS, rec.LatencyNS = "overflow", off(actual), plan.RequestTimeout.Nanoseconds()
			acksMu.Lock()
			run.Acks[i] = rec
			acksMu.Unlock()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer inflight.Add(-1)
			deadline, cancel := context.WithDeadline(ctx, scheduled.Add(plan.RequestTimeout))
			defer cancel()
			var res AckResult
			var err error
			before := p.ensures.Load()
			if arm == ArmA {
				res, err = p.AckCold(deadline, i, 0, false)
			} else {
				res, err = p.AckPinned(deadline, i)
			}
			end := time.Now()
			rec.EndedNS, rec.LatencyNS = off(end), end.Sub(scheduled).Nanoseconds()
			rec.Status, rec.Committed, rec.MountNS = res.Status, res.Committed, res.MountNS
			rec.ColdEnsures = int(p.ensures.Load() - before)
			switch {
			case err != nil:
				rec.Outcome, rec.Error = "error", err.Error()
				if errors.Is(err, context.DeadlineExceeded) {
					rec.Outcome = "timeout"
				}
			case res.Status == 503:
				rec.Outcome = "unavailable503"
			case rec.LatencyNS > plan.RequestTimeout.Nanoseconds():
				rec.Outcome = "timeout" // committed late: still a failure, never trimmed.
			default:
				rec.Outcome = "ok"
			}
			if arm == ArmA && res.Committed {
				// The scope is disposed after the response, outside the latency.
				wg.Add(1)
				go func() {
					defer wg.Done()
					cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
					defer cancel()
					if err := p.disposeTree(cleanup, rec.Label); err != nil {
						acksMu.Lock()
						run.Errors = append(run.Errors, fmt.Sprintf("arm A dispose of label %d: %v", rec.Label, err))
						acksMu.Unlock()
						p.release(rec.Label)
					}
				}()
			}
			acksMu.Lock()
			run.Acks[i] = rec
			acksMu.Unlock()
		}()
	}

	churn := func(i int, scheduled, actual time.Time) {
		rec := ChurnRecord{Seq: i, Warmup: scheduled.Sub(t0) < plan.Warmup, Label: i % plan.Labels, ScheduledNS: off(scheduled), StartedNS: off(actual),
			StartLagNS: actual.Sub(scheduled).Nanoseconds(), SchedulerOnly: arm != ArmB1}
		save := func() {
			churnMu.Lock()
			run.Churn[i] = rec
			churnMu.Unlock()
		}
		if arm != ArmB1 {
			rec.Outcome = "idle(B0)" // the same scheduler, no mount started.
			save()
			return
		}
		// A late start means the offered churn rate was not sustained (§4): the mount
		// still runs, but the run fails.
		rec.Late = rec.StartLagNS > churnInterval.Nanoseconds()
		if churnInflight.Add(1) > int64(plan.ChurnCap) {
			churnInflight.Add(-1)
			rec.Outcome = "overflow"
			save()
			return
		}
		if !p.busy[rec.Label].CompareAndSwap(false, true) {
			churnInflight.Add(-1)
			rec.Outcome = "collision"
			save()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer churnInflight.Add(-1)
			mountCtx, cancel := context.WithTimeout(ctx, TrialGuard)
			defer cancel()
			begin := time.Now()
			err := p.r.MountTreeAwait(mountCtx, rec.Label)
			rec.MountedNS, rec.MountLatency, rec.MountedOK = off(time.Now()), time.Since(begin).Nanoseconds(), err == nil
			if err != nil {
				rec.Outcome, rec.Error = "error", err.Error()
				p.release(rec.Label)
				save()
				return
			}
			time.Sleep(plan.IdleBeforeDispose) // the tree is idle: no request uses it.
			rec.DisposeAtNS = off(time.Now())
			err = p.disposeTree(mountCtx, rec.Label)
			rec.DisposedNS, rec.DisposedOK, rec.Outcome = off(time.Now()), err == nil, "ok"
			if err != nil {
				rec.Outcome, rec.Error = "error", err.Error()
				p.release(rec.Label)
			}
			save()
		}()
	}

	sched := &sync.WaitGroup{}
	sched.Add(2)
	go func() { defer sched.Done(); schedule(ctx, t0, reqInterval, totalReq, handle) }()
	go func() { defer sched.Done(); schedule(ctx, t0, churnInterval, totalChurn, churn) }()
	sched.Wait()
	drainStart.Store(time.Now().UnixNano())
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(plan.Drain):
		run.Outstanding = int(inflight.Load() + churnInflight.Load())
		run.Errors = append(run.Errors, fmt.Sprintf("outstanding work %d after the %s drain", run.Outstanding, plan.Drain))
	}
	run.DrainNS = time.Now().UnixNano() - drainStart.Load()
	run.ColdEnsures = p.ensures.Load()
	p.mu.Lock()
	run.Ledger = len(p.ledger)
	for _, rec := range run.Acks {
		if c, ok := p.ledger[rec.Seq]; rec.Committed != ok || (ok && c.Seq != rec.Seq) {
			run.Errors = append(run.Errors, fmt.Sprintf("request %d: acknowledged-committed=%v but ledger has it=%v", rec.Seq, rec.Committed, ok))
			break
		}
	}
	committed := 0
	for _, rec := range run.Acks {
		if rec.Committed {
			committed++
		}
	}
	if committed != len(p.ledger) {
		run.Errors = append(run.Errors, fmt.Sprintf("ledger holds %d commits but %d requests were acknowledged-committed", len(p.ledger), committed))
	}
	p.mu.Unlock()
	cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
	defer cancel()
	if err := p.teardown(cleanup, &RunResult{}); err != nil {
		run.Errors = append(run.Errors, "teardown: "+err.Error())
	}
	after := checkpoint(nil, true)
	run.GoroutinesOK = true
	for owner, count := range after.Goroutines.Counts {
		if owner != "external" && count > baseline.Goroutines.Counts[owner] {
			run.GoroutinesOK = false
			run.Errors = append(run.Errors, fmt.Sprintf("candidate goroutine owner remains after root Close: %s count=%d baseline=%d", owner, count, baseline.Goroutines.Counts[owner]))
		}
	}
	return run
}

// ColdTimeoutResult is the outcome of the separate cold-timeout fixture.
type ColdTimeoutResult struct {
	Backend            string `json:"backend"`
	SyncStatus         int    `json:"syncStatus"`
	SyncRetryAfterNS   int64  `json:"syncRetryAfterNS"`
	SyncCommitted      bool   `json:"syncCommitted"`
	JobSnoozed         bool   `json:"jobSnoozed"`
	JobCommitted       bool   `json:"jobCommitted"`
	LedgerAfter        int    `json:"ledgerCommitsAfter"`
	ForeignContributed bool   `json:"foreignScopeContributed"`
	Warm               bool   `json:"warmOtherScopeMounted"`
	Error              string `json:"error,omitempty"`
}

// RunColdTimeoutFixture executes the cold-timeout correctness fixture once: a
// warm tree of ANOTHER scope exists; a request for a cold scope whose mount
// cannot finish inside the 50 ms cold-ensure budget must answer 503 with
// Retry-After (synchronous lookup) or snooze (job), commit nothing, and never read
// the warm foreign scope's contribution. No tenant cache is involved.
func RunColdTimeoutFixture(ctx context.Context, backend Backend, plan ColdPlan) (res ColdTimeoutResult) {
	res.Backend = backend.Name
	small := plan
	small.Labels = max(2, min(plan.Labels, 8))
	p, err := newColdProbe(ctx, backend, small, BodySize{Name: "timeout-fixture", Items: 8})
	if err != nil {
		res.Error = err.Error()
		return
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), TrialGuard)
		defer cancel()
		if err := p.teardown(cleanup, &RunResult{}); err != nil && res.Error == "" {
			res.Error = "teardown: " + err.Error()
		}
	}()
	// A warm foreign scope (label 1) holds a contribution a fall-through could read.
	if err := p.r.MountTreeAwait(ctx, 1); err != nil {
		res.Error = "warm scope: " + err.Error()
		return
	}
	p.busy[1].Store(true)
	res.Warm = true
	// The cold scope's real provider cannot finish loading inside the budget: its
	// body waits (cooperating with cancellation) until the fixture is torn down.
	never := make(chan struct{})
	p.r.Hold = func(tree int) <-chan struct{} {
		if tree == 0 {
			return never
		}
		return nil
	}
	budget := 50 * time.Millisecond
	label := 0 // seq 0 -> label 0; the warm label is 1.
	sync503, err := p.AckCold(ctx, label, budget, false)
	if err != nil {
		res.Error = "synchronous lookup: " + err.Error()
		return
	}
	res.SyncStatus, res.SyncRetryAfterNS, res.SyncCommitted = sync503.Status, sync503.RetryAfter.Nanoseconds(), sync503.Committed
	snooze, err := p.AckCold(ctx, small.Labels, budget, true) // seq == Labels -> label 0 again.
	if err != nil {
		res.Error = "job: " + err.Error()
		return
	}
	res.JobSnoozed, res.JobCommitted = snooze.Snoozed, snooze.Committed
	p.mu.Lock()
	res.LedgerAfter = len(p.ledger)
	for _, c := range p.ledger {
		if c.Label != label {
			res.ForeignContributed = true
		}
	}
	p.mu.Unlock()
	return
}

// Findings of the timeout fixture: any acknowledgement of an uncommitted write,
// any commit at all, or any read of a foreign scope is a candidate failure.
func (r ColdTimeoutResult) Findings() []Finding {
	var out []Finding
	if r.Error != "" {
		out = append(out, fail("%s cold-timeout fixture: %s", r.Backend, r.Error))
	}
	if r.SyncStatus != 503 || r.SyncRetryAfterNS <= 0 || r.SyncCommitted {
		out = append(out, fail("%s cold-timeout: synchronous lookup answered status %d retry-after %dns committed=%v, want 503 with Retry-After and no commit", r.Backend, r.SyncStatus, r.SyncRetryAfterNS, r.SyncCommitted))
	}
	if !r.JobSnoozed || r.JobCommitted {
		out = append(out, fail("%s cold-timeout: job snoozed=%v committed=%v, want snoozed without a commit", r.Backend, r.JobSnoozed, r.JobCommitted))
	}
	if r.LedgerAfter != 0 || r.ForeignContributed {
		out = append(out, fail("%s cold-timeout: ledger=%d foreign-scope contribution=%v, want an empty ledger and no fall-through", r.Backend, r.LedgerAfter, r.ForeignContributed))
	}
	if !r.Warm {
		out = append(out, inconclusive("%s cold-timeout: the foreign warm scope was not established", r.Backend))
	}
	return out
}
