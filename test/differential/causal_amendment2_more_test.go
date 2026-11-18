// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"errors"
	"strings"
	"testing"
)

type a2Mut = func(t *testing.T, tr *Trace, cuts []CausalCut) []CausalCut

func a2Table(t *testing.T, id, path string, cases map[string]a2Mut) {
	t.Helper()
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) { a2Case(t, id, path, mutate) })
	}
}

// a2OneSided requires a rejection when only the right trace is corrupted. It is
// used for content changes: a value is compared between the traces, never
// recomputed, so an identical value change on both sides is not detectable.
func a2OneSided(t *testing.T, id, path string, mutate func(*Trace)) {
	t.Helper()
	f := a2Load(t, id, path)
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	mutate(r)
	if err := a2Cmp(t, f.script, l, lc, r, rc); err == nil || errors.Is(err, ErrCausalUnsupported) {
		t.Fatalf("one-sided content change not rejected: %v", err)
	}
}

// C1 positive: independent actors' blocks commute, nothing else.
func TestCausal2IndependentBlocksCommute(t *testing.T) {
	f := a2Load(t, "D13", "corpus/revision-3/D13.json")
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	// [config a, apply a, mark active-apply] and [config f, apply f, error f,
	// status f UNLOADING] belong to different fibers and different labels.
	block := append([]Event(nil), r.Events[14:17]...)
	other := append([]Event(nil), r.Events[17:21]...)
	copy(r.Events[14:18], other)
	copy(r.Events[18:21], block)
	if err := a2Cmp(t, f.script, l, lc, r, rc); err != nil {
		t.Fatalf("independent actors' blocks must commute: %v", err)
	}
	// The same swap is rejected if it moves a block across the waitMark barrier:
	// the root disposal of d may not precede the awaited active-apply arrival.
	bad, bc := f.fresh(t)
	a2Move(bad, a2At(t, bad, "plugin", "fiber", "d", "disposed", true), a2At(t, bad, "mark", "label", "active-apply"))
	if err := a2Cmp(t, f.script, l, lc, bad, bc); err == nil {
		t.Fatal("root disposal crossed its awaited mark")
	}
}

// C2 child fibers
func TestCausal2ChildNegatives(t *testing.T) {
	a2Table(t, "D22", "corpus/revision-4/D22.json", map[string]a2Mut{
		"child-mounted-before-parent-apply": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "plugin", "fiber", "child", "disposed", false), a2At(t, tr, "apply", "fiber", "parent"))
			return c
		},
		"child-disposed-before-parent": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "plugin", "fiber", "child", "disposed", true), a2At(t, tr, "plugin", "fiber", "parent", "disposed", true))
			return c
		},
		"parent-disposed-before-child": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "status", "fiber", "parent", "to", "DISPOSED"), a2At(t, tr, "status", "fiber", "child", "to", "DISPOSED"))
			return c
		},
		"child-mount-missing": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "plugin", "fiber", "child", "disposed", false))
		},
		"child-mount-duplicated": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "plugin", "fiber", "child", "disposed", false))
		},
		// The effect cleanup gate may not arrive before its asynchronous setup gate is
		// released: the cut taken while parked proves the release was not issued.
		"effect-cleanup-before-setup-release": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			for i := range c {
				if c[i].Label == "joining-setup" {
					c[i].AfterEvent = a2At(t, tr, "gate", "label", "effect-cleanup") + 1
				}
			}
			return c
		},
	})
	a2Table(t, "D06", a2Path("D06"), map[string]a2Mut{
		"children-created-out-of-program-order": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			i, j := a2At(t, tr, "plugin", "fiber", "child", "disposed", false), a2At(t, tr, "plugin", "fiber", "private-child", "disposed", false)
			tr.Events[i], tr.Events[j] = tr.Events[j], tr.Events[i]
			return c
		},
	})
}

// C3 effects
func TestCausal2EffectNegatives(t *testing.T) {
	a2Table(t, "D18", a2Path("D18"), map[string]a2Mut{
		"cleanup-never-ran": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "mark", "label", "unwound"))
		},
		"cleanup-ran-twice": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "mark", "label", "unwound"))
		},
		"cleanup-before-unwind": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "mark", "label", "unwound"), a2At(t, tr, "error", "fiber", "c"))
			return c
		},
	})
	a2Table(t, "D23", a2Path("D23"), map[string]a2Mut{
		"single-shot-withdraw-before-partial-unwind": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "mark", "label", "once"), a2At(t, tr, "mark", "label", "second"))
			return c
		},
		"caught-error-after-apply-returned": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "error", "op", "effect"), a2At(t, tr, "status", "fiber", "f", "to", "ACTIVE"))
			return c
		},
	})
}

// C4 modes, intercepts, availability
func TestCausal2ModeNegatives(t *testing.T) {
	a2Table(t, "D09", a2Path("D09"), map[string]a2Mut{
		"consumer-loads-while-unavailable": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "status", "fiber", "cb", "to", "LOADING"), a2At(t, tr, "output", "to", "availability", "value", map[string]any{"A": true, "B": true}))
			return c
		},
		"notification-without-command": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "service", "key", "svc")+0)
		},
	})
	for _, id := range []string{"D07", "D08"} {
		t.Run(id+"-merged-config-value", func(t *testing.T) {
			a2OneSided(t, id, a2Path(id), func(tr *Trace) {
				i := a2At(t, tr, "output", "to", map[string]string{"D07": "view", "D08": "merged"}[id])
				tr.Events[i].Data["value"] = map[string]any{"winner": "outer"}
			})
		})
	}
}

// C5 shared output cell
func TestCausal2SharedOutputNegatives(t *testing.T) {
	a2Table(t, "D03", a2Path("D03"), map[string]a2Mut{
		"write-by-unready-fibers": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "output", "to", "joined"), a2At(t, tr, "status", "fiber", "c", "to", "LOADING"))
			return c
		},
		"write-duplicated": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "output", "to", "joined"))
		},
		"write-missing": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "output", "to", "joined"))
		},
		"write-after-apply-finished": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "output", "to", "joined"), a2At(t, tr, "status", "fiber", "c", "to", "ACTIVE")+0)
			return c
		},
	})
}

// C6 providers
func TestCausal2ProviderNegatives(t *testing.T) {
	a2Table(t, "D15", a2Path("D15"), map[string]a2Mut{
		"replacement-published-over-live-owner": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			// the new provision is published before the old one is deleted
			a2Move(tr, a2At(t, tr, "service", "value", "new"), a2At(t, tr, "service", "value", map[string]any{"$undefined": true}))
			return c
		},
		"consumer-bound-before-new-provider-active": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			i := a2At(t, tr, "status", "fiber", "p2", "to", "ACTIVE")
			a2Move(tr, i+1, i)
			return c
		},
		"old-provider-deleted-before-consumer-unloaded": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "service", "value", map[string]any{"$undefined": true}), a2At(t, tr, "status", "fiber", "c", "from", "ACTIVE", "to", "UNLOADING"))
			return c
		},
	})
	a2Table(t, "D04", a2Path("D04"), map[string]a2Mut{
		"refused-provider-publishes": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			i := a2At(t, tr, "error", "fiber", "duplicate")
			tr.Events[i] = Event{Kind: "service", Data: map[string]any{"key": "svc", "realm": "root:svc", "value": "second"}}
			return c
		},
	})
	// Error text of the proven refusal is compared by class: reworded text on one
	// side is accepted, anything else about the event is not.
	f := a2Load(t, "D04", a2Path("D04"))
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	i := a2At(t, r, "error", "fiber", "duplicate")
	r.Events[i].Data["error"] = map[string]any{"name": "Error", "message": "duplicate provision is already owned"}
	if err := a2Cmp(t, f.script, l, lc, r, rc); err != nil {
		t.Fatalf("refusal wording must be compared by class: %v", err)
	}
	for name, change := range map[string]func(*Event){
		"error-name":  func(e *Event) { e.Data["error"] = map[string]any{"name": "TypeError", "message": "x"} },
		"extra-field": func(e *Event) { e.Data["error"] = map[string]any{"name": "Error", "message": "x", "code": "E"} },
		"other-fiber": func(e *Event) { e.Data["fiber"] = "p" },
	} {
		t.Run("class-fold-"+name, func(t *testing.T) {
			bad, bc := f.fresh(t)
			change(&bad.Events[a2At(t, bad, "error", "fiber", "duplicate")])
			if err := a2Cmp(t, f.script, l, lc, bad, bc); err == nil {
				t.Fatal("class fold hid more than the message wording")
			}
		})
	}
	t.Run("class-fold-needs-refusable-body", func(t *testing.T) {
		if !causalRefusableBody([]Action{{Op: "provide"}, {Op: "output"}}) || causalRefusableBody([]Action{{Op: "provide"}, {Op: "fail"}}) || causalRefusableBody([]Action{{Op: "read"}}) {
			t.Fatal("refusal fold must apply to provide/output/mark bodies only")
		}
	})
}

// Meta: controls the amendment accepts only on the operations it has rules for.
func TestCausal2MetadataStaysScoped(t *testing.T) {
	for name, script := range map[string]string{
		"task-on-update":        strings.Replace(causalUnitScript, `{"op":"settle","label":"ready"}`, `{"op":"update","fiber":"p","task":"u"},{"op":"settle","label":"ready"}`, 1),
		"await-false-on-read":   strings.Replace(causalUnitScript, `"op":"read","key":"a","to":"left"`, `"op":"read","key":"a","to":"left","await":false`, 1),
		"catch-on-provide":      strings.Replace(causalUnitScript, `"op":"provide","key":"a"`, `"op":"provide","catch":true,"key":"a"`, 1),
		"count-on-gate":         strings.Replace(causalUnitScript, `"op":"read","key":"a","to":"left"`, `"op":"gate","label":"g","count":2`, 1),
		"foreign-field-on-mark": strings.Replace(causalUnitScript, `"op":"read","key":"a","to":"left"`, `"op":"mark","label":"g","key":"zzz"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := compileCausalModel([]byte(script)); !errors.Is(err, ErrCausalUnsupported) {
				t.Fatalf("want ErrCausalUnsupported, got %v", err)
			}
		})
	}
}

// A failure (or refusal) whose error event is gone is not a match either way
// (unproved or rejected): the comparator never accepts a FAILED fiber without its
// cause, on one side or on both.
func TestCausal2FailureWithoutCauseIsNeverAMatch(t *testing.T) {
	for _, c := range []struct{ id, fiber string }{{"D18", "c"}, {"D04", "duplicate"}} {
		t.Run(c.id, func(t *testing.T) {
			f := a2Load(t, c.id, a2Path(c.id))
			l, lc := f.fresh(t)
			r, rc := f.fresh(t)
			rc = a2Drop(r, rc, a2At(t, r, "error", "fiber", c.fiber))
			if err := a2Cmp(t, f.script, l, lc, r, rc); err == nil {
				t.Fatal("accepted a failure without its error")
			}
			l2, lc2 := f.fresh(t)
			lc2 = a2Drop(l2, lc2, a2At(t, l2, "error", "fiber", c.fiber))
			r2, rc2 := f.fresh(t)
			rc2 = a2Drop(r2, rc2, a2At(t, r2, "error", "fiber", c.fiber))
			if err := a2Cmp(t, f.script, l2, lc2, r2, rc2); err == nil {
				t.Fatal("accepted identical failure-without-cause streams")
			}
		})
	}
}

// Root-command obligations (addendum A1, A2, A6).
func TestCausal2RootCommandNegatives(t *testing.T) {
	a2Table(t, "D18", a2Path("D18"), map[string]a2Mut{
		"mount-published-before-awaited-disposal-finished": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "plugin", "fiber", "p2", "disposed", false), a2At(t, tr, "status", "fiber", "p1", "to", "DISPOSED"))
			return c
		},
	})
	a2Table(t, "D13", "corpus/revision-3/D13.json", map[string]a2Mut{
		"awaitFiber-rejects-before-target-failed": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "error", "op", "awaitFiber"), a2At(t, tr, "error", "fiber", "f")+1)
			return c
		},
		"awaitFiber-rejection-missing": func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "error", "op", "awaitFiber"))
		},
	})
}
