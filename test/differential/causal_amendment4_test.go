// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Amendment 4 tests (docs/design/s0-causal-comparison-amendment-4.md): a cut after
// an un-awaited disposal does not order the teardown work it started. Recorded
// pinned-oracle traces live in testdata/causal4; D97 is D22 plus an unrelated fiber,
// D96 an awaited disposal. The relaxation must accept the cut positions the Go
// candidates produce and nothing else.

func a4Load(t *testing.T, id string) a2Fix {
	t.Helper()
	script, err := os.ReadFile("testdata/causal4/" + id + ".script.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/causal4/" + id + ".json")
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

// a4Accept compares the oracle trace with a mutated copy of itself.
func a4Accept(t *testing.T, f a2Fix, mutate func(tr *Trace, cuts []CausalCut) []CausalCut) {
	t.Helper()
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	rc = mutate(r, rc)
	if err := a2Cmp(t, f.script, l, lc, r, rc); err != nil {
		t.Fatal(err)
	}
}

func a4Snapshot(tr *Trace, ci, fiber int, from *Trace, fromCi int) {
	tr.Checkpoints[ci].States[fiber] = from.Checkpoints[fromCi].States[fiber]
}

func TestCausal4FloatingCutIsAccepted(t *testing.T) {
	f := a4Load(t, "D22")
	t.Run("self", func(t *testing.T) { a4Accept(t, f, func(tr *Trace, c []CausalCut) []CausalCut { return c }) })
	t.Run("cut-after-parent-cleanup-15", func(t *testing.T) {
		a4Accept(t, f, func(tr *Trace, c []CausalCut) []CausalCut { return a3Cut(c, "joining-setup", 16) })
	})
	t.Run("cut-with-child-unloading-18", func(t *testing.T) {
		a4Accept(t, f, func(tr *Trace, c []CausalCut) []CausalCut {
			a4Snapshot(tr, 0, 1, tr, 1) // the child is UNLOADING at the cut
			return a3Cut(c, "joining-setup", 18)
		})
	})
	t.Run("cut-before-parent-unloading-13", func(t *testing.T) {
		a4Accept(t, f, func(tr *Trace, c []CausalCut) []CausalCut {
			st := tr.Checkpoints[0].States[0]
			st.State = "ACTIVE"
			st.Epoch = &Epoch{Needs: []Binding{}}
			tr.Checkpoints[0].States[0] = st
			return a3Cut(c, "joining-setup", 13)
		})
	})
	g := a4Load(t, "D97")
	t.Run("unrelated-lane-interleaves-with-the-float", func(t *testing.T) {
		a4Accept(t, g, func(tr *Trace, c []CausalCut) []CausalCut {
			// parent cleanup and its mark run before the unrelated disposal of o
			a2Move(tr, 21, 19)
			a2Move(tr, 22, 20)
			return a3Cut(c, "joining-setup", 21)
		})
	})
}

func TestCausal4FloatingCutNegatives(t *testing.T) {
	f := a4Load(t, "D22")
	g := a4Load(t, "D97")
	cases := map[string]struct {
		f      a2Fix
		want   string
		mutate func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut
	}{
		"downstream-event-missing": {f, "body of", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "mark", "label", "parent-cleanup"))
		}},
		"downstream-event-duplicated": {f, "breaks program order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "mark", "label", "parent-cleanup"))
		}},
		"downstream-events-reordered-in-a-lane": {f, "outside the frame of every possible owner", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "mark", "label", "parent-cleanup"), a2At(t, tr, "cleanup", "fiber", "parent"))
			return c
		}},
		"fiber-observed-in-a-state-its-events-never-produce": {f, "ordered fiber/status prefix", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Checkpoints[0].States[1].State = "FAILED"
			return c
		}},
		"unrelated-event-moved-across-the-cut": {g, "root mount/disposal prefix", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Checkpoints[0].States[0].FiberUID = nil // o, whose disposal command follows the snapshot
			return a3Cut(c, "joining-setup", a2At(t, tr, "plugin", "fiber", "o", "disposed", true)+1)
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a3Reject(t, c.f, c.want, func(tr *Trace, cuts []CausalCut) []CausalCut { return c.mutate(t, tr, cuts) })
		})
	}
	// Completeness at the ordered cut: the parent cleanup and its mark fall after the
	// barrier cut. Each lane order stays valid, so only the window comparison can
	// reject it; one side only (two identical sides are equal by construction).
	t.Run("completeness-at-the-ordered-cut-violated", func(t *testing.T) {
		l, lc := f.fresh(t)
		r, rc := f.fresh(t)
		i := a2At(t, r, "cleanup", "fiber", "parent")
		cleanup, mark := r.Events[i], r.Events[i+1]
		r.Events = append(r.Events[:i], r.Events[i+2:]...)
		gate := a2At(t, r, "gate", "label", "effect-cleanup")
		r.Events = append(r.Events[:gate+1], append([]Event{cleanup, mark}, r.Events[gate+1:]...)...)
		rc = a3Cut(rc, "joining-cleanup", gate+1)
		err := a2Cmp(t, f.script, l, lc, r, rc)
		if err == nil || !strings.Contains(err.Error(), "event window") {
			t.Fatalf("incomplete work at the ordered cut accepted: %v", err)
		}
	})
	t.Run("downstream-event-floats-past-the-barrier", func(t *testing.T) {
		a3Reject(t, f, "outside the frame of every possible owner", func(tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "status", "fiber", "child", "from", "ACTIVE", "to", "UNLOADING"), a2At(t, tr, "gate", "label", "effect-cleanup"))
			return c
		})
	})
}

// The relaxation applies only to an un-awaited disposal: the same cut shift after an
// awaited disposal is rejected by the strict comparison.
func TestCausal4AwaitedDisposalCutStaysStrict(t *testing.T) {
	f := a4Load(t, "D96")
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	st := r.Checkpoints[0].States
	st[0].State, st[0].Epoch = "ACTIVE", &Epoch{Needs: []Binding{}}
	st[1].State, st[1].Epoch, st[1].FiberUID = "ACTIVE", &Epoch{Needs: []Binding{}}, float64(2)
	rc = a3Cut(rc, "after", a2At(t, r, "plugin", "fiber", "parent", "disposed", true)+1)
	err := a2Cmp(t, f.script, l, lc, r, rc)
	if err == nil || strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), "record count mismatch") {
		t.Fatalf("awaited disposal floated across a cut: %v", err)
	}
	// both sides shifted identically are equal events and cuts: accepted only
	// because nothing differs; the model itself has no window for it.
	m, mErr := compileCausalModel(f.script)
	if mErr != nil || len(m.floats) != 0 {
		t.Fatalf("an awaited disposal must not open a window: %v %d", mErr, len(m.floats))
	}
}

func TestCausal4WindowsAreDecidedFromTheScript(t *testing.T) {
	m, err := compileCausalModel(a4Load(t, "D22").script)
	if err != nil || len(m.floats) != 1 {
		t.Fatalf("D22 must open one window: %v %d", err, len(m.floats))
	}
	w := m.floats[0]
	if w.first != 0 || w.barrier != 1 || !w.fl["parent"] || !w.fl["child"] || len(w.fl) != 2 {
		t.Fatalf("unexpected window %+v", w)
	}
	// An ordering command before the first cut leaves nothing floating.
	var doc map[string]any
	if err := json.Unmarshal(a4Load(t, "D22").script, &doc); err != nil {
		t.Fatal(err)
	}
	steps := doc["steps"].([]any)
	var out []any
	for _, s := range steps {
		out = append(out, s)
		if s.(map[string]any)["op"] == "dispose" {
			out = append(out, map[string]any{"op": "waitMark", "label": "effect-setup"})
		}
	}
	doc["steps"] = out
	raw, _ := json.Marshal(doc)
	if m, err := compileCausalModel(raw); err != nil || len(m.floats) != 0 {
		t.Fatalf("a cut ordered after the work must not float: %v %d", err, len(m.floats))
	}
}

// floatStates must refuse a state the expected lane never holds in the window.
func TestCausal4FloatStatesRefuseForeignStates(t *testing.T) {
	events := []Event{
		{Kind: "plugin", Data: a3Data("fiber", "p", "disposed", true)},
		{Kind: "status", Data: a3Data("fiber", "p", "from", "ACTIVE", "to", "UNLOADING")},
		{Kind: "status", Data: a3Data("fiber", "p", "from", "UNLOADING", "to", "DISPOSED")},
	}
	f := &causalFloat{fl: map[string]bool{"p": true}}
	snap := func(state string) []FiberSnapshot { return []FiberSnapshot{{Fiber: "p", State: state}} }
	if err := floatStates(f, snap("UNLOADING"), snap("UNLOADING"), events, 0, 2); err != nil {
		t.Fatal(err)
	}
	if err := floatStates(f, snap("UNLOADING"), snap("DISPOSED"), events, 0, 2); err == nil {
		t.Fatal("a state the expected lane reaches only after the window was accepted")
	}
	if err := floatStates(f, snap("UNLOADING"), snap("FAILED"), events, 0, 3); err == nil {
		t.Fatal("a state off the expected lane was accepted")
	}
}
