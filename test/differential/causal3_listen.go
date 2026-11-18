// SPDX-License-Identifier: Apache-2.0

package differential

import "strings"

// causalLateEffects counts the top-level lateEffect operations of a program;
// each produces two events (error and output) instead of one.
func causalLateEffects(actions []Action) int {
	n := 0
	for _, a := range actions {
		if a.Op == "lateEffect" && a.To != "" {
			n++
		}
	}
	return n
}

// updateListener registers the internal/update listener installed by a fiber's
// Apply (K7).
func (m *causalModel) updateListener(actor string, a Action, frame causalFrame) error {
	f := m.fibers[actor]
	if !frame.top || frame.phase != "apply" || a.Key != "internal/update" || a.Symbol || a.Label == "" || m.a3.listenLabels[a.Label] || f.parent != "" ||
		m.a3.updListener[actor] != nil || f.plugin.Update != "" || len(f.plugin.ConfigTransform) != 0 {
		return causalUnsupported("update listener form")
	}
	var check func([]Action) error
	check = func(actions []Action) error {
		for _, step := range actions {
			if !strings.Contains(" when gate mark output fail return ", " "+step.Op+" ") {
				return causalUnsupported("update listener operation %q", step.Op)
			}
			if err := hookFieldsOK(step); err != nil {
				return err
			}
			if step.Op != "when" && len(step.Program) != 0 {
				return causalUnsupported("nested program in update listener")
			}
			if err := check(step.Program); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(a.Program); err != nil {
		return err
	}
	m.a3.listenLabels[a.Label] = true
	m.a3.updListener[actor] = &causalUpdateListener{label: a.Label, program: a.Program}
	return nil
}
