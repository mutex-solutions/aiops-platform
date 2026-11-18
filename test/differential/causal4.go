// SPDX-License-Identifier: Apache-2.0

package differential

import "fmt"

// Amendment 4 (docs/design/s0-causal-comparison-amendment-4.md): a cut after an
// un-awaited root disposal does not order the teardown work the disposal started.

// causalFloat is one window of that relaxation: the segments first..barrier
// (barrier == len(cuts) means the tail), the cuts first..barrier-1 float, and fl
// is the target with its structural descendants.
type causalFloat struct {
	cmd            int
	fiber          string
	fl             map[string]bool
	first, barrier int
}

// computeFloats finds the windows of rule L1 from the script alone. A disposal whose
// scope condition fails, or whose window would overlap an earlier one, gets no
// window and keeps the strict comparison.
func (m *causalModel) computeFloats() {
	steps := m.script.Steps
	cut, pc, until := 0, 0, -1
	for i, a := range steps {
		switch a.Op {
		case "settle", "snapshot":
			cut++
		case "mount":
			pc++
		case "dispose":
			pc++
			if a.Await == nil || *a.Await || i < until {
				continue
			}
			f := &causalFloat{cmd: pc, fiber: a.Fiber, fl: map[string]bool{a.Fiber: true}, first: cut}
			for _, d := range m.descendants(a.Fiber) {
				f.fl[d] = true
			}
			if !m.floatScope(f, i) {
				continue
			}
			ordered, c := false, cut
			f.barrier = len(m.cuts)
			end := len(steps)
		scan:
			for j := i + 1; j < len(steps); j++ {
				switch s := steps[j]; s.Op {
				case "awaitFiber":
					ordered = ordered || f.fl[s.Fiber]
				case "waitMark":
					defs := m.labels[s.Label]
					all := len(defs) != 0
					for _, d := range defs {
						all = all && f.fl[d.actor]
					}
					ordered = ordered || all
				case "snapshot":
					if ordered {
						f.barrier, end = c, j
						break scan
					}
					c++
				case "settle":
					f.barrier, end = c, j
					break scan
				}
			}
			until = end + 1
			if f.barrier > f.first {
				m.floats = append(m.floats, f)
			}
		}
	}
}

// floatScope is the scope condition of L1: nothing of the float set provides an
// address, writes an output cell or is the target of another root command.
func (m *causalModel) floatScope(f *causalFloat, at int) bool {
	for id := range f.fl {
		if len(m.provides[id]) != 0 {
			return false
		}
	}
	for _, owners := range m.outputs {
		for _, o := range owners {
			if f.fl[o] {
				return false
			}
		}
	}
	for j, s := range m.script.Steps {
		if s.Fiber == "" || !f.fl[s.Fiber] {
			continue
		}
		switch s.Op {
		case "mount", "awaitFiber":
		case "dispose":
			if j != at {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// floatFlags marks, per trace, the floating events of every window: events after the
// disposal's own event, inside the window, whose actor is in the float set.
func (m *causalModel) floatFlags(obs []causalObservation, w CausalWitness) ([]bool, []int, error) {
	flags := make([]bool, len(obs))
	ud := make([]int, len(m.floats))
	for k, f := range m.floats {
		at := -1
		for i, o := range obs {
			if o.rootCmd == f.cmd {
				at = i
				break
			}
		}
		if at < 0 {
			return nil, nil, fmt.Errorf("own event of un-awaited disposal %q not found", f.fiber)
		}
		ud[k] = at
		_, end := m.floatBounds(f, w, len(obs))
		for i := at + 1; i < end; i++ {
			flags[i] = f.fl[obs[i].actor]
		}
	}
	return flags, ud, nil
}

// floatBounds returns the event index range [start,end) of a window in a trace.
func (m *causalModel) floatBounds(f *causalFloat, w CausalWitness, n int) (int, int) {
	start, end := 0, n
	if f.first > 0 {
		start = w.Checkpoints[f.first-1].AfterEvent
	}
	if f.barrier < len(w.Checkpoints) {
		end = w.Checkpoints[f.barrier].AfterEvent
	}
	return start, end
}

func causalPinned(obs []causalObservation, flags []bool, from, to int) []causalObservation {
	var out []causalObservation
	for i := from; i < to; i++ {
		if !flags[i] {
			out = append(out, obs[i])
		}
	}
	return out
}

// floatingCut returns the window whose floating cut ci is, if any.
func (m *causalModel) floatingCut(ci int) (int, *causalFloat) {
	for k, f := range m.floats {
		if ci >= f.first && ci < f.barrier {
			return k, f
		}
	}
	return -1, nil
}

// floatStates compares the states of a floating cut (rule L1.3): a fiber of the float
// set may be in any state its expected lane holds between the disposal and the end
// of the window; everything else is compared exactly.
func floatStates(f *causalFloat, expected, actual []FiberSnapshot, events []Event, ud, end int) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("state count mismatch")
	}
	for i := range expected {
		e, a := expected[i], actual[i]
		if !f.fl[e.Fiber] {
			if !causalEqual(e, a) {
				return fmt.Errorf("state of %q outside the float set differs", e.Fiber)
			}
			continue
		}
		allowed := map[string]bool{}
		last := ""
		for j, ev := range events[:end] {
			if ev.Kind == "status" && ev.Data["fiber"] == e.Fiber {
				to, _ := ev.Data["to"].(string)
				if j <= ud {
					last = to
				} else {
					allowed[to] = true
				}
			}
		}
		if last != "" {
			allowed[last] = true
		}
		if e.Fiber != a.Fiber || !allowed[a.State] {
			return fmt.Errorf("fiber %q observed in state %q, not on its expected lane between the disposal and the barrier", e.Fiber, a.State)
		}
		if e.State == a.State {
			if !causalEqual(e, a) {
				return fmt.Errorf("state of %q differs", e.Fiber)
			}
			continue
		}
		e.State, a.State, e.FiberUID, a.FiberUID, e.Epoch, a.Epoch = "", "", nil, nil, nil, nil
		if !causalEqual(e, a) {
			return fmt.Errorf("revision or config of %q differs", e.Fiber)
		}
	}
	return nil
}
