// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Fuzz targets for the places where the kernel validates what a caller supplies
// and for the lifecycle itself. Under a plain `go test` each target runs its seed
// corpus and the inputs kept under testdata/fuzz, which keeps every failing input
// a regression test. A target is run for its time budget with
//
//	go test -run '^$' -fuzz '^FuzzXxx$' -fuzztime 60s .

// fuzzConfig is a valid configuration with small capacities and short deadlines,
// so that an execution that blocks a fiber ends within milliseconds.
func fuzzConfig() Config {
	return Config{
		LoadDeadline: 20 * time.Millisecond, DrainDeadline: 20 * time.Millisecond, DisposeDeadline: 20 * time.Millisecond,
		ArchiveLimit: 4, EventWindow: 64, ObserverQueue: 32, OwnerInbox: 8, TerminalLimit: 4,
	}
}

// attempt runs fn and reports the value it panicked with, if it did.
func attempt[T any](fn func() T) (v T, panicked bool, value any) {
	defer func() {
		if p := recover(); p != nil {
			panicked, value = true, p
		}
	}()
	return fn(), false, nil
}

// closeKernel closes r and fails the input when Close does not return in time.
func closeKernel(t *testing.T, r *Root) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := r.Close(ctx)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		t.Fatalf("Close did not return within the bound: %v", err)
	}
	return err
}

func expectErrors(t *testing.T, what string, err error, want ...error) {
	t.Helper()
	if len(want) == 0 {
		if err != nil {
			t.Fatalf("%s: unexpected error %v", what, err)
		}
		return
	}
	if err == nil {
		t.Fatalf("%s: accepted, want %v", what, want)
	}
	for _, w := range want {
		if !errors.Is(err, w) {
			t.Fatalf("%s: %v does not match %v", what, err, w)
		}
	}
}

// build is New with a closing step for the accepted case.
func build(t *testing.T, cfg Config) error {
	t.Helper()
	r, err := New(cfg)
	if err == nil {
		if r == nil {
			t.Fatal("New returned neither a kernel nor an error")
		}
		_ = closeKernel(t, r)
	} else if r != nil {
		t.Fatalf("New returned a kernel with the error %v", err)
	}
	return err
}

// FuzzNewKey covers key construction and the provider and consumer binding maps
// that New checks.
func FuzzNewKey(f *testing.F) {
	f.Add("svc", 1, uint8(0))
	f.Add("svc", 0, uint8(1))
	f.Add("", 0, uint8(0))
	f.Add("x", -1, uint8(3))
	f.Add("a@b/c", 1<<30, uint8(7))
	f.Add("\xff\x00\xfe", 2, uint8(0x1f))
	f.Add("bound", 3, uint8(0x0b))
	f.Fuzz(func(t *testing.T, name string, major int, flags uint8) {
		var opts []KeyOption
		for bit, opt := range []KeyOption{ProviderBound(), ConsumerBound(), Signed(), InterceptConfig[int]()} {
			if flags&(1<<bit) != 0 {
				opts = append(opts, opt)
			}
		}
		key, panicked, value := attempt(func() Key[int] { return NewKey[int](name, major, opts...) })
		if want := name == "" || major < 0; panicked != want {
			t.Fatalf("NewKey(%q, %d) panicked=%v, want %v", name, major, panicked, want)
		}
		if panicked {
			if msg, _ := value.(string); !strings.HasPrefix(msg, "kernel.NewKey: invalid name") {
				t.Fatalf("NewKey panicked with %#v, not its documented message", value)
			}
			return
		}
		if key.Name() != name || key.Major() != major {
			t.Fatalf("key reports %q@%d, built as %q@%d", key.Name(), key.Major(), name, major)
		}
		providerBound := flags&1 != 0
		consumerBound := flags&6 != 0
		defined := Define(Plugin{ID: "defined"})
		undefined := Plugin{ID: "undefined"}
		alias := Key[string]{k: key.k} // A second AnyKey value for the same key.

		cfg := fuzzConfig()
		cfg.Providers = Bindings{key: {defined}}
		if providerBound {
			expectErrors(t, "provider binding", build(t, cfg))
		} else {
			expectErrors(t, "provider binding of an open key", build(t, cfg), ErrBinding)
		}
		cfg.Providers = nil
		cfg.Consumers = Bindings{key: {defined}}
		if consumerBound {
			expectErrors(t, "consumer binding", build(t, cfg))
		} else {
			expectErrors(t, "consumer binding of an open key", build(t, cfg), ErrBinding)
		}
		if providerBound {
			cfg = fuzzConfig()
			cfg.Providers = Bindings{key: {undefined}}
			expectErrors(t, "undefined entry", build(t, cfg), ErrBinding)
			cfg.Providers = Bindings{key: {defined}, alias: {defined}}
			expectErrors(t, "key listed twice", build(t, cfg), ErrDuplicate)
		}
		cfg = fuzzConfig()
		cfg.Providers = Bindings{nil: {defined}}
		cfg.Consumers = Bindings{Key[int]{}: {defined}}
		expectErrors(t, "nil and zero keys", build(t, cfg), ErrKey)
	})
}

// FuzzHookDefinition covers NewEvent and NewWaterfall, the stages of a hook, the
// HookUse checks and the listener binding map that New checks.
func FuzzHookDefinition(f *testing.F) {
	f.Add("evt", "a,b", "a", uint8(0))
	f.Add("evt", "", "default", uint8(0x10))
	f.Add("internal/evt", "a", "a", uint8(0))
	f.Add("", "a", "a", uint8(0))
	f.Add("evt", "a,a", "a", uint8(1))
	f.Add("evt", "a,", "", uint8(1))
	f.Add("wf", "x,y", "z", uint8(0x0b))
	f.Add("wf", "x", "x", uint8(0x07))
	f.Add("\xff", "\x00", "\x00", uint8(0x3f))
	f.Fuzz(func(t *testing.T, name, stageList, use string, flags uint8) {
		owner := NewKey[int]("owner", 1)
		var opts []HookOption
		if flags&1 == 0 {
			opts = append(opts, OwnedBy(owner))
		}
		var stages []string
		if flags&0x10 == 0 {
			stages = strings.Split(stageList, ",")
			opts = append(opts, Stages(stages...))
		}
		sealed := flags&2 != 0
		if sealed {
			opts = append(opts, Sealed())
		}
		if flags&4 != 0 {
			opts = append(opts, Scoped())
		}
		wantPanic := name == "" || strings.HasPrefix(name, "internal/") || flags&1 != 0
		for i, s := range stages {
			wantPanic = wantPanic || s == "" || slices.Contains(stages[:i], s)
		}
		var hook AnyHook
		var u HookUse
		_, panicked, _ := attempt(func() struct{} {
			if flags&8 == 0 {
				e := NewEvent[int](name, opts...)
				hook, u = e, e.Use(use)
			} else {
				w := NewWaterfall[int, int](name, opts...)
				hook, u = w, w.Use(use)
			}
			return struct{}{}
		})
		if panicked != wantPanic {
			t.Fatalf("hook %q stages %q flags %#x: panicked=%v, want %v", name, stages, flags, panicked, wantPanic)
		}
		if panicked {
			return
		}
		if hook.Name() != name || u.Hook() != name {
			t.Fatalf("hook names %q and %q, defined as %q", hook.Name(), u.Hook(), name)
		}
		known := slices.Contains(stages, use) || (stages == nil && use == DefaultStage)
		wantStage := ""
		if known {
			wantStage = use
		}
		if u.Stage() != wantStage {
			t.Fatalf("stage %q of %q reads back as %q (known=%v)", use, stages, u.Stage(), known)
		}
		if known {
			expectErrors(t, "declared use", checkUses([]HookUse{u}))
			expectErrors(t, "repeated use", checkUses([]HookUse{u, u}), ErrDuplicate)
		} else {
			expectErrors(t, "unknown stage", checkUses([]HookUse{u}), ErrStage)
		}
		expectErrors(t, "zero use", checkUses([]HookUse{{}}), ErrHook)

		defined := Define(Plugin{ID: "defined"})
		cfg := fuzzConfig()
		cfg.Listeners = map[AnyHook][]Plugin{hook: {defined}}
		if sealed {
			expectErrors(t, "listener binding", build(t, cfg))
			cfg.Listeners = map[AnyHook][]Plugin{hook: {defined, defined}}
			expectErrors(t, "entry listed twice", build(t, cfg), ErrDuplicate)
			cfg.Listeners = map[AnyHook][]Plugin{hook: {{ID: "undefined"}}}
			expectErrors(t, "undefined entry", build(t, cfg), ErrBinding)
		} else {
			expectErrors(t, "listener binding of a hook that is not sealed", build(t, cfg), ErrSealed)
		}
		cfg.Listeners = map[AnyHook][]Plugin{nil: {defined}}
		expectErrors(t, "nil hook", build(t, cfg), ErrHook)
	})
}

// FuzzConfig covers the validation of the numbers New takes: each must be positive,
// and every refusal names the number.
func FuzzConfig(f *testing.F) {
	f.Add(int64(1), int64(1), int64(1), 1, 1, 1, 1, 1)
	f.Add(int64(time.Second), int64(time.Second), int64(time.Second), 8, 8, 8, 8, 8)
	f.Add(int64(0), int64(1), int64(1), 1, 1, 1, 1, 1)
	f.Add(int64(-1), int64(-1), int64(-1), -1, -1, -1, -1, -1)
	f.Add(int64(1), int64(1), int64(1), 0, 1, 1, 1, 1)
	f.Add(int64(1), int64(1), int64(1), 1, 1, 1, 1, -4)
	f.Add(int64(1<<62), int64(1), int64(-1<<62), 3, 1<<20, 2, 0, 1)
	f.Fuzz(func(t *testing.T, load, drain, dispose int64, archive, window, queue, inbox, terminal int) {
		// The sizes allocate: clamp them, keeping sign and zero.
		clamp := func(n int) int { return n % 4096 }
		cfg := Config{
			LoadDeadline: time.Duration(load), DrainDeadline: time.Duration(drain), DisposeDeadline: time.Duration(dispose),
			ArchiveLimit: clamp(archive), EventWindow: clamp(window), ObserverQueue: clamp(queue), OwnerInbox: clamp(inbox), TerminalLimit: clamp(terminal),
		}
		fields := []struct {
			name  string
			value int64
		}{
			{"loadDeadline", load}, {"drainDeadline", drain}, {"disposeDeadline", dispose},
			{"archiveLimit", int64(cfg.ArchiveLimit)}, {"eventWindow", int64(cfg.EventWindow)}, {"observerQueue", int64(cfg.ObserverQueue)},
			{"ownerInbox", int64(cfg.OwnerInbox)}, {"terminalLimit", int64(cfg.TerminalLimit)},
		}
		err := build(t, cfg)
		var bad []string
		for _, field := range fields {
			if field.value <= 0 {
				bad = append(bad, field.name)
			}
		}
		if len(bad) == 0 {
			expectErrors(t, "valid configuration", err)
			return
		}
		if err == nil {
			t.Fatalf("configuration with non-positive %v accepted", bad)
		}
		for _, field := range fields {
			named := strings.Contains(err.Error(), "coreConfig."+field.name+" must be positive")
			if named != (field.value <= 0) {
				t.Fatalf("%s=%d: reported=%v in %q", field.name, field.value, named, err)
			}
		}
	})
}

// A decodeInput is the raw config of FuzzConfigPipeline: its mode says what the
// pure Decode makes of it.
type decodeInput struct {
	Mode    uint8
	Payload string
}

var errDecoded = errors.New("decode refused")

// decodeOf is the Decode of the fuzz plugin and what it must produce.
func decodeOf(raw any) (any, error) {
	in, ok := raw.(decodeInput)
	if !ok {
		return raw, nil
	}
	switch in.Mode % 6 {
	case 0:
		return raw, nil
	case 1:
		return in.Payload, nil
	case 2:
		return nil, nil
	case 3:
		return nil, fmt.Errorf("%w: %s", errDecoded, in.Payload)
	case 4:
		panic(in.Payload)
	default:
		panic(fmt.Errorf("%w: %s", errDecoded, in.Payload))
	}
}

// expectedDecode is what the pipeline must report for raw: the value, whether it
// fails and the check that the failure carries the right identity.
func expectedDecode(raw decodeInput) (value any, failed bool, identity func(error) bool) {
	switch raw.Mode % 6 {
	case 0:
		return raw, false, nil
	case 1:
		return raw.Payload, false, nil
	case 2:
		return nil, false, nil
	case 3:
		return nil, true, func(err error) bool { return errors.Is(err, errDecoded) && strings.Contains(err.Error(), raw.Payload) }
	case 4:
		return nil, true, func(err error) bool {
			var p *PanicError
			return errors.As(err, &p) && p.Value == raw.Payload
		}
	}
	return nil, true, func(err error) bool {
		var p *PanicError
		return errors.As(err, &p) && errors.Is(err, errDecoded)
	}
}

// FuzzConfigPipeline drives a fuzzed raw value through the config pipeline of a
// fiber, at mount and at Update: Decode may return any value, an error or a panic,
// and the kernel must contain it and keep an active fiber on its last config when
// an Update is refused.
func FuzzConfigPipeline(f *testing.F) {
	f.Add(uint8(0), "a", uint8(1), "b")
	f.Add(uint8(3), "bad", uint8(0), "ok")
	f.Add(uint8(4), "boom", uint8(4), "again")
	f.Add(uint8(5), "", uint8(2), "")
	f.Add(uint8(1), "x", uint8(3), "y")
	f.Add(uint8(0), "\xff", uint8(5), "\x00")
	f.Fuzz(func(t *testing.T, mode1 uint8, payload1 string, mode2 uint8, payload2 string) {
		// This target checks decoding, not deadlines: under many fuzz workers a
		// 20 ms load deadline can pass before Decode is even scheduled, and the
		// kernel then rightly reports the deadline instead of the decode error.
		cfg := fuzzConfig()
		cfg.LoadDeadline, cfg.DrainDeadline, cfg.DisposeDeadline = time.Minute, time.Minute, time.Minute
		r, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = closeKernel(t, r) }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var mu sync.Mutex
		var applied []any
		p := Define(Plugin{ID: "fuzz/cfg", Decode: decodeOf, Apply: func(_ *Context, cfg any) error {
			mu.Lock()
			applied = append(applied, cfg)
			mu.Unlock()
			return nil
		}})
		first, second := decodeInput{mode1, payload1}, decodeInput{mode2, payload2}
		fiber := r.Plugin(p, first)
		err = r.Settle(ctx)
		want, failed, identity := expectedDecode(first)
		state := func() State {
			s, serr := fiber.State(ctx)
			if serr != nil {
				t.Fatal(serr)
			}
			return s
		}
		if failed {
			if err == nil || !identity(err) || state() != Failed {
				t.Fatalf("decode failure of %+v: Settle=%v state=%s", first, err, state())
			}
		} else if err != nil || state() != Active || len(applied) != 1 || !reflect.DeepEqual(applied[0], want) {
			t.Fatalf("decode of %+v: Settle=%v state=%s applied=%v want %#v", first, err, state(), applied, want)
		}

		before := len(applied)
		uerr := fiber.Update(ctx, second)
		want, failed, identity = expectedDecode(second)
		serr := r.Settle(ctx)
		switch {
		case !failed:
			if uerr != nil {
				t.Fatalf("Update to %+v refused: %v", second, uerr)
			}
			if serr != nil || state() != Active || len(applied) != before+1 || !reflect.DeepEqual(applied[len(applied)-1], want) {
				t.Fatalf("Update to %+v: Settle=%v state=%s applied=%v", second, serr, state(), applied)
			}
		case before == 0 || failedFirst(first):
			// A failed fiber has no target: the config is retained and the restart
			// fails at its activation.
			if uerr != nil || serr == nil || !identity(serr) || state() != Failed || len(applied) != before {
				t.Fatalf("Update of a failed fiber to %+v: Update=%v Settle=%v state=%s", second, uerr, serr, state())
			}
		default:
			// An active fiber refuses the update at once and keeps running.
			if uerr == nil || !identity(uerr) || serr != nil || state() != Active || len(applied) != before {
				t.Fatalf("refused Update to %+v: Update=%v Settle=%v state=%s applied=%d/%d", second, uerr, serr, state(), len(applied), before)
			}
		}
	})
}

func failedFirst(in decodeInput) bool {
	_, failed, _ := expectedDecode(in)
	return failed
}

// The operation sequence fuzzer. Each byte string decodes into a bounded list of
// lifecycle operations on a small key set. After the sequence the kernel settles
// and closes, and its invariants are checked.
const (
	fuzzKeys    = 4
	fuzzMaxOps  = 24
	fuzzMaxHang = 2
)

type fuzzFiber struct {
	fiber    *Fiber
	id       string
	needs    []int
	provides []int
	ctx      atomic.Pointer[Context] // The context of its latest activation, the parent of a child mount.
}

type fuzzRun struct {
	t      *testing.T
	ctx    context.Context
	root   *Root
	keys   [fuzzKeys]Key[int]
	gate   chan struct{}
	hangs  int
	nextID int
	fibers []*fuzzFiber
	byID   map[string]*fuzzFiber
}

type reader struct {
	data []byte
	pos  int
}

func (r *reader) next() byte {
	if r.pos >= len(r.data) {
		return 0
	}
	b := r.data[r.pos]
	r.pos++
	return b
}

func (r *reader) more() bool { return r.pos < len(r.data) }

func (run *fuzzRun) keyName(i int) string { return fmt.Sprintf("k%d@1", i) }

// spec decodes one plugin: needs and provides over the key set, and a behaviour.
func (run *fuzzRun) spec(rd *reader) (Plugin, *fuzzFiber) {
	needs, provides, behaviour := rd.next(), rd.next(), rd.next()
	ff := &fuzzFiber{id: fmt.Sprintf("fuzz/p%d", run.nextID)}
	run.nextID++
	var inject []Dep
	var declared []AnyKey
	for i := 0; i < fuzzKeys; i++ {
		// A fiber that provides a key does not need it: the needs and provides of
		// one fiber are disjoint, as a plugin declares them.
		switch {
		case provides&(1<<i) != 0:
			ff.provides = append(ff.provides, i)
			declared = append(declared, run.keys[i])
		case needs&(1<<i) != 0:
			ff.needs = append(ff.needs, i)
			inject = append(inject, Need(run.keys[i]))
		}
	}
	mode := behaviour % 9
	if mode == 4 || mode == 5 {
		if run.hangs >= fuzzMaxHang {
			mode = 0
		}
		run.hangs++
	}
	gate := run.gate
	// Decode refuses a config whose first byte is a multiple of four, so an
	// update with a config can be refused whole.
	decode := func(raw any) (any, error) {
		if n, ok := raw.(uint8); ok && n%4 == 0 {
			return nil, errors.New("decode refused")
		}
		return raw, nil
	}
	plugin := Define(Plugin{ID: ff.id, Inject: inject, Provides: declared, Decode: decode, Apply: func(c *Context, _ any) error {
		ff.ctx.Store(c)
		provide := func() error {
			for _, i := range ff.provides {
				if err := run.keys[i].Provide(c, i); err != nil {
					return err
				}
			}
			return nil
		}
		switch mode {
		case 1:
			return nil // Declares keys and provides none.
		case 2:
			if err := provide(); err != nil {
				return err
			}
			return errors.New("apply failed")
		case 3:
			if err := provide(); err != nil {
				return err
			}
			panic("apply panicked")
		case 4: // Cooperative hang: ends when the activation is quiesced.
			<-c.Std().Done()
			return nil
		case 5: // Uncooperative hang: ends only when the run releases it.
			<-gate
			return nil
		case 6:
			if err := provide(); err != nil {
				return err
			}
			return c.h.requestDispose()
		case 7:
			c.Defer("failing", func(context.Context) error { return errors.New("cleanup failed") })
			c.Go("worker", func(ctx context.Context) error { <-ctx.Done(); return nil })
			return provide()
		case 8:
			var provideErr error
			if err := c.Effect("setup", func() (Disposer, error) {
				provideErr = provide()
				return func(context.Context) error { return nil }, nil
			}); err != nil {
				return err
			}
			return provideErr
		}
		return provide()
	}})
	return plugin, ff
}

func (run *fuzzRun) mount(rd *reader) {
	p, ff := run.spec(rd)
	ff.fiber = run.root.Plugin(p, nil)
	run.fibers = append(run.fibers, ff)
	run.byID[ff.id] = ff
}

func (run *fuzzRun) pick(rd *reader) *fuzzFiber {
	b := rd.next()
	if len(run.fibers) == 0 {
		return nil
	}
	return run.fibers[int(b)%len(run.fibers)]
}

func (run *fuzzRun) step(rd *reader) {
	switch op := rd.next() % 8; op {
	case 0, 1:
		run.mount(rd)
	case 2:
		if ff := run.pick(rd); ff != nil {
			_ = ff.fiber.Update(run.ctx, nil)
		}
	case 3:
		if ff := run.pick(rd); ff != nil {
			_ = ff.fiber.Dispose(run.ctx)
		}
	case 4:
		var ops []BatchOp
		var added []*fuzzFiber
		for n := int(rd.next()%3) + 1; n > 0; n-- {
			switch rd.next() % 5 {
			case 0:
				p, ff := run.spec(rd)
				added = append(added, ff)
				ops = append(ops, MountOp(p, nil))
			case 1:
				if ff := run.pick(rd); ff != nil {
					ops = append(ops, UpdateOp(ff.fiber))
				}
			case 2: // A child of a picked fiber's latest activation, or of the root.
				parent := run.root.Context()
				if ff := run.pick(rd); ff != nil && ff.ctx.Load() != nil {
					parent = ff.ctx.Load()
				}
				p, ff := run.spec(rd)
				added = append(added, ff)
				ops = append(ops, parent.MountOp(p, nil))
			case 3: // An update that carries a config.
				if ff := run.pick(rd); ff != nil {
					ops = append(ops, UpdateOpWith(ff.fiber, rd.next()))
				}
			default:
				if ff := run.pick(rd); ff != nil {
					ops = append(ops, DisposeOp(ff.fiber))
				}
			}
		}
		retained := make(map[*cfgCell]any)
		for _, op := range ops {
			if op.hasCfg {
				retained[op.fiber.cell] = op.fiber.cell.load()
			}
		}
		fibers, err := run.root.Batch(run.ctx, ops...)
		if err != nil {
			// A refused batch retains no config.
			for cell, was := range retained {
				if now := cell.load(); now != was {
					run.t.Fatalf("a refused batch (%v) retained the config %v over %v", err, now, was)
				}
			}
		}
		if err == nil {
			mounted := 0
			for i, op := range ops {
				if op.kind == batchMount {
					added[mounted].fiber = fibers[i]
					run.fibers = append(run.fibers, added[mounted])
					run.byID[added[mounted].id] = added[mounted]
					mounted++
				}
			}
		}
	case 5:
		b := rd.next()
		var keys []AnyKey
		for i := 0; i < fuzzKeys; i++ {
			if b&(1<<i) != 0 {
				keys = append(keys, run.keys[i])
			}
		}
		if err := run.root.Notify(run.ctx, keys...); err != nil {
			run.t.Fatalf("Notify: %v", err)
		}
	case 6:
		run.settle()
		run.checkGraph()
	default:
		// A fiber that disposes another's key space: restart a provider's consumers
		// by updating a random fiber twice in a row.
		if ff := run.pick(rd); ff != nil {
			_ = ff.fiber.Update(run.ctx, nil)
			_ = ff.fiber.Update(run.ctx, nil)
		}
	}
}

// settle must return: a blocked fiber is reported, never waited on forever.
func (run *fuzzRun) settle() error {
	err := run.root.Settle(run.ctx)
	if run.ctx.Err() != nil {
		run.t.Fatalf("Settle did not return within the bound: %v", err)
	}
	return err
}

var allStates = []State{Pending, Loading, Active, Failed, Unloading, Disposed}

// checkGraph asserts the structural invariants on one owner turn.
func (run *fuzzRun) checkGraph() {
	run.t.Helper()
	view, err := run.root.Inspect(run.ctx)
	if err != nil {
		run.t.Fatalf("Inspect: %v", err)
	}
	active := make(map[string]bool)
	seen := make(map[string]int)
	for _, p := range view.Provisions {
		seen[p.Key]++
		if seen[p.Key] > 1 {
			run.t.Fatalf("key %s has %d providers: %+v", p.Key, seen[p.Key], view.Provisions)
		}
		if p.Active {
			active[p.Key] = true
		}
	}
	for _, s := range view.Fibers {
		if !slices.Contains(allStates, s.State) {
			run.t.Fatalf("fiber %d is in state %q", s.ID, s.State)
		}
		ff := run.byID[s.Plugin]
		if s.State != Active || ff == nil {
			continue
		}
		for _, n := range ff.needs {
			if !active[run.keyName(n)] {
				run.t.Fatalf("fiber %d (%s) is ACTIVE but %s has no ACTIVE provider: %+v", s.ID, s.Plugin, run.keyName(n), view)
			}
		}
	}
}

// FuzzOperationSequence decodes the input into mounts, updates, disposals,
// batches, self-disposals, notifications and settles, then settles and closes.
func FuzzOperationSequence(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 1, 0})                                           // one provider
	f.Add([]byte{0, 0, 1, 0, 0, 1, 0, 0, 6, 3, 0})                      // provider, consumer, settle, dispose provider
	f.Add([]byte{0, 0, 1, 0, 0, 1, 0, 0, 2, 0, 6, 5, 1, 6})             // update, notify, settle
	f.Add([]byte{0, 0, 1, 6, 0, 1, 0, 0, 6})                            // self-disposing provider with a consumer
	f.Add([]byte{0, 0, 1, 4, 0, 1, 0, 0, 6, 3, 0, 6})                   // cooperative hang, dispose
	f.Add([]byte{0, 0, 1, 5, 6, 3, 0, 6})                               // uncooperative hang, dispose
	f.Add([]byte{4, 2, 0, 0, 1, 0, 0, 1, 0, 0, 0, 6})                   // batch: provider and consumer
	f.Add([]byte{0, 0, 1, 0, 0, 1, 0, 0, 4, 2, 1, 0, 2, 0, 6})          // batch: update and dispose
	f.Add([]byte{0, 0, 1, 7, 0, 3, 0, 0, 0, 3, 4, 1, 0, 0, 5, 3, 0, 6}) // defer and tracked worker, consumers
	f.Add([]byte{0, 0, 1, 2, 0, 1, 0, 3, 7, 0, 3, 1, 6})                // failing provider, panicking consumer
	f.Add([]byte{0, 0, 1, 0, 4, 0, 2, 0, 0, 0, 0, 6})                   // batch: child of a fiber
	f.Add([]byte{0, 0, 1, 0, 4, 0, 3, 0, 5, 6})                         // batch: update with a config
	f.Add([]byte{0, 0, 1, 0, 4, 0, 3, 0, 4, 6})                         // batch: update with a refused config
	f.Add([]byte{0, 0, 1, 0, 4, 2, 2, 0, 0, 0, 0, 3, 0, 4, 0, 3, 0, 9}) // batch: child, dispose, update with a config
	f.Fuzz(func(t *testing.T, data []byte) {
		before := runtime.NumGoroutine()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		root, err := New(fuzzConfig())
		if err != nil {
			t.Fatal(err)
		}
		run := &fuzzRun{t: t, ctx: ctx, root: root, gate: make(chan struct{}), byID: make(map[string]*fuzzFiber)}
		for i := range run.keys {
			run.keys[i] = NewKey[int](fmt.Sprintf("k%d", i), 1)
		}
		rd := &reader{data: data}
		for n := 0; n < fuzzMaxOps && rd.more(); n++ {
			run.step(rd)
		}
		var once sync.Once
		release := func() { once.Do(func() { close(run.gate) }) }
		defer release()
		run.settle()
		run.checkGraph()
		release()
		run.settle()
		run.checkGraph()
		closeErr := closeKernel(t, root)
		_ = closeErr // A failed or blocked fiber is reported by Close, not a defect.

		// The owner exits only when no fiber and not even the root is left.
		select {
		case <-root.core.done:
		case <-time.After(3 * time.Second):
			t.Fatal("a fiber is still live after Close")
		}
		for deadline := time.Now().Add(3 * time.Second); runtime.NumGoroutine() > before+2; {
			if time.Now().After(deadline) {
				buf := make([]byte, 1<<16)
				t.Fatalf("goroutines: %d before, %d after Close\n%s", before, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
			}
			time.Sleep(time.Millisecond)
		}
	})
}
