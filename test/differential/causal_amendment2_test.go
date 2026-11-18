// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// Amendment 2 unit tests. Positive cases run the comparator on recorded oracle
// traces (testdata/causal2, fresh pinned-oracle executions of the corpus) and on
// hand-built minimal scripts. Negative cases mutate those traces into real
// semantic violations. Most are applied to BOTH sides identically: the earlier
// review of this comparator found same-invalid-stream false greens, so a rule is
// only credited when the identical corruption on both sides is still rejected.

type a2Fix struct {
	script []byte
	trace  *Trace
	cuts   []CausalCut
}

func a2Load(t *testing.T, id, scriptPath string) a2Fix {
	t.Helper()
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/causal2/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cuts  []CausalCut `json:"cuts"`
		Trace Trace       `json:"trace"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return a2Fix{script: script, trace: &doc.Trace, cuts: doc.Cuts}
}

func (f a2Fix) fresh(t *testing.T) (*Trace, []CausalCut) {
	t.Helper()
	raw, err := json.Marshal(f.trace)
	if err != nil {
		t.Fatal(err)
	}
	var tr Trace
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatal(err)
	}
	return &tr, append([]CausalCut(nil), f.cuts...)
}

func a2Cmp(t *testing.T, script []byte, l *Trace, lc []CausalCut, r *Trace, rc []CausalCut) error {
	t.Helper()
	lb, rb := causalUnitJSON(t, l), causalUnitJSON(t, r)
	return CompareCausal(script, lb, rb, causalUnitWitness(script, lb, lc...), causalUnitWitness(script, rb, rc...))
}

// a2At returns the index of the first event of kind whose data has all key/value pairs.
func a2At(t *testing.T, tr *Trace, kind string, kv ...any) int {
	t.Helper()
	for i, e := range tr.Events {
		if e.Kind != kind {
			continue
		}
		ok := true
		for j := 0; j+1 < len(kv); j += 2 {
			if !causalEqual(e.Data[kv[j].(string)], kv[j+1]) {
				ok = false
			}
		}
		if ok {
			return i
		}
	}
	t.Fatalf("no %s event %v", kind, kv)
	return -1
}

func a2Move(tr *Trace, from, to int) { causalUnitMoveEvent(tr, from, to) }

func a2Drop(tr *Trace, cuts []CausalCut, i int) []CausalCut {
	tr.Events = append(tr.Events[:i], tr.Events[i+1:]...)
	out := append([]CausalCut(nil), cuts...)
	for k := range out {
		if out[k].AfterEvent > i {
			out[k].AfterEvent--
		}
	}
	return out
}

func a2Dup(tr *Trace, cuts []CausalCut, i int) []CausalCut {
	e := tr.Events[i]
	data := map[string]any{}
	for k, v := range e.Data {
		data[k] = v
	}
	tr.Events = append(tr.Events[:i+1], append([]Event{{Kind: e.Kind, Data: data}}, tr.Events[i+1:]...)...)
	out := append([]CausalCut(nil), cuts...)
	for k := range out {
		if out[k].AfterEvent > i {
			out[k].AfterEvent++
		}
	}
	return out
}

func a2Path(id string) string { return "corpus/" + id + ".json" }

var a2Fixtures = []struct{ id, path string }{
	{"D03", a2Path("D03")}, {"D04", a2Path("D04")}, {"D06", a2Path("D06")}, {"D07", a2Path("D07")},
	{"D08", a2Path("D08")}, {"D09", a2Path("D09")}, {"D11", a2Path("D11")}, {"D13", "corpus/revision-3/D13.json"},
	{"D15", a2Path("D15")}, {"D18", a2Path("D18")}, {"D21", a2Path("D21")}, {"D22", "corpus/revision-4/D22.json"},
	{"D23", a2Path("D23")},
}

// The model must accept every real oracle execution of the newly supported
// scenarios, compared with itself. This is not parity evidence, it proves that the
// rules are satisfiable by the behaviour they describe.
func TestCausal2OracleTracesSatisfyTheirOwnRules(t *testing.T) {
	for _, fx := range a2Fixtures {
		t.Run(fx.id, func(t *testing.T) {
			f := a2Load(t, fx.id, fx.path)
			l, lc := f.fresh(t)
			r, rc := f.fresh(t)
			if err := a2Cmp(t, f.script, l, lc, r, rc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// a2Reject applies mutate to the right side only and, separately, to both sides,
// and requires a rejection each time (never ErrCausalUnsupported: a violation must
// be proved, not merely unproved).
func a2Reject(t *testing.T, f a2Fix, mutate func(tr *Trace, cuts []CausalCut) []CausalCut) {
	t.Helper()
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	rc = mutate(r, rc)
	err := a2Cmp(t, f.script, l, lc, r, rc)
	if err == nil || errors.Is(err, ErrCausalUnsupported) {
		t.Fatalf("one-sided violation not rejected: %v", err)
	}
	t.Logf("one-sided: %v", err)
	l2, lc2 := f.fresh(t)
	r2, rc2 := f.fresh(t)
	lc2, rc2 = mutate(l2, lc2), mutate(r2, rc2)
	err = a2Cmp(t, f.script, l2, lc2, r2, rc2)
	if err == nil || errors.Is(err, ErrCausalUnsupported) {
		t.Fatalf("identical violation on both sides accepted: %v", err)
	}
	t.Logf("both sides: %v", err)
}

func a2Case(t *testing.T, id, path string, mutate func(t *testing.T, tr *Trace, cuts []CausalCut) []CausalCut) {
	t.Helper()
	a2Reject(t, a2Load(t, id, path), func(tr *Trace, cuts []CausalCut) []CausalCut { return mutate(t, tr, cuts) })
}

// C1 gate / mark / waitMark / release
func TestCausal2GateMarkNegatives(t *testing.T) {
	cases := map[string]struct {
		id, path string
		mutate   func(t *testing.T, tr *Trace, cuts []CausalCut) []CausalCut
	}{
		// D21: consumer gated in cleanup. Its continuation (the during-teardown
		// output) is moved in front of the gate arrival.
		"continuation-before-gate": {"D21", a2Path("D21"), func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "output", "to", "during-teardown"), a2At(t, tr, "gate", "label", "consumer-unload"))
			return c
		}},
		// D21: the whole continuation sits before the cut taken while parked, so it
		// ran before release (the cut proves release was not issued yet).
		"release-overtaken": {"D21", a2Path("D21"), func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			for i := range c {
				if c[i].Label == "retained" {
					c[i].AfterEvent += 3 // output, mark, status c UNLOADING->PENDING
				}
			}
			return c
		}},
		// D11: gate arrival lost (missing event on both sides).
		"gate-arrival-missing": {"D11", a2Path("D11"), func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "gate", "label", "load"))
		}},
		"gate-arrival-duplicated": {"D11", a2Path("D11"), func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "gate", "label", "load"))
		}},
		// D13: the root disposes d before the awaited active-apply mark arrived.
		"root-overtakes-waitMark": {"D13", "corpus/revision-3/D13.json", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "plugin", "fiber", "d", "disposed", true), a2At(t, tr, "mark", "label", "active-apply"))
			return c
		}},
		// D13: wrong owner, the mark of a is attributed to a fiber outside its frame
		// by moving it before that fiber's apply.
		"mark-outside-frame": {"D13", "corpus/revision-3/D13.json", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "mark", "label", "active-apply"), a2At(t, tr, "apply", "fiber", "a"))
			return c
		}},
		// D23: swapped same-lane marks (second, first) in a body without a cleanup
		// phase witness for either order; the oracle order is retained exactly.
		"same-lane-marks-swapped": {"D23", a2Path("D23"), func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "mark", "label", "once"), a2At(t, tr, "mark", "label", "second"))
			return c
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { a2Case(t, c.id, c.path, c.mutate) })
	}
}
