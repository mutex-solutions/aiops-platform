// SPDX-License-Identifier: Apache-2.0

// Package replay executes neutral S0 actions against a real concurrency Runtime.
// It is a spike adapter, not a differential gate. RuntimeEvents and raw snapshots
// are lossless observations. If Runtime implements Recorder, Trace projects the
// actual shared ledger order; otherwise it has fixture callbacks only. Polling
// debug Events cannot recover status/body interleaving without Recorder, and
// provision/config inspection is incomplete, so ComparisonError is ErrUnsupported.
// No expected value, saved oracle trace, or scenario-specific response drives execution.
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
	"sync/atomic"

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

// Result deliberately separates partial public observations from raw scheduler
// diagnostics. Empty Trace.Provisions means unavailable, NOT an observed empty
// provision store. Checkpoints use fixture-owned raw/effective configs, not kernel
// config hooks. Consult ComparisonError before any oracle comparison.
type Result struct {
	Trace            differential.Trace
	RuntimeEvents    []concurrency.Event
	RuntimeSnapshots map[string][]concurrency.Snapshot
	RuntimeErrors    []RuntimeFailure
	ComparisonError  error
}

type fiber struct {
	name, plugin   string
	id             uint64
	needs          []string
	raw, applied   any
	ctx            *concurrency.Context
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
	ctx    *concurrency.Context
	config any // Immutable copy for this activation, including its cleanup.
	std    context.Context
}

type interpreter struct {
	ctx          context.Context
	runtime      concurrency.Runtime
	mu           sync.Mutex
	changed      chan struct{}
	result       Result
	plugins      map[string]differential.PluginSpec
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
// per script. It never reads Script.Expect. CheckExpectations can independently
// inspect the returned checkpoints, but does not establish trace equivalence.
func Run(ctx context.Context, runtime concurrency.Runtime, script differential.Script) (Result, error) {
	r := &interpreter{ctx: ctx, runtime: runtime, changed: make(chan struct{}),
		plugins: make(map[string]differential.PluginSpec), fibers: make(map[string]*fiber),
		outputs: make(map[string]any), gates: make(map[string]*gate), marks: make(map[string]int), tasks: make(map[string]task)}
	r.result = Result{Trace: differential.Trace{Schema: differential.TraceSchema, Scenario: script.ID,
		Events: []differential.Event{}, Checkpoints: []differential.Checkpoint{}},
		RuntimeSnapshots: make(map[string][]concurrency.Snapshot),
		ComparisonError:  unsupported("comparison", "provision inspection, native config hooks/target epochs, and exact source cleanup/await semantics remain incomplete")}
	if _, ok := runtime.(concurrency.Recorder); !ok {
		r.result.ComparisonError = errors.Join(r.result.ComparisonError,
			unsupported("comparison", "ordered state/body observer or shared Recorder ledger unavailable"))
	}
	if runtime == nil || script.Schema != differential.ScriptSchema {
		return r.result, errors.New("replay requires a Runtime and s0-script/v1")
	}
	if len(script.Contexts) != 0 {
		return r.result, unsupported("contexts", "Runtime has no context parent/realm API")
	}
	if script.Diagnostic != "" {
		return r.result, unsupported("diagnostic", "Go self-disposal is request-only; awaited self-disposal is not emulated")
	}
	for _, plugin := range script.Plugins {
		if _, exists := r.plugins[plugin.ID]; exists || plugin.ID == "" {
			return r.result, fmt.Errorf("invalid plugin %q", plugin.ID)
		}
		if plugin.Update != "" || len(plugin.ConfigTransform) != 0 {
			return r.result, unsupported("plugin config/update hooks", "Runtime has no hook continuation API")
		}
		for _, need := range plugin.Needs {
			if need != nil {
				return r.result, unsupported("needs", "only required, ordinary string keys are supported")
			}
		}
		r.plugins[plugin.ID] = plugin
		if err := validate(plugin.Apply); err != nil {
			return r.result, err
		}
		if err := validate(plugin.Cleanup); err != nil {
			return r.result, err
		}
	}
	if err := validate(script.Steps); err != nil {
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
	result.RuntimeSnapshots = make(map[string][]concurrency.Snapshot, len(r.result.RuntimeSnapshots))
	for label, snapshots := range r.result.RuntimeSnapshots {
		result.RuntimeSnapshots[label] = append([]concurrency.Snapshot(nil), snapshots...)
	}
	fatal := append(append([]error(nil), r.fatal...), r.recordErrors...)
	r.mu.Unlock()
	return result, errors.Join(append([]error{err, eventErr}, fatal...)...)
}

func unsupported(op, reason string) error { return &UnsupportedError{op, reason} }

func validate(actions []differential.Action) error {
	for _, a := range actions {
		if a.Context != "" {
			return unsupported(a.Op, "explicit context targeting needs parent/realm support")
		}
		switch a.Op {
		case "mount", "settle", "snapshot", "waitMark", "mark", "gate", "release", "output", "when", "fail", "awaitTask":
		case "provide", "read":
			if a.Mode != "" || a.To != "" && a.Op == "provide" {
				return unsupported(a.Op, "service/get/config/disposer modes need a native API")
			}
		case "update", "dispose", "awaitFiber":
			if a.Mode != "" {
				return unsupported(a.Op, "nondefault mode needs config hooks")
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
		case "service", "service:publish", "service:remove":
			value := event.Value
			if event.State != concurrency.Active {
				value = undefined()
			}
			appendEvent("service", map[string]any{"key": event.Key, "realm": "root:" + event.Key, "value": value})
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
	value := r.evaluate(a.Value, env)
	switch a.Op {
	case "mount":
		if env.fiber != nil {
			return unsupported(a.Op, "nested mounting needs parent/child lifecycle support")
		}
		return r.mount(a, r.evaluate(a.Config, env))
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
			config := r.evaluate(a.Config, env)
			r.mu.Lock()
			f.raw = clone(config) // Publish under lock before the restart command.
			r.mu.Unlock()
			err = r.runtime.Update(r.ctx, f.id)
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

func (r *interpreter) mount(a differential.Action, config any) error {
	r.mu.Lock()
	plugin, ok := r.plugins[a.Plugin]
	_, exists := r.fibers[a.Fiber]
	if !ok || exists || a.Fiber == "" {
		r.mu.Unlock()
		return errors.New("invalid mount")
	}
	f := &fiber{name: a.Fiber, plugin: a.Plugin, raw: clone(config), applied: undefined()}
	for key := range plugin.Needs {
		f.needs = append(f.needs, key)
	}
	// Needs is a JSON object represented by a Go map. This is declaration order
	// for the candidate only; multi-key source insertion order needs protocol help.
	sort.Strings(f.needs)
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
	id, err := r.runtime.Mount(r.ctx, concurrency.Spec{ID: f.name, Needs: f.needs, Provides: provides, Apply: func(c *concurrency.Context) error {
		r.mu.Lock()
		config := clone(f.raw)
		f.applied, f.ctx, f.startupFailure = config, c, nil
		r.eventLocked("apply", map[string]any{"fiber": f.name, "config": config})
		r.mu.Unlock()
		env := environment{fiber: f, ctx: c, config: config, std: c.Std()}
		var successful atomic.Bool
		// Runtime has no Apply-returned disposer slot. Pre-own a guarded slot so
		// request-only selfDispose still joins cleanup returned by successful Apply.
		// This is a documented Go binding; its effect ordering is not oracle equality.
		if len(plugin.Cleanup) != 0 {
			if err := c.Defer("plugin:"+f.name, func(ctx context.Context) error {
				if !successful.Load() {
					return nil
				}
				r.event("cleanup", map[string]any{"fiber": f.name})
				cleanup := env
				cleanup.std = ctx
				return r.capture(f, r.run(plugin.Cleanup, cleanup))
			}); err != nil {
				return r.capture(f, err)
			}
		}
		err := r.run(plugin.Apply, env)
		successful.Store(err == nil)
		r.mu.Lock()
		f.startupFailure = err
		r.mu.Unlock()
		return r.capture(f, err)
	}})
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
	snapshots, err := r.runtime.Snapshots(r.ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if label == "" {
		return errors.New("empty checkpoint label")
	}
	if _, exists := r.result.RuntimeSnapshots[label]; exists {
		return fmt.Errorf("duplicate checkpoint %q", label)
	}
	r.result.RuntimeSnapshots[label] = snapshots
	byID := make(map[uint64]concurrency.Snapshot)
	for _, s := range snapshots {
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
		if s.State == concurrency.Active || s.State == concurrency.Loading || s.State == concurrency.Unloading {
			if len(s.Epoch) == len(f.needs) {
				epoch = &differential.Epoch{Revision: int(s.Revision), Needs: []differential.Binding{}}
				for i, key := range f.needs {
					epoch.Needs = append(epoch.Needs, differential.Binding{Key: key, ProvisionUID: s.Epoch[i]})
				}
			}
		}
		checkpoint.States = append(checkpoint.States, differential.FiberSnapshot{Fiber: f.name,
			State: string(s.State), FiberUID: s.ID, Revision: int(s.Revision), Epoch: epoch,
			RawConfig: clone(f.raw), EffectiveConfig: clone(f.applied)})
	}
	r.result.Trace.Checkpoints = append(r.result.Trace.Checkpoints, checkpoint)
	return nil
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
		if _, ok := v["$meta"]; ok {
			return undefined() // No declared contexts accepted by this binding.
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
