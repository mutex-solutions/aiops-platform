// SPDX-License-Identifier: Apache-2.0

package differential

import "fmt"

type causalStoreEntry struct {
	owner      string
	value      any
	activation int
}

// causalGate is a gate arrival that still blocks (rel is the segment of the
// release command, causalNever when the script never releases it).
type causalGate struct {
	label string
	rel   int
}

// observe validates the sidecar against facts reconstructible from the raw
// stream. A hash is binding, not authenticity: the recorder is still responsible
// for observing the cut. Impossible output/state/plugin prefixes are rejected.
// Every rule here runs on one trace alone, so a violation shared by both traces
// is rejected before any comparison.
func (m *causalModel) observe(trace *Trace, witness CausalWitness) ([]causalObservation, error) {
	observations := make([]causalObservation, len(trace.Events))
	states := make(map[string]string)
	outputs := make(map[string]any)
	configs := make(map[string]any)
	disposed := make(map[string]bool)
	activations := make(map[string]int)
	applied, failed := make(map[string]bool), make(map[string]bool)
	cleaned := make(map[string]bool)
	bound := make(map[string]map[causalAddress]causalBind)
	retiring := make(map[string]int)
	mutationAnchors := make(map[string]bool)
	arrivals := make(map[string]int)
	syncBlock := make(map[string]causalGate)
	asyncGate := make(map[string]map[string]causalGate)
	writeCount := make(map[string]int)
	rootOutputSeen := make(map[string]int)
	o := m.newA3obs()
	// nextRootError returns the next unconsumed root error command of an op.
	nextRootError := func(op string) *causalRootError {
		n, seen := o.errSeen[op], 0
		for i := range m.rootErrors {
			if m.rootErrors[i].op == op {
				if seen == n {
					return &m.rootErrors[i]
				}
				seen++
			}
		}
		return nil
	}
	notifyUsed := make(map[string]int)
	refusing, refusalPending := make(map[string]bool), make(map[string]bool)
	seqPos := make(map[string]int)
	effectRan := make(map[string]int)
	stepPos := make(map[string]int)
	reachedActive := make(map[string]bool)
	order := make([]string, 0)
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
	store := make(map[causalAddress]causalStoreEntry)
	invalidated := make(map[causalAddress]bool)
	publishedAct := make(map[string]map[causalAddress]int)
	// For every provider activation, these mounted pending consumers became
	// ready at its ACTIVE transition and must enter LOADING before publication.
	triggered := make(map[string]map[string]bool)
	audience := func(actor string) any { return m.ctxMeta[m.fibers[actor].context]["audience"] }
	available := func(address causalAddress, actor string) bool {
		cell := m.avail[address]
		if cell == "" {
			return true
		}
		ready, _ := outputs[cell].(map[string]any)
		key, ok := audience(actor).(string)
		return ok && ready != nil && ready[key] == true
	}
	readiness := func(actor string) map[causalAddress]causalBind {
		fiber := m.fibers[actor]
		bindings := make(map[causalAddress]causalBind)
		for _, key := range fiber.needs {
			address := m.address(fiber.context, key)
			owner := ""
			for _, c := range m.providers[address] {
				if states[c] == "ACTIVE" && activations[c] > 0 {
					if owner != "" {
						return nil
					}
					owner = c
				}
			}
			if owner == "" || !available(address, actor) {
				return nil
			}
			bindings[address] = causalBind{owner, activations[owner]}
		}
		return bindings
	}
	// blockedUnreleased: the actor's frame is parked at a gate whose release
	// command had not been issued before cut ci and whose arrival a waitMark
	// awaited. This is the phase witness for a LOADING/UNLOADING checkpoint (C1.5).
	gateBlocked := func(g causalGate, ci int) bool { return g.rel > ci && m.waited(g.label, ci) }
	asyncPending := func(actor string, ci int) bool {
		for _, g := range asyncGate[actor] {
			if gateBlocked(g, ci) {
				return true
			}
		}
		return false
	}
	// An UNLOADING fiber is also parked when something it must join is parked: a
	// descendant with a pending asynchronous setup, or a bound consumer whose own
	// teardown waits at a gate (a provider is retained through that teardown).
	var blockedAt func(actor string, ci int) bool
	blockedAt = func(actor string, ci int) bool {
		if g, ok := syncBlock[actor]; ok && gateBlocked(g, ci) {
			return true
		}
		if states[actor] != "UNLOADING" {
			return false
		}
		if asyncPending(actor, ci) {
			return true
		}
		for _, d := range m.descendants(actor) {
			if asyncPending(d, ci) || blockedAt(d, ci) {
				return true
			}
		}
		for consumer, bindings := range bound {
			if states[consumer] != "UNLOADING" {
				continue
			}
			for _, b := range bindings {
				if b.provider == actor && b.act == retiring[actor] && blockedAt(consumer, ci) {
					return true
				}
			}
		}
		return false
	}
	pluginCount, nextCut := 0, 0
	check := func(ci int) error {
		cp, spec := &trace.Checkpoints[ci], m.cuts[ci]
		if pluginCount != spec.plugins || len(cp.States) != len(order) {
			return fmt.Errorf("checkpoint %q contradicts root mount/disposal prefix", cp.Label)
		}
		for _, w := range spec.waits {
			if arrivals[w.label] < w.count {
				return fmt.Errorf("checkpoint %q: waitMark %q needs %d arrivals, trace has %d", cp.Label, w.label, w.count, arrivals[w.label])
			}
		}
		if err := o.checkCut(ci, cp.Label); err != nil {
			return err
		}
		if !spec.snapshot {
			// K3: a settled cut shows every fiber that requested its own disposal
			// as disposed.
			for actor, requested := range o.selfReq {
				if requested && states[actor] != "DISPOSED" {
					return fmt.Errorf("checkpoint %q: %q requested its disposal but is %s", cp.Label, actor, states[actor])
				}
			}
		}
		if !causalEqual(cp.Outputs, outputs) {
			return fmt.Errorf("checkpoint %q contradicts output-event prefix", cp.Label)
		}
		// A provision registered by a gated LOADING provider before its gate is
		// visible, inactive, before any service event (D11-shaped witness).
		pending := make(map[causalAddress]string)
		for _, actor := range order {
			if g, ok := syncBlock[actor]; ok && states[actor] == "LOADING" && g.rel > ci && m.waited(g.label, ci) {
				for _, address := range m.provBefore[g.label] {
					if _, exists := store[address]; !exists && m.isCandidate(address, actor) {
						pending[address] = actor
					}
				}
			}
		}
		if len(cp.Provisions) != len(store)+len(pending) {
			return fmt.Errorf("checkpoint %q contradicts observed provision-store domain", cp.Label)
		}
		seenProvisions := make(map[causalAddress]bool)
		for _, provision := range cp.Provisions {
			address := causalAddress{provision.Key, provision.Realm}
			if seenProvisions[address] {
				return fmt.Errorf("checkpoint %q contradicts published provision ownership/value/state", cp.Label)
			}
			observed, ok := store[address]
			if ok {
				if provision.Provider != observed.owner || !causalEqual(provision.Value, observed.value) || provision.Active != (states[provision.Provider] == "ACTIVE") {
					return fmt.Errorf("checkpoint %q contradicts published provision ownership/value/state", cp.Label)
				}
			} else if owner, isPending := pending[address]; !isPending || provision.Provider != owner || provision.Active {
				return fmt.Errorf("checkpoint %q contradicts published provision ownership/value/state", cp.Label)
			}
			seenProvisions[address] = true
		}
		for i, state := range cp.States {
			if state.Fiber != order[i] || state.State != states[state.Fiber] {
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
			if (state.State == "LOADING" || state.State == "UNLOADING") && !blockedAt(state.Fiber, ci) {
				return causalUnsupported("unsettled lifecycle checkpoint without phase witness")
			}
			if state.State == "ACTIVE" {
				for _, address := range m.provides[state.Fiber] {
					if entry, ok := store[address]; !ok || entry.owner != state.Fiber {
						return fmt.Errorf("active provider lacks observed publication")
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
			if m.a3.updListener[state.Fiber] != nil {
				// K7.5: the publication of an update with a listener is deferred
				// and conditional; the effective configuration may only be one
				// value the fiber has applied or resolved.
				known := false
				for _, c := range append(append([]any(nil), o.appliedCfgs[state.Fiber]...), o.candidates[state.Fiber]...) {
					known = known || causalEqual(c, state.EffectiveConfig)
				}
				if !known {
					return fmt.Errorf("checkpoint %q effective config of %q is no applied or resolved value", cp.Label, state.Fiber)
				}
			} else if config, ok := configs[state.Fiber]; ok && !causalEqual(config, state.EffectiveConfig) {
				return fmt.Errorf("checkpoint %q differs from captured effective config", cp.Label)
			}
		}
		return nil
	}

	phaseClass := func(phase string) string {
		if phase == "cleanup" || phase == "effect-cleanup" {
			return "cleanup"
		}
		return "apply"
	}
	legalPhase := func(def causalLabelDef) bool {
		actor, st := def.actor, states[def.actor]
		switch def.phase {
		case "apply", "effect-setup":
			return st == "LOADING" && applied[actor]
		case "cleanup":
			return st == "UNLOADING" && cleaned[actor]
		case "effect-async":
			return st == "LOADING" && applied[actor] || st == "ACTIVE" || st == "UNLOADING"
		case "effect-cleanup":
			if st == "UNLOADING" {
				return true
			}
			eff := m.effects[def.effect]
			return st == "LOADING" && applied[actor] && eff != nil && (eff.partial || eff.withdraw)
		}
		return false
	}
	// resolve picks the unique owner of an event that carries no fiber: the only
	// candidate inside the frame that can emit it. No candidate is a mismatch,
	// several are unprovable, never a guess (vocabulary rule of amendment 2).
	resolve := func(defs []causalLabelDef, cell string) (causalLabelDef, error) {
		var legal []causalLabelDef
		for _, def := range defs {
			if !legalPhase(def) {
				continue
			}
			if cell != "" {
				budget := 0
				for _, other := range defs {
					if other.actor == def.actor && phaseClass(other.phase) == phaseClass(def.phase) {
						budget++
					}
				}
				if writeCount[def.actor+"\x00"+cell+"\x00"+phaseClass(def.phase)] >= budget {
					continue
				}
			}
			legal = append(legal, def)
		}
		if len(legal) == 0 {
			return causalLabelDef{}, fmt.Errorf("event outside the frame of every possible owner")
		}
		actors := make(map[string]bool)
		for _, def := range legal {
			actors[def.actor] = true
		}
		if len(actors) > 1 {
			var open []causalLabelDef
			for _, def := range legal {
				if _, parked := syncBlock[def.actor]; !parked {
					open = append(open, def)
				}
			}
			legal, actors = open, make(map[string]bool)
			for _, def := range legal {
				actors[def.actor] = true
			}
			if len(actors) != 1 {
				return causalLabelDef{}, causalUnsupported("ambiguous event owner")
			}
		}
		return legal[0], nil
	}
	// effectStep enforces program order and single execution of an ordered
	// effect's cleanup program (C3.2).
	effectStep := func(id string, item causalItem) error {
		eff := m.effects[id]
		if eff != nil && eff.ordered && (effectRan[id] >= len(eff.items) || eff.items[effectRan[id]] != item) {
			return fmt.Errorf("effect %q cleanup event %s:%s breaks program order or repeats", id, item.kind, item.key)
		}
		effectRan[id]++
		return nil
	}
	rootWaits := func(waits []causalWait) error {
		for _, w := range waits {
			if w.fiber != "" {
				if s := states[w.fiber]; s == "" || s == "LOADING" || s == "UNLOADING" {
					return fmt.Errorf("root command overtook awaited fiber %q (state %q)", w.fiber, s)
				}
			} else if len(w.lanes) != 0 {
				if !o.lanesDone(w.lanes) {
					return fmt.Errorf("root command overtook an awaited dispatch, update chain or task")
				}
			} else if arrivals[w.label] < w.count {
				return fmt.Errorf("root command ran before arrival %d at %q (seen %d)", w.count, w.label, arrivals[w.label])
			}
		}
		return nil
	}
	deps := a3deps{states: states, activations: activations, rootWaits: rootWaits}
	programEvent := func(actor, phase string, item causalItem) error {
		cs := m.seq[actor]
		if cs == nil {
			return nil
		}
		items, exact := cs.apply, cs.applyExact
		if phase == "cleanup" {
			items, exact = cs.cleanup, cs.cleanupExact
		}
		if !exact {
			return nil
		}
		key := actor + "\x00" + phase
		if seqPos[key] >= len(items) || items[seqPos[key]] != item {
			return fmt.Errorf("%s body event %s:%s of %q breaks program order", phase, item.kind, item.key, actor)
		}
		seqPos[key]++
		return nil
	}
	programDone := func(actor, phase string) error {
		cs := m.seq[actor]
		if cs == nil {
			return nil
		}
		items, exact := cs.apply, cs.applyExact
		if phase == "cleanup" {
			items, exact = cs.cleanup, cs.cleanupExact
		}
		if exact && seqPos[actor+"\x00"+phase] != len(items) {
			return fmt.Errorf("%s body of %q ended with %d of %d events", phase, actor, seqPos[actor+"\x00"+phase], len(items))
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
		cmdDriven := false
		observation := causalObservation{}
		var folded map[string]any
		var def causalLabelDef
		switch e.Kind {
		case "listener", "inner":
			// K8: a host listener starts, or the built-in step of a waterfall runs.
			key, _ := e.Data["label"].(string)
			if e.Kind == "inner" {
				key, _ = e.Data["key"].(string)
			}
			l, err := o.laneFor(e.Kind, key, deps)
			if err != nil {
				return nil, err
			}
			if l == nil {
				return nil, causalUnsupported("unproved %s owner %q", e.Kind, key)
			}
			actor = l.spec.actor
		case "status", "plugin", "config", "apply", "update", "commit", "cleanup", "error", "selfDispose":
			actor, _ = e.Data["fiber"].(string)
			rootLane = e.Kind == "config" && m.rootCommandActors[actor]
			if m.a3.newShape[actor] && m.fibers[actor] != nil {
				switch e.Kind {
				case "config":
					if states[actor] == "LOADING" && !o.frameSeen[actor] {
						// Frame-driven: the first config event of a loading frame is the
						// latest raw configuration resolved (K6.4).
						o.frameSeen[actor], o.frameCfg[actor] = true, e.Data["config"]
						if len(m.fibers[actor].plugin.ConfigTransform) == 0 && !causalEqual(e.Data["config"], o.rawLatest[actor]) {
							return nil, fmt.Errorf("loading frame of %q resolves a stale target configuration", actor)
						}
						break
					}
					fallthrough
				case "commit", "update":
					effective, hasEffective := configs[actor]
					if err := o.cmdEvent(actor, e.Kind, e.Data, effective, hasEffective, deps, applied[actor]); err != nil {
						return nil, err
					}
					cmdDriven, rootLane = true, true
				}
			}
			if e.Kind == "error" && actor == "" {
				// A caught error carries its operation, not its fiber (C3.3).
				op, _ := e.Data["op"].(string)
				if op == "awaitFiber" || op == "awaitTask" {
					cmd := nextRootError(op)
					if cmd == nil {
						return nil, fmt.Errorf("extra root %s error", op)
					}
					if err := rootWaits(cmd.waits); err != nil {
						return nil, err
					}
					if op == "awaitFiber" && states[cmd.target] != "FAILED" {
						return nil, fmt.Errorf("awaitFiber rejected before %q FAILED", cmd.target)
					}
					o.errSeen[op]++
					actor, rootLane = causalRootActor, true
					break
				}
				if op == "dispatch" {
					l, err := o.laneFor("error", "dispatch", deps)
					if err != nil {
						return nil, err
					}
					if l == nil {
						return nil, causalUnsupported("unproved dispatch error owner")
					}
					actor = l.spec.actor
					break
				}
				if op == "lateEffect" {
					// K4.3: a fiber in teardown, or an unconsumed root command.
					var fiberDefs []causalLabelDef
					for _, le := range m.a3.lateEffects {
						d := causalLabelDef{actor: le.actor, phase: "effect-cleanup", effect: le.effect}
						if legalPhase(d) {
							fiberDefs = append(fiberDefs, d)
						}
					}
					rootCmd := nextRootError("lateEffect")
					rootOK := rootCmd != nil && rootWaits(rootCmd.waits) == nil && (rootCmd.previousDisposed == "" || states[rootCmd.previousDisposed] == "DISPOSED")
					target := ""
					switch {
					case len(fiberDefs)+boolInt(rootOK) == 0:
						return nil, fmt.Errorf("lateEffect refusal outside the frame of every possible executor")
					case len(fiberDefs)+boolInt(rootOK) > 1:
						return nil, causalUnsupported("ambiguous lateEffect executor")
					case rootOK:
						target = rootCmd.target
						o.errSeen["lateEffect"]++
						actor, rootLane = causalRootActor, true
					default:
						def, actor = fiberDefs[0], fiberDefs[0].actor
						for _, le := range m.a3.lateEffects {
							if le.actor == actor && le.effect == def.effect {
								target = le.target
							}
						}
					}
					if s := states[target]; s != "UNLOADING" && s != "DISPOSED" {
						return nil, fmt.Errorf("lateEffect refused while its target %q is %q", target, s)
					}
					if payload, _ := e.Data["error"].(map[string]any); payload["code"] != "INACTIVE_EFFECT" {
						return nil, fmt.Errorf("lateEffect refusal lacks the INACTIVE_EFFECT code")
					}
					break
				}
				var defs []causalLabelDef
				for _, c := range m.catchers[op] {
					defs = append(defs, causalLabelDef{actor: c, phase: "apply"})
				}
				if len(defs) == 0 {
					return nil, causalUnsupported("unproved %s owner %q", e.Kind, op)
				}
				d, err := resolve(defs, "")
				if err != nil {
					return nil, fmt.Errorf("caught %q error: %w", op, err)
				}
				actor = d.actor
				break
			}
			if m.fibers[actor] == nil {
				return nil, causalUnsupported("unproved %s owner %q", e.Kind, actor)
			}
		case "gate", "mark":
			label, _ := e.Data["label"].(string)
			if l, err := o.laneFor(e.Kind, label, deps); err != nil {
				return nil, err
			} else if l != nil {
				def, actor = causalLabelDef{actor: l.spec.actor, phase: "hook", isGate: e.Kind == "gate"}, l.spec.actor
				break
			}
			defs := m.labels[label]
			if len(defs) == 0 {
				return nil, causalUnsupported("unproved %s owner %q", e.Kind, label)
			}
			var typed []causalLabelDef
			for _, d := range defs {
				if d.isGate == (e.Kind == "gate") {
					typed = append(typed, d)
				}
			}
			d, err := resolve(typed, "")
			if err != nil {
				return nil, fmt.Errorf("%s %q: %w", e.Kind, label, err)
			}
			def, actor = d, d.actor
		case "service":
			key, keyOK := e.Data["key"].(string)
			realm, realmOK := e.Data["realm"].(string)
			if !keyOK || !realmOK {
				return nil, causalUnsupported("service event without address")
			}
			address := causalAddress{key, realm}
			candidates := m.providers[address]
			if len(candidates) == 0 {
				return nil, causalUnsupported("unproved service provider for %s/%s", key, realm)
			}
			value, exists := e.Data["value"]
			if !exists {
				return nil, fmt.Errorf("service event lacks value")
			}
			if rp := m.a3.rootProvides[address]; rp != nil && !o.rootPublished[address] {
				// K2: the publication of a root provision command.
				if err := rootWaits(rp.waits); err != nil {
					return nil, err
				}
				if rp.previousDisposed != "" && states[rp.previousDisposed] != "DISPOSED" {
					return nil, fmt.Errorf("root provision overtook an awaited disposal")
				}
				if !causalEqual(value, rp.value) {
					return nil, fmt.Errorf("root provision %s/%s published a value other than its command's", key, realm)
				}
				o.rootPublished[address] = true
				store[address] = causalStoreEntry{owner: rootProviderName, value: value, activation: 1}
				states[rootProviderName], activations[rootProviderName] = "ACTIVE", 1
				actor, rootLane = causalRootActor, true
				break
			}
			prior, occupied := store[address]
			if !occupied {
				// Publication: the unique candidate which became ACTIVE and has not
				// published this activation (C6.1). Never inferred from its payload.
				var publishers []string
				for _, c := range candidates {
					if states[c] == "ACTIVE" && !disposed[c] && activations[c] > 0 && publishedAct[c][address] != activations[c] {
						publishers = append(publishers, c)
					}
				}
				if len(publishers) == 0 {
					return nil, fmt.Errorf("service event without a publishing or retiring owner")
				}
				if len(publishers) > 1 {
					return nil, causalUnsupported("ambiguous ordinary publication window")
				}
				actor = publishers[0]
				activation := activations[actor]
				for consumer, pending := range triggered[actor] {
					if pending {
						return nil, fmt.Errorf("last enabling provider %q published before consumer %q LOADING", actor, consumer)
					}
				}
				store[address] = causalStoreEntry{owner: actor, value: value, activation: activation}
				invalidated[address] = false
				if publishedAct[actor] == nil {
					publishedAct[actor] = make(map[causalAddress]int)
				}
				publishedAct[actor][address] = activation
				observation.publication, observation.publicationActivation, observation.serviceAddress = true, activation, address
				break
			}
			actor = prior.owner
			if states[actor] == "ACTIVE" && !disposed[actor] {
				// A notify command re-notifies an unchanged, active provision (C4.4).
				budget := 0
				for i := 0; i <= segment && i < len(m.notifyCuts); i++ {
					budget += m.notifyCuts[i][key]
				}
				if !causalEqual(value, prior.value) || notifyUsed[key] >= budget {
					return nil, fmt.Errorf("provider republished or re-notified without a notify command before retiring former provision")
				}
				notifyUsed[key]++
				rootLane = true
				break
			}
			if states[actor] != "UNLOADING" {
				return nil, fmt.Errorf("service retirement without owned unloading provision")
			}
			undefined := causalEqual(value, map[string]any{"$undefined": true})
			if !invalidated[address] && causalEqual(value, prior.value) {
				for consumer, bindings := range bound {
					if bindings[address] == (causalBind{actor, prior.activation}) && (states[consumer] == "ACTIVE" || states[consumer] == "LOADING") {
						return nil, fmt.Errorf("retained withdrawal precedes bound consumer %q UNLOADING", consumer)
					}
				}
				invalidated[address] = true
			} else if undefined {
				for consumer, bindings := range bound {
					if bindings[address] == (causalBind{actor, prior.activation}) {
						return nil, fmt.Errorf("provider deletion precedes retained consumer %q stable teardown", consumer)
					}
				}
				delete(store, address)
				delete(invalidated, address)
			} else {
				return nil, fmt.Errorf("retirement notification changed retained service value")
			}
		case "output":
			to, ok := e.Data["to"].(string)
			if !ok || len(m.outputs[to]) == 0 {
				return nil, causalUnsupported("unproved output owner %q", to)
			}
			value, exists := e.Data["value"]
			if !exists {
				return nil, fmt.Errorf("output event lacks value")
			}
			if m.outputs[to][0] == causalRootActor {
				seen := rootOutputSeen[to]
				if seen >= len(m.rootOutputWaits[to]) {
					return nil, fmt.Errorf("extra root output event for %q", to)
				}
				if err := rootWaits(m.rootOutputWaits[to][seen]); err != nil {
					return nil, err
				}
				if expect := m.a3.rootOutExpect[to][seen]; expect != nil && !causalEqual(value, expect.value) {
					return nil, fmt.Errorf("root output %q differs from its static metadata value", to)
				}
				rootOutputSeen[to]++
				actor, rootLane = causalRootActor, true
			} else if l, err := o.laneFor("output", to, deps); err != nil {
				return nil, err
			} else if l != nil {
				def, actor = causalLabelDef{actor: l.spec.actor, phase: "hook"}, l.spec.actor
			} else {
				d, err := resolve(m.cellDefs[to], to)
				if err != nil {
					return nil, fmt.Errorf("output %q: %w", to, err)
				}
				if d.meta != nil && !causalEqual(value, d.meta.value) {
					return nil, fmt.Errorf("output %q differs from its static metadata value", to)
				}
				def, actor = d, d.actor
				writeCount[actor+"\x00"+to+"\x00"+phaseClass(d.phase)]++
			}
			outputs[to] = value
		default:
			return nil, causalUnsupported("event kind %q", e.Kind)
		}
		if actor != causalRootActor && states[actor] == "" && e.Kind != "plugin" && !o.isLane(actor) {
			return nil, fmt.Errorf("event before fixed mount publication")
		}
		// C1.3: a parked gate blocks every later event of its lane until the
		// recorder's cut proves the release command had been issued. Command-driven
		// events are not frame events (K6.2) and neither clear nor obey the gate.
		if g, parked := syncBlock[actor]; parked && e.Kind != "plugin" && !cmdDriven {
			if nextCut < g.rel {
				return nil, fmt.Errorf("lane %q continued past gate %q before its release", actor, g.label)
			}
			delete(syncBlock, actor)
		}
		if (e.Kind == "gate" || e.Kind == "mark") && def.phase == "effect-cleanup" {
			if g, ok := asyncGate[actor][def.effect]; ok && nextCut < g.rel {
				return nil, fmt.Errorf("effect cleanup %q ran before its asynchronous setup gate %q was released", def.effect, g.label)
			}
		}
		if def.stepped {
			// Events of one synchronous frame follow the program order of the
			// top-level operations that trigger them.
			key := actor + "\x00" + states[actor]
			if last, seen := stepPos[key]; seen && def.step < last {
				return nil, fmt.Errorf("body event of %q for step %d follows step %d", actor, def.step, last)
			}
			stepPos[key] = def.step
		}
		if e.Kind == "gate" || e.Kind == "mark" {
			label, _ := e.Data["label"].(string)
			arrivals[label]++
			if def.phase == "effect-cleanup" {
				if err := effectStep(def.effect, causalItem{e.Kind, label}); err != nil {
					return nil, err
				}
			}
			if phase := causalBodyPhase(def.phase); phase != "" {
				if err := programEvent(actor, phase, causalItem{e.Kind, label}); err != nil {
					return nil, err
				}
			}
			if e.Kind == "gate" {
				rel, released := m.release[label]
				if !released {
					rel = causalNever
				}
				if def.phase == "effect-async" {
					if asyncGate[actor] == nil {
						asyncGate[actor] = make(map[string]causalGate)
					}
					asyncGate[actor][def.effect] = causalGate{label, rel}
				} else {
					syncBlock[actor] = causalGate{label, rel}
				}
			}
		}
		if e.Kind == "output" && actor != causalRootActor {
			to, _ := e.Data["to"].(string)
			if def.phase == "effect-cleanup" {
				if err := effectStep(def.effect, causalItem{"output", to}); err != nil {
					return nil, err
				}
			}
			if phase := causalBodyPhase(def.phase); phase != "" {
				if err := programEvent(actor, phase, causalItem{"output", to}); err != nil {
					return nil, err
				}
			}
		}
		if e.Kind == "plugin" {
			isDisposed, ok := e.Data["disposed"].(bool)
			if !ok {
				return nil, fmt.Errorf("invalid/extra root plugin event")
			}
			f := m.fibers[actor]
			if f.parent == "" && isDisposed && m.a3.selfDispose[actor] {
				// K3.2: the request of the fiber's own Apply, not a root command.
				if !o.selfReq[actor] {
					return nil, fmt.Errorf("disposal of self-disposing %q without its selfDispose request", actor)
				}
			} else if f.parent == "" {
				if pluginCount >= len(m.pluginCommands) {
					return nil, fmt.Errorf("invalid/extra root plugin event")
				}
				cmd := m.pluginCommands[pluginCount]
				if cmd.fiber != actor || cmd.disposed != isDisposed {
					return nil, fmt.Errorf("root plugin command order mismatch")
				}
				if cmd.previousDisposed != "" && states[cmd.previousDisposed] != "DISPOSED" {
					return nil, fmt.Errorf("root disposal overtook awaited prior disposal")
				}
				if err := rootWaits(cmd.waits); err != nil {
					return nil, err
				}
				pluginCount++
				rootLane = true
			} else {
				// A child is mounted by its parent's Apply and torn down by its
				// parent's disposal (C2.1, C2.2). It is never a root command.
				if isDisposed {
					if states[f.parent] != "UNLOADING" && !disposed[f.parent] {
						return nil, fmt.Errorf("child %q disposed before its parent %q", actor, f.parent)
					}
				} else if states[f.parent] != "LOADING" || !applied[f.parent] {
					return nil, fmt.Errorf("child %q mounted outside its parent %q Apply", actor, f.parent)
				} else if err := programEvent(f.parent, "apply", causalItem{"child", actor}); err != nil {
					return nil, err
				}
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
				order = append(order, actor)
				observation.pureMount = f.parent == "" && f.pureMount
			}
		}
		if e.Kind == "selfDispose" {
			// K3.1: one request per activation, inside the fiber's own Apply frame.
			if !m.a3.selfDispose[actor] || states[actor] != "LOADING" || !applied[actor] || o.selfReq[actor] || e.Data["await"] != false {
				return nil, fmt.Errorf("invalid selfDispose request of %q", actor)
			}
			o.selfReq[actor] = true
			if err := programEvent(actor, "apply", causalItem{"selfDispose", ""}); err != nil {
				return nil, err
			}
		}
		if e.Kind == "error" && def.phase == "effect-cleanup" {
			if err := effectStep(def.effect, causalItem{"error", "lateEffect"}); err != nil {
				return nil, err
			}
		}
		if e.Kind == "cleanup" {
			if states[actor] != "UNLOADING" || len(m.fibers[actor].plugin.Cleanup) == 0 {
				return nil, fmt.Errorf("cleanup event of %q outside UNLOADING or without a cleanup body", actor)
			}
			cleaned[actor] = true
			seqPos[actor+"\x00cleanup"] = 0
		}
		if e.Kind == "status" {
			from, _ := e.Data["from"].(string)
			to, _ := e.Data["to"].(string)
			if states[actor] != from || !causalTransition(from, to) {
				return nil, fmt.Errorf("illegal lifecycle order for %q: %s -> %s", actor, from, to)
			}
			if from == "UNLOADING" {
				// Teardown joins pending asynchronous effect setups (C1.3).
				for _, g := range asyncGate[actor] {
					if nextCut < g.rel {
						return nil, fmt.Errorf("%q left UNLOADING before asynchronous setup gate %q was released", actor, g.label)
					}
				}
				delete(asyncGate, actor)
				if cleaned[actor] {
					if err := programDone(actor, "cleanup"); err != nil {
						return nil, err
					}
				}
				cleaned[actor] = false
				// Every registered effect of a simple body ran its whole cleanup.
				if plan := m.effectPlan[actor]; plan.complete && applied[actor] && (reachedActive[actor] || failed[actor]) {
					for i, id := range plan.effects {
						if effectRan[id] != plan.counts[i] {
							return nil, fmt.Errorf("effect %q of %q ran %d of %d cleanup events before teardown ended", id, actor, effectRan[id], plan.counts[i])
						}
					}
				}
			}
			if to == "UNLOADING" && (from == "ACTIVE" || from == "LOADING") {
				cause := disposed[actor] || mutationAnchors[actor] || failed[actor] || m.a3.newShape[actor] && o.anchors[actor] > 0
				if from == "LOADING" && o.selfReq[actor] {
					// K3.3: a request does not abort a running Apply.
					if err := programDone(actor, "apply"); err != nil {
						return nil, err
					}
				}
				bindings := make(map[causalAddress]causalBind)
				for address, bind := range bound[actor] {
					bindings[address] = bind
					if states[bind.provider] == "UNLOADING" && retiring[bind.provider] == bind.act {
						cause = true
					}
				}
				if !cause && m.a3.newShape[actor] {
					return nil, fmt.Errorf("%q left %s without a pending update request, a disposal, a failure or a retiring provider", actor, from)
				}
				if !cause {
					return nil, causalUnsupported("UNLOADING without exact bound invalidator or observed own command/error")
				}
				delete(stepPos, actor+"\x00UNLOADING")
				observation.unloadingRunning, observation.teardownProviders = true, bindings
				if from == "ACTIVE" {
					observation.unloadingActive = true
					observation.unloadingActivation = activations[actor]
					retiring[actor] = activations[actor]
				}
				for key := range writeCount {
					if len(key) > len(actor) && key[:len(actor)+1] == actor+"\x00" && key[len(key)-7:] == "cleanup" {
						delete(writeCount, key)
					}
				}
			}
			if to == "LOADING" {
				if m.a3.newShape[actor] {
					// K6.3: one reload serves every request made before it started; a
					// reload that no request asked for is a violation.
					if from == "UNLOADING" && o.anchors[actor] == 0 {
						return nil, fmt.Errorf("%q reloaded without a pending update request", actor)
					}
					o.anchors[actor], o.frameSeen[actor] = 0, false
				}
				delete(retiring, actor)
				applied[actor], failed[actor], reachedActive[actor] = false, false, false
				for _, id := range m.effectIDs(actor) {
					delete(effectRan, id)
				}
				bindings := readiness(actor)
				if bindings == nil {
					return nil, fmt.Errorf("consumer %q LOADING before every required provider ACTIVE and available", actor)
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
			if to == "FAILED" && o.anchors[actor] > 0 {
				return nil, fmt.Errorf("%q FAILED while a newer update request is pending", actor)
			}
			if to == "ACTIVE" && o.selfReq[actor] {
				return nil, fmt.Errorf("%q ACTIVE after its own disposal request", actor)
			}
			if from == "UNLOADING" && to != "LOADING" {
				delete(bound, actor)
			}
			if to == "DISPOSED" {
				for _, child := range m.children[actor] {
					if states[child] != "DISPOSED" {
						return nil, fmt.Errorf("parent %q DISPOSED before child %q", actor, child)
					}
				}
			}
			states[actor] = to
			if to == "ACTIVE" {
				reachedActive[actor] = true
				if readiness(actor) == nil {
					return nil, fmt.Errorf("consumer %q ACTIVE before every required provider ACTIVE", actor)
				}
				if !applied[actor] || failed[actor] {
					return nil, fmt.Errorf("ACTIVE without one successful Apply in current loading frame")
				}
				if refusing[actor] {
					return nil, fmt.Errorf("provider %q ACTIVE although its provision was refused", actor)
				}
				if err := programDone(actor, "apply"); err != nil {
					return nil, err
				}
				pending := make(map[string]bool)
				for _, consumer := range m.mountOrder {
					if states[consumer] == "PENDING" && !disposed[consumer] && readiness(consumer) != nil {
						for _, key := range m.fibers[consumer].needs {
							if m.isCandidate(m.address(m.fibers[consumer].context, key), actor) {
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
				o.appliedCfgs[actor] = append(o.appliedCfgs[actor], config)
				if m.a3.newShape[actor] {
					if o.hasWantApply[actor] {
						if !causalEqual(o.wantApply[actor], config) {
							return nil, fmt.Errorf("restart of %q applies a configuration other than its update hook's", actor)
						}
						o.hasWantApply[actor] = false
					}
					if len(m.fibers[actor].plugin.ConfigTransform) == 0 && o.frameSeen[actor] && !causalEqual(o.frameCfg[actor], config) {
						return nil, fmt.Errorf("Apply of %q carries a configuration other than its frame's resolved target", actor)
					}
				}
				seqPos[actor+"\x00apply"] = 0
				delete(stepPos, actor+"\x00LOADING")
				refusing[actor], refusalPending[actor] = false, false
				for key := range writeCount {
					if len(key) > len(actor) && key[:len(actor)+1] == actor+"\x00" && key[len(key)-5:] == "apply" {
						delete(writeCount, key)
					}
				}
				// C6.4: a provide on an address held by a live owner is refused.
				for _, address := range m.provides[actor] {
					if held, taken := store[address]; taken && held.owner != actor {
						if invalidated[address] {
							return nil, causalUnsupported("provide while a retiring owner retains %s/%s", address.key, address.realm)
						}
						refusing[actor], refusalPending[actor] = true, true
					}
				}
			}
		}
		if e.Kind == "error" {
			payload, ok := e.Data["error"].(map[string]any)
			if !ok || payload["message"] == nil || payload["name"] == nil {
				return nil, fmt.Errorf("error event lacks exact error payload")
			}
			if actor != causalRootActor {
				failed[actor] = e.Data["fiber"] != nil || failed[actor]
			}
			if refusalPending[actor] && e.Data["fiber"] != nil {
				refusalPending[actor] = false
				// C6.5: the refusal message is compared by class, only for the
				// proven shape: refusable body, name "Error", name and message only.
				if m.refusable[actor] && payload["name"] == "Error" && len(payload) == 2 {
					clone := make(map[string]any, len(e.Data))
					for k, v := range e.Data {
						clone[k] = v
					}
					clone["error"] = map[string]any{"name": "Error", "message": "\x00class:duplicate-provision-refused"}
					folded = clone
				}
			}
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
			if command := mutationCommand(actor); !m.a3.newShape[actor] && command != nil && command.Mode != "noSave" && causalEqual(command.Config, e.Data["config"]) {
				mutationAnchors[actor] = true
			}
		}
		data := e.Data
		if folded != nil {
			data = folded
		}
		observation.fingerprint = causalJSON(struct {
			Kind string         `json:"kind"`
			Data map[string]any `json:"data"`
		}{e.Kind, data})
		observation.actor, observation.rootLane = actor, rootLane
		observations[index] = observation
	}
	consumed := 0
	for _, n := range o.errSeen {
		consumed += n
	}
	if nextCut != len(witness.Checkpoints) || pluginCount != len(m.pluginCommands) || consumed != len(m.rootErrors) {
		return nil, fmt.Errorf("incomplete checkpoint/root command prefix")
	}
	for name, waits := range m.rootOutputWaits {
		if rootOutputSeen[name] != len(waits) {
			return nil, fmt.Errorf("root output %q produced %d of %d expected events", name, rootOutputSeen[name], len(waits))
		}
	}
	if err := o.finish(); err != nil {
		return nil, err
	}
	if err := m.rootOutcomes(trace, witness, o); err != nil {
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

// causalBodyPhase maps an event's frame to the body sequence it belongs to; effect
// cleanup and asynchronous programs are outside the exact sequences.
func causalBodyPhase(phase string) string {
	switch phase {
	case "apply", "effect-setup":
		return "apply"
	case "cleanup":
		return "cleanup"
	}
	return ""
}
