// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// activation is the small action-engine bridge shared by native and composed
// activations. Composition retains the actual native captured context internally;
// this interface is not a replacement lifecycle or a mock context tree.
type activation interface {
	Config() any
	Std() context.Context
	Provide(string, any) error
	Read(string) (any, error)
	Defer(string, cc.Disposer) error
	Dispose() error
}

// RunComposedJSON executes neutral script data through the shared action engine,
// genuine composition metadata, and the caller's actual scheduler. It never reads
// Script.Expect. Ordinary source keys become open, typed Key[any] values at major
// zero; stronger bound-key assertions live separately in package composition.
// Root provisions use the actual trusted root activation; unchecked plugin
// reads remain explicitly unsupported.
// ComparisonError is capability information, not a source-equivalence verdict.
func RunComposedJSON(ctx context.Context, backend cc.Runtime, data []byte) (Result, error) {
	script, err := differential.ParseScript(data)
	if err != nil {
		return Result{}, err
	}
	order, err := decodeNeedsOrder(data)
	if err != nil {
		return Result{}, err
	}
	if err := applyMissingValues(data, script); err != nil {
		return Result{}, err
	}
	if backend == nil {
		return Result{}, errors.New("composed replay requires a Runtime")
	}
	// The forwarding observation adapter must not advertise capabilities that its
	// underlying runtime does not possess.
	if _, ok := backend.(cc.Inspector); !ok {
		return Result{}, unsupported("composed observations", "native Inspector required")
	}
	if _, ok := backend.(cc.Recorder); !ok {
		return Result{}, unsupported("composed observations", "native shared Recorder ledger required")
	}
	observed := &composedObservations{Runtime: backend}
	frontend := &composedFrontend{script: script, orderedNeeds: order}
	result, runErr := runAdapted(ctx, observed, *script, order, frontend)
	decodeErr := frontend.decode(&result)
	// Preserve the unmodified native diagnostic envelopes (including typed
	// provision records). Only the public projection uses neutral observations.
	events, eventErr := backend.Events(ctx)
	result.RuntimeEvents = events
	return result, errors.Join(runErr, decodeErr, eventErr)
}

type composedAddress struct{ key, realm string }

type composedFrontend struct {
	script       *differential.Script
	orderedNeeds map[string][]string
	r            *interpreter
	root         *co.RootAuthority
	hooks        *hookBundle
	updates      *updateHookAdapter
	effects      *effectRequests
	keys         map[string]cm.Key[any]
	keyOrder     []string
	entries      map[string]cm.Plugin
	mu           sync.RWMutex
	contexts     map[string]*composedActivation
	labels       map[string]cm.Label
	realmNames   map[cm.Label]string
	addresses    map[string]composedAddress
}

func (f *composedFrontend) init(r *interpreter) error {
	f.r = r
	f.keys = make(map[string]cm.Key[any])
	f.entries = make(map[string]cm.Plugin)
	f.contexts = make(map[string]*composedActivation)
	f.labels = make(map[string]cm.Label)
	f.realmNames = make(map[cm.Label]string)
	f.addresses = make(map[string]composedAddress)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, exists := f.keys[name]; exists {
			return
		}
		f.keys[name] = cm.NewKey[any](name, 0)
		f.keyOrder = append(f.keyOrder, name)
	}
	var collect func([]differential.Action)
	collect = func(actions []differential.Action) {
		for _, action := range actions {
			if action.Op != "listen" && action.Op != "dispatch" {
				add(action.Key)
			}
			for _, key := range action.Keys {
				add(key)
			}
			collect(action.Program)
			collect(action.Cleanup)
		}
	}
	for _, context := range f.script.Contexts {
		if context.Isolate != nil {
			add(context.Isolate.Key)
		}
		if context.Intercept != nil {
			add(context.Intercept.Key)
		}
	}
	var entries []cm.Plugin
	for _, plugin := range f.script.Plugins {
		entry := cm.NewEntry(plugin.ID)
		f.entries[plugin.ID] = entry
		entries = append(entries, entry)
		for _, key := range f.orderedNeeds[plugin.ID] {
			add(key)
		}
		collect(plugin.Apply)
		collect(plugin.Cleanup)
	}
	collect(f.script.Steps)
	keys := make([]cm.AnyKey, 0, len(f.keyOrder))
	for _, name := range f.keyOrder {
		keys = append(keys, f.keys[name].Any())
	}
	keys = append(keys, f.hookKeys()...)
	root, err := co.NewRuntime(r.runtime, cm.RootSpec{Entries: entries, Keys: keys})
	if err != nil {
		return err
	}
	f.root = root
	if err := f.initHooks(); err != nil {
		return err
	}
	rootContext := &composedActivation{frontend: f, context: root.Context(), trusted: true}
	f.contexts["root"] = rootContext
	if err := f.register(rootContext.context); err != nil {
		return err
	}
	for _, spec := range f.script.Contexts {
		f.mu.RLock()
		parent := f.contexts[spec.Parent]
		f.mu.RUnlock()
		if err := f.derive(spec.ID, parent, spec.Meta, spec.Isolate, spec.Intercept); err != nil {
			return err
		}
	}
	return nil
}

// register records addresses actually compiled by composition. Opaque labels and
// parent/declaration semantics belong exclusively to contextmodel, not this map.
func (f *composedFrontend) register(context *co.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, name := range f.keyOrder {
		key := f.keys[name].Any()
		address, err := context.NativeAddress(key)
		if err != nil {
			return err
		}
		label, err := context.Metadata().Realm(key)
		if err != nil {
			return err
		}
		realm := "root:" + name
		if label != (cm.Label{}) {
			var exists bool
			realm, exists = f.realmNames[label]
			if !exists {
				return unsupported("realm projection", "native opaque label has no declared script name")
			}
		}
		value := composedAddress{key: name, realm: realm}
		if old, exists := f.addresses[address]; exists && old != value {
			return errors.New("composition returned a conflicting native address")
		}
		f.addresses[address] = value
	}
	return nil
}

func (f *composedFrontend) derive(id string, parent *composedActivation, meta map[string]any, isolate *differential.IsolationSpec, intercept *differential.InterceptSpec) error {
	f.mu.Lock()
	_, exists := f.contexts[id]
	f.mu.Unlock()
	if parent == nil || exists || id == "" {
		return fmt.Errorf("invalid context %s", id)
	}
	context := parent.context.Extend()
	if len(meta) != 0 {
		// Values live on the genuine immutable contextmodel tree. Cloning the
		// neutral input keeps caller-owned JSON containers out of metadata;
		// entry/scope/provision authority is never derived from value names.
		context = context.WithValues(clone(meta).(map[string]any))
	}
	if isolate != nil {
		key, ok := f.keys[isolate.Key]
		if !ok {
			return cm.ErrKey
		}
		name := isolate.Key + ":@" + id
		if isolate.Label != "" {
			name = isolate.Key + ":" + isolate.Label
		}
		f.mu.Lock()
		label, exists := f.labels[name]
		if !exists {
			label = cm.NewLabel(name)
			f.labels[name] = label
			f.realmNames[label] = name
		}
		f.mu.Unlock()
		var err error
		context, err = context.Isolate(key.Any(), label)
		if err != nil {
			return err
		}
	}
	if intercept != nil {
		key, ok := f.keys[intercept.Key]
		if !ok {
			return cm.ErrKey
		}
		var err error
		context, err = context.Intercept(key.Any(), clone(intercept.Config))
		if err != nil {
			return err
		}
	}
	if err := f.register(context); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.contexts[id]; exists {
		return fmt.Errorf("invalid context %s", id)
	}
	f.contexts[id] = &composedActivation{frontend: f, context: context, owner: parent.owner, trusted: parent.trusted}
	return nil
}

func (f *composedFrontend) validate(actions []differential.Action) error {
	for _, action := range actions {
		if handled, err := f.validateUpdateHook(action); handled {
			if err != nil {
				return err
			}
			continue
		}
		if handled, err := f.validateHook(action); handled {
			if err != nil {
				return err
			}
			continue
		}
		if handled, err := f.validateEffect(action); handled {
			if err != nil {
				return err
			}
			continue
		}
		switch action.Op {
		case "child", "notify":
		case "context":
			if action.Mode != "" && action.Mode != "isolate" && action.Mode != "intercept" {
				return unsupported("context", "unknown derivation mode")
			}
		case "provide":
			if action.To != "" || action.Mode != "" && action.Mode != "service" && action.Mode != "available" {
				return unsupported("provide", "withdrawable provision modes are not implemented by composed replay")
			}
		case "read":
			if action.Mode != "" && action.Mode != "get" && action.Mode != "config" {
				return unsupported("read", "unknown typed service read mode")
			}
		default:
			plain := action
			plain.Context, plain.Program, plain.Cleanup = "", nil, nil
			if err := validate([]differential.Action{plain}); err != nil {
				return err
			}
		}
		if err := f.validate(action.Program); err != nil {
			return err
		}
		if err := f.validate(action.Cleanup); err != nil {
			return err
		}
	}
	return nil
}

func (f *composedFrontend) selected(action differential.Action, env environment) (*composedActivation, error) {
	name := action.Context
	if name == "" && action.Op != "mount" && action.Op != "child" && action.Fiber != "" {
		name = action.Fiber
	}
	if name != "" {
		f.mu.RLock()
		context := f.contexts[name]
		f.mu.RUnlock()
		if context == nil {
			return nil, fmt.Errorf("unknown context %s", name)
		}
		return context, nil
	}
	if current, ok := env.ctx.(*composedActivation); ok {
		return current, nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.contexts["root"], nil
}

func (f *composedFrontend) execute(action differential.Action, env environment) (bool, error) {
	if handled, err := f.executeHook(action, env); handled {
		return true, err
	}
	if handled, err := f.executeEffect(action, env); handled {
		return true, err
	}
	switch action.Op {
	case "child":
		caller, err := f.selected(action, env)
		if err != nil {
			return true, err
		}
		env.ctx = caller
		return true, f.r.mount(action, f.r.evaluate(action.Config, env), env)
	case "context", "read", "provide", "notify":
		caller, err := f.selected(action, env)
		if err != nil {
			return true, err
		}
		env.ctx = caller
		switch action.Op {
		case "context":
			var isolate *differential.IsolationSpec
			var intercept *differential.InterceptSpec
			if action.Mode == "isolate" {
				isolate = &differential.IsolationSpec{Key: action.Key, Label: action.Label}
			}
			if action.Mode == "intercept" {
				intercept = &differential.InterceptSpec{Key: action.Key, Config: f.r.evaluate(action.Config, env)}
			}
			var meta map[string]any
			if action.Meta != nil {
				var ok bool
				meta, ok = f.r.evaluate(action.Meta, env).(map[string]any)
				if !ok {
					return true, unsupported("context metadata", "evaluated extension metadata must be a neutral JSON object")
				}
			}
			return true, f.derive(action.To, caller, meta, isolate, intercept)
		case "notify":
			keys := make([]cm.AnyKey, 0, len(action.Keys))
			for _, name := range action.Keys {
				key, ok := f.keys[name]
				if !ok {
					return true, cm.ErrKey
				}
				keys = append(keys, key.Any())
			}
			return true, f.root.Notify(f.r.ctx, caller.context, keys...)
		case "provide":
			value := clone(f.r.evaluate(action.Value, env))
			if action.Mode == "service" || action.Mode == "available" {
				value = &composedService{value: value, availability: action.Mode == "available"}
			}
			return true, caller.Provide(action.Key, value)
		case "read":
			var value any
			var err error
			if action.Mode == "get" {
				if !caller.trusted {
					return true, unsupported("read get", "unchecked plugin reads are outside the accepted typed context contract")
				}
				value, err = f.inspectValue(caller, action.Key, action.Strict == nil || *action.Strict)
			} else if caller.trusted {
				if action.Mode == "config" {
					return true, unsupported("read config", "a root observer has no committed typed service binding")
				}
				// A source fiber-less property access is an unchecked, non-strict
				// store observation. It is not a plugin optional read capability.
				value, err = f.inspectValue(caller, action.Key, false)
			} else {
				value, err = caller.Read(action.Key)
			}
			if err != nil {
				return true, err
			}
			if action.Mode == "config" {
				if _, ok := value.(*composedService); !ok {
					return true, unsupported("read config", "the actual provision is not a source fixture Service")
				}
				key := f.keys[action.Key]
				configs := []any{f.r.evaluate(action.Base, env)}
				configs = append(configs, cm.Intercepts[any](caller.context.Metadata(), key.Any())...)
				configs = append(configs, f.r.evaluate(action.Head, env))
				merged := make(map[string]any)
				for _, config := range configs {
					if config == nil {
						continue
					}
					object, ok := config.(map[string]any)
					if !ok {
						return true, unsupported("service config", "only ordinary JSON object shallow merges are supported")
					}
					for name, value := range object {
						merged[name] = clone(value)
					}
				}
				value = merged
			}
			f.r.put(action.To, value)
			return true, nil
		}
	}
	if action.Context != "" {
		caller, err := f.selected(action, env)
		if err != nil {
			return true, err
		}
		action.Context = ""
		env.ctx = caller
		// Mount must retain its explicit parent selector until frontend.mount.
		if action.Op == "mount" {
			return true, f.r.mount(action, f.r.evaluate(action.Config, env), env)
		}
		return true, f.r.execute(action, env)
	}
	return false, nil
}

func (f *composedFrontend) meta(path any, env environment) any {
	caller, ok := env.ctx.(*composedActivation)
	if !ok {
		f.mu.RLock()
		caller = f.contexts["root"]
		f.mu.RUnlock()
	}
	if caller == nil {
		return undefined()
	}
	var value any = caller.context.Metadata().Values()
	if name := fmt.Sprint(path); name != "" {
		for _, key := range strings.Split(name, ".") {
			object, ok := value.(map[string]any)
			if !ok {
				return undefined()
			}
			value, ok = object[key]
			if !ok {
				return undefined()
			}
		}
	}
	return clone(value)
}

func (f *composedFrontend) mount(action differential.Action, env environment, target *fiber, plugin differential.PluginSpec, provides []string, apply func(activation) (cc.Disposer, error)) (uint64, error) {
	parent, err := f.selected(action, env)
	if err != nil {
		return 0, err
	}
	metadata := cm.Spec{ID: target.name}
	for _, name := range f.orderedNeeds[plugin.ID] {
		key := f.keys[name]
		if config := plugin.Needs[name]; config != nil {
			metadata.Inject = append(metadata.Inject, cm.NeedWith(key, clone(config)))
		} else {
			metadata.Inject = append(metadata.Inject, cm.Need(key))
		}
	}
	for _, name := range provides {
		metadata.Provides = append(metadata.Provides, f.keys[name].Any())
	}
	bind := func(context *co.Context, actual bool) (*composedActivation, error) {
		if err := f.register(context); err != nil {
			return nil, err
		}
		caller := &composedActivation{frontend: f, context: context, owner: target}
		var needs []string
		for _, dep := range context.Metadata().Dependencies() {
			if !dep.Optional() {
				needs = append(needs, dep.Key().Name())
			}
		}
		f.r.mu.Lock()
		target.needs = needs
		f.r.mu.Unlock()
		f.mu.Lock()
		if existing := f.contexts[target.name]; actual || existing == nil {
			f.contexts[target.name] = caller
		}
		f.mu.Unlock()
		return caller, nil
	}
	spec := co.Spec{Metadata: metadata, Config: target.config, Apply: func(context *co.Context) (cc.Disposer, error) {
		caller, err := bind(context, true)
		if err != nil {
			return nil, err
		}
		return apply(caller)
	}}
	var mounted *co.Fiber
	if parent.trusted {
		mounted, err = f.root.Start(f.r.ctx, parent.context, f.entries[plugin.ID], spec)
	} else {
		mounted, err = parent.context.Child(spec)
	}
	if err != nil {
		return 0, err
	}
	if _, err := bind(mounted.Context, false); err != nil {
		return mounted.ID, err
	}
	return mounted.ID, nil
}

// Only a trusted, fiber-less fixture observer can query the native store. A
// missing address or an inactive reservation is observed undefined, not a value
// guessed from plugin bodies or the expected checkpoint.
func (f *composedFrontend) inspectValue(caller *composedActivation, name string, strict bool) (any, error) {
	key, ok := f.keys[name]
	if !ok {
		return nil, cm.ErrKey
	}
	address, err := caller.context.NativeAddress(key.Any())
	if err != nil {
		return nil, err
	}
	inspection, err := f.r.runtime.(cc.Inspector).Inspect(f.r.ctx)
	if err != nil {
		return nil, err
	}
	for _, provision := range inspection.Provisions {
		if provision.Key == address && (!strict || provision.Active) {
			return clone(provision.Value), nil
		}
	}
	return undefined(), nil
}

type composedActivation struct {
	frontend *composedFrontend
	context  *co.Context
	owner    *fiber
	trusted  bool
}

func (c *composedActivation) Config() any          { return c.context.Config() }
func (c *composedActivation) Std() context.Context { return c.context.Std() }
func (c *composedActivation) RequestEffectScope(label string, setup func(*cc.EffectScope) error) (*cc.EffectHandle, error) {
	return c.context.RequestEffectScope(label, setup)
}
func (c *composedActivation) Effect(label string, setup func() (cc.Disposer, error)) error {
	return c.context.Effect(label, setup)
}
func (c *composedActivation) Go(label string, run func(context.Context) error) error {
	return c.context.Go(label, run)
}
func (c *composedActivation) Defer(label string, disposer cc.Disposer) error {
	return c.context.Defer(label, disposer)
}
func (c *composedActivation) Dispose() error { return c.context.Dispose() }
func (c *composedActivation) Provide(name string, value any) error {
	key, ok := c.frontend.keys[name]
	if !ok {
		return cm.ErrKey
	}
	var available func(*co.Context) bool
	if service, ok := value.(*composedService); ok && service.availability {
		available = func(caller *co.Context) bool {
			// Native Spec.Check invokes this with the actual consumer wrapper
			// off the graph owner. No substitute provider/root context, scope
			// authority, expected value, or callback under an interpreter lock.
			audience, exists := caller.Metadata().Value("audience")
			if !exists {
				return false
			}
			audienceName, ok := audience.(string)
			if !ok {
				return false
			}
			object, ok := service.value.(map[string]any)
			if !ok {
				return false
			}
			readyName, ok := object["ready"].(string)
			if !ok {
				return false
			}
			c.frontend.r.mu.Lock()
			defer c.frontend.r.mu.Unlock()
			ready, ok := c.frontend.r.outputs[readyName].(map[string]any)
			return ok && composedTruthy(ready[audienceName])
		}
	}
	if c.trusted {
		return co.ProvideRoot(c.frontend.r.ctx, c.frontend.root, c.context, key, value, available)
	}
	return co.Provide(c.context, key, value, available)
}
func (c *composedActivation) Read(name string) (any, error) {
	key, ok := c.frontend.keys[name]
	if !ok {
		return nil, cm.ErrKey
	}
	return co.Resolve(c.context, key)
}

// The service marker is the actual typed provision value. Its JSON observation
// matches the source fixture's own snapshot(service.value), not a golden value.
type composedService struct {
	value        any
	availability bool
}

func (s *composedService) MarshalJSON() ([]byte, error) { return json.Marshal(s.value) }

// composedTruthy models only neutral JSON values used by the source fixture's
// !!availability[audience]. In particular empty arrays/objects are truthy; a
// missing entry and explicit null are false. It never changes stored values.
func composedTruthy(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case json.Number:
		number, err := value.Float64()
		return err == nil && number != 0
	case float64:
		return value != 0
	default:
		return true
	}
}

// composedObservations unwraps only native immutable ordinary-provision records.
// It never invokes a provider view, availability predicate, or expected assertion.
// All native IDs, key addresses, state transitions and ordering are preserved.
type composedObservations struct{ cc.Runtime }

func (o *composedObservations) RootContext(ctx context.Context) (*cc.Context, error) {
	root, ok := o.Runtime.(cc.RootProvider)
	if !ok {
		return nil, unsupported("root", "native root activation required")
	}
	return root.RootContext(ctx)
}

func (o *composedObservations) Inspect(ctx context.Context) (cc.Inspection, error) {
	inspection, err := o.Runtime.(cc.Inspector).Inspect(ctx)
	if err != nil {
		return cc.Inspection{}, err
	}
	inspection.Provisions = append([]cc.ProvisionSnapshot(nil), inspection.Provisions...)
	for i := range inspection.Provisions {
		value, ok := co.ProvisionObservation(inspection.Provisions[i].Value)
		if !ok {
			return cc.Inspection{}, unsupported("provision observation", "native typed provider view has no immutable published observation")
		}
		inspection.Provisions[i].Value = clone(value)
	}
	return inspection, nil
}
func (o *composedObservations) Events(ctx context.Context) ([]cc.Event, error) {
	events, err := o.Runtime.Events(ctx)
	if err != nil {
		return nil, err
	}
	events = append([]cc.Event(nil), events...)
	for i := range events {
		if events[i].Kind != "service" && events[i].Kind != "service:publish" && events[i].Kind != "service:withdraw" {
			continue
		}
		value, ok := co.ProvisionObservation(events[i].Value)
		if !ok {
			return nil, unsupported("service observation", "native typed provider view has no immutable published observation")
		}
		events[i].Value = clone(value)
	}
	return events, nil
}
func (o *composedObservations) Record(ctx context.Context, kind string, data map[string]any) error {
	return o.Runtime.(cc.Recorder).Record(ctx, kind, data)
}
func (o *composedObservations) AwaitFiber(ctx context.Context, id uint64) error {
	waiter, ok := o.Runtime.(cc.Waiter)
	if !ok {
		return unsupported("awaitFiber", "native target-only Waiter required")
	}
	return waiter.AwaitFiber(ctx, id)
}
func (o *composedObservations) RequestUpdateConfig(ctx context.Context, id uint64, value any, noSave bool) (cc.ConfigRequest, error) {
	requester, ok := o.Runtime.(cc.ConfigRequester)
	if !ok {
		return cc.ConfigRequest{}, unsupported("update", "native ConfigRequester required")
	}
	return requester.RequestUpdateConfig(ctx, id, value, noSave)
}
func (o *composedObservations) Notify(ctx context.Context, keys ...string) error {
	notifier, ok := o.Runtime.(cc.Notifier)
	if !ok {
		return unsupported("notify", "native Notifier required")
	}
	return notifier.Notify(ctx, keys...)
}

func (f *composedFrontend) decode(result *Result) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	lookup := func(address string) (composedAddress, error) {
		value, exists := f.addresses[address]
		if !exists {
			return composedAddress{}, unsupported("address projection", fmt.Sprintf("native address %q has no actual typed context mapping", address))
		}
		return value, nil
	}
	for i := range result.Trace.Events {
		event := &result.Trace.Events[i]
		if event.Kind != "service" {
			continue
		}
		encoded, _ := event.Data["key"].(string)
		address, err := lookup(encoded)
		if err != nil {
			return err
		}
		event.Data["key"], event.Data["realm"] = address.key, address.realm
	}
	for i := range result.Trace.Checkpoints {
		for j := range result.Trace.Checkpoints[i].Provisions {
			provision := &result.Trace.Checkpoints[i].Provisions[j]
			address, err := lookup(provision.Key)
			if err != nil {
				return err
			}
			provision.Key, provision.Realm = address.key, address.realm
		}
	}
	return nil
}
