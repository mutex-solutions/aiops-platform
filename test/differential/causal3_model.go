// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"strings"
)

// Compile-time facts of amendment 3 (docs/design/s0-causal-comparison-amendment-3.md).

type causalExpect struct{ value any }

// causalFiberCommand is one root command whose bundle of command-driven events
// (config, commit, update) belongs to a new-shape fiber (K5, K6).
type causalFiberCommand struct {
	op      string // update or resolveConfig
	config  any
	noSave  bool
	policy  string
	restart bool // default continuation: the command is a restart request
	waits   []causalWait
	segment int
	chain   string // chain lane (K7), empty when the plugin has no update listener
}

type causalUpdateListener struct {
	label   string
	program []Action
}

type causalLateEffect struct{ actor, effect, target string }

type causalRootProvide struct {
	value            any
	waits            []causalWait
	previousDisposed string
}

type causalLaneNeed struct {
	lane string
	pos  int
}

// causalLaneReq says that consuming item `at` of a lane needs `other` to have
// consumed n items.
type causalLaneReq struct {
	at    int
	other string
	n     int
}

// causalLaneSpec is the planned, exact event program of one hook or chain lane.
type causalLaneSpec struct {
	actor      string
	items      []causalItem
	prefix     int
	reqs       []causalLaneReq
	waits      []causalWait
	chainFiber string
	chainCmd   int
}

type causalTask struct {
	needs    []causalLaneNeed
	rejected bool
	restart  bool
}

type causalA3 struct {
	newShape      map[string]bool
	fiberCmds     map[string][]*causalFiberCommand
	mountConfig   map[string]any
	updListener   map[string]*causalUpdateListener
	listenLabels  map[string]bool
	selfDispose   map[string]bool
	lateEffects   []causalLateEffect
	rootProvides  map[causalAddress]*causalRootProvide
	lanes         []*causalLaneSpec
	laneBy        map[string]*causalLaneSpec
	laneItems     map[string][]*causalLaneSpec
	tasks         map[string]*causalTask
	listens       []Action
	dispatched    map[string]bool
	rootOutExpect map[string][]*causalExpect
	taskNeeds     []causalLaneNeed
	listenItems   map[string]bool
}

func (m *causalModel) initA3() {
	m.a3 = causalA3{
		newShape: make(map[string]bool), fiberCmds: make(map[string][]*causalFiberCommand),
		mountConfig: make(map[string]any), updListener: make(map[string]*causalUpdateListener),
		listenLabels: make(map[string]bool), selfDispose: make(map[string]bool),
		rootProvides: make(map[causalAddress]*causalRootProvide), laneBy: make(map[string]*causalLaneSpec),
		laneItems: make(map[string][]*causalLaneSpec), tasks: make(map[string]*causalTask),
		dispatched: make(map[string]bool), rootOutExpect: make(map[string][]*causalExpect), listenItems: make(map[string]bool),
	}
}

func causalUndefined() any { return map[string]any{"$undefined": true} }

// metaStatic resolves a $meta path inside the statically merged metadata of a
// context (K1.1 and K1.2). ok is false when the path leaves static data.
func (m *causalModel) metaStatic(ctx, path string) (any, bool) {
	meta, known := m.ctxMeta[ctx]
	if !known || path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	value, present := meta[parts[0]]
	if !present {
		return nil, false
	}
	for _, key := range parts[1:] {
		if _, isArray := value.([]any); isArray {
			return nil, false
		}
		object, isObject := value.(map[string]any)
		if !isObject {
			return causalUndefined(), true
		}
		next, present := object[key]
		if !present {
			return causalUndefined(), true
		}
		value = next
	}
	return value, true
}

// metaExpectation returns the static value of a value that is exactly one $meta
// expression, evaluated in ctx.
func (m *causalModel) metaExpectation(ctx string, value any) *causalExpect {
	object, ok := value.(map[string]any)
	if !ok || len(object) != 1 {
		return nil
	}
	path, ok := object["$meta"].(string)
	if !ok {
		return nil
	}
	resolved, ok := m.metaStatic(ctx, path)
	if !ok {
		return nil
	}
	return &causalExpect{resolved}
}

// metaReferencesOK reports whether every $meta expression nested in value
// resolves statically in ctx.
func (m *causalModel) metaReferencesOK(ctx string, value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if path, has := v["$meta"]; has {
			text, ok := path.(string)
			if !ok || ctx == "" {
				return false
			}
			_, static := m.metaStatic(ctx, text)
			return static
		}
		for _, nested := range v {
			if !m.metaReferencesOK(ctx, nested) {
				return false
			}
		}
	case []any:
		for _, nested := range v {
			if !m.metaReferencesOK(ctx, nested) {
				return false
			}
		}
	}
	return true
}

func (m *causalModel) registerLane(l *causalLaneSpec) {
	m.a3.lanes = append(m.a3.lanes, l)
	m.a3.laneBy[l.actor] = l
	seen := make(map[string]bool)
	for _, item := range l.items {
		key := item.kind + "\x00" + item.key
		if !seen[key] {
			seen[key] = true
			m.a3.laneItems[key] = append(m.a3.laneItems[key], l)
		}
	}
}

// ---- listener programs ----------------------------------------------------

var causalHookFields = map[string]string{
	"mark": "op label value", "gate": "op label", "output": "op to value", "fail": "op error",
	"return": "op value", "next": "op to", "when": "op base value program",
}

// hookValue evaluates a literal; ok is false for any expression other than the
// undefined marker (and $config of the candidate when allowConfig).
func hookValue(v any, cand any, allowConfig bool) (any, bool) {
	object, isObject := v.(map[string]any)
	if isObject && len(object) == 1 {
		if _, ok := object["$undefined"]; ok {
			return causalUndefined(), true
		}
		if path, ok := object["$config"].(string); ok && allowConfig {
			cur := cand
			if path == "" {
				return cur, true
			}
			for _, k := range strings.Split(path, ".") {
				mm, isMap := cur.(map[string]any)
				if !isMap {
					return causalUndefined(), true
				}
				next, present := mm[k]
				if !present {
					return causalUndefined(), true
				}
				cur = next
			}
			return cur, true
		}
	}
	if v != nil && !causalPureMountConfig(v) {
		return nil, false
	}
	return v, true
}

// hookBailed is the pinned isBailed: neither null, false nor undefined.
func hookBailed(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok && !b {
		return false
	}
	return !causalEqual(v, causalUndefined())
}

func hookFieldsOK(a Action) error {
	allowed := " " + causalHookFields[a.Op] + " "
	for _, f := range causalActionFields(a) {
		if !strings.Contains(allowed, " "+f+" ") {
			return causalUnsupported("field %q is not defined for listener operation %q", f, a.Op)
		}
	}
	if a.Catch || a.Context != "" || a.Fiber != "" || len(a.Cleanup) != 0 {
		return causalUnsupported("catch, retargeting or cleanup inside a listener program")
	}
	if a.Value != nil && a.Op != "when" && !causalPureMountConfig(a.Value) {
		return causalUnsupported("listener value is not a pure literal")
	}
	return nil
}

// hookRun is the abstract execution of one listener body.
type hookRun struct {
	items  []causalItem
	prefix int
	threw  bool
	ret    any
}

func hookExec(label string, program []Action, allowed string) (hookRun, error) {
	run := hookRun{items: []causalItem{{"listener", label}}, ret: causalUndefined()}
	for _, a := range program {
		if !strings.Contains(" "+allowed+" ", " "+a.Op+" ") {
			return run, causalUnsupported("listener operation %q is not covered in this dispatch mode", a.Op)
		}
		if err := hookFieldsOK(a); err != nil {
			return run, err
		}
		switch a.Op {
		case "mark":
			run.items = append(run.items, causalItem{"mark", a.Label})
			run.ret = causalUndefined()
		case "gate":
			run.items = append(run.items, causalItem{"gate", a.Label})
			if run.prefix == 0 {
				run.prefix = len(run.items)
			}
			run.ret = causalUndefined()
		case "output":
			if a.To != "" {
				run.items = append(run.items, causalItem{"output", a.To})
			}
			run.ret = causalUndefined()
		case "fail":
			run.threw = true
			if run.prefix == 0 {
				run.prefix = len(run.items)
			}
			return run, nil
		case "return":
			v, _ := hookValue(a.Value, nil, false)
			run.ret = v
			if run.prefix == 0 {
				run.prefix = len(run.items)
			}
			return run, nil
		}
	}
	if run.prefix == 0 {
		run.prefix = len(run.items)
	}
	return run, nil
}

// hookWaterfall builds the nested call chain of a waterfall dispatch (K8).
func hookWaterfall(key string, listeners []Action, i int, out *[]causalItem) (threw bool, err error) {
	if i == len(listeners) {
		*out = append(*out, causalItem{"inner", key})
		return false, nil
	}
	l := listeners[i]
	*out = append(*out, causalItem{"listener", l.Label})
	called := false
	for _, a := range l.Program {
		if !strings.Contains(" mark output fail return next ", " "+a.Op+" ") {
			return false, causalUnsupported("listener operation %q is not covered in a waterfall", a.Op)
		}
		if err := hookFieldsOK(a); err != nil {
			return false, err
		}
		switch a.Op {
		case "mark":
			*out = append(*out, causalItem{"mark", a.Label})
		case "output":
			if a.To != "" {
				*out = append(*out, causalItem{"output", a.To})
			}
		case "fail":
			return true, nil
		case "return":
			return false, nil
		case "next":
			if called {
				return true, nil // next() called multiple times
			}
			called = true
			threw, err := hookWaterfall(key, listeners, i+1, out)
			if err != nil || threw {
				return threw, err
			}
			if a.To != "" {
				*out = append(*out, causalItem{"output", a.To})
			}
		}
	}
	return false, nil
}

func hookIdentity(a Action) string {
	if a.Symbol {
		return a.Key + "\x00symbol"
	}
	return a.Key + "\x00string"
}

func hookLaneName(prefix string, a Action) string {
	name := prefix + ":" + a.Key
	if a.Symbol {
		name += ":symbol"
	}
	return name
}

// hookListen registers a root listener (K8).
func (m *causalModel) hookListen(a Action) error {
	if a.Key == "" || a.Label == "" || m.a3.listenLabels[a.Label] || a.Fiber != "" || a.Context != "" && a.Context != "root" {
		return causalUnsupported("listener without a unique label, or retargeted")
	}
	if a.Key == "internal/update" && !a.Symbol {
		return causalUnsupported("internal/update listener outside a plugin Apply")
	}
	for _, step := range a.Program {
		if step.Op == "when" {
			return causalUnsupported("when inside a host listener")
		}
		if err := hookFieldsOK(step); err != nil {
			return err
		}
	}
	m.a3.listenLabels[a.Label] = true
	m.a3.listens = append(m.a3.listens, a)
	return nil
}

func (m *causalModel) laneOutputs(l *causalLaneSpec) error {
	for _, item := range l.items {
		if item.kind != "output" {
			continue
		}
		known := false
		for _, owner := range m.outputs[item.key] {
			if owner != l.actor {
				return causalUnsupported("output %q written by a hook lane and another actor", item.key)
			}
			known = true
		}
		if !known {
			m.outputs[item.key] = append(m.outputs[item.key], l.actor)
		}
	}
	return nil
}

func hookAllowed(mode string) string {
	switch mode {
	case "emit":
		return "mark gate output return"
	case "parallel", "serial":
		return "mark gate output fail return"
	case "bail":
		return "mark output fail return"
	}
	return ""
}

// hookDispatch plans a root dispatch (K8). It returns the lanes later commands
// must wait for (nil when not awaited), the task it defines, and compile errors.
func (m *causalModel) hookDispatch(a Action, waits []causalWait) (awaited []causalLaneNeed, task *causalTask, err error) {
	switch a.Mode {
	case "emit", "parallel", "serial", "bail", "waterfall":
	default:
		return nil, nil, causalUnsupported("dispatch mode %q", a.Mode)
	}
	if a.Key == "" || a.Fiber != "" || a.Context != "" && a.Context != "root" || a.Value != nil && !causalPureMountConfig(a.Value) {
		return nil, nil, causalUnsupported("retargeted or non-literal dispatch")
	}
	async := a.Await != nil && !*a.Await
	if async && (a.Mode != "serial" && a.Mode != "parallel" || a.Catch) || a.Task != "" && !async {
		return nil, nil, causalUnsupported("dispatch await/task/catch form")
	}
	if a.Task != "" && m.a3.tasks[a.Task] != nil {
		return nil, nil, causalUnsupported("duplicate task %q", a.Task)
	}
	identity := hookIdentity(a)
	if m.a3.dispatched[identity] {
		return nil, nil, causalUnsupported("event key %q dispatched twice", a.Key)
	}
	m.a3.dispatched[identity] = true
	var listeners []Action
	for _, l := range m.a3.listens {
		if hookIdentity(l) == identity {
			listeners = append(listeners, l)
		}
	}
	dispatch := &causalLaneSpec{actor: hookLaneName("dispatch", a), chainCmd: -1, waits: waits}
	var lanes []*causalLaneSpec
	rejected := false
	switch a.Mode {
	case "emit", "parallel":
		for i, l := range listeners {
			run, err := hookExec(l.Label, l.Program, hookAllowed(a.Mode))
			if err != nil {
				return nil, nil, err
			}
			lane := &causalLaneSpec{actor: "hook:" + l.Label, items: run.items, prefix: run.prefix, waits: waits, chainCmd: -1}
			if i > 0 {
				lane.reqs = append(lane.reqs, causalLaneReq{0, lanes[i-1].actor, lanes[i-1].prefix})
			}
			rejected = rejected || run.threw
			lanes = append(lanes, lane)
			need := run.prefix
			if a.Mode == "parallel" {
				need = len(run.items)
			}
			dispatch.reqs = append(dispatch.reqs, causalLaneReq{0, lane.actor, need})
		}
	case "serial", "bail":
		stopped := false
		for _, l := range listeners {
			run, err := hookExec(l.Label, l.Program, hookAllowed(a.Mode))
			if err != nil {
				return nil, nil, err
			}
			if stopped {
				continue // a listener that never runs must still be well formed
			}
			dispatch.items = append(dispatch.items, run.items...)
			if run.threw {
				rejected, stopped = true, true
			} else if hookBailed(run.ret) {
				stopped = true
			}
		}
	case "waterfall":
		for _, l := range listeners {
			for _, step := range l.Program {
				if !strings.Contains(" mark output fail return next ", " "+step.Op+" ") {
					return nil, nil, causalUnsupported("listener operation %q is not covered in a waterfall", step.Op)
				}
			}
		}
		threw, err := hookWaterfall(a.Key, listeners, 0, &dispatch.items)
		if err != nil {
			return nil, nil, err
		}
		rejected = threw
		planned := false
		for _, item := range dispatch.items {
			planned = planned || item.kind == "inner"
		}
		if !planned {
			// A veto, a bail value or a failure stops the chain before the
			// built-in step: no inner event may occur.
			m.a3.listenItems["inner\x00"+a.Key] = true
		}
	}
	if rejected && !async && !a.Catch {
		return nil, nil, causalUnsupported("uncaught dispatch rejection")
	}
	// A resolved dispatch with a cell writes it, awaited or not (the unawaited
	// task chain ends in the same put). A rejected awaited dispatch with catch
	// records the error and the caught value; a rejected unawaited one only
	// rejects its task.
	switch {
	case !rejected && a.To != "":
		dispatch.items = append(dispatch.items, causalItem{"output", a.To})
	case rejected && !async:
		dispatch.items = append(dispatch.items, causalItem{"error", "dispatch"})
		if a.To != "" {
			dispatch.items = append(dispatch.items, causalItem{"output", a.To})
		}
	}
	dispatch.prefix = len(dispatch.items)
	for _, l := range append([]*causalLaneSpec{dispatch}, lanes...) {
		if err := m.laneOutputs(l); err != nil {
			return nil, nil, err
		}
		for _, item := range l.items {
			if item.kind == "mark" || item.kind == "gate" {
				if len(m.labels[item.key]) != 0 || len(m.a3.laneItems["mark\x00"+item.key])+len(m.a3.laneItems["gate\x00"+item.key]) != 0 {
					return nil, nil, causalUnsupported("label %q is used by a hook lane and elsewhere", item.key)
				}
			}
		}
		m.registerLane(l)
	}
	m.link(causalRootActor, dispatch.actor)
	if async {
		// The task is complete when the whole dispatch is finished, listener
		// continuations included.
		full := []causalLaneNeed{{dispatch.actor, len(dispatch.items)}}
		for _, l := range lanes {
			full = append(full, causalLaneNeed{l.actor, len(l.items)})
		}
		return nil, &causalTask{needs: full, rejected: rejected}, nil
	}
	needs := []causalLaneNeed{{dispatch.actor, len(dispatch.items)}}
	for _, l := range lanes {
		if a.Mode == "emit" {
			needs = append(needs, causalLaneNeed{l.actor, l.prefix})
		} else {
			needs = append(needs, causalLaneNeed{l.actor, len(l.items)})
		}
	}
	return needs, nil, nil
}

// ---- K7: update listeners installed by Apply ------------------------------

// causalUpdateProgram validates and statically evaluates an internal/update
// listener for a candidate configuration (K7).
func causalUpdateProgram(label string, program []Action, cand any) ([]causalItem, bool, error) {
	items := []causalItem{{"listener", label}}
	rejected := false
	var exec func([]Action) (bool, error)
	exec = func(actions []Action) (bool, error) {
		for _, a := range actions {
			if err := hookFieldsOK(a); err != nil {
				return false, err
			}
			switch a.Op {
			case "when":
				base, ok1 := hookValue(a.Base, cand, true)
				value, ok2 := hookValue(a.Value, cand, true)
				if !ok1 || !ok2 {
					return false, causalUnsupported("update listener condition is not statically evaluable")
				}
				if causalEqual(base, value) {
					stop, err := exec(a.Program)
					if err != nil || stop {
						return stop, err
					}
				}
			case "gate":
				items = append(items, causalItem{"gate", a.Label})
			case "mark":
				items = append(items, causalItem{"mark", a.Label})
			case "output":
				if a.To != "" {
					items = append(items, causalItem{"output", a.To})
				}
			case "fail":
				rejected = true
				return true, nil
			case "return":
				return true, nil
			default:
				return false, causalUnsupported("update listener operation %q", a.Op)
			}
		}
		return false, nil
	}
	if _, err := exec(program); err != nil {
		return nil, false, err
	}
	return items, rejected, nil
}

func causalActionFields(a Action) []string {
	var out []string
	add := func(ok bool, name string) {
		if ok {
			out = append(out, name)
		}
	}
	add(a.Context != "", "context")
	add(a.Fiber != "", "fiber")
	add(a.Plugin != "", "plugin")
	add(a.Key != "", "key")
	add(a.Label != "", "label")
	add(a.To != "", "to")
	add(a.Mode != "", "mode")
	add(a.Task != "", "task")
	add(a.Error != "", "error")
	add(a.Value != nil, "value")
	add(a.Config != nil, "config")
	add(a.Meta != nil, "meta")
	add(a.Base != nil, "base")
	add(a.Head != nil, "head")
	add(len(a.Program) != 0, "program")
	add(len(a.Cleanup) != 0, "cleanup")
	add(len(a.Keys) != 0, "keys")
	add(a.Await != nil, "await")
	add(a.Strict != nil, "strict")
	add(a.Catch, "catch")
	add(a.Prepend, "prepend")
	add(a.Global, "global")
	add(a.Symbol, "symbol")
	add(a.Count != 0, "count")
	return out
}
