// SPDX-License-Identifier: Apache-2.0

package differential

import "fmt"

// identities anchors IDs by static ownership and per-address lifetime occurrence,
// not global provision creation order. Every opaque value has exactly one owner
// in its namespace across ALL checkpoints, including retired occurrences.
func (m *causalModel) identities(trace *Trace, witness CausalWitness) error {
	fiberDomain, fiberOwner := make(map[string]string), make(map[string]string)
	provisionDomain := make(map[string]string)
	occurrences := make(map[causalAddress]causalBind)
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
			if !m.isCandidate(address, p.Provider) || m.fibers[p.Provider] == nil && p.Provider != rootProviderName {
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
				if p.Provider == rootProviderName {
					activation = 1 // K2.4: the root provider has one activation for the whole script
				}
				if activation == 0 || occurrences[address] == (causalBind{p.Provider, activation}) {
					return fmt.Errorf("provision identity changed without a distinct provider activation")
				}
				occurrences[address] = causalBind{p.Provider, activation}
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
					if !present || p.Key != address.key || p.Realm != address.realm || !m.isCandidate(address, p.Provider) || !p.Active {
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
func (m *causalModel) rootOutcomes(trace *Trace, witness CausalWitness, o *a3obs) error {
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
				if m.a3.newShape[target] {
					// K5, K6: bundles, requests and revisions of a new-shape fiber are
					// validated event by event while observing (a3obs).
					rawConfigs[target] = command.Config
					continue
				}
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
			if m.a3.newShape[state.Fiber] {
				// K6.5: one revision per restart request, however many were coalesced.
				if state.Revision != o.requestsAtCut[ci][state.Fiber] || state.Epoch != nil && state.Epoch.Revision != state.Revision {
					return fmt.Errorf("checkpoint %q revision of %q contradicts its restart requests", cp.Label, state.Fiber)
				}
			} else if state.Revision != revisions[state.Fiber] || state.Epoch != nil && state.Epoch.Revision != state.Revision {
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
