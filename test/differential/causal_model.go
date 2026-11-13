// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const causalRootActor = "<root-command>"

type causalAddress struct{ key, realm string }
type causalFootprint struct{ reads, writes map[string]bool }
type causalFiber struct {
	plugin    PluginSpec
	context   string
	needs     []string
	pureMount bool
}
type causalCheckpointSpec struct {
	label    string
	fibers   []string
	plugins  int
	commands []Action
}
type causalPluginCommand struct {
	fiber            string
	disposed         bool
	previousDisposed string
}
type causalModel struct {
	script            *Script
	contexts          map[string]map[string]string
	fibers            map[string]*causalFiber
	mountOrder        []string
	providers         map[causalAddress]string
	outputs           map[string]string
	footprints        map[string]*causalFootprint
	conflicts         map[string]map[string]bool
	cuts              []causalCheckpointSpec
	pluginCommands    []causalPluginCommand
	rootCommandActors map[string]bool
}

func causalUnsupported(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCausalUnsupported, fmt.Sprintf(format, args...))
}

func compileCausalModel(raw []byte) (*causalModel, error) {
	script, err := ParseScript(raw)
	if err != nil {
		return nil, err
	}
	if script.Diagnostic != "" {
		return nil, causalUnsupported("diagnostic execution")
	}
	orderedNeeds, err := causalOrderedNeeds(raw)
	if err != nil {
		return nil, err
	}
	m := &causalModel{
		script: script, contexts: map[string]map[string]string{"root": {}},
		fibers: make(map[string]*causalFiber), providers: make(map[causalAddress]string),
		outputs: make(map[string]string), footprints: make(map[string]*causalFootprint),
		conflicts: make(map[string]map[string]bool),
	}
	for _, ctx := range script.Contexts {
		if ctx.Intercept != nil {
			return nil, causalUnsupported("intercept context %q", ctx.ID)
		}
		inherited := make(map[string]string)
		for key, realm := range m.contexts[ctx.Parent] {
			inherited[key] = realm
		}
		if ctx.Isolate != nil {
			key := causalBuiltin(ctx.Isolate.Key)
			// The existing contract renames only the root built-in realm;
			// custom realm labels remain verbatim semantic strings.
			realm := ctx.Isolate.Key + ":@" + ctx.ID
			if ctx.Isolate.Label != "" {
				realm = ctx.Isolate.Key + ":" + ctx.Isolate.Label
			}
			inherited[key] = realm
		}
		m.contexts[ctx.ID] = inherited
	}
	plugins := make(map[string]PluginSpec)
	var supportedSyntax func([]Action) error
	supportedSyntax = func(actions []Action) error {
		for _, a := range actions {
			if err := causalSynchronous(a); err != nil {
				return err
			}
			switch a.Op {
			case "provide":
				if a.Mode != "" || a.To != "" {
					return causalUnsupported("non-ordinary provision syntax")
				}
			case "read", "output", "fail", "when":
			default:
				return causalUnsupported("body operation %q", a.Op)
			}
			if err := supportedSyntax(a.Program); err != nil {
				return err
			}
			if err := supportedSyntax(a.Cleanup); err != nil {
				return err
			}
		}
		return nil
	}
	for _, p := range script.Plugins {
		if p.Update != "" {
			return nil, causalUnsupported("custom update continuation/veto policy")
		}
		// Non-null injection configuration can participate in tracked service
		// resolution; v1 proves only ordinary read-only Needs membership.
		for _, value := range p.Needs {
			if value != nil {
				return nil, causalUnsupported("non-null Needs configuration")
			}
		}
		// Even unused plugin controls are not silently accepted by v1.
		if err := supportedSyntax(p.Apply); err != nil {
			return nil, err
		}
		if err := supportedSyntax(p.Cleanup); err != nil {
			return nil, err
		}
		plugins[p.ID] = p
	}
	// All mounts are root program commands. A fiber name is a single lifetime;
	// dynamic mounts, alias contexts and child activations are not inferred.
	for _, a := range script.Steps {
		if a.Op != "mount" {
			continue
		}
		p, ok := plugins[a.Plugin]
		if !ok || m.fibers[a.Fiber] != nil || a.Fiber == "root" || a.Fiber == causalRootActor {
			return nil, causalUnsupported("non-unique/unknown fixed mount %q", a.Fiber)
		}
		context := a.Context
		if context == "" {
			context = "root"
		}
		if _, ok := m.contexts[context]; !ok {
			return nil, causalUnsupported("non-static mount context %q", context)
		}
		m.fibers[a.Fiber] = &causalFiber{plugin: p, context: context, needs: orderedNeeds[p.ID], pureMount: causalPureMountConfig(a.Config)}
		m.mountOrder = append(m.mountOrder, a.Fiber)
	}
	for _, actor := range append([]string{causalRootActor}, m.mountOrder...) {
		m.footprints[actor] = &causalFootprint{reads: make(map[string]bool), writes: make(map[string]bool)}
	}
	// Compile entire synchronous activations, not just the action which happened
	// to emit a particular event. An unobserved $output read still causes conflict.
	for _, actor := range m.mountOrder {
		f := m.fibers[actor]
		if err := m.references(actor, f.plugin.ConfigTransform); err != nil {
			return nil, err
		}
		for _, value := range f.plugin.Needs {
			if err := m.references(actor, value); err != nil {
				return nil, err
			}
		}
		for _, key := range f.needs {
			m.footprints[actor].reads[m.serviceCell(f.context, key)] = true
		}
		if err := m.body(f.plugin.Apply, actor, false); err != nil {
			return nil, err
		}
		if err := m.body(f.plugin.Cleanup, actor, true); err != nil {
			return nil, err
		}
	}
	if err := m.rootSteps(); err != nil {
		return nil, err
	}
	// Needs edges are validated for cycles independently of trace scheduling.
	dependencies := make(map[string][]string)
	for _, actor := range m.mountOrder {
		f := m.fibers[actor]
		for _, key := range f.needs {
			if p := m.providers[m.address(f.context, key)]; p != "" {
				dependencies[actor] = append(dependencies[actor], p)
				m.link(actor, p)
			}
		}
	}
	visiting, visited := make(map[string]bool), make(map[string]bool)
	var visit func(string) bool
	visit = func(actor string) bool {
		if visiting[actor] {
			return false
		}
		if visited[actor] {
			return true
		}
		visiting[actor] = true
		for _, p := range dependencies[actor] {
			if !visit(p) {
				return false
			}
		}
		visiting[actor], visited[actor] = false, true
		return true
	}
	for _, actor := range m.mountOrder {
		if !visit(actor) {
			return nil, causalUnsupported("static Needs cycle")
		}
	}
	actors := append([]string{causalRootActor}, m.mountOrder...)
	for i, a := range actors {
		for _, b := range actors[i+1:] {
			af, bf := m.footprints[a], m.footprints[b]
			for cell := range af.writes {
				if bf.reads[cell] || bf.writes[cell] {
					m.link(a, b)
				}
			}
			for cell := range bf.writes {
				if af.reads[cell] || af.writes[cell] {
					m.link(a, b)
				}
			}
		}
	}
	return m, nil
}

// Metadata order, never event/provision order: JavaScript Object.keys visits
// integer-index keys numerically, then the remaining original JSON keys. A map
// decode alone would lose the ordered Needs contract.
func causalOrderedNeeds(raw []byte) (map[string][]string, error) {
	var doc struct {
		Plugins []struct {
			ID    string          `json:"id"`
			Needs json.RawMessage `json:"needs"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	result := make(map[string][]string)
	for _, p := range doc.Plugins {
		keys := make([]string, 0)
		if len(p.Needs) != 0 && string(p.Needs) != "null" {
			dec := json.NewDecoder(bytes.NewReader(p.Needs))
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := token.(string)
				if !ok {
					return nil, causalUnsupported("non-string Needs key")
				}
				var value json.RawMessage
				if err := dec.Decode(&value); err != nil {
					return nil, err
				}
				keys = append(keys, key)
			}
		}
		numeric := make([]string, 0)
		ordinary := make([]string, 0)
		for _, key := range keys {
			n, err := strconv.ParseUint(key, 10, 32)
			if err == nil && n < 1<<32-1 && strconv.FormatUint(n, 10) == key {
				numeric = append(numeric, key)
			} else {
				ordinary = append(ordinary, key)
			}
		}
		sort.Slice(numeric, func(i, j int) bool {
			a, _ := strconv.ParseUint(numeric[i], 10, 32)
			b, _ := strconv.ParseUint(numeric[j], 10, 32)
			return a < b
		})
		seen := make(map[string]bool)
		for _, key := range append(numeric, ordinary...) {
			key = causalBuiltin(key)
			if key == "" || seen[key] {
				return nil, causalUnsupported("ambiguous Needs key after built-in rename")
			}
			seen[key] = true
			result[p.ID] = append(result[p.ID], key)
		}
	}
	return result, nil
}

func (m *causalModel) address(context, key string) causalAddress {
	key = causalBuiltin(key)
	realm := m.contexts[context][key]
	if realm == "" {
		realm = "root:" + key
	}
	return causalAddress{key, realm}
}
func (m *causalModel) serviceCell(context, key string) string {
	a := m.address(context, key)
	return "service:" + causalJSON([]string{a.key, a.realm})
}
func (m *causalModel) link(a, b string) {
	if m.conflicts[a] == nil {
		m.conflicts[a] = make(map[string]bool)
	}
	if m.conflicts[b] == nil {
		m.conflicts[b] = make(map[string]bool)
	}
	m.conflicts[a][b], m.conflicts[b][a] = true, true
}
func (m *causalModel) actorsConflict(a, b string) bool { return a == b || m.conflicts[a][b] }
func (m *causalModel) eventsConflict(a, b causalObservation) bool {
	if a.actor == b.actor || a.rootLane && b.rootLane {
		return true
	}
	// A pure mount publication only declares this fixed root fiber. Its Needs
	// do not make the declaration a service read. Disposal is never exempted.
	if a.pureMount || b.pureMount {
		return false
	}
	publication, loading := a, b
	if !publication.publication {
		publication, loading = b, a
	}
	if publication.publication && loading.startupLoading && loading.startupProviders[publication.serviceAddress] == publication.publicationActivation && m.directNeedsAddress(loading.actor, publication.serviceAddress) && !m.nonServiceConflict(publication.actor, loading.actor) {
		// Both streams already proved all providers ACTIVE and the local last
		// enabling provider's status-before-notification bracket. Which branch
		// completed readiness is schedule-dependent, not event semantics.
		return false
	}
	teardownPair := func(provider, consumer causalObservation) bool {
		if !provider.unloadingActive || !consumer.unloadingRunning || m.nonServiceConflict(provider.actor, consumer.actor) {
			return false
		}
		for address, activation := range consumer.teardownProviders {
			if m.providers[address] == provider.actor && activation == provider.unloadingActivation {
				return true
			}
		}
		return false
	}
	if teardownPair(a, b) || teardownPair(b, a) {
		return false
	}
	return m.actorsConflict(a.actor, b.actor)
}

func causalPureMountConfig(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "$output" || key == "$meta" || key == "$config" || !causalPureMountConfig(child) {
				return false
			}
		}
	case []any:
		for _, child := range v {
			if !causalPureMountConfig(child) {
				return false
			}
		}
	}
	return true
}

func (m *causalModel) directNeedsAddress(actor string, address causalAddress) bool {
	fiber := m.fibers[actor]
	if fiber == nil {
		return false
	}
	for _, key := range fiber.needs {
		if m.address(fiber.context, key) == address {
			return true
		}
	}
	return false
}

func (m *causalModel) nonServiceConflict(a, b string) bool {
	af, bf := m.footprints[a], m.footprints[b]
	if af == nil || bf == nil {
		return true
	}
	for cell := range af.writes {
		if !strings.HasPrefix(cell, "service:") && (bf.reads[cell] || bf.writes[cell]) {
			return true
		}
	}
	for cell := range bf.writes {
		if !strings.HasPrefix(cell, "service:") && (af.reads[cell] || af.writes[cell]) {
			return true
		}
	}
	return false
}

func (m *causalModel) output(actor, name string) error {
	if name == "" {
		return nil
	}
	if owner := m.outputs[name]; owner != "" && owner != actor {
		return causalUnsupported("output %q has ambiguous owner", name)
	}
	if strings.Contains(name, ".") {
		return causalUnsupported("ambiguous dotted output cell %q", name)
	}
	m.outputs[name] = actor
	m.footprints[actor].writes["output:"+name] = true
	return nil
}
func (m *causalModel) references(actor string, value any) error {
	switch v := value.(type) {
	case map[string]any:
		// Meta can expose runtime context/fiber/service objects; static labels
		// do not prove that arbitrary metadata paths are immutable data.
		if _, exists := v["$meta"]; exists {
			return causalUnsupported("runtime metadata expression")
		}
		if output, exists := v["$output"]; exists {
			name, ok := output.(string)
			if !ok || name == "" {
				return causalUnsupported("dynamic/whole output-map reference")
			}
			m.footprints[actor].reads["output:"+strings.Split(name, ".")[0]] = true
		}
		for _, nested := range v {
			if err := m.references(actor, nested); err != nil {
				return err
			}
		}
	case map[string]string:
		for _, nested := range v {
			if err := m.references(actor, nested); err != nil {
				return err
			}
		}
	case []any:
		for _, nested := range v {
			if err := m.references(actor, nested); err != nil {
				return err
			}
		}
	}
	return nil
}
func causalSynchronous(a Action) error {
	if a.Task != "" || a.Await != nil && !*a.Await || a.Count != 0 || a.Catch || a.Global || a.Symbol || a.Prepend {
		return causalUnsupported("async/control/catch metadata on %q", a.Op)
	}
	return nil
}
func (m *causalModel) body(actions []Action, actor string, cleanup bool) error {
	f := m.fibers[actor]
	for _, a := range actions {
		if err := causalSynchronous(a); err != nil {
			return err
		}
		if a.Context != "" || a.Fiber != "" || len(a.Cleanup) != 0 {
			return causalUnsupported("body context override or cleanup callback on %q", a.Op)
		}
		if a.Op != "when" && len(a.Program) != 0 {
			return causalUnsupported("nested body on %q", a.Op)
		}
		if err := m.references(actor, a.Value); err != nil {
			return err
		}
		if err := m.references(actor, a.Base); err != nil {
			return err
		}
		switch a.Op {
		case "provide":
			if cleanup || a.Mode != "" || a.To != "" {
				return causalUnsupported("non-ordinary/dynamic provision")
			}
			address := m.address(f.context, a.Key)
			if m.providers[address] != "" {
				return causalUnsupported("non-unique provider for %s/%s", address.key, address.realm)
			}
			m.providers[address] = actor
			m.footprints[actor].writes[m.serviceCell(f.context, a.Key)] = true
		case "read":
			if a.Mode != "" && a.Mode != "get" {
				return causalUnsupported("tracked/dynamic service read")
			}
			m.footprints[actor].reads[m.serviceCell(f.context, a.Key)] = true
			if err := m.output(actor, a.To); err != nil {
				return err
			}
		case "output":
			if err := m.output(actor, a.To); err != nil {
				return err
			}
		case "fail":
		case "when":
			// Both branches contribute conservative footprints. Conditional
			// provision creation needs a richer lifetime witness and is excluded.
			var hasProvide func([]Action) bool
			hasProvide = func(program []Action) bool {
				for _, nested := range program {
					if nested.Op == "provide" || hasProvide(nested.Program) {
						return true
					}
				}
				return false
			}
			if hasProvide(a.Program) {
				return causalUnsupported("conditional provision")
			}
			if err := m.body(a.Program, actor, cleanup); err != nil {
				return err
			}
		default:
			return causalUnsupported("body operation %q", a.Op)
		}
	}
	return nil
}

// observe validates the sidecar against facts reconstructible from the raw
// stream. A hash is binding, not authenticity: the recorder is still responsible
// for observing the cut. Impossible output/state/plugin prefixes are rejected.
func (m *causalModel) observe(trace *Trace, witness CausalWitness) ([]causalObservation, error) {
	observations := make([]causalObservation, len(trace.Events))
	states := make(map[string]string)
	outputs := make(map[string]any)
	configs := make(map[string]any)
	disposed := make(map[string]bool)
	activations := make(map[string]int)
	applied, failed := make(map[string]bool), make(map[string]bool)
	bound := make(map[string]map[causalAddress]int)
	retiring := make(map[string]int)
	mutationAnchors := make(map[string]bool)
	segment := 0
	mutationCommand := func(actor string) *Action {
		if segment >= len(m.cuts) {
			return nil
		}
		for i := range m.cuts[segment].commands {
			command := &m.cuts[segment].commands[i]
			if command.Fiber == actor && command.Op == "update" {
				return command
			}
		}
		return nil
	}
	type observedProvision struct {
		value      any
		activation int
	}
	store := make(map[causalAddress]observedProvision)
	invalidated := make(map[causalAddress]bool)
	published := make(map[causalAddress]int)
	// For every provider activation, these mounted pending consumers became
	// ready at its ACTIVE transition and must enter LOADING before publication.
	triggered := make(map[string]map[string]bool)
	readiness := func(actor string) map[causalAddress]int {
		fiber := m.fibers[actor]
		bindings := make(map[causalAddress]int)
		for _, key := range fiber.needs {
			address := m.address(fiber.context, key)
			provider := m.providers[address]
			if provider == "" || states[provider] != "ACTIVE" || activations[provider] == 0 {
				return nil
			}
			bindings[address] = activations[provider]
		}
		return bindings
	}
	pluginCount, nextCut := 0, 0
	check := func(ci int) error {
		cp, spec := &trace.Checkpoints[ci], m.cuts[ci]
		if pluginCount != spec.plugins || len(cp.States) != len(spec.fibers) {
			return fmt.Errorf("checkpoint %q contradicts root mount/disposal prefix", cp.Label)
		}
		if !causalEqual(cp.Outputs, outputs) {
			return fmt.Errorf("checkpoint %q contradicts output-event prefix", cp.Label)
		}
		if len(cp.Provisions) != len(store) {
			return fmt.Errorf("checkpoint %q contradicts observed provision-store domain", cp.Label)
		}
		seenProvisions := make(map[causalAddress]bool)
		for _, provision := range cp.Provisions {
			address := causalAddress{provision.Key, provision.Realm}
			observed, ok := store[address]
			if !ok || seenProvisions[address] || provision.Provider != m.providers[address] || !causalEqual(provision.Value, observed.value) || provision.Active != (states[provision.Provider] == "ACTIVE") {
				return fmt.Errorf("checkpoint %q contradicts published provision ownership/value/state", cp.Label)
			}
			seenProvisions[address] = true
		}
		for i, state := range cp.States {
			if state.Fiber != spec.fibers[i] || state.State != states[state.Fiber] {
				return fmt.Errorf("checkpoint %q contradicts ordered fiber/status prefix", cp.Label)
			}
			if disposed[state.Fiber] != (state.FiberUID == nil) {
				return fmt.Errorf("checkpoint %q contradicts live fiber identity", cp.Label)
			}
			if state.State == "DISPOSED" && !disposed[state.Fiber] {
				return fmt.Errorf("disposed state without root disposal")
			}
			if state.State == "ACTIVE" && state.Epoch == nil {
				return fmt.Errorf("active state without epoch")
			}
			if state.State == "LOADING" || state.State == "UNLOADING" {
				return causalUnsupported("unsettled lifecycle checkpoint without phase witness")
			}
			if state.State == "ACTIVE" {
				for address, provider := range m.providers {
					if provider == state.Fiber {
						if _, ok := store[address]; !ok {
							return fmt.Errorf("active provider lacks observed publication")
						}
					}
				}
			}
			if state.Epoch != nil {
				needs := m.fibers[state.Fiber].needs
				if len(needs) != len(state.Epoch.Needs) {
					return fmt.Errorf("checkpoint %q Needs count mismatch", cp.Label)
				}
				for ni, key := range needs {
					if key != state.Epoch.Needs[ni].Key {
						return fmt.Errorf("checkpoint %q Needs JSON order mismatch", cp.Label)
					}
				}
			}
			if config, ok := configs[state.Fiber]; ok && !causalEqual(config, state.EffectiveConfig) {
				return fmt.Errorf("checkpoint %q differs from captured effective config", cp.Label)
			}
		}
		return nil
	}
	for index := 0; index <= len(trace.Events); index++ {
		for nextCut < len(witness.Checkpoints) && witness.Checkpoints[nextCut].AfterEvent == index {
			if err := check(nextCut); err != nil {
				return nil, err
			}
			nextCut++
			segment = nextCut
			mutationAnchors = make(map[string]bool)
		}
		if index == len(trace.Events) {
			break
		}
		e := trace.Events[index]
		actor := ""
		rootLane := false
		observation := causalObservation{}
		switch e.Kind {
		case "status", "plugin", "config", "apply", "update", "commit", "cleanup", "error":
			actor, _ = e.Data["fiber"].(string)
			rootLane = e.Kind == "config" && m.rootCommandActors[actor]
			if m.fibers[actor] == nil {
				return nil, causalUnsupported("unproved %s owner %q", e.Kind, actor)
			}
		case "service":
			key, keyOK := e.Data["key"].(string)
			realm, realmOK := e.Data["realm"].(string)
			if !keyOK || !realmOK {
				return nil, causalUnsupported("service event without address")
			}
			address := causalAddress{key, realm}
			actor = m.providers[address]
			if actor == "" {
				return nil, causalUnsupported("unproved service provider for %s/%s", key, realm)
			}
			// Publication is the unique notification while this ordinary
			// provider activation is ACTIVE, never inferred from its payload.
			// Retained-value invalidation/removal notifications stay strict.
			if states[actor] == "ACTIVE" && !disposed[actor] {
				activation := activations[actor]
				if activation == 0 || published[address] == activation {
					return nil, causalUnsupported("ambiguous ordinary publication window")
				}
				for consumer, pending := range triggered[actor] {
					if pending {
						return nil, fmt.Errorf("last enabling provider %q published before consumer %q LOADING", actor, consumer)
					}
				}
				if _, exists := store[address]; exists {
					return nil, fmt.Errorf("provider republished before retiring former provision")
				}
				value, exists := e.Data["value"]
				if !exists {
					return nil, fmt.Errorf("service publication lacks value")
				}
				store[address] = observedProvision{value: value, activation: activation}
				invalidated[address] = false
				published[address] = activation
				observation.publication, observation.publicationActivation, observation.serviceAddress = true, activation, address
			} else {
				prior, present := store[address]
				if !present || states[actor] != "UNLOADING" {
					return nil, fmt.Errorf("service retirement without owned unloading provision")
				}
				value, exists := e.Data["value"]
				if !exists {
					return nil, fmt.Errorf("service retirement lacks value")
				}
				undefined := causalEqual(value, map[string]any{"$undefined": true})
				if !invalidated[address] && causalEqual(value, prior.value) {
					for consumer, bindings := range bound {
						if bindings[address] == prior.activation && (states[consumer] == "ACTIVE" || states[consumer] == "LOADING") {
							return nil, fmt.Errorf("retained withdrawal precedes bound consumer %q UNLOADING", consumer)
						}
					}
					invalidated[address] = true
				} else if undefined {
					for consumer, bindings := range bound {
						if bindings[address] == prior.activation {
							return nil, fmt.Errorf("provider deletion precedes retained consumer %q stable teardown", consumer)
						}
					}
					delete(store, address)
					delete(invalidated, address)
				} else {
					return nil, fmt.Errorf("retirement notification changed retained service value")
				}
			}
		case "output":
			to, ok := e.Data["to"].(string)
			if !ok || m.outputs[to] == "" {
				return nil, causalUnsupported("unproved output owner %q", to)
			}
			actor = m.outputs[to]
			value, exists := e.Data["value"]
			if !exists {
				return nil, fmt.Errorf("output event lacks value")
			}
			outputs[to] = value
			rootLane = actor == causalRootActor
		default:
			return nil, causalUnsupported("event kind %q", e.Kind)
		}
		if actor != causalRootActor && states[actor] == "" && e.Kind != "plugin" {
			return nil, fmt.Errorf("event before fixed mount publication")
		}
		if e.Kind == "plugin" {
			isDisposed, ok := e.Data["disposed"].(bool)
			if !ok || pluginCount >= len(m.pluginCommands) {
				return nil, fmt.Errorf("invalid/extra root plugin event")
			}
			cmd := m.pluginCommands[pluginCount]
			if cmd.fiber != actor || cmd.disposed != isDisposed {
				return nil, fmt.Errorf("root plugin command order mismatch")
			}
			if cmd.previousDisposed != "" && states[cmd.previousDisposed] != "DISPOSED" {
				return nil, fmt.Errorf("root disposal overtook awaited prior disposal")
			}
			if isDisposed {
				if disposed[actor] || states[actor] == "" {
					return nil, fmt.Errorf("duplicate/unmounted disposal")
				}
				disposed[actor] = true
			} else {
				if states[actor] != "" {
					return nil, fmt.Errorf("duplicate fixed mount publication")
				}
				states[actor] = "PENDING"
				observation.pureMount = m.fibers[actor].pureMount
			}
			pluginCount++
			rootLane = true
		}
		if e.Kind == "status" {
			from, _ := e.Data["from"].(string)
			to, _ := e.Data["to"].(string)
			if states[actor] != from || !causalTransition(from, to) {
				return nil, fmt.Errorf("illegal lifecycle order for %q: %s -> %s", actor, from, to)
			}
			if to == "UNLOADING" && (from == "ACTIVE" || from == "LOADING") {
				cause := disposed[actor] || mutationAnchors[actor] || failed[actor]
				bindings := make(map[causalAddress]int)
				for address, activation := range bound[actor] {
					bindings[address] = activation
					provider := m.providers[address]
					if states[provider] == "UNLOADING" && retiring[provider] == activation {
						cause = true
					}
				}
				if !cause {
					return nil, causalUnsupported("UNLOADING without exact bound invalidator or observed own command/error")
				}
				observation.unloadingRunning, observation.teardownProviders = true, bindings
				if from == "ACTIVE" {
					observation.unloadingActive = true
					observation.unloadingActivation = activations[actor]
					retiring[actor] = activations[actor]
				}
			}
			if to == "LOADING" {
				delete(retiring, actor)
				applied[actor], failed[actor] = false, false
				bindings := readiness(actor)
				if bindings == nil {
					return nil, fmt.Errorf("consumer %q LOADING before every required provider ACTIVE", actor)
				}
				bound[actor] = bindings
				if from == "PENDING" {
					observation.startupLoading, observation.startupProviders = true, bindings
				}
				for _, pending := range triggered {
					delete(pending, actor)
				}
			}
			if to == "FAILED" && !failed[actor] {
				return nil, fmt.Errorf("FAILED state without captured error")
			}
			if from == "UNLOADING" && to != "LOADING" {
				delete(bound, actor)
			}
			states[actor] = to
			if to == "ACTIVE" {
				if readiness(actor) == nil {
					return nil, fmt.Errorf("consumer %q ACTIVE before every required provider ACTIVE", actor)
				}
				if !applied[actor] || failed[actor] {
					return nil, fmt.Errorf("ACTIVE without one successful Apply in current loading frame")
				}
				pending := make(map[string]bool)
				for _, consumer := range m.mountOrder {
					if states[consumer] == "PENDING" && !disposed[consumer] && readiness(consumer) != nil {
						for _, key := range m.fibers[consumer].needs {
							if m.providers[m.address(m.fibers[consumer].context, key)] == actor {
								pending[consumer] = true
							}
						}
					}
				}
				triggered[actor] = pending
			}
		}
		if e.Kind == "config" || e.Kind == "apply" {
			config, exists := e.Data["config"]
			if !exists {
				return nil, fmt.Errorf("%s event lacks captured config", e.Kind)
			}
			// resolveConfig emits config without publishing; only Apply/commit
			// capture the retained effective object.
			if e.Kind == "apply" {
				if states[actor] != "LOADING" || applied[actor] || failed[actor] {
					return nil, fmt.Errorf("Apply outside a fresh LOADING frame")
				}
				applied[actor] = true
				activations[actor]++
				configs[actor] = config
			}
		}
		if e.Kind == "error" {
			payload, ok := e.Data["error"].(map[string]any)
			if !ok || payload["message"] == nil || payload["name"] == nil {
				return nil, fmt.Errorf("error event lacks exact error payload")
			}
			failed[actor] = true
		}
		if e.Kind == "update" {
			config, exists := e.Data["config"]
			if !exists {
				return nil, fmt.Errorf("update event lacks captured config")
			}
			configs[actor] = config
		}
		if e.Kind == "commit" {
			if _, exists := e.Data["config"]; !exists {
				return nil, fmt.Errorf("commit event lacks captured config")
			}
			if command := mutationCommand(actor); command != nil && command.Mode != "noSave" && causalEqual(command.Config, e.Data["config"]) {
				mutationAnchors[actor] = true
			}
		}
		observation.fingerprint = causalJSON(struct {
			Kind string         `json:"kind"`
			Data map[string]any `json:"data"`
		}{e.Kind, e.Data})
		observation.actor, observation.rootLane = actor, rootLane
		observations[index] = observation
	}
	if nextCut != len(witness.Checkpoints) || pluginCount != len(m.pluginCommands) {
		return nil, fmt.Errorf("incomplete checkpoint/root command prefix")
	}
	if err := m.rootOutcomes(trace, witness); err != nil {
		return nil, err
	}
	return observations, nil
}

func causalTransition(from, to string) bool {
	switch from {
	case "PENDING":
		return to == "LOADING" || to == "UNLOADING"
	case "LOADING":
		return to == "ACTIVE" || to == "FAILED" || to == "UNLOADING"
	case "ACTIVE":
		return to == "UNLOADING"
	case "FAILED":
		return to == "LOADING" || to == "UNLOADING"
	case "UNLOADING":
		return to == "PENDING" || to == "LOADING" || to == "FAILED" || to == "DISPOSED"
	}
	return false
}

// identities anchors IDs by static ownership and per-address lifetime occurrence,
// not global provision creation order. Every opaque value has exactly one owner
// in its namespace across ALL checkpoints, including retired occurrences.
func (m *causalModel) identities(trace *Trace, witness CausalWitness) error {
	fiberDomain, fiberOwner := make(map[string]string), make(map[string]string)
	provisionDomain := make(map[string]string)
	occurrences := make(map[causalAddress]int)
	previous := make(map[causalAddress]string)
	previousTokens := make(map[causalAddress]string)
	previousCut := 0
	activations := make(map[string]int)
	for ci := range trace.Checkpoints {
		for _, event := range trace.Events[previousCut:witness.Checkpoints[ci].AfterEvent] {
			if event.Kind == "apply" {
				actor, _ := event.Data["fiber"].(string)
				activations[actor]++
			}
		}
		cp := &trace.Checkpoints[ci]
		current, currentTokens := make(map[causalAddress]string), make(map[causalAddress]string)
		byToken := make(map[string]ProvisionSnapshot)
		for pi := range cp.Provisions {
			p := &cp.Provisions[pi]
			address := causalAddress{p.Key, p.Realm}
			if m.providers[address] != p.Provider || m.fibers[p.Provider] == nil {
				return causalUnsupported("unproved provision ownership %s/%s/%s", p.Key, p.Realm, p.Provider)
			}
			if current[address] != "" {
				return fmt.Errorf("duplicate provision address")
			}
			rawID := causalJSON(p.ProvisionUID)
			token := ""
			if previous[address] == rawID {
				// If a provider started another Apply, its former opaque identity
				// cannot name the new provision even when its value stayed equal.
				for _, e := range trace.Events[previousCut:witness.Checkpoints[ci].AfterEvent] {
					if e.Kind == "apply" && e.Data["fiber"] == p.Provider {
						return fmt.Errorf("provision UID reused across provider activation")
					}
				}
				token = previousTokens[address]
			} else {
				if provisionDomain[rawID] != "" {
					return fmt.Errorf("provision UID aliased/reused across lifetime or address")
				}
				activation := activations[p.Provider]
				if activation == 0 || occurrences[address] == activation {
					return fmt.Errorf("provision identity changed without a distinct provider activation")
				}
				occurrences[address] = activation
				token = "provision:" + causalJSON([]any{p.Key, p.Realm, p.Provider, activation})
				provisionDomain[rawID] = token
			}
			current[address], currentTokens[address] = rawID, token
			p.ProvisionUID = token
			byToken[token] = *p
			for _, state := range cp.States {
				if state.Fiber == p.Provider && p.Active != (state.State == "ACTIVE") {
					return fmt.Errorf("provision active flag contradicts provider state")
				}
			}
		}
		for si := range cp.States {
			state := &cp.States[si]
			if state.FiberUID != nil {
				rawID := causalJSON(state.FiberUID)
				if prior := fiberDomain[rawID]; prior != "" && prior != state.Fiber {
					return fmt.Errorf("fiber UID alias across owners")
				}
				if prior := fiberOwner[state.Fiber]; prior != "" && prior != rawID {
					return fmt.Errorf("fiber UID changed within fixed lifetime")
				}
				fiberDomain[rawID], fiberOwner[state.Fiber] = state.Fiber, rawID
				state.FiberUID = "fiber:" + state.Fiber
			}
			if state.Epoch != nil {
				for ni := range state.Epoch.Needs {
					binding := &state.Epoch.Needs[ni]
					token := provisionDomain[causalJSON(binding.ProvisionUID)]
					if token == "" {
						return fmt.Errorf("binding references unobserved provision UID")
					}
					address := m.address(m.fibers[state.Fiber].context, binding.Key)
					p, present := byToken[token]
					if !present || p.Key != address.key || p.Realm != address.realm || p.Provider != m.providers[address] || !p.Active {
						return fmt.Errorf("binding references stale/wrong provider epoch or realm")
					}
					binding.ProvisionUID = token
				}
			}
		}
		previous, previousTokens = current, currentTokens
		previousCut = witness.Checkpoints[ci].AfterEvent
	}
	return nil
}

// rootOutcomes validates root command anchors against each observed segment,
// independently of the other trace. It never executes a plugin or reads Expect.
// Unproved target policies are excluded by compilation rather than guessed.
func (m *causalModel) rootOutcomes(trace *Trace, witness CausalWitness) error {
	revisions := make(map[string]int)
	rawConfigs := make(map[string]any)
	previous := 0
	for ci, spec := range m.cuts {
		cp := &trace.Checkpoints[ci]
		events := trace.Events[previous:witness.Checkpoints[ci].AfterEvent]
		for _, command := range spec.commands {
			target := command.Fiber
			switch command.Op {
			case "mount", "rawConfig":
				if !causalPureMountConfig(command.Config) {
					return causalUnsupported("root config expression outcome cannot be proved")
				}
				rawConfigs[target] = command.Config
			case "update", "restart":
				if ci == 0 {
					return causalUnsupported("mutation without settled prior target")
				}
				var before, after *FiberSnapshot
				for i := range trace.Checkpoints[ci-1].States {
					s := &trace.Checkpoints[ci-1].States[i]
					if s.Fiber == target {
						before = s
					}
				}
				for i := range cp.States {
					s := &cp.States[i]
					if s.Fiber == target {
						after = s
					}
				}
				if before == nil || after == nil {
					return fmt.Errorf("root mutation has no observed target")
				}
				if before.State != "ACTIVE" && before.State != "FAILED" {
					return causalUnsupported("mutation outcome for non-active/non-failed target")
				}
				loads, unloads, applies, commits := 0, 0, 0, 0
				for _, event := range events {
					if event.Data["fiber"] != target {
						continue
					}
					if event.Kind == "status" {
						if event.Data["to"] == "LOADING" {
							loads++
						}
						if event.Data["to"] == "UNLOADING" {
							unloads++
						}
					}
					if event.Kind == "apply" {
						applies++
					}
					if event.Kind == "commit" {
						commits++
						if command.Op != "update" || command.Mode == "noSave" || !causalEqual(event.Data["config"], command.Config) {
							return fmt.Errorf("root mutation commit contradicts command")
						}
					}
				}
				if command.Op == "update" {
					if !causalPureMountConfig(command.Config) {
						return causalUnsupported("dynamic update config outcome")
					}
					rawConfigs[target] = command.Config
					want := 1
					if command.Mode == "noSave" {
						want = 0
					}
					if commits != want {
						return fmt.Errorf("missing/extra update command commit")
					}
				} else if commits != 0 {
					return fmt.Errorf("restart unexpectedly committed input")
				}
				if loads != 1 || before.State == "ACTIVE" && unloads < 1 || applies > 1 {
					return fmt.Errorf("missing/duplicate root mutation lifecycle")
				}
				if after.State != "ACTIVE" && after.State != "FAILED" && after.State != "PENDING" {
					return fmt.Errorf("unsettled root mutation outcome")
				}
				revisions[target]++
			}
		}
		for _, state := range cp.States {
			if state.Revision != revisions[state.Fiber] || state.Epoch != nil && state.Epoch.Revision != state.Revision {
				return fmt.Errorf("checkpoint %q contradicts command revision", cp.Label)
			}
			if config, known := rawConfigs[state.Fiber]; known && !causalEqual(config, state.RawConfig) {
				return fmt.Errorf("checkpoint %q contradicts retained raw config", cp.Label)
			}
		}
		previous = witness.Checkpoints[ci].AfterEvent
	}
	return nil
}

func (m *causalModel) rootSteps() error {
	mounted, disposed := make(map[string]bool), make(map[string]bool)
	order := make([]string, 0)
	dirty, mutation, previousDispose := false, false, ""
	observation := false
	rootActors := make(map[string]bool)
	m.rootCommandActors = rootActors
	rootObserved := func(actor string) {
		rootActors[actor] = true
		m.link(causalRootActor, actor)
		observation = true
	}
	labels := make(map[string]bool)
	commands := make([]Action, 0)
	for _, a := range m.script.Steps {
		if a.Op != "settle" && a.Op != "snapshot" {
			commands = append(commands, a)
		}
		if err := causalSynchronous(a); err != nil {
			return err
		}
		if len(a.Program) != 0 || len(a.Cleanup) != 0 {
			return causalUnsupported("nested root operation %q", a.Op)
		}
		if mutation && a.Op != "settle" {
			return causalUnsupported("root mutation without immediate settled witness")
		}
		if previousDispose != "" && a.Op != "dispose" && a.Op != "settle" {
			return causalUnsupported("root disposal batch without settled witness")
		}
		if err := m.references(causalRootActor, a.Config); err != nil {
			return err
		}
		if err := m.references(causalRootActor, a.Value); err != nil {
			return err
		}
		context := a.Context
		if context == "" {
			context = "root"
			if a.Op != "mount" && a.Fiber != "" {
				if f := m.fibers[a.Fiber]; f != nil {
					context = f.context
				}
			}
		}
		if _, ok := m.contexts[context]; !ok {
			return causalUnsupported("dynamic root context %q", context)
		}
		target := func() error {
			if !mounted[a.Fiber] || disposed[a.Fiber] {
				return causalUnsupported("non-live root target %q", a.Fiber)
			}
			return nil
		}
		switch a.Op {
		case "mount":
			if mounted[a.Fiber] || previousDispose != "" || observation {
				return causalUnsupported("unwitnessed fixed mount command boundary")
			}
			if err := m.references(a.Fiber, a.Config); err != nil {
				return err
			}
			mounted[a.Fiber] = true
			order = append(order, a.Fiber)
			m.pluginCommands = append(m.pluginCommands, causalPluginCommand{fiber: a.Fiber})
			dirty, previousDispose = true, ""
		case "settle", "snapshot":
			if a.Op == "snapshot" && dirty {
				return causalUnsupported("snapshot before a settled root boundary")
			}
			if labels[a.Label] {
				return causalUnsupported("duplicate checkpoint label")
			}
			labels[a.Label] = true
			m.cuts = append(m.cuts, causalCheckpointSpec{label: a.Label, fibers: append([]string(nil), order...), plugins: len(m.pluginCommands), commands: commands})
			commands = make([]Action, 0)
			dirty, mutation, previousDispose = false, false, ""
			observation = false
		case "update", "restart":
			if err := target(); err != nil {
				return err
			}
			if dirty || previousDispose != "" || observation || a.Mode != "" && !(a.Op == "update" && a.Mode == "noSave") {
				return causalUnsupported("unwitnessed root mutation")
			}
			mutation, previousDispose = true, ""
			// The root reads candidate config, while lifecycle/config/update
			// events belong to the target. Shared output reads must link both.
			if err := m.references(a.Fiber, a.Config); err != nil {
				return err
			}
		case "dispose":
			if err := target(); err != nil {
				return err
			}
			if dirty {
				return causalUnsupported("disposal before a settled root boundary")
			}
			disposed[a.Fiber] = true
			m.pluginCommands = append(m.pluginCommands, causalPluginCommand{fiber: a.Fiber, disposed: true, previousDisposed: previousDispose})
			previousDispose = a.Fiber
		case "awaitFiber":
			if err := target(); err != nil {
				return err
			}
		case "rawConfig", "resolveConfig", "inspect":
			rootObserved(a.Fiber)
			if err := target(); err != nil {
				return err
			}
			if dirty {
				return causalUnsupported("config observation during unwitnessed startup")
			}
			if a.Op == "inspect" && a.Mode != "effects" && a.Mode != "configIdentity" {
				return causalUnsupported("inspect mode")
			}
			if err := m.references(a.Fiber, a.Config); err != nil {
				return err
			}
			if a.Op != "rawConfig" {
				if err := m.output(causalRootActor, a.To); err != nil {
					return err
				}
			}
		case "read":
			if dirty || a.Mode != "" && a.Mode != "get" {
				return causalUnsupported("root service read without settled ordinary realm")
			}
			m.footprints[causalRootActor].reads[m.serviceCell(context, a.Key)] = true
			if err := m.output(causalRootActor, a.To); err != nil {
				return err
			}
		case "output":
			if err := m.output(causalRootActor, a.To); err != nil {
				return err
			}
		default:
			return causalUnsupported("root operation %q", a.Op)
		}
	}
	if len(m.cuts) == 0 || len(m.script.Steps) == 0 || (m.script.Steps[len(m.script.Steps)-1].Op != "settle" && m.script.Steps[len(m.script.Steps)-1].Op != "snapshot") {
		return causalUnsupported("missing final witnessed checkpoint")
	}
	// Root observations form one sequential command lane, even when commands
	// target different fibers. This does not link ordinary sibling activations.
	for a := range rootActors {
		for b := range rootActors {
			m.link(a, b)
		}
	}
	return nil
}
