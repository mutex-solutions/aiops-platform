// SPDX-License-Identifier: Apache-2.0

package differential

// rootSteps compiles the root command program. Every root operation is either a
// command whose effect is recorded (mount, dispose, output, read, awaitFiber with
// catch) or a causal control (waitMark, awaitFiber, release, notify). A root
// command inherits every earlier waitMark/awaitFiber obligation (C1.2).
func (m *causalModel) rootSteps() error {
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
	rootOut := func(name string) error {
		if err := m.output(causalRootActor, name, causalFrame{}); err != nil {
			return err
		}
		if name != "" {
			m.rootOutputWaits[name] = append(m.rootOutputWaits[name], copyWaits())
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
		if len(a.Program) != 0 || len(a.Cleanup) != 0 {
			return causalUnsupported("nested root operation %q", a.Op)
		}
		if mutation && a.Op != "settle" {
			return causalUnsupported("root mutation without immediate settled witness")
		}
		switch a.Op {
		case "dispose", "mount", "settle", "snapshot", "waitMark", "release", "awaitFiber":
		default:
			if previousDispose != "" {
				return causalUnsupported("root disposal batch without settled witness")
			}
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
			if mounted[a.Fiber] || observation {
				return causalUnsupported("unwitnessed fixed mount command boundary")
			}
			if err := m.references(a.Fiber, a.Config); err != nil {
				return err
			}
			mounted[a.Fiber] = true
			m.mountEpoch[a.Fiber] = settles
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
			m.cuts = append(m.cuts, causalCheckpointSpec{label: a.Label, fibers: append([]string(nil), order...), plugins: len(m.pluginCommands), commands: commands, waits: cutWaits, snapshot: a.Op == "snapshot"})
			m.notifyCuts = append(m.notifyCuts, notifies)
			commands, cutWaits, notifies = make([]Action, 0), nil, make(map[string]int)
			dirty, mutation, previousDispose, waited = false, false, "", false
			observation = false
		case "update", "restart":
			if err := target(); err != nil {
				return err
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
				m.rootErrors = append(m.rootErrors, causalRootError{target: a.Fiber, waits: copyWaits()})
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
			if a.Op != "rawConfig" {
				if err := rootOut(a.To); err != nil {
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
			if err := rootOut(a.To); err != nil {
				return err
			}
		case "output":
			if err := rootOut(a.To); err != nil {
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
