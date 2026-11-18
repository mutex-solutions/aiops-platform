// SPDX-License-Identifier: Apache-2.0

package differential

import "fmt"

// classifyNewShape marks the fibers whose update commands follow the amendment-3
// rules (K5 to K7): a plugin with an update policy or an internal/update
// listener, or a target of an update with await:false or task, or of an update
// issued while a mount or an earlier update of the same segment is unsettled.
func (m *causalModel) classifyNewShape() {
	active := false
	for _, a := range m.script.Steps {
		switch a.Op {
		case "mount":
			active = true
		case "settle", "snapshot":
			active = false
		case "update":
			if f := m.fibers[a.Fiber]; f != nil {
				if f.plugin.Update != "" || m.a3.updListener[a.Fiber] != nil || a.Task != "" || a.Await != nil && !*a.Await || active {
					m.a3.newShape[a.Fiber] = true
				}
			}
			active = true
		}
	}
	for _, id := range m.mountOrder {
		if f := m.fibers[id]; f.plugin.Update != "" || m.a3.updListener[id] != nil {
			m.a3.newShape[id] = true
		}
	}
}

// rootSteps compiles the root command program. Every root operation is either a
// command whose effect is recorded (mount, dispose, output, read, awaitFiber with
// catch) or a causal control (waitMark, awaitFiber, release, notify). A root
// command inherits every earlier waitMark/awaitFiber obligation (C1.2).
func (m *causalModel) rootSteps() error {
	m.classifyNewShape()
	mounted, disposed := make(map[string]bool), make(map[string]bool)
	order := make([]string, 0)
	dirty, mutation, previousDispose := false, false, ""
	settles := 0
	waited := false // a waitMark was issued since the last checkpoint
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
	var waits, cutWaits []causalWait
	notifies := make(map[string]int)
	copyWaits := func() []causalWait { return append([]causalWait(nil), waits...) }
	rootOut := func(name string, expect *causalExpect) error {
		if err := m.output(causalRootActor, name, causalFrame{}); err != nil {
			return err
		}
		if name != "" {
			m.rootOutputWaits[name] = append(m.rootOutputWaits[name], copyWaits())
			m.a3.rootOutExpect[name] = append(m.a3.rootOutExpect[name], expect)
		}
		return nil
	}
	for _, a := range m.script.Steps {
		if a.Op != "settle" && a.Op != "snapshot" {
			commands = append(commands, a)
		}
		if err := causalSynchronous(a); err != nil {
			return err
		}
		if err := causalFieldsOK(a); err != nil {
			return err
		}
		if (len(a.Program) != 0 && a.Op != "listen") || len(a.Cleanup) != 0 {
			return causalUnsupported("nested root operation %q", a.Op)
		}
		if mutation && a.Op != "settle" {
			return causalUnsupported("root mutation without immediate settled witness")
		}
		switch a.Op {
		case "dispose", "mount", "settle", "snapshot", "waitMark", "release", "awaitFiber", "lateEffect":
		default:
			if previousDispose != "" {
				return causalUnsupported("root disposal batch without settled witness")
			}
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
		if err := m.referencesIn(context, causalRootActor, a.Config); err != nil {
			return err
		}
		if err := m.referencesIn(context, causalRootActor, a.Value); err != nil {
			return err
		}
		target := func() error {
			if !mounted[a.Fiber] || disposed[a.Fiber] {
				return causalUnsupported("non-live root target %q", a.Fiber)
			}
			if m.a3.selfDispose[a.Fiber] {
				return causalUnsupported("root command on a self-disposing fiber %q", a.Fiber)
			}
			return nil
		}
		switch a.Op {
		case "mount":
			if mounted[a.Fiber] || observation {
				return causalUnsupported("unwitnessed fixed mount command boundary")
			}
			if err := m.referencesIn(context, a.Fiber, a.Config); err != nil {
				return err
			}
			mounted[a.Fiber] = true
			m.mountEpoch[a.Fiber] = settles
			m.a3.mountConfig[a.Fiber] = a.Config
			order = append(order, a.Fiber)
			m.pluginCommands = append(m.pluginCommands, causalPluginCommand{fiber: a.Fiber, previousDisposed: previousDispose, waits: copyWaits()})
			dirty, previousDispose = true, ""
		case "settle", "snapshot":
			// A snapshot does not wait. Fibers whose state is not settled can only
			// appear in it with a gate witness, which observe() verifies (C1.5).
			// Without any waitMark since the last cut a dirty snapshot is a race.
			if a.Op == "snapshot" && dirty && !waited {
				return causalUnsupported("snapshot before a settled root boundary")
			}
			if labels[a.Label] {
				return causalUnsupported("duplicate checkpoint label")
			}
			labels[a.Label] = true
			if a.Op == "settle" {
				settles++
			}
			spec := causalCheckpointSpec{label: a.Label, fibers: append([]string(nil), order...), plugins: len(m.pluginCommands), commands: commands, waits: cutWaits, snapshot: a.Op == "snapshot"}
			if a.Op == "settle" {
				spec.taskNeeds = append([]causalLaneNeed(nil), m.a3.taskNeeds...)
			}
			m.cuts = append(m.cuts, spec)
			m.notifyCuts = append(m.notifyCuts, notifies)
			commands, cutWaits, notifies = make([]Action, 0), nil, make(map[string]int)
			dirty, mutation, previousDispose, waited = false, false, "", false
			observation = false
		case "update", "restart":
			if err := target(); err != nil {
				return err
			}
			if a.Op == "update" && m.a3.newShape[a.Fiber] {
				if previousDispose != "" || observation || a.Mode != "" && a.Mode != "noSave" || dirty && !waited || len(m.children[a.Fiber]) != 0 || !causalPureMountConfig(a.Config) {
					return causalUnsupported("unwitnessed asynchronous or policy update")
				}
				f := m.fibers[a.Fiber]
				policy, listener := f.plugin.Update, m.a3.updListener[a.Fiber]
				if policy != "" && dirty {
					return causalUnsupported("policy update while the target may be in flux")
				}
				awaited := a.Await == nil || *a.Await
				cmd := &causalFiberCommand{op: "update", config: a.Config, noSave: a.Mode == "noSave", policy: policy, restart: policy == "" && listener == nil, waits: copyWaits(), segment: len(m.cuts)}
				restartCapable := cmd.restart || policy == "unchanged"
				position := len(m.a3.fiberCmds[a.Fiber])
				var task *causalTask
				if listener != nil {
					chain, rejected, err := m.chainLane(a.Fiber, listener, a.Config, position, copyWaits())
					if err != nil {
						return err
					}
					cmd.chain = chain.actor
					need := []causalLaneNeed{{chain.actor, len(chain.items)}}
					if awaited {
						for _, item := range chain.items {
							if item.kind == "gate" {
								return causalUnsupported("awaited update whose listener parks at a gate")
							}
						}
						waits = append(waits, causalWait{lanes: need})
					}
					task = &causalTask{needs: need, rejected: rejected}
				} else {
					task = &causalTask{restart: restartCapable}
				}
				if a.Task != "" {
					if m.a3.tasks[a.Task] != nil {
						return causalUnsupported("duplicate task %q", a.Task)
					}
					m.a3.tasks[a.Task] = task
					m.a3.taskNeeds = append(m.a3.taskNeeds, task.needs...)
				}
				m.a3.fiberCmds[a.Fiber] = append(m.a3.fiberCmds[a.Fiber], cmd)
				if awaited && restartCapable {
					mutation = true
				}
				if !awaited {
					dirty = true
				}
				if err := m.references(a.Fiber, a.Config); err != nil {
					return err
				}
				break
			}
			if a.Op == "restart" && (a.Await != nil && !*a.Await || a.Task != "" || m.a3.newShape[a.Fiber]) {
				return causalUnsupported("asynchronous restart")
			}
			if dirty || previousDispose != "" || observation || a.Mode != "" && !(a.Op == "update" && a.Mode == "noSave") {
				return causalUnsupported("unwitnessed root mutation")
			}
			if len(m.children[a.Fiber]) != 0 {
				return causalUnsupported("restart of a parent fiber with children")
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
			if dirty && !waited {
				return causalUnsupported("disposal before a settled root boundary")
			}
			disposed[a.Fiber] = true
			m.pluginCommands = append(m.pluginCommands, causalPluginCommand{fiber: a.Fiber, disposed: true, previousDisposed: previousDispose, waits: copyWaits()})
			previousDispose = a.Fiber
			if a.Await != nil && !*a.Await {
				previousDispose = "" // not awaited: a later command does not wait for it
			}
			kept := waits[:0:0]
			for _, w := range waits {
				if w.fiber != a.Fiber { // the disposal itself ends the awaited lifetime
					kept = append(kept, w)
				}
			}
			waits = kept
		case "awaitFiber":
			if m.fibers[a.Fiber] == nil || disposed[a.Fiber] {
				return causalUnsupported("non-live root target %q", a.Fiber)
			}
			waits = append(waits, causalWait{fiber: a.Fiber})
			if a.Catch {
				m.rootErrors = append(m.rootErrors, causalRootError{op: "awaitFiber", target: a.Fiber, waits: copyWaits()})
			}
		case "waitMark":
			if a.Label == "" || a.Count < 0 {
				return causalUnsupported("waitMark without a label")
			}
			count := a.Count
			if count == 0 {
				count = 1
			}
			w := causalWait{label: a.Label, count: count}
			waits, cutWaits, waited = append(waits, w), append(cutWaits, w), true
		case "release":
			if a.Label == "" {
				return causalUnsupported("release without a label")
			}
			if _, done := m.release[a.Label]; !done {
				m.release[a.Label] = len(m.cuts)
			}
		case "notify":
			for _, key := range a.Keys {
				notifies[causalBuiltin(key)]++
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
			if a.Op == "resolveConfig" && m.a3.newShape[a.Fiber] {
				m.a3.fiberCmds[a.Fiber] = append(m.a3.fiberCmds[a.Fiber], &causalFiberCommand{op: "resolveConfig", config: a.Config, waits: copyWaits(), segment: len(m.cuts)})
			}
			if a.Op != "rawConfig" {
				if err := rootOut(a.To, nil); err != nil {
					return err
				}
			}
		case "read":
			// A root read while startups are in flight is compared by content and
			// conflict order against every provider event (footprint link below).
			if dirty && !waited || a.Mode != "" && a.Mode != "get" {
				return causalUnsupported("root service read without settled ordinary realm")
			}
			m.footprints[causalRootActor].reads[m.serviceCell(context, a.Key)] = true
			if err := rootOut(a.To, nil); err != nil {
				return err
			}
		case "output":
			if err := rootOut(a.To, m.metaExpectation(context, a.Value)); err != nil {
				return err
			}
		case "provide":
			// K2: a root-lane publication by the provider "root".
			address := m.address(context, a.Key)
			if a.To != "" || a.Mode != "" || a.Key == "" || a.Value == nil || !causalPureMountConfig(a.Value) || len(m.providers[address]) != 0 || m.a3.rootProvides[address] != nil {
				return causalUnsupported("root provision form or second provider of %s/%s", address.key, address.realm)
			}
			m.providers[address] = []string{rootProviderName}
			m.provides[rootProviderName] = append(m.provides[rootProviderName], address)
			m.a3.rootProvides[address] = &causalRootProvide{value: a.Value, waits: copyWaits(), previousDisposed: previousDispose}
			m.footprints[causalRootActor].writes[m.serviceCell(context, a.Key)] = true
		case "lateEffect":
			t := m.fibers[a.Fiber]
			if t == nil || t.parent != "" || !a.Catch || a.To == "" || a.Label == "" {
				return causalUnsupported("root lateEffect form")
			}
			m.rootErrors = append(m.rootErrors, causalRootError{op: "lateEffect", target: a.Fiber, waits: copyWaits(), previousDisposed: previousDispose})
			if err := rootOut(a.To, nil); err != nil {
				return err
			}
		case "listen":
			if err := m.hookListen(a); err != nil {
				return err
			}
		case "dispatch":
			needs, task, err := m.hookDispatch(a, copyWaits())
			if err != nil {
				return err
			}
			if needs != nil {
				waits = append(waits, causalWait{lanes: needs})
			}
			if task != nil && a.Task != "" {
				m.a3.tasks[a.Task] = task
				m.a3.taskNeeds = append(m.a3.taskNeeds, task.needs...)
			}
		case "awaitTask":
			t := m.a3.tasks[a.Task]
			if t == nil || t.restart {
				return causalUnsupported("awaitTask of an unknown or restart-capable task %q", a.Task)
			}
			if t.rejected && (!a.Catch || a.To == "") {
				return causalUnsupported("awaitTask of a rejected task without catch and cell")
			}
			waits = append(waits, causalWait{lanes: t.needs})
			if t.rejected {
				m.rootErrors = append(m.rootErrors, causalRootError{op: "awaitTask", waits: copyWaits()})
				if err := rootOut(a.To, nil); err != nil {
					return err
				}
			}
		default:
			return causalUnsupported("root operation %q", a.Op)
		}
	}
	if len(m.cuts) == 0 || len(m.script.Steps) == 0 || (m.script.Steps[len(m.script.Steps)-1].Op != "settle" && m.script.Steps[len(m.script.Steps)-1].Op != "snapshot") {
		return causalUnsupported("missing final witnessed checkpoint")
	}
	// A listener the plan never runs (after a bail, a veto or a failure, or one of
	// a key that is never dispatched after it) must not produce its listener event
	// or any mark or gate of its program.
	for _, l := range m.a3.listens {
		if len(m.a3.laneItems["listener\x00"+l.Label]) != 0 {
			continue
		}
		m.a3.listenItems["listener\x00"+l.Label] = true
		for _, step := range l.Program {
			if step.Op == "mark" || step.Op == "gate" {
				m.a3.listenItems[step.Op+"\x00"+step.Label] = true
			}
		}
	}
	// Every hook or chain gate needs a release command, as for plugin gates.
	for _, lane := range m.a3.lanes {
		for _, item := range lane.items {
			if item.kind == "gate" {
				if _, released := m.release[item.key]; !released {
					return causalUnsupported("gate %q is never released", item.key)
				}
			}
		}
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

// rootProviderName is the provider recorded for a root provision (K2).
const rootProviderName = "root"

// chainLane plans the internal/update listener invocation of one update command
// (K7) and links its lane to the root and to its fiber.
func (m *causalModel) chainLane(fiber string, l *causalUpdateListener, candidate any, position int, waits []causalWait) (*causalLaneSpec, bool, error) {
	items, rejected, err := causalUpdateProgram(l.label, l.program, candidate)
	if err != nil {
		return nil, false, err
	}
	ordinal := 0
	for _, c := range m.a3.fiberCmds[fiber] {
		if c.chain != "" {
			ordinal++
		}
	}
	lane := &causalLaneSpec{actor: fmt.Sprintf("chain:%s:%s#%d", fiber, l.label, ordinal), items: items, waits: waits, chainFiber: fiber, chainCmd: position}
	lane.prefix = len(items)
	for _, item := range items {
		if (item.kind == "mark" || item.kind == "gate") && len(m.labels[item.key]) != 0 {
			return nil, false, causalUnsupported("label %q is used by a chain lane and a plugin body", item.key)
		}
	}
	if err := m.laneOutputs(lane); err != nil {
		return nil, false, err
	}
	m.registerLane(lane)
	m.link(causalRootActor, lane.actor)
	m.link(fiber, lane.actor)
	return lane, rejected, nil
}
