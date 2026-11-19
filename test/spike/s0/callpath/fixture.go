// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// ErrOwnerInactive is the refusal of the provider's native Enter lease.
var ErrOwnerInactive = errors.New("callpath: contribution owner activation refused Enter")

// Outcome classifies one observation. Refusals are counted, never retried.
type Outcome uint8

const (
	OutcomeOK Outcome = iota
	OutcomeRefusedRead
	OutcomeRefusedEnter
	OutcomeError
)

var outcomeNames = [...]string{"ok", "refused_read", "refused_enter", "error"}

func (o Outcome) String() string { return outcomeNames[o] }
func (o Outcome) char() byte     { return '0' + byte(o) }

// Owner is one activation of the contribution owner (the leased provider).
type Owner struct {
	fx     *Fixture
	ctx    *co.Context
	Gen    uint64
	closed atomic.Bool // Set by the owner's native disposer, after its leases drained.
}

// Context is the provider's actual native activation wrapper (the lease holder).
func (o *Owner) Context() *co.Context { return o.ctx }

// Closed reports whether this activation's cleanup has run.
func (o *Owner) Closed() bool { return o.closed.Load() }

// BoundView is the value returned by the provider's view function. It is a
// value type, so no pointer is allocated per lookup. It retains the resolving
// consumer and the actual resource; Call leases the PROVIDER, not the consumer.
type BoundView struct {
	consumer *co.Context
	owner    *Owner
}

func (v BoundView) Consumer() *co.Context { return v.consumer }
func (v BoundView) Owner() *Owner         { return v.owner }

// Call is the actual contribution method: provider Enter, body, provider Leave.
func (v BoundView) Call() (uint64, error) {
	o := v.owner
	if !o.ctx.Enter() {
		return 0, ErrOwnerInactive
	}
	gen := o.fx.body(o)
	o.ctx.Leave()
	return gen, nil
}

func defaultBody(o *Owner) uint64 {
	if o.closed.Load() { // A body running after cleanup is an inactive-owner execution.
		o.fx.violations.Add(1)
	}
	return o.Gen
}

// Binding is one consumer activation, published atomically on every activation.
type Binding struct {
	Ctx *co.Context
	Gen uint64
}

type FixtureOptions struct {
	Leaves int
	// Gate runs inside the provider lease before the default body. Tests only;
	// measurements leave it nil so no extra code sits on the call path.
	Gate func(*Owner)
}

// Fixture is one fresh real candidate root with the composed topology:
// provider (leased contribution owner), consumer, and unrelated mounted leaves.
type Fixture struct {
	Name     string
	Seq      uint64 // Process-unique: a fresh fixture always has a new sequence number.
	Runtime  cc.Runtime
	Key      cm.Key[BoundView]
	Provider *co.Fiber
	Consumer *co.Fiber
	Leaves   []*co.Fiber

	owner      atomic.Pointer[Owner]
	binding    atomic.Pointer[Binding]
	violations atomic.Int64
	ownerGens  atomic.Uint64
	bindGens   atomic.Uint64
	body       func(*Owner) uint64
	mu         sync.Mutex
	changed    chan struct{}
	goroutines int
}

var fixtureSeq atomic.Uint64

func NewFixture(ctx context.Context, backend scale.Backend, opt FixtureOptions) (*Fixture, error) {
	fx := &Fixture{Name: backend.Name, Seq: fixtureSeq.Add(1), Runtime: backend.New(), changed: make(chan struct{}), body: defaultBody, goroutines: candidateGoroutines()}
	if opt.Gate != nil {
		gate := opt.Gate
		fx.body = func(o *Owner) uint64 { gate(o); return defaultBody(o) }
	}
	fail := func(err error) (*Fixture, error) {
		return nil, errors.Join(err, fx.Runtime.Close(context.WithoutCancel(ctx)))
	}
	entry := cm.NewEntry("callpath")
	fx.Key = cm.NewKey[BoundView]("contribution", 1)
	auth, err := co.NewRuntime(fx.Runtime, cm.RootSpec{Entries: []cm.Plugin{entry}, Keys: []cm.AnyKey{fx.Key.Any()}})
	if err != nil {
		return fail(err)
	}
	root := auth.Context()
	fx.Provider, err = auth.Start(ctx, root, entry, co.Spec{
		Metadata: cm.Spec{ID: "contribution-owner", Provides: []cm.AnyKey{fx.Key.Any()}},
		Apply: func(c *co.Context) (cc.Disposer, error) {
			o := &Owner{fx: fx, ctx: c, Gen: fx.ownerGens.Add(1)}
			// Built once per activation; Resolve calls only invoke it.
			view := func(caller *co.Context) BoundView { return BoundView{consumer: caller, owner: o} }
			if err := co.ProvideFor(c, fx.Key, view, nil); err != nil {
				return nil, err
			}
			fx.owner.Store(o)
			return func(context.Context) error { o.closed.Store(true); return nil }, nil
		}})
	if err != nil {
		return fail(err)
	}
	fx.Consumer, err = auth.Start(ctx, root, entry, co.Spec{
		Metadata: cm.Spec{ID: "contribution-consumer", Inject: []cm.Dep{cm.Need(fx.Key)}},
		Apply: func(c *co.Context) (cc.Disposer, error) {
			fx.publish(&Binding{Ctx: c, Gen: fx.bindGens.Add(1)})
			return nil, nil
		}})
	if err != nil {
		return fail(err)
	}
	for i := range opt.Leaves {
		leaf, err := auth.Start(ctx, root, entry, co.Spec{Metadata: cm.Spec{ID: fmt.Sprintf("leaf-%03d", i)}})
		if err != nil {
			return fail(err)
		}
		fx.Leaves = append(fx.Leaves, leaf)
	}
	for _, f := range append([]*co.Fiber{fx.Provider, fx.Consumer}, fx.Leaves...) {
		if err := f.Await(ctx); err != nil {
			return fail(err)
		}
	}
	if err := fx.verifyActive(ctx); err != nil {
		return fail(err)
	}
	return fx, nil
}

func (fx *Fixture) publish(b *Binding) {
	fx.binding.Store(b)
	fx.mu.Lock()
	close(fx.changed)
	fx.changed = make(chan struct{})
	fx.mu.Unlock()
}

// Binding returns the current consumer activation (nil before the first).
func (fx *Fixture) Binding() *Binding { return fx.binding.Load() }
func (fx *Fixture) Owner() *Owner     { return fx.owner.Load() }

// Violations counts bodies that observed their owner already cleaned up.
func (fx *Fixture) Violations() int64 { return fx.violations.Load() }

// WaitBindingGen blocks until a binding newer than gen is published.
func (fx *Fixture) WaitBindingGen(ctx context.Context, gen uint64) error {
	for {
		fx.mu.Lock()
		changed := fx.changed
		fx.mu.Unlock()
		if b := fx.binding.Load(); b != nil && b.Gen > gen {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (fx *Fixture) verifyActive(ctx context.Context) error {
	in, err := fx.Runtime.(cc.Inspector).Inspect(ctx)
	if err != nil {
		return err
	}
	want := 2 + len(fx.Leaves)
	active := 0
	for _, f := range in.Fibers {
		if f.State == cc.Active && f.Alive {
			active++
		}
	}
	if active != want || len(in.Provisions) != 1 || !in.Provisions[0].Active {
		return fmt.Errorf("fixture not fully ACTIVE: active=%d want=%d provisions=%d", active, want, len(in.Provisions))
	}
	return nil
}

// Close closes the real root and reports a candidate goroutine that outlives it.
func (fx *Fixture) Close(ctx context.Context) error {
	if err := fx.Runtime.Close(ctx); err != nil {
		return err
	}
	for {
		if candidateGoroutines() <= fx.goroutines {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("candidate goroutines remain after Close: %d, baseline %d", candidateGoroutines(), fx.goroutines)
		default:
			runtime.Gosched() // Worker exit after Close is observed, not slept on.
		}
	}
}

// candidateGoroutines counts goroutines owned by a candidate or its event queue.
func candidateGoroutines() int {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			count := 0
			for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
				if strings.Contains(stack, "/test/spike/s0/concurrency.") || strings.Contains(stack, "/test/spike/s0/internal/eventqueue.") {
					count++
				}
			}
			return count
		}
		buf = make([]byte, len(buf)*2)
	}
}

// classify maps a call-path error to its outcome class.
func classify(err error) Outcome {
	switch {
	case err == nil:
		return OutcomeOK
	case errors.Is(err, ErrOwnerInactive):
		return OutcomeRefusedEnter
	case errors.Is(err, cc.ErrInactive):
		return OutcomeRefusedRead
	}
	return OutcomeError
}

// Op is one observed call-path operation: no retry, no filtering.
type Op func() (Outcome, uint64)

func noop() (Outcome, uint64) { return OutcomeOK, 0 }

// Operation returns the measured function for op. Timer is the empty body.
func (fx *Fixture) Operation(op Operation) Op {
	switch op {
	case LookupView:
		return func() (Outcome, uint64) {
			b := fx.binding.Load()
			if b == nil {
				return OutcomeError, 0
			}
			v, err := co.Resolve(b.Ctx, fx.Key)
			if err != nil {
				return classify(err), 0
			}
			if v.owner == nil || v.consumer != b.Ctx {
				return OutcomeError, 0
			}
			return OutcomeOK, v.owner.Gen
		}
	case Lease:
		return func() (Outcome, uint64) {
			o := fx.owner.Load()
			if o == nil {
				return OutcomeError, 0
			}
			if !o.ctx.Enter() {
				return OutcomeRefusedEnter, 0
			}
			o.ctx.Leave()
			return OutcomeOK, o.Gen
		}
	case Dispatch:
		return func() (Outcome, uint64) {
			b := fx.binding.Load()
			if b == nil {
				return OutcomeError, 0
			}
			v, err := co.Resolve(b.Ctx, fx.Key)
			if err != nil {
				return classify(err), 0
			}
			gen, err := v.Call()
			return classify(err), gen
		}
	}
	return noop
}

// ProvisionRecord and FiberRecord are causal checkpoint snapshots taken from the
// candidate's actual Inspect, never from expected fixture data.
type ProvisionRecord struct {
	Key    string `json:"key"`
	UID    uint64 `json:"uid"`
	Owner  uint64 `json:"ownerFiber"`
	Active bool   `json:"active"`
}

type FiberRecord struct {
	ID        uint64   `json:"id"`
	Name      string   `json:"name"`
	State     cc.State `json:"state"`
	Revision  uint64   `json:"revision"`
	Epoch     []uint64 `json:"epoch"`
	TargetUID []uint64 `json:"targetUIDs"`
}

type Checkpoint struct {
	Label      string            `json:"label"`
	Item       int               `json:"item"`
	AtNS       int64             `json:"atNS"`
	OwnerGen   uint64            `json:"ownerActivationGen"`
	BindingGen uint64            `json:"bindingGen"`
	Violations int64             `json:"inactiveBodyExecutions"`
	Provisions []ProvisionRecord `json:"provisions"`
	Fibers     []FiberRecord     `json:"providerAndConsumer"`
	LeafStates map[string]int    `json:"leafStates"`
	Error      string            `json:"error,omitempty"`
}

func (fx *Fixture) Checkpoint(ctx context.Context, label string, item int, atNS int64) Checkpoint {
	cp := Checkpoint{Label: label, Item: item, AtNS: atNS, OwnerGen: fx.ownerGens.Load(), BindingGen: fx.bindGens.Load(),
		Violations: fx.violations.Load(), LeafStates: make(map[string]int)}
	in, err := fx.Runtime.(cc.Inspector).Inspect(ctx)
	if err != nil {
		cp.Error = err.Error()
		return cp
	}
	for _, p := range in.Provisions {
		cp.Provisions = append(cp.Provisions, ProvisionRecord{Key: p.Key, UID: p.UID, Owner: p.Owner, Active: p.Active})
	}
	for _, f := range in.Fibers {
		if f.ID == fx.Provider.ID || f.ID == fx.Consumer.ID {
			r := FiberRecord{ID: f.ID, Name: f.Name, State: f.State, Revision: f.Revision, Epoch: f.Epoch}
			if f.Target != nil {
				r.TargetUID = f.Target.UIDs
			}
			cp.Fibers = append(cp.Fibers, r)
		} else {
			cp.LeafStates[string(f.State)]++
		}
	}
	return cp
}
