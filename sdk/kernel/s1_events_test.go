// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

// Tests of the three kernel events (ADR-059 decision 2): the mapping from the
// ledger, the one order, the rule that a listener cannot hold the kernel, the
// overflow notice, and the cost of having no listener.

type hookSeen struct {
	hook string
	ev   LedgerEvent
}

// hookLog records what the listeners of the three hooks are given.
type hookLog struct {
	mu   sync.Mutex
	seen []hookSeen
}

// listen registers one root listener on each of the three hooks. after, when it is
// not nil, runs in the listener once the event is recorded.
func (l *hookLog) listen(r *Root, after func(hook string, e LedgerEvent)) {
	for _, h := range []*Event[LedgerEvent]{InternalPlugin, InternalStatus, InternalService} {
		h.On(r.Context(), DefaultStage, func(_ context.Context, e LedgerEvent) (any, error) {
			l.mu.Lock()
			l.seen = append(l.seen, hookSeen{h.Name(), e})
			l.mu.Unlock()
			if after != nil {
				after(h.Name(), e)
			}
			return nil, nil
		})
	}
}

func (l *hookLog) all() []hookSeen {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.seen)
}

func (l *hookLog) has(kind string) bool {
	return slices.ContainsFunc(l.all(), func(s hookSeen) bool { return s.ev.Kind == kind })
}

// hooksOf is the table of InternalStatus's documentation.
func hooksOf(e LedgerEvent) []string {
	switch e.Kind {
	case "mount":
		return []string{InternalPlugin.Name()}
	case "state":
		if e.State == Disposed {
			return []string{InternalStatus.Name(), InternalPlugin.Name()}
		}
		return []string{InternalStatus.Name()}
	case "provide", "service", "remove":
		return []string{InternalService.Name()}
	}
	return nil
}

func TestKernelEventsFollowTheLedgerInOneOrder(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	var log hookLog
	log.listen(r, nil)
	from := r.RetentionStatus().EventsTotal

	k := NewKey[int]("s1.events", 1)
	prov := r.Plugin(plug("s1/evprov", nil, []AnyKey{k}, func(c *Context) error { return k.Provide(c, 1) }), nil)
	want(t, prov, Active)
	cons := r.Plugin(plug("s1/evcons", []Dep{Need(k)}, nil, nil), nil)
	want(t, cons, Active)
	if err := r.Notify(ctx, k); err != nil {
		t.Fatal(err)
	}
	if err := prov.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := prov.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cons.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cons.Await(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}

	// What the listeners saw is the ledger after the first listener, mapped by Kind,
	// in the order of Sequence; an event that maps to two hooks reaches Status first.
	ledger, err := r.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	accepted := r.ObserverStatus().Accepted
	var expected []hookSeen
	for _, e := range ledger {
		if e.Sequence > from && e.Sequence <= accepted {
			for _, h := range hooksOf(e) {
				expected = append(expected, hookSeen{h, e})
			}
		}
	}
	got := log.all()
	if !slices.Equal(got, expected) {
		t.Fatalf("listeners saw\n%v\nwant\n%v", got, expected)
	}
	// Each hook was reached by each of its kinds.
	reached := make(map[string]bool)
	for _, s := range got {
		reached[s.hook+" "+s.ev.Kind+" "+string(s.ev.State)] = true
	}
	for _, w := range []string{
		"internal/plugin mount PENDING", "internal/status state ACTIVE", "internal/plugin state DISPOSED",
		"internal/service provide LOADING", "internal/service service ACTIVE", "internal/service remove UNLOADING",
	} {
		if !reached[w] {
			t.Errorf("no event %q in %v", w, reached)
		}
	}
}

func TestKernelEventsWithNoListenerSubscribeToNothing(t *testing.T) {
	r := newKernel(t)
	ctx := frontCtx(t)
	want(t, r.Plugin(plug("s1/quiet", nil, nil, nil), nil), Active)
	if err := r.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	if st := r.ObserverStatus(); st.Observers != 0 || st.Callbacks != 0 {
		t.Fatalf("a kernel with no listener has a subscription: %+v", st)
	}
}

func TestAFailingListenerOfAKernelEventIsSkippedAndLogged(t *testing.T) {
	var mu sync.Mutex
	var logged []error
	cfg := frontConfig()
	cfg.ObserverLog = func(err error) { mu.Lock(); logged = append(logged, err); mu.Unlock() }
	r := NewForTest(t, cfg)
	ctx := frontCtx(t)
	InternalStatus.On(r.Context(), DefaultStage, func(context.Context, LedgerEvent) (any, error) { panic("listener panics") })
	InternalStatus.On(r.Context(), DefaultStage, func(context.Context, LedgerEvent) (any, error) { return nil, errors.New("listener fails") })
	var log hookLog
	log.listen(r, nil)

	f := r.Plugin(plug("s1/unbothered", nil, nil, nil), nil)
	want(t, f, Active)
	if err := r.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	if !log.has("state") {
		t.Fatal("the listener after the failing ones saw no state event")
	}
	mu.Lock()
	defer mu.Unlock()
	var panics, fails int
	for _, err := range logged {
		var le *ListenerError
		var pe *PanicError
		if !errors.As(err, &le) {
			t.Fatalf("logged %v, not a ListenerError", err)
		}
		if errors.As(err, &pe) {
			panics++
		} else {
			fails++
		}
	}
	if panics == 0 || fails == 0 {
		t.Fatalf("logged %d panics and %d failures: %v", panics, fails, logged)
	}
}

func TestAListedPluginListensOnAKernelEvent(t *testing.T) {
	got := make(chan LedgerEvent, 256)
	listed := Define(Plugin{ID: "s1/listed", Hooks: []HookUse{InternalService.Use(DefaultStage)}, Apply: func(c *Context, _ any) error {
		InternalService.On(c, DefaultStage, func(_ context.Context, e LedgerEvent) (any, error) { got <- e; return nil, nil })
		return nil
	}})
	r, err := newListening(t, map[AnyHook][]Plugin{InternalService: {listed}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := frontCtx(t)
	want(t, r.Plugin(listed, nil), Active)
	k := NewKey[int]("s1.listed", 1)
	want(t, r.Plugin(plug("s1/listedprov", nil, []AnyKey{k}, func(c *Context) error { return k.Provide(c, 1) }), nil), Active)
	for {
		select {
		case e := <-got:
			if e.Kind == "provide" && e.Key == "s1.listed@1" {
				return
			}
		case <-ctx.Done():
			t.Fatal("the listed plugin was not given the provision event")
		}
	}
}

func TestKernelEventOverflowIsVisibleToTheListeners(t *testing.T) {
	cfg := frontConfig()
	cfg.ObserverQueue = 4
	r := NewForTest(t, cfg)
	ctx := frontCtx(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var log hookLog
	// The first event delivered stalls its listener. The dispatcher is then inside
	// the relay, so the events that follow overflow the queue of four.
	log.listen(r, func(string, LedgerEvent) { once.Do(func() { close(entered); <-release }) })
	// Drained, the dispatcher is inside no callback, so no event before the stall can
	// overflow the queue and be lost.
	if err := r.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}

	// A listener that stalls does not hold the owner: fibers keep mounting.
	want(t, r.Plugin(plug("s1/ov0", nil, nil, nil), nil), Active)
	<-entered
	for i := 1; i <= 4; i++ {
		want(t, r.Plugin(plug(fmt.Sprintf("s1/ov%d", i), nil, nil, nil), nil), Active)
	}
	if st := r.ObserverStatus(); st.Overflowed != 1 || st.Observers != 0 {
		t.Fatalf("a stalled listener did not detach the relay: %+v", st)
	}
	close(release)

	// The relay subscribes again; events go on until the first one it delivers, which
	// is preceded by the notice.
	for i := 0; !log.has("overflow"); i++ {
		if ctx.Err() != nil {
			t.Fatal("no overflow notice")
		}
		want(t, r.Plugin(plug(fmt.Sprintf("s1/after%d", i), nil, nil, nil), nil), Active)
		if err := r.AwaitObservers(ctx); err != nil && !errors.Is(err, ErrObserverOverflow) {
			t.Fatal(err)
		}
	}
	if err := r.AwaitObservers(ctx); !errors.Is(err, ErrObserverOverflow) {
		t.Fatalf("AwaitObservers after an overflow: %v", err)
	}
	seen := log.all()
	at := slices.IndexFunc(seen, func(s hookSeen) bool { return s.ev.Kind == "overflow" })
	if at < 0 || at+3 >= len(seen) {
		t.Fatalf("overflow notice at %d of %d", at, len(seen))
	}
	notice := seen[at].ev
	for i, h := range []string{InternalPlugin.Name(), InternalStatus.Name(), InternalService.Name()} {
		if s := seen[at+i]; s.hook != h || s.ev != notice {
			t.Fatalf("notice %d is %+v, want %s %+v", i, s, h, notice)
		}
	}
	// The notice names the sequence just before the first event delivered after it.
	if next := seen[at+3].ev; next.Kind == "overflow" || next.Sequence != notice.Sequence+1 {
		t.Fatalf("event after the notice %+v, notice %+v", next, notice)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i].ev.Sequence < seen[i-1].ev.Sequence {
			t.Fatalf("sequence went back at %d: %+v then %+v", i, seen[i-1], seen[i])
		}
	}
	// A listener resyncs from Inspect: its watermark is not below the notice.
	view, err := r.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.Sequence < notice.Sequence {
		t.Fatalf("Inspect sequence %d is below the notice %d", view.Sequence, notice.Sequence)
	}
}

// The relay leaves no effect on the root per subscription: however often a listener
// overflows it, the root's effects do not grow.
func TestRelayResubscriptionLeavesNoEffectOnTheRoot(t *testing.T) {
	cfg := frontConfig()
	cfg.ObserverQueue = 4
	r := NewForTest(t, cfg)
	ctx := frontCtx(t)
	var mu sync.Mutex
	var gate chan struct{} // Set: the next listener call stalls on it.
	entered := make(chan struct{}, 1)
	for _, h := range []*Event[LedgerEvent]{InternalPlugin, InternalStatus, InternalService} {
		h.On(r.Context(), DefaultStage, func(context.Context, LedgerEvent) (any, error) {
			mu.Lock()
			g := gate
			gate = nil
			mu.Unlock()
			if g != nil {
				entered <- struct{}{}
				<-g
			}
			return nil, nil
		})
	}
	rootEffects := func() int {
		t.Helper()
		events, err := r.Events(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range events {
			if e.Fiber == 0 && e.Kind == "effect" && e.Label == "observer kernel/events" {
				n++
			}
		}
		return n
	}
	if err := r.AwaitObservers(ctx); err != nil {
		t.Fatal(err)
	}
	before := rootEffects()
	mounts := 0
	mount := func() {
		t.Helper()
		if mounts > 1000 {
			t.Fatalf("no progress after %d mounts: %+v", mounts, r.ObserverStatus())
		}
		want(t, r.Plugin(plug(fmt.Sprintf("s1/relay%d", mounts), nil, nil, nil), nil), Active)
		mounts++
	}
	for cycle := uint64(1); cycle <= 3; cycle++ {
		g := make(chan struct{})
		mu.Lock()
		gate = g
		mu.Unlock()
		// Mount until the relay stalls, then until that stall detaches it.
		for stalled := false; !stalled; {
			mount()
			select {
			case <-entered:
				stalled = true
			default:
			}
		}
		for r.ObserverStatus().Overflowed < cycle {
			mount()
		}
		close(g)
		// The relay subscribes again once the stalled callback has returned and its
		// goroutine is gone; each mount paces the wait.
		for st := r.ObserverStatus(); st.Observers == 0 || st.Orphaned != 0; st = r.ObserverStatus() {
			mount()
		}
	}
	if after := rootEffects(); after != before {
		t.Fatalf("the root holds %d relay effects after three overflows, %d before", after, before)
	}
}
