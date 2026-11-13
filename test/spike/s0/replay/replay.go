// SPDX-License-Identifier: Apache-2.0

// Package replay executes neutral S0 actions against a real concurrency Runtime.
// RuntimeEvents and inspections preserve actual observations. Recorder projects
// the shared state/service/body ledger; Inspector supplies native Alive, target,
// configuration and provision-store snapshots. ComparisonError describes missing
// observation capabilities, not semantic mismatches: nil means eligible for a
// full differential.Compare, never that comparison passed. Unsupported language
// operations fail explicitly. Expected values and saved traces never drive execution.
package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

var ErrUnsupported = errors.New("replay operation unsupported")

// UnsupportedError is never swallowed by an action's catch:true. Interpreter
// limitations are execution failures, not fixture errors or compatibility passes.
type UnsupportedError struct{ Operation, Reason string }

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%v: %s: %s", ErrUnsupported, e.Operation, e.Reason)
}
func (e *UnsupportedError) Unwrap() error { return ErrUnsupported }

// FiberAwaiter names the optional target-only completion contract. Runtime.Settle
// cannot implement await(dispose(d)) while an unrelated fiber is gated.
type FiberAwaiter = concurrency.Waiter

type RuntimeFailure struct {
	Operation string
	Error     error
}

// Result separates public observations from raw scheduler diagnostics. Checkpoints
// require Inspector: an empty provision array is an observed empty store, never
// unavailable inspection. ComparisonError == nil permits a full comparison; only
// differential.Compare establishes whether the complete ordered trace matches.
type Result struct {
	Trace              differential.Trace
	RuntimeEvents      []concurrency.Event
	RuntimeSnapshots   map[string][]concurrency.Snapshot
	RuntimeInspections map[string]concurrency.Inspection
	RuntimeErrors      []RuntimeFailure
	ComparisonError    error
	// CheckpointAfterEvent records observed semantic-prefix lengths; it is a
	// causal sidecar only, never an inline event or an expected-output control.
	CheckpointAfterEvent []differential.CausalCut
}

type fiber struct {
	name           string
	id             uint64
	needs          []string
	config         *concurrency.Configuration
	ctx            activation
	startupFailure error
}

type task struct {
	fiber *fiber
	op    string
}

type gate struct {
	done     chan struct{}
	released bool
	arrivals int
}

type environment struct {
	fiber  *fiber
	ctx    activation
	config any // Immutable copy for this activation, including its cleanup.
	std    context.Context
}

type interpreter struct {
	ctx          context.Context
	runtime      concurrency.Runtime
	frontend     *composedFrontend
	mu           sync.Mutex
	changed      chan struct{}
	result       Result
	plugins      map[string]differential.PluginSpec
	orderedNeeds map[string][]string
	fibers       map[string]*fiber
	order        []*fiber
	outputs      map[string]any
	gates        map[string]*gate
	marks        map[string]int
	tasks        map[string]task
	failures     []error
	fatal        []error // Interpreter limits must survive asynchronous Apply failures.
	recordErrors []error
}

// Run does not close runtime: the caller owns it and must use a fresh instance
// per script. It never reads Script.Expect. A typed Needs map has lost source key
// order: multi-key plugins execute lexically but explicitly cannot claim ordered
// comparison support. Use RunJSON to preserve the original declaration order.
func Run(ctx context.Context, runtime concurrency.Runtime, script differential.Script) (Result, error) {
	return run(ctx, runtime, script, nil)
}

// RunJSON validates the unchanged differential protocol, then separately decodes
// only each plugin's needs key order. Expected assertions never drive execution.
func RunJSON(ctx context.Context, runtime concurrency.Runtime, data []byte) (Result, error) {
	script, err := differential.ParseScript(data)
	if err != nil {
		return Result{}, err
	}
	orderedNeeds, err := decodeNeedsOrder(data)
	if err != nil {
		return Result{}, err
	}
	if err := applyMissingValues(data, script); err != nil {
		return Result{}, err
	}
	return run(ctx, runtime, *script, orderedNeeds)
}

func run(ctx context.Context, runtime concurrency.Runtime, script differential.Script, orderedNeeds map[string][]string) (Result, error) {
	return runAdapted(ctx, runtime, script, orderedNeeds, nil)
}

func runAdapted(ctx context.Context, runtime concurrency.Runtime, script differential.Script, orderedNeeds map[string][]string, frontend *composedFrontend) (Result, error) {
	r := &interpreter{ctx: ctx, runtime: runtime, frontend: frontend, changed: make(chan struct{}),
		plugins: make(map[string]differential.PluginSpec), orderedNeeds: orderedNeeds, fibers: make(map[string]*fiber),
		outputs: make(map[string]any), gates: make(map[string]*gate), marks: make(map[string]int), tasks: make(map[string]task)}
	r.result = Result{Trace: differential.Trace{Schema: differential.TraceSchema, Scenario: script.ID,
		Events: []differential.Event{}, Checkpoints: []differential.Checkpoint{}},
		RuntimeSnapshots: make(map[string][]concurrency.Snapshot), RuntimeInspections: make(map[string]concurrency.Inspection)}
	if _, ok := runtime.(concurrency.Recorder); !ok {
		r.result.ComparisonError = errors.Join(r.result.ComparisonError,
			unsupported("comparison", "ordered state/body observer or shared Recorder ledger unavailable"))
	}
	if _, ok := runtime.(concurrency.Inspector); !ok {
		r.result.ComparisonError = errors.Join(r.result.ComparisonError,
			unsupported("comparison", "native fiber/target/provision-store Inspector unavailable"))
	}
	if runtime == nil || script.Schema != differential.ScriptSchema {
		return r.result, errors.New("replay requires a Runtime and s0-script/v1")
	}
	if frontend == nil && len(script.Contexts) != 0 {
		return r.result, unsupported("contexts", "Runtime has no context parent/realm API")
	}
	if script.Diagnostic != "" {
		return r.result, unsupported("diagnostic", "Go self-disposal is request-only; awaited self-disposal is not emulated")
	}
	validateActions := validate
	if frontend != nil {
		if err := frontend.init(r); err != nil {
			return r.result, err
		}
		validateActions = frontend.validate
	}
	for _, plugin := range script.Plugins {
		if _, exists := r.plugins[plugin.ID]; exists || plugin.ID == "" {
			return r.result, fmt.Errorf("invalid plugin %q", plugin.ID)
		}
		switch plugin.Update {
		case "", "retain", "next", "reject", "unchanged":
		default:
			return r.result, unsupported("plugin update hook", fmt.Sprintf("unknown mode %q", plugin.Update))
		}
		for _, need := range plugin.Needs {
			if frontend == nil && need != nil {
				return r.result, unsupported("needs", "only required, ordinary string keys are supported")
			}
		}
		if orderedNeeds == nil && len(plugin.Needs) > 1 {
			r.result.ComparisonError = errors.Join(r.result.ComparisonError,
				unsupported("needs order", fmt.Sprintf("typed plugin %q lost JSON declaration order; lexical execution only; use RunJSON", plugin.ID)))
		}
		r.plugins[plugin.ID] = plugin
		if err := validateActions(plugin.Apply); err != nil {
			return r.result, err
		}
		if err := validateActions(plugin.Cleanup); err != nil {
			return r.result, err
		}
	}
	if err := validateActions(script.Steps); err != nil {
		return r.result, err
	}
	err := r.run(script.Steps, environment{std: ctx})
	if err != nil {
		// Do not leave fixture gates permanently holding caller-owned runtimes.
		// These releases are error-path cancellation, not semantic release events.
		r.mu.Lock()
		for _, g := range r.gates {
			if !g.released {
				g.released = true
				close(g.done)
			}
		}
		r.mu.Unlock()
	}
	events, eventErr := runtime.Events(ctx)
	r.mu.Lock()
	r.result.RuntimeEvents = events // Preserve original order and every debug kind.
	if _, ok := runtime.(concurrency.Recorder); ok {
		projected, projectionErr := project(events)
		r.result.Trace.Events = projected
		if projectionErr != nil {
			r.result.ComparisonError = errors.Join(r.result.ComparisonError, projectionErr)
		}
	}
	result := r.result
	// Returning partial observations while the caller still owns live fibers
	// must not leave shared mutable slices/maps in the result.
	result.Trace.Events = append([]differential.Event(nil), result.Trace.Events...)
	result.Trace.Checkpoints = append([]differential.Checkpoint(nil), result.Trace.Checkpoints...)
	result.RuntimeErrors = append([]RuntimeFailure(nil), result.RuntimeErrors...)
	result.CheckpointAfterEvent = append([]differential.CausalCut(nil), result.CheckpointAfterEvent...)
	result.RuntimeSnapshots = make(map[string][]concurrency.Snapshot, len(r.result.RuntimeSnapshots))
	for label, snapshots := range r.result.RuntimeSnapshots {
		result.RuntimeSnapshots[label] = copyInspection(concurrency.Inspection{Fibers: snapshots}).Fibers
	}
	result.RuntimeInspections = make(map[string]concurrency.Inspection, len(r.result.RuntimeInspections))
	for label, inspection := range r.result.RuntimeInspections {
		result.RuntimeInspections[label] = copyInspection(inspection)
	}
	fatal := append(append([]error(nil), r.fatal...), r.recordErrors...)
	r.mu.Unlock()
	return result, errors.Join(append([]error{err, eventErr}, fatal...)...)
}

func unsupported(op, reason string) error { return &UnsupportedError{op, reason} }

// sameIdentity implements the fixture's strict scalar/reference identity test.
// In particular, equal JSON objects are not the same resolved object. The
// configuration cell preserves these references; this helper never deep-compares
// maps or arrays to make an identity assertion pass.
func sameIdentity(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, right := reflect.ValueOf(a), reflect.ValueOf(b)
	if left.Type() != right.Type() {
		return false
	}
	switch left.Kind() {
	case reflect.Map:
		return left.UnsafePointer() == right.UnsafePointer()
	case reflect.Slice:
		return left.IsNil() == right.IsNil() && left.Pointer() == right.Pointer() && left.Len() == right.Len()
	case reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
		return left.Pointer() == right.Pointer()
	default:
		if left.Type().Comparable() {
			return left.Interface() == right.Interface()
		}
		return false
	}
}

func validate(actions []differential.Action) error {
	for _, a := range actions {
		if a.Context != "" {
			return unsupported(a.Op, "explicit context targeting needs parent/realm support")
		}
		switch a.Op {
		case "mount", "settle", "snapshot", "waitMark", "mark", "gate", "release", "output", "when", "fail", "awaitTask", "rawConfig", "resolveConfig":
		case "provide", "read":
			if a.Mode != "" || a.To != "" && a.Op == "provide" {
				return unsupported(a.Op, "service/get/config/disposer modes need a native API")
			}
		case "update":
			if a.Mode != "" && a.Mode != "noSave" {
				return unsupported(a.Op, "only default and noSave native configuration updates are supported")
			}
		case "dispose", "awaitFiber":
			if a.Mode != "" {
				return unsupported(a.Op, "nondefault mode not supported")
			}
		case "inspect":
			if a.Mode != "configIdentity" {
				return unsupported(a.Op, "only actual applied/effective configuration identity is available")
			}
		case "selfDispose":
			if a.Await != nil && *a.Await || a.Task != "" {
				return unsupported(a.Op, "awaited or task-tracked own-disposal is not a supported Go request")
			}
		case "effect":
			if a.Mode != "" || a.To != "" || a.Await != nil && !*a.Await {
				return unsupported(a.Op, "partial/withdrawable/asynchronous setup needs a native effect API")
			}
		default:
			return unsupported(a.Op, "not implemented by this minimal adapter")
		}
		if err := validate(a.Program); err != nil {
			return err
		}
		if err := validate(a.Cleanup); err != nil {
			return err
		}
	}
	return nil
}

func (r *interpreter) eventLocked(kind string, data map[string]any) {
	payload := clone(data).(map[string]any)
	if recorder, ok := r.runtime.(concurrency.Recorder); ok {
		// The owner only copies data; it never invokes fixture code. The ack makes
		// the fixture operation causal with earlier status/service records.
		if err := recorder.Record(r.ctx, kind, payload); err != nil {
			r.recordErrors = append(r.recordErrors, err)
		}
	}
	r.result.Trace.Events = append(r.result.Trace.Events, differential.Event{
		Seq: len(r.result.Trace.Events) + 1, Kind: kind, Data: payload})
}

// project never sorts or merges events. Administrative debug records stay in
// RuntimeEvents; every actual state record is represented, including candidate-
// specific transitions that will be mismatches rather than normalized away.
func project(events []concurrency.Event) ([]differential.Event, error) {
	out := []differential.Event{}
	var limitations []error
	appendEvent := func(kind string, data map[string]any) {
		out = append(out, differential.Event{Seq: len(out) + 1, Kind: kind, Data: clone(data).(map[string]any)})
	}
	for _, event := range events {
		switch event.Kind {
		case "fixture":
			if event.Key == "" || event.Data == nil {
				limitations = append(limitations, unsupported("projection", "incomplete fixture envelope"))
				continue
			}
			appendEvent(event.Key, event.Data)
		case "state":
			appendEvent("status", map[string]any{"fiber": event.Name, "from": string(event.From), "to": string(event.State)})
			if event.Name == "" || event.From == "" || event.From == event.State {
				limitations = append(limitations, unsupported("projection", "state event lacks real name/from transition"))
			}
		case "plugin", "mount":
			appendEvent("plugin", map[string]any{"fiber": event.Name, "disposed": false})
		case "dispose:request", "plugin:dispose":
			appendEvent("plugin", map[string]any{"fiber": event.Name, "disposed": true})
		case "service", "service:publish", "service:withdraw":
			// A real service notification can still carry the old value while its
			// provider is UNLOADING. State is not a substitute for event.Value.
			appendEvent("service", map[string]any{"key": event.Key, "realm": "root:" + event.Key, "value": event.Value})
		case "service:remove", "remove":
			// Removal is an actual store operation, not inferred availability.
			appendEvent("service", map[string]any{"key": event.Key, "realm": "root:" + event.Key, "value": undefined()})
		}
	}
	return out, errors.Join(limitations...)
}
func (r *interpreter) event(kind string, data map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventLocked(kind, data)
}
func (r *interpreter) put(name string, value any) {
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[name] = clone(value)
	r.eventLocked("output", map[string]any{"to": name, "value": value})
}
func (r *interpreter) getGate(label string) *gate {
	g := r.gates[label]
	if g == nil {
		g = &gate{done: make(chan struct{})}
		r.gates[label] = g
	}
	return g
}
func (r *interpreter) mark(label, kind string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventLocked(kind, map[string]any{"label": label, "value": value})
	r.marks[label]++
	if kind == "gate" {
		r.getGate(label).arrivals++
	}
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *interpreter) run(actions []differential.Action, env environment) error {
	for index, a := range actions {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		err := r.execute(a, env)
		if err == nil {
			continue
		}
		if !a.Catch || errors.Is(err, ErrUnsupported) {
			if env.fiber == nil {
				return fmt.Errorf("step %d %s fiber=%q label=%q: %w", index+1, a.Op, a.Fiber, a.Label, err)
			}
			return err
		}
		value := errorValue(err)
		r.event("error", map[string]any{"op": a.Op, "error": value})
		r.put(a.To, value)
	}
	return nil
}

func (r *interpreter) execute(a differential.Action, env environment) error {
	if r.frontend != nil {
		if handled, err := r.frontend.execute(a, env); handled {
			return err
		}
	}
	value := r.evaluate(a.Value, env)
	switch a.Op {
	case "mount":
		if env.fiber != nil && r.frontend == nil {
			return unsupported(a.Op, "nested mounting needs parent/child lifecycle support")
		}
		return r.mount(a, r.evaluate(a.Config, env), env)
	case "output":
		r.put(a.To, value)
	case "mark", "gate":
		r.mark(a.Label, a.Op, value)
		if a.Op == "gate" {
			r.mu.Lock()
			done := r.getGate(a.Label).done
			r.mu.Unlock()
			select {
			case <-done:
			case <-env.std.Done():
				return env.std.Err()
			}
		}
	case "release":
		r.mu.Lock()
		g := r.getGate(a.Label)
		if !g.released {
			g.released = true
			close(g.done)
		}
		r.mu.Unlock()
	case "waitMark":
		count := a.Count
		if count == 0 {
			count = 1
		}
		for {
			r.mu.Lock()
			ready, changed := r.marks[a.Label] >= count, r.changed
			r.mu.Unlock()
			if ready {
				break
			}
			select {
			case <-changed:
			case <-env.std.Done():
				return env.std.Err()
			}
		}
	case "settle":
		if env.fiber != nil {
			return unsupported(a.Op, "Settle from Apply would join its own callback")
		}
		if err := r.settle(); err != nil {
			return err
		}
		return r.checkpoint(a.Label)
	case "snapshot":
		return r.checkpoint(a.Label)
	case "when":
		if reflect.DeepEqual(r.evaluate(a.Base, env), value) {
			return r.run(a.Program, env)
		}
	case "provide":
		if env.ctx == nil {
			return unsupported(a.Op, "root provisions need a native root context")
		}
		return env.ctx.Provide(a.Key, clone(value))
	case "read":
		if env.ctx == nil || a.Fiber != "" && a.Fiber != env.fiber.name {
			return unsupported(a.Op, "only this activation's committed Context.Read is available")
		}
		result, err := env.ctx.Read(a.Key)
		if err != nil {
			return err
		}
		r.put(a.To, result)
	case "effect":
		if env.ctx == nil {
			return unsupported(a.Op, "effect outside Apply")
		}
		if err := r.run(a.Program, env); err != nil {
			return err
		}
		return env.ctx.Defer(a.Label, func(ctx context.Context) error {
			cleanup := env
			cleanup.std = ctx
			return r.run(a.Cleanup, cleanup)
		})
	case "fail":
		message := a.Error
		if message == "" {
			message = "fixture-failed"
		}
		return errors.New(message)
	case "selfDispose":
		if env.ctx == nil {
			return unsupported(a.Op, "selfDispose is only meaningful inside Apply")
		}
		r.event("selfDispose", map[string]any{"fiber": env.fiber.name, "await": false})
		return env.ctx.Dispose()
	case "rawConfig", "resolveConfig", "inspect":
		if env.fiber != nil {
			return unsupported(a.Op, "external configuration operations from a plugin body are not implemented")
		}
		r.mu.Lock()
		f := r.fibers[a.Fiber]
		var applied any
		var wasApplied bool
		if f != nil && f.ctx != nil {
			applied, wasApplied = f.ctx.Config(), true
		}
		r.mu.Unlock()
		if f == nil {
			return fmt.Errorf("unknown fiber %q", a.Fiber)
		}
		switch a.Op {
		case "rawConfig":
			f.config.RetainRaw(clone(r.evaluate(a.Config, env)))
		case "resolveConfig":
			candidate, err := f.config.ResolveCandidate(r.ctx, clone(r.evaluate(a.Config, env)))
			if err != nil {
				return err
			}
			r.put(a.To, candidate)
		case "inspect":
			snapshot := f.config.Snapshot()
			// Plain Go maps are not comparable. Identity is not DeepEqual: two
			// independently resolved but equal object values are different values.
			r.put(a.To, snapshot.Prepared == wasApplied && (!wasApplied || sameIdentity(snapshot.Effective, applied)))
		}
	case "update", "dispose", "awaitFiber", "awaitTask":
		if env.fiber != nil {
			return unsupported(a.Op, "external lifecycle commands from a plugin body need a native context-aware join API")
		}
		r.mu.Lock()
		f := r.fibers[a.Fiber]
		joinMode := a.Op
		if a.Op == "awaitTask" {
			tracked := r.tasks[a.Task]
			f, joinMode = tracked.fiber, tracked.op
		}
		r.mu.Unlock()
		if f == nil {
			return fmt.Errorf("unknown fiber/task %q/%q", a.Fiber, a.Task)
		}
		var err error
		if a.Op == "update" {
			updater, ok := r.runtime.(concurrency.ConfigRequester)
			if !ok {
				return unsupported(a.Op, "native ConfigRequester required to separate the resolve/commit prefix from lifecycle completion")
			}
			// The prefix is synchronous just like the source update call. Only
			// its lifecycle completion is deferred by await:false; spawning the
			// whole prefix would reorder commit/raw retention and lose coalescing.
			_, err = updater.RequestUpdateConfig(r.ctx, f.id, clone(r.evaluate(a.Config, env)), a.Mode == "noSave")
		} else if a.Op == "dispose" {
			err = r.runtime.Dispose(r.ctx, f.id)
		}
		if err != nil {
			return err
		}
		if a.Task != "" && a.Op != "awaitTask" {
			r.mu.Lock()
			r.tasks[a.Task] = task{fiber: f, op: a.Op}
			r.mu.Unlock()
		}
		if a.Await == nil || *a.Await {
			return r.await(f, joinMode)
		}
	default:
		return unsupported(a.Op, "not implemented")
	}
	return nil
}

func (r *interpreter) mount(a differential.Action, config any, env environment) error {
	r.mu.Lock()
	plugin, ok := r.plugins[a.Plugin]
	_, exists := r.fibers[a.Fiber]
	if !ok || exists || a.Fiber == "" {
		r.mu.Unlock()
		return errors.New("invalid mount")
	}
	f := &fiber{name: a.Fiber}
	if r.orderedNeeds != nil {
		f.needs = append([]string(nil), r.orderedNeeds[plugin.ID]...)
	} else {
		for key := range plugin.Needs {
			f.needs = append(f.needs, key)
		}
		// A map has no recoverable declaration order. Deterministic lexical
		// execution is useful diagnostics only, explicitly unsupported above.
		sort.Strings(f.needs)
	}
	r.fibers[f.name] = f
	r.order = append(r.order, f)
	r.mu.Unlock()
	provides := []string{}
	var collect func([]differential.Action)
	collect = func(actions []differential.Action) {
		for _, action := range actions {
			if action.Op == "provide" {
				found := false
				for _, key := range provides {
					found = found || key == action.Key
				}
				if !found {
					provides = append(provides, action.Key)
				}
			}
			collect(action.Program)
		}
	}
	collect(plugin.Apply)
	f.config = concurrency.NewConfiguration(clone(config), concurrency.ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) {
			// This is an actual native Prepare/ResolveCandidate callback, not a
			// surrogate event at Apply. The oracle's observer is inside the
			// prepended transform: it observes the identity-resolved raw input.
			r.event("config", map[string]any{"fiber": f.name, "config": raw})
			if len(plugin.ConfigTransform) == 0 {
				return raw, nil // Preserve the native identity for an identity decode.
			}
			resolved := map[string]any{}
			if object, ok := raw.(map[string]any); ok {
				for key, value := range object {
					resolved[key] = value
				}
			} else if raw != nil {
				return nil, unsupported("config transform", "only ordinary JSON object shallow merges are supported")
			}
			for key, value := range plugin.ConfigTransform {
				resolved[key] = clone(value)
			}
			return resolved, nil
		},
		Commit: func(_ context.Context, raw any) error {
			r.event("commit", map[string]any{"fiber": f.name, "config": raw})
			return nil
		},
		Update: func(_ context.Context, candidate any, next func() error) error {
			if plugin.Update == "" {
				return next()
			}
			r.mu.Lock()
			applied := f.ctx != nil // Source installs its update hook in Apply.
			r.mu.Unlock()
			if !applied {
				return next()
			}
			r.event("update", map[string]any{"fiber": f.name, "config": candidate, "mode": plugin.Update})
			switch plugin.Update {
			case "retain":
				return nil
			case "reject":
				return errors.New("fixture-update-rejected")
			case "unchanged":
				if reflect.DeepEqual(f.config.Snapshot().Effective, candidate) {
					return nil
				}
			}
			return next()
		},
	})
	apply := func(c activation) (concurrency.Disposer, error) {
		config := c.Config() // Actual immutable native preparation for this activation.
		r.mu.Lock()
		f.ctx, f.startupFailure = c, nil
		r.eventLocked("apply", map[string]any{"fiber": f.name, "config": config})
		r.mu.Unlock()
		env := environment{fiber: f, ctx: c, config: config, std: c.Std()}
		err := r.run(plugin.Apply, env)
		r.mu.Lock()
		f.startupFailure = err
		r.mu.Unlock()
		if err != nil {
			return nil, r.capture(f, err)
		}
		if len(plugin.Cleanup) == 0 {
			return nil, nil
		}
		// The real runtime owns this returned disposer after all body effects,
		// including successful Apply that already requested self-disposal. There
		// is no pre-owned slot, guarded no-op or late Context.Defer approximation.
		return func(ctx context.Context) error {
			r.event("cleanup", map[string]any{"fiber": f.name})
			cleanup := env
			cleanup.std = ctx
			return r.capture(f, r.run(plugin.Cleanup, cleanup))
		}, nil
	}
	var id uint64
	var err error
	if r.frontend != nil {
		id, err = r.frontend.mount(a, env, f, plugin, provides, apply)
	} else {
		id, err = r.runtime.Mount(r.ctx, concurrency.Spec{ID: f.name, Needs: f.needs, Provides: provides, Config: f.config,
			ApplyEffect: func(c *concurrency.Context) (concurrency.Disposer, error) { return apply(c) }})
	}
	r.mu.Lock()
	f.id = id
	r.mu.Unlock()
	return err
}

func (r *interpreter) capture(f *fiber, err error) error {
	if err != nil {
		r.mu.Lock()
		r.failures = append(r.failures, err)
		if errors.Is(err, ErrUnsupported) {
			r.fatal = append(r.fatal, err)
		}
		r.eventLocked("error", map[string]any{"fiber": f.name, "error": errorValue(err)})
		r.mu.Unlock()
	}
	return err
}

func (r *interpreter) await(f *fiber, mode string) error {
	waiter, ok := r.runtime.(FiberAwaiter)
	if !ok {
		return unsupported("awaitFiber", "target-only Waiter is required; global Settle is not an equivalent operation")
	}
	r.mu.Lock()
	startupFailure := f.startupFailure
	r.mu.Unlock()
	err := waiter.AwaitFiber(r.ctx, f.id)
	if err == nil {
		return nil
	}
	r.mu.Lock()
	r.result.RuntimeErrors = append(r.result.RuntimeErrors, RuntimeFailure{mode, err})
	unchangedFailure := startupFailure != nil && startupFailure == f.startupFailure
	r.mu.Unlock()
	// Source Dispose returns its teardown completion promise, unlike Fiber.Await
	// which also rethrows the retained startup failure. Ignore exactly that one
	// identity only after the real target is DISPOSED; never mask a new failure,
	// another fiber's error, cleanup errors, a deadline, or an unknown target.
	if mode == "dispose" && unchangedFailure && !errors.Is(err, ErrUnsupported) &&
		!errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) && onlyFailure(err, startupFailure) {
		snapshots, snapshotErr := r.runtime.Snapshots(r.ctx)
		if snapshotErr != nil {
			return errors.Join(err, snapshotErr)
		}
		for _, snapshot := range snapshots {
			if snapshot.ID == f.id && snapshot.State == concurrency.Disposed {
				return nil
			}
		}
	}
	return err
}

func onlyFailure(err, known error) bool {
	if err == nil || known == nil {
		return false
	}
	if err == known {
		return true
	}
	if group, ok := err.(interface{ Unwrap() []error }); ok {
		children := group.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyFailure(child, known) {
				return false
			}
		}
		return true
	}
	if child := errors.Unwrap(err); child != nil {
		return onlyFailure(child, known)
	}
	return false
}
func (r *interpreter) settle() error {
	return r.acceptFailure("settle", r.runtime.Settle(r.ctx))
}
func (r *interpreter) acceptFailure(op string, err error) error {
	if err == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.result.RuntimeErrors = append(r.result.RuntimeErrors, RuntimeFailure{op, err})
	var explained func(error) bool
	explained = func(e error) bool {
		if errors.Is(e, ErrUnsupported) {
			return false
		}
		if group, ok := e.(interface{ Unwrap() []error }); ok {
			for _, child := range group.Unwrap() {
				if !explained(child) {
					return false
				}
			}
			return true
		}
		if child := errors.Unwrap(e); child != nil {
			return explained(child)
		}
		for _, known := range r.failures {
			if errors.Is(e, known) {
				return true
			}
		}
		return false
	}
	if explained(err) {
		return nil // Fixture error independently captured; FAILED remains FAILED.
	}
	return err
}

func (r *interpreter) checkpoint(label string) error {
	// Fixture outputs and their acknowledged native Record envelopes share this
	// recorder lock. Freeze them before Inspect so the checkpoint cannot combine
	// a past graph watermark with future fixture output. Native owner inspection
	// never waits for plugin callbacks; this is not a graph-held callback lock.
	r.mu.Lock()
	defer r.mu.Unlock()
	inspector, ok := r.runtime.(concurrency.Inspector)
	if !ok {
		return unsupported("snapshot", "native Inspector required for Alive, desired target and provision-store observations")
	}
	inspection, err := inspector.Inspect(r.ctx)
	if err != nil {
		return err
	}
	inspection = copyInspection(inspection) // Clone actual values, never fixture expectations.
	if label == "" {
		return errors.New("empty checkpoint label")
	}
	if _, exists := r.result.RuntimeInspections[label]; exists {
		return fmt.Errorf("duplicate checkpoint %q", label)
	}
	r.result.RuntimeSnapshots[label] = copyInspection(inspection).Fibers
	r.result.RuntimeInspections[label] = inspection
	byID := make(map[uint64]concurrency.Snapshot)
	for _, s := range inspection.Fibers {
		if _, exists := byID[s.ID]; exists {
			return unsupported("snapshot", fmt.Sprintf("runtime returned duplicate fiber identity %d", s.ID))
		}
		byID[s.ID] = s
	}
	checkpoint := differential.Checkpoint{Label: label, States: []differential.FiberSnapshot{},
		Provisions: []differential.ProvisionSnapshot{}, Outputs: clone(r.outputs).(map[string]any)}
	for _, f := range r.order { // Explicit mount order; do not sort/filter states.
		s, ok := byID[f.id]
		if !ok {
			return unsupported("snapshot", fmt.Sprintf("runtime omitted mounted fiber %q; retired inspection history is unavailable", f.name))
		}
		var epoch *differential.Epoch
		if s.Target != nil {
			if len(s.Target.UIDs) != len(f.needs) {
				return unsupported("snapshot", fmt.Sprintf("fiber %q target has %d UIDs for %d declared needs", f.name, len(s.Target.UIDs), len(f.needs)))
			}
			epoch = &differential.Epoch{Revision: int(s.Target.Revision), Needs: []differential.Binding{}}
			for i, key := range f.needs {
				epoch.Needs = append(epoch.Needs, differential.Binding{Key: key, ProvisionUID: s.Target.UIDs[i]})
			}
		}
		var uid any
		if s.Alive {
			uid = s.ID
		}
		if !s.Configured {
			return unsupported("snapshot", fmt.Sprintf("fiber %q lacks native configuration ownership", f.name))
		}
		var effective any = undefined()
		if s.ConfigPrepared {
			effective = clone(s.EffectiveConfig)
		}
		checkpoint.States = append(checkpoint.States, differential.FiberSnapshot{Fiber: f.name,
			State: string(s.State), FiberUID: uid, Revision: int(s.Revision), Epoch: epoch,
			RawConfig: clone(s.RawConfig), EffectiveConfig: effective})
	}
	for _, provision := range inspection.Provisions { // Native reservation UID order, never key-sorted.
		checkpoint.Provisions = append(checkpoint.Provisions, differential.ProvisionSnapshot{
			Key: provision.Key, Realm: "root:" + provision.Key, Provider: provision.Name,
			ProvisionUID: provision.UID, Value: clone(provision.Value), Active: provision.Active,
		})
	}
	events, err := r.runtime.Events(r.ctx)
	if err != nil {
		return err
	}
	prefix := events[:0:0]
	for _, event := range events {
		if event.Sequence > inspection.Sequence {
			break
		}
		prefix = append(prefix, event)
	}
	observed, err := project(prefix)
	if err != nil {
		return err
	}
	r.result.Trace.Checkpoints = append(r.result.Trace.Checkpoints, checkpoint)
	r.result.CheckpointAfterEvent = append(r.result.CheckpointAfterEvent,
		differential.CausalCut{Label: label, AfterEvent: len(observed)})
	return nil
}

// copyInspection preserves committed Epoch separately from desired Target and
// owns every mutable container. Service values in this adapter are neutral JSON.
func copyInspection(in concurrency.Inspection) concurrency.Inspection {
	out := concurrency.Inspection{Fibers: make([]concurrency.Snapshot, len(in.Fibers)),
		Provisions: make([]concurrency.ProvisionSnapshot, len(in.Provisions)), Sequence: in.Sequence}
	for i, snapshot := range in.Fibers {
		snapshot.Epoch = append([]uint64(nil), snapshot.Epoch...)
		snapshot.Effects = append([]string(nil), snapshot.Effects...)
		snapshot.Tracked = append([]string(nil), snapshot.Tracked...)
		snapshot.RawConfig = clone(snapshot.RawConfig)
		snapshot.EffectiveConfig = clone(snapshot.EffectiveConfig)
		if snapshot.Target != nil {
			snapshot.Target = &concurrency.EpochSnapshot{Revision: snapshot.Target.Revision,
				UIDs: append([]uint64(nil), snapshot.Target.UIDs...)}
		}
		out.Fibers[i] = snapshot
	}
	for i, provision := range in.Provisions {
		provision.Value = clone(provision.Value)
		out.Provisions[i] = provision
	}
	return out
}

func (r *interpreter) evaluate(value any, env environment) any {
	path := func(value any, p string) any {
		if p == "" {
			return clone(value)
		}
		for _, key := range strings.Split(p, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				return undefined()
			}
			value, ok = object[key]
			if !ok {
				return undefined()
			}
		}
		return clone(value)
	}
	switch v := value.(type) {
	case map[string]any:
		if p, ok := v["$config"]; ok {
			return path(env.config, fmt.Sprint(p))
		}
		if p, ok := v["$output"]; ok {
			r.mu.Lock()
			defer r.mu.Unlock()
			return path(r.outputs, fmt.Sprint(p))
		}
		if p, ok := v["$meta"]; ok {
			if r.frontend != nil {
				return r.frontend.meta(p, env)
			}
			return undefined() // No declared contexts accepted by the root-only binding.
		}
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = r.evaluate(item, env)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = r.evaluate(item, env)
		}
		return out
	default:
		return value
	}
}
func undefined() map[string]any { return map[string]any{"$undefined": true} }
func errorValue(err error) map[string]any {
	return map[string]any{"name": "Error", "message": err.Error()}
}
func clone(value any) any {
	// Neutral fixture values are JSON, including json.Number. Round-tripping is
	// independent of Script.Expect and prevents mutable configuration aliasing.
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("non-JSON fixture value: %v", err))
	}
	var out any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		panic(err)
	}
	return out
}
