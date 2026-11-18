// SPDX-License-Identifier: Apache-2.0

package differential

// floatStart returns the window whose first floating cut ends segment s.
func (m *causalModel) floatStart(s int) *causalFloat {
	for _, f := range m.floats {
		if f.first == s {
			return f
		}
	}
	return nil
}

// segmentRange is the event index range [from,to) of segment s of a trace (the
// segment after the last checkpoint is the tail).
func segmentRange(w CausalWitness, n, s int) (int, int) {
	from, to := 0, n
	if s > 0 {
		from = w.Checkpoints[s-1].AfterEvent
	}
	if s < len(w.Checkpoints) {
		to = w.Checkpoints[s].AfterEvent
	}
	return from, to
}
