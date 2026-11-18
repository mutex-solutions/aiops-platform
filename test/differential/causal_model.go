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

// causalNever is a segment index no cut ever reaches: a gate with no release.
const causalNever = 1 << 30

type causalAddress struct{ key, realm string }
type causalFootprint struct{ reads, writes map[string]bool }
type causalFiber struct {
	plugin    PluginSpec
	context   string
	needs     []string
	pureMount bool
	parent    string // non-empty for a child mounted from a parent body (amendment 2, C2)
}

// causalBind names the exact provider activation a consumer is bound to. With
// more than one candidate provider of an address (C6) an activation number alone
// is ambiguous.
type causalBind struct {
	provider string
	act      int
}

// causalWait is a causal obligation inherited by every later root command: the
// n-th arrival at a label (waitMark) or a settled fiber (awaitFiber). C1.
type causalWait struct {
	label string
	count int
	fiber string
}

// causalLabelDef is one static place where a gate/mark/output operation can run.
type causalLabelDef struct {
	actor  string
	phase  string // apply, cleanup, effect-setup, effect-async, effect-cleanup
	effect string // owning effect id for effect programs
	isGate bool
	// step is the index of the top-level body operation that triggers this
	// event; events of one frame must come in non-decreasing step order. stepped
	// is false for teardown-driven events and asynchronous programs.
	step    int
	stepped bool
}

type causalEffect struct {
	id       string
	actor    string
	partial  bool
	async    bool
	to       string
	withdraw bool // a withdraw of this effect's disposer exists in the same body
	// ordered is true for an effect whose cleanup program is a plain sequence:
	// its events must occur in program order, once per activation (C3.2).
	ordered bool
	items   []causalItem
}

type causalRootError struct {
	target string
	waits  []causalWait
}

type causalCheckpointSpec struct {
	label    string
	fibers   []string
	plugins  int
	commands []Action
	waits    []causalWait // waitMark obligations issued in this segment
	snapshot bool
}
type causalPluginCommand struct {
	fiber            string
	disposed         bool
	previousDisposed string
	waits            []causalWait
}
type causalModel struct {
	script            *Script
	contexts          map[string]map[string]string
	ctxMeta           map[string]map[string]any
	fibers            map[string]*causalFiber
	mountOrder        []string
	providers         map[causalAddress][]string
	provides          map[string][]causalAddress
	provBefore        map[string][]causalAddress // gate label -> addresses provided earlier in its body
	avail             map[causalAddress]string
	outputs           map[string][]string
	cellDefs          map[string][]causalLabelDef
	labels            map[string][]causalLabelDef
	effects           map[string]*causalEffect
	effectCount       map[string]int
	catchers          map[string][]string
	refusable         map[string]bool
	children          map[string][]string
	footprints        map[string]*causalFootprint
	conflicts         map[string]map[string]bool
	cuts              []causalCheckpointSpec
	pluginCommands    []causalPluginCommand
	rootCommandActors map[string]bool
	release           map[string]int
	rootOutputWaits   map[string][][]causalWait
	rootErrors        []causalRootError
	notifyCuts        []map[string]int
	seq               map[string]*causalSeq
	effectPlan        map[string]causalEffectPlan
	mountEpoch        map[string]int // settle checkpoints before a root mount (children: their root ancestor)
}

// causalItem is one event a body must produce, in order (a frame's lane program).
type causalItem struct{ kind, key string }

// causalSeq is the ordered, event-producing program of one fiber's Apply body
// and plugin cleanup body. exact is false whenever an operation makes the exact
// sequence unprovable (when, partial effects, caught errors, asynchronous
// continuations); then only window, lane and content rules apply.
type causalSeq struct {
	apply, cleanup           []causalItem
	applyExact, cleanupExact bool
}

// causalFrame says where in a plugin a body runs.
type causalFrame struct {
	phase, effect string
	step          int
	top           bool
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
		script: script, contexts: map[string]map[string]string{"root": {}}, ctxMeta: map[string]map[string]any{"root": {}},
		fibers: make(map[string]*causalFiber), providers: make(map[causalAddress][]string),
		provides: make(map[string][]causalAddress), provBefore: make(map[string][]causalAddress), avail: make(map[causalAddress]string),
		outputs: make(map[string][]string), cellDefs: make(map[string][]causalLabelDef),
		labels: make(map[string][]causalLabelDef), effects: make(map[string]*causalEffect),
		effectCount: make(map[string]int), catchers: make(map[string][]string),
		refusable: make(map[string]bool), seq: make(map[string]*causalSeq), mountEpoch: make(map[string]int), effectPlan: make(map[string]causalEffectPlan), children: make(map[string][]string),
		footprints: make(map[string]*causalFootprint), conflicts: make(map[string]map[string]bool),
		release: make(map[string]int), rootOutputWaits: make(map[string][][]causalWait),
	}
	for _, ctx := range script.Contexts {
		// An intercept context layers tracked-service configuration; it never
		// changes which realm an address resolves to (C4.1).
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
		m.ctxMeta[ctx.ID] = causalMergeMeta(m.ctxMeta[ctx.Parent], ctx.Meta)
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
				if a.To != "" || a.Mode != "" && a.Mode != "service" && a.Mode != "available" {
					return causalUnsupported("non-ordinary provision syntax")
				}
			case "read", "output", "fail", "when", "gate", "mark", "effect", "withdraw", "child", "context":
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
		// NeedWith configuration that is plain data cannot reorder events; its
		// effect is visible only in output values, which are compared (C4.2).
		for _, value := range p.Needs {
			if value != nil && !causalPureMountConfig(value) {
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
	// dynamic mounts and alias contexts are not inferred. Children (C2) are
	// registered while their parent body is compiled.
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
	m.footprint(causalRootActor)
	for _, actor := range m.mountOrder {
		m.footprint(actor)
	}
	// Compile entire synchronous activations, not just the action which happened
	// to emit a particular event. An unobserved $output read still causes conflict.
	// The slice grows while child fibers are registered.
	for i := 0; i < len(m.mountOrder); i++ {
		actor := m.mountOrder[i]
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
		m.refusable[actor] = causalRefusableBody(f.plugin.Apply)
		applyExact, cleanupExact := true, true
		cs := &causalSeq{}
		cs.apply = causalItems(f.plugin.Apply, &applyExact)
		cs.cleanup = causalItems(f.plugin.Cleanup, &cleanupExact)
		cs.applyExact, cs.cleanupExact = applyExact, cleanupExact
		m.seq[actor] = cs
		m.effectPlan[actor] = causalPlanEffects(actor, f.plugin.Apply)
		scope := &causalScope{context: f.context, local: make(map[string]string)}
		if err := m.body(f.plugin.Apply, actor, causalFrame{phase: "apply", top: true}, scope, orderedNeeds, plugins); err != nil {
			return nil, err
		}
		if err := m.body(f.plugin.Cleanup, actor, causalFrame{phase: "cleanup", top: true}, scope, orderedNeeds, plugins); err != nil {
			return nil, err
		}
	}
	if err := m.rootSteps(); err != nil {
		return nil, err
	}
	if err := m.contention(plugins); err != nil {
		return nil, err
	}
	// Needs edges are validated for cycles independently of trace scheduling.
	dependencies := make(map[string][]string)
	for _, actor := range m.mountOrder {
		f := m.fibers[actor]
		for _, key := range f.needs {
			for _, p := range m.providers[m.address(f.context, key)] {
				dependencies[actor] = append(dependencies[actor], p)
				m.link(actor, p)
			}
			if cell := m.avail[m.address(f.context, key)]; cell != "" {
				m.footprints[actor].reads["output:"+cell] = true
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

func causalMergeMeta(parent, own map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range parent {
		out[k] = v
	}
	for k, v := range own {
		out[k] = v
	}
	return out
}

// causalRefusableBody reports whether an Apply body has no error source other
// than a refused provision: only provide, output and mark (C6.4).
func causalRefusableBody(actions []Action) bool {
	for _, a := range actions {
		switch a.Op {
		case "provide", "output", "mark":
		default:
			return false
		}
		if len(a.Program) != 0 || len(a.Cleanup) != 0 || a.Catch {
			return false
		}
	}
	return true
}

func (m *causalModel) footprint(actor string) {
	if m.footprints[actor] == nil {
		m.footprints[actor] = &causalFootprint{reads: make(map[string]bool), writes: make(map[string]bool)}
	}
}

func (m *causalModel) isCandidate(address causalAddress, actor string) bool {
	for _, p := range m.providers[address] {
		if p == actor {
			return true
		}
	}
	return false
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
	if publication.publication && loading.startupLoading && loading.startupProviders[publication.serviceAddress] == (causalBind{publication.actor, publication.publicationActivation}) && m.directNeedsAddress(loading.actor, publication.serviceAddress) && !m.nonServiceConflict(publication.actor, loading.actor) {
		// Both streams already proved all providers ACTIVE and the local last
		// enabling provider's status-before-notification bracket. Which branch
		// completed readiness is schedule-dependent, not event semantics.
		return false
	}
	teardownPair := func(provider, consumer causalObservation) bool {
		if !provider.unloadingActive || !consumer.unloadingRunning || m.nonServiceConflict(provider.actor, consumer.actor) {
			return false
		}
		for address, bind := range consumer.teardownProviders {
			if m.isCandidate(address, provider.actor) && bind == (causalBind{provider.actor, provider.unloadingActivation}) {
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

// output registers a writer of a named output cell. Several fibers may write one
// cell (C5); every writer write-conflicts. The root command lane never shares a
// cell with a fiber, because the writer of an event would then be unprovable.
func (m *causalModel) output(actor, name string, frame causalFrame) error {
	if name == "" {
		return nil
	}
	if strings.Contains(name, ".") {
		return causalUnsupported("ambiguous dotted output cell %q", name)
	}
	for _, owner := range m.outputs[name] {
		if (owner == causalRootActor) != (actor == causalRootActor) {
			return causalUnsupported("output %q has ambiguous owner", name)
		}
	}
	known := false
	for _, owner := range m.outputs[name] {
		known = known || owner == actor
	}
	if !known {
		m.outputs[name] = append(m.outputs[name], actor)
	}
	m.footprints[actor].writes["output:"+name] = true
	if actor != causalRootActor {
		m.cellDefs[name] = append(m.cellDefs[name], causalLabelDef{actor: actor, phase: frame.phase, effect: frame.effect, step: frame.step, stepped: causalStepped(frame.phase)})
	}
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

// causalSynchronous rejects control metadata the comparator cannot prove. The
// metadata accepted by amendment 2 is accepted only on the operations whose
// semantics it has a rule for: await:false on effect and root dispose, task on
// dispose and withdraw, count on waitMark, catch on effect, read and awaitFiber.
func causalSynchronous(a Action) error {
	bad := a.Global || a.Symbol || a.Prepend
	bad = bad || a.Task != "" && a.Op != "dispose" && a.Op != "withdraw"
	bad = bad || a.Await != nil && !*a.Await && a.Op != "effect" && a.Op != "dispose"
	bad = bad || a.Count != 0 && a.Op != "waitMark"
	bad = bad || a.Catch && a.Op != "effect" && a.Op != "read" && a.Op != "awaitFiber"
	if bad {
		return causalUnsupported("async/control/catch metadata on %q", a.Op)
	}
	return nil
}

// causalScope is the static context environment of one fiber body. Contexts a
// body creates with a fixed isolation label are static realm assignments.
type causalScope struct {
	context string
	local   map[string]string // body context name -> registered context id
}

func (m *causalModel) body(actions []Action, actor string, frame causalFrame, scope *causalScope, orderedNeeds map[string][]string, plugins map[string]PluginSpec) error {
	f := m.fibers[actor]
	cleanup := frame.phase == "cleanup" || frame.phase == "effect-cleanup"
	for opIndex, a := range actions {
		fr := frame
		if frame.top {
			fr.step, fr.top = opIndex, false
		}
		if err := causalSynchronous(a); err != nil {
			return err
		}
		if err := causalFieldsOK(a); err != nil {
			return err
		}
		if a.Op != "child" && a.Op != "context" && (a.Context != "" || a.Fiber != "") {
			return causalUnsupported("body context override or cleanup callback on %q", a.Op)
		}
		if a.Op != "effect" && len(a.Cleanup) != 0 {
			return causalUnsupported("body context override or cleanup callback on %q", a.Op)
		}
		if a.Op != "when" && a.Op != "effect" && len(a.Program) != 0 {
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
			if fr.phase != "apply" || a.To != "" {
				return causalUnsupported("non-ordinary/dynamic provision")
			}
			address := m.address(scope.context, a.Key)
			for _, p := range m.providers[address] {
				if p == actor {
					return causalUnsupported("non-unique provider for %s/%s", address.key, address.realm)
				}
			}
			m.providers[address] = append(m.providers[address], actor)
			m.provides[actor] = append(m.provides[actor], address)
			m.footprints[actor].writes[m.serviceCell(scope.context, a.Key)] = true
			if a.Mode == "available" {
				value, _ := a.Value.(map[string]any)
				cell, _ := value["ready"].(string)
				if cell == "" || strings.Contains(cell, ".") || m.avail[address] != "" && m.avail[address] != cell {
					return causalUnsupported("availability predicate cell")
				}
				m.avail[address] = cell
				m.footprints[actor].reads["output:"+cell] = true
			} else if m.avail[address] != "" {
				return causalUnsupported("mixed availability modes for %s/%s", address.key, address.realm)
			}
		case "read":
			if a.Mode != "" && a.Mode != "get" && a.Mode != "config" {
				return causalUnsupported("tracked/dynamic service read")
			}
			m.footprints[actor].reads[m.serviceCell(scope.context, a.Key)] = true
			if a.Catch {
				m.catchers["read"] = append(m.catchers["read"], actor)
			}
			if err := m.output(actor, a.To, fr); err != nil {
				return err
			}
		case "output":
			if err := m.output(actor, a.To, fr); err != nil {
				return err
			}
		case "fail":
		case "gate", "mark":
			if a.Label == "" {
				return causalUnsupported("unlabelled %s", a.Op)
			}
			phase := fr.phase
			m.labels[a.Label] = append(m.labels[a.Label], causalLabelDef{actor: actor, phase: phase, effect: fr.effect, isGate: a.Op == "gate", step: fr.step, stepped: causalStepped(phase)})
			m.footprints[actor].writes["label:"+a.Label] = true
			if a.Op == "gate" && phase == "apply" {
				m.provBefore[a.Label] = append([]causalAddress(nil), m.provides[actor]...)
			}
		case "effect":
			if fr.phase != "apply" {
				return causalUnsupported("effect outside an Apply body")
			}
			id := fmt.Sprintf("%s#%d", actor, m.effectCount[actor])
			m.effectCount[actor]++
			async := a.Await != nil && !*a.Await
			eff := &causalEffect{id: id, actor: actor, partial: a.Mode == "partial", async: async}
			if a.Mode != "" && a.Mode != "partial" {
				return causalUnsupported("effect mode %q", a.Mode)
			}
			if a.Catch {
				m.catchers["effect"] = append(m.catchers["effect"], actor)
				if err := m.output(actor, a.To, fr); err != nil {
					return err
				}
			} else {
				eff.to = a.To
			}
			eff.ordered = !eff.partial && !a.Catch
			if eff.ordered {
				exact := true
				eff.items = causalItems(a.Cleanup, &exact)
				eff.ordered = exact && len(eff.items) == len(a.Cleanup)
			}
			m.effects[id] = eff
			setup := causalFrame{phase: "effect-setup", effect: id}
			if async {
				setup.phase = "effect-async"
			}
			if err := m.body(a.Program, actor, setup, scope, orderedNeeds, plugins); err != nil {
				return err
			}
			if err := m.body(a.Cleanup, actor, causalFrame{phase: "effect-cleanup", effect: id}, scope, orderedNeeds, plugins); err != nil {
				return err
			}
			if eff.partial {
				m.stepEffectCleanup(id, fr.step)
			}
		case "withdraw":
			if fr.phase != "apply" || a.Task == "" {
				return causalUnsupported("withdraw outside an Apply body or without a named disposer")
			}
			found := false
			for _, id := range m.effectIDs(actor) {
				if m.effects[id].to == a.Task {
					m.effects[id].withdraw, found = true, true
					m.stepEffectCleanup(id, fr.step)
				}
			}
			if !found {
				return causalUnsupported("withdraw of unknown disposer %q", a.Task)
			}
		case "child":
			if fr.phase != "apply" || cleanup {
				return causalUnsupported("child outside an Apply body")
			}
			p, ok := plugins[a.Plugin]
			if !ok || m.fibers[a.Fiber] != nil || a.Fiber == "" || a.Fiber == "root" || a.Fiber == causalRootActor {
				return causalUnsupported("non-unique/unknown child %q", a.Fiber)
			}
			context := scope.context
			if a.Context != "" {
				id, local := scope.local[a.Context]
				if local {
					context = id
				} else {
					context = a.Context
				}
			}
			if _, ok := m.contexts[context]; !ok {
				return causalUnsupported("non-static child context %q", context)
			}
			m.fibers[a.Fiber] = &causalFiber{plugin: p, context: context, needs: orderedNeeds[p.ID], parent: actor}
			m.mountOrder = append(m.mountOrder, a.Fiber)
			m.children[actor] = append(m.children[actor], a.Fiber)
			m.footprint(a.Fiber)
			if err := m.references(actor, a.Config); err != nil {
				return err
			}
		case "context":
			if fr.phase != "apply" || a.To == "" || a.Mode != "isolate" && a.Mode != "intercept" && a.Mode != "" {
				return causalUnsupported("dynamic context form")
			}
			parent := scope.context
			if a.Context != "" {
				if id, local := scope.local[a.Context]; local {
					parent = id
				} else {
					parent = a.Context
				}
			}
			if _, ok := m.contexts[parent]; !ok {
				return causalUnsupported("non-static context parent %q", parent)
			}
			// Each fiber of one plugin executes this body; without a fixed label an
			// isolated realm would be fresh per execution and unprovable.
			realms := make(map[string]string)
			for k, v := range m.contexts[parent] {
				realms[k] = v
			}
			if a.Mode == "isolate" {
				if a.Label == "" || a.Key == "" {
					return causalUnsupported("unlabelled dynamic isolate")
				}
				realms[causalBuiltin(a.Key)] = a.Key + ":" + a.Label
			}
			id := a.To
			if prior, exists := m.contexts[id]; exists && !causalEqual(prior, realms) {
				return causalUnsupported("conflicting dynamic context %q", id)
			}
			if _, exists := m.contexts[id]; !exists {
				m.contexts[id] = realms
				m.ctxMeta[id] = causalMergeMeta(m.ctxMeta[parent], a.Meta)
			}
			scope.local[a.To] = id
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
			if err := m.body(a.Program, actor, fr, scope, orderedNeeds, plugins); err != nil {
				return err
			}
		default:
			return causalUnsupported("body operation %q", a.Op)
		}
	}
	_ = f
	return nil
}

func (m *causalModel) effectIDs(actor string) []string {
	ids := make([]string, 0, m.effectCount[actor])
	for i := 0; i < m.effectCount[actor]; i++ {
		ids = append(ids, fmt.Sprintf("%s#%d", actor, i))
	}
	return ids
}

// descendants lists every transitive child of a fiber.
func (m *causalModel) descendants(actor string) []string {
	var out []string
	for _, c := range m.children[actor] {
		out = append(out, c)
		out = append(out, m.descendants(c)...)
	}
	return out
}

// waited reports whether a waitMark for the label was issued in a segment up to
// and including cut ci: the recorder's evidence that a script awaited it.
func (m *causalModel) waited(label string, ci int) bool {
	for i := 0; i <= ci && i < len(m.cuts); i++ {
		for _, w := range m.cuts[i].waits {
			if w.label == label {
				return true
			}
		}
	}
	return false
}

// causalItems flattens a body into the events it must emit, in order. The
// walker never guesses: an operation whose events cannot be placed exactly
// clears *exact, which disables only the order/completeness check of this body.
func causalItems(actions []Action, exact *bool) []causalItem {
	var items []causalItem
	for _, a := range actions {
		switch a.Op {
		case "gate", "mark":
			items = append(items, causalItem{a.Op, a.Label})
		case "output":
			items = append(items, causalItem{"output", a.To})
		case "read":
			if a.Catch {
				*exact = false
			}
			if a.To != "" {
				items = append(items, causalItem{"output", a.To})
			}
		case "fail":
			items = append(items, causalItem{"fail", ""})
			return items
		case "child":
			items = append(items, causalItem{"child", a.Fiber})
		case "effect":
			// Only a synchronous effect with no cleanup, catch or disposer name
			// places its program events exactly in the Apply sequence.
			if a.Mode == "partial" || a.Catch || len(a.Cleanup) != 0 || a.To != "" || a.Await != nil && !*a.Await {
				*exact = false
				continue
			}
			items = append(items, causalItems(a.Program, exact)...)
		case "withdraw", "when":
			*exact = false
		}
	}
	return items
}

// causalFields lists the JSON fields an operation may carry. An action with any
// other field is malformed for its operation and is not interpreted.
var causalFields = map[string]string{
	"child": "op fiber plugin context config", "context": "op to mode key label context meta",
	"gate": "op label", "mark": "op label value", "withdraw": "op task",
	"effect":   "op label program cleanup to mode error catch await",
	"waitMark": "op label count", "release": "op label", "notify": "op keys",
	"awaitFiber": "op fiber catch", "dispose": "op fiber await task",
}

func causalFieldsOK(a Action) error {
	allowed, ok := causalFields[a.Op]
	if !ok {
		return nil
	}
	raw, _ := json.Marshal(a) // an Action always marshals
	var used map[string]json.RawMessage
	_ = json.Unmarshal(raw, &used)
	permitted := make(map[string]bool)
	for _, f := range strings.Fields(allowed) {
		permitted[f] = true
	}
	for f := range used {
		if !permitted[f] {
			return causalUnsupported("field %q is not defined for operation %q", f, a.Op)
		}
	}
	return nil
}

// inert reports a fiber that can never leave PENDING: one of its Needs addresses
// has no candidate provider anywhere in the script, so it emits no body event.
func (m *causalModel) inert(actor string) bool {
	f := m.fibers[actor]
	for _, key := range f.needs {
		if len(m.providers[m.address(f.context, key)]) == 0 {
			return true
		}
	}
	return false
}

// contention enforces the sequencing that gives a second provider of one
// address (C6), a second writer of one output cell (C5) and a mounted plugin with
// controls a defined meaning. Concurrent contention has no schedule-independent
// winner, so it is not proved.
func (m *causalModel) contention(plugins map[string]PluginSpec) error {
	mountedPlugin := make(map[string]bool)
	for _, a := range m.mountOrder {
		mountedPlugin[m.fibers[a].plugin.ID] = true
	}
	for id, p := range plugins {
		if !mountedPlugin[id] && causalUsesControls(p.Apply, p.Cleanup) {
			return causalUnsupported("plugin %q uses controls but no fiber mounts it", id)
		}
	}
	for label, defs := range m.labels {
		for _, d := range defs {
			if d.isGate {
				if _, released := m.release[label]; !released {
					return causalUnsupported("gate %q is never released", label)
				}
			}
		}
	}
	epoch := func(actor string) int {
		for m.fibers[actor].parent != "" {
			actor = m.fibers[actor].parent
		}
		return m.mountEpoch[actor]
	}
	distinct := func(actors []string) bool {
		seen := make(map[int]bool)
		for _, a := range actors {
			if seen[epoch(a)] {
				return false
			}
			seen[epoch(a)] = true
		}
		return true
	}
	for address, candidates := range m.providers {
		if len(candidates) > 1 && !distinct(candidates) {
			return causalUnsupported("concurrent providers of %s/%s", address.key, address.realm)
		}
	}
	for cell, writers := range m.outputs {
		var live []string
		for _, w := range writers {
			if w != causalRootActor && !m.inert(w) {
				live = append(live, w)
			}
		}
		if len(live) > 1 && !distinct(live) {
			return causalUnsupported("concurrent writers of output %q", cell)
		}
	}
	return nil
}

func causalUsesControls(bodies ...[]Action) bool {
	for _, body := range bodies {
		for _, a := range body {
			switch a.Op {
			case "gate", "mark", "effect", "withdraw", "child", "context":
				return true
			}
			if causalUsesControls(a.Program, a.Cleanup) {
				return true
			}
		}
	}
	return false
}

// causalEffectPlan says which effects an Apply body registers when its shape is
// simple enough to know without guessing: only synchronous effects without catch,
// partial mode or disposer name, whose cleanup holds only mark/gate operations, in
// a body of effect, output, mark, provide and a final fail. For such a fiber
// every registered effect must run its cleanup program completely at teardown.
type causalEffectPlan struct {
	complete bool
	effects  []string
	counts   []int
}

func causalPlanEffects(actor string, body []Action) causalEffectPlan {
	plan := causalEffectPlan{complete: true}
	n := 0
	for _, a := range body {
		switch a.Op {
		case "effect":
			if a.Mode != "" || a.Catch || a.To != "" || a.Await != nil && !*a.Await {
				return causalEffectPlan{}
			}
			count := 0
			for _, c := range a.Cleanup {
				if c.Op != "mark" && c.Op != "gate" {
					return causalEffectPlan{}
				}
				count++
			}
			plan.effects = append(plan.effects, fmt.Sprintf("%s#%d", actor, n))
			plan.counts = append(plan.counts, count)
			n++
		case "fail":
			return plan
		case "output", "mark", "provide":
		default:
			return causalEffectPlan{}
		}
	}
	return plan
}

// causalStepped says whether events of a frame are triggered by the top-level
// operation that is executing (so their order is program order). Teardown-driven
// effect cleanup and asynchronous setup are not.
func causalStepped(phase string) bool {
	return phase == "apply" || phase == "cleanup" || phase == "effect-setup"
}

// stepEffectCleanup records that the cleanup program of an effect runs at the given
// top-level step (its own op for a partial unwind, the withdraw op otherwise).
func (m *causalModel) stepEffectCleanup(id string, step int) {
	for label, defs := range m.labels {
		for i := range defs {
			if defs[i].effect == id && defs[i].phase == "effect-cleanup" {
				m.labels[label][i].step, m.labels[label][i].stepped = step, true
			}
		}
	}
	for cell, defs := range m.cellDefs {
		for i := range defs {
			if defs[i].effect == id && defs[i].phase == "effect-cleanup" {
				m.cellDefs[cell][i].step, m.cellDefs[cell][i].stepped = step, true
			}
		}
	}
}
