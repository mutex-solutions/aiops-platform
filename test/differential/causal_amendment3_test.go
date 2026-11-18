// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// Amendment 3 unit tests (docs/design/s0-causal-comparison-amendment-3.md). They
// follow the structure of causal_amendment2_test.go: recorded pinned-oracle traces
// (testdata/causal3) satisfy their own rules, and mutated traces holding a real
// violation are rejected one-sidedly and with the identical corruption on both
// sides. A rejection must be proved, never ErrCausalUnsupported, and every case
// names the text of the rule that must reject it so a case cannot pass for an
// unrelated reason.

func a3Load(t *testing.T, id, scriptPath string) a2Fix {
	t.Helper()
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/causal3/" + id + ".json")
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

var a3Targets = []string{"D01", "D17", "D20", "D24", "D25", "D28", "D29", "D30", "D31", "D32", "D33", "D34", "D35", "D36"}

// The model must accept every real oracle execution of the newly supported
// scenarios, compared with itself. This proves the rules are satisfiable by the
// behaviour they describe; it is not parity evidence.
func TestCausal3OracleTracesSatisfyTheirOwnRules(t *testing.T) {
	for _, id := range a3Targets {
		t.Run(id, func(t *testing.T) {
			f := a3Load(t, id, a2Path(id))
			l, lc := f.fresh(t)
			r, rc := f.fresh(t)
			if err := a2Cmp(t, f.script, l, lc, r, rc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type a3Mut struct {
	want   string
	mutate func(t *testing.T, tr *Trace, cuts []CausalCut) []CausalCut
}

// a3Insert places a new event at index at.
func a3Insert(tr *Trace, cuts []CausalCut, at int, kind string, data map[string]any) []CausalCut {
	tr.Events = append(tr.Events[:at], append([]Event{{Kind: kind, Data: data}}, tr.Events[at:]...)...)
	out := append([]CausalCut(nil), cuts...)
	for k := range out {
		if out[k].AfterEvent > at {
			out[k].AfterEvent++
		}
	}
	return out
}

func a3Cut(cuts []CausalCut, label string, after int) []CausalCut {
	out := append([]CausalCut(nil), cuts...)
	for i := range out {
		if out[i].Label == label {
			out[i].AfterEvent = after
		}
	}
	return out
}

// a3Reject requires a proved rejection whose text contains want, with the
// corruption applied to the right trace only and to both.
func a3Reject(t *testing.T, f a2Fix, want string, mutate func(tr *Trace, cuts []CausalCut) []CausalCut) {
	t.Helper()
	check := func(side string, err error) {
		if err == nil || errors.Is(err, ErrCausalUnsupported) {
			t.Fatalf("%s: violation not rejected as a mismatch: %v", side, err)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: rejected for another reason: %v (want %q)", side, err, want)
		}
		t.Logf("%s: %v", side, err)
	}
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	rc = mutate(r, rc)
	check("one-sided", a2Cmp(t, f.script, l, lc, r, rc))
	l2, lc2 := f.fresh(t)
	r2, rc2 := f.fresh(t)
	lc2, rc2 = mutate(l2, lc2), mutate(r2, rc2)
	check("both sides", a2Cmp(t, f.script, l2, lc2, r2, rc2))
}

func a3Table(t *testing.T, id string, cases map[string]a3Mut) {
	t.Helper()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			a3Reject(t, a3Load(t, id, a2Path(id)), c.want, func(tr *Trace, cuts []CausalCut) []CausalCut { return c.mutate(t, tr, cuts) })
		})
	}
}

// a3OneSided is for content changes: a value is compared between the traces and
// recomputed only where a rule says so, so an identical change on both sides is
// out of reach by design.
func a3OneSided(t *testing.T, id string, mutate func(t *testing.T, tr *Trace)) {
	t.Helper()
	f := a3Load(t, id, a2Path(id))
	l, lc := f.fresh(t)
	r, rc := f.fresh(t)
	mutate(t, r)
	if err := a2Cmp(t, f.script, l, lc, r, rc); err == nil || errors.Is(err, ErrCausalUnsupported) {
		t.Fatalf("one-sided content change not rejected: %v", err)
	}
}

func a3Data(kv ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func TestCausal3MetadataAndRootProvisionNegatives(t *testing.T) {
	a3Table(t, "D01", map[string]a3Mut{
		"meta-value-wrong": {"static metadata value", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "output", "to", "inherited")].Data["value"] = "parent"
			return c
		}},
		"root-meta-value-wrong": {"static metadata value", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "output", "to", "parent-name")].Data["value"] = "inner"
			return c
		}},
		"meta-output-missing": {"body of", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "output", "to", "shadow"))
		}},
		"consumer-loads-before-publication": {"LOADING before every required provider", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "service", "key", "svc"), a2At(t, tr, "status", "fiber", "c", "to", "LOADING"))
			return c
		}},
		"publication-value-changed": {"value other than its command", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "service", "key", "svc")].Data["value"] = "forged"
			return c
		}},
		"publication-missing": {"LOADING before every required provider", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "service", "key", "svc"))
		}},
		"publication-duplicated": {"re-notified without a notify command", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "service", "key", "svc"))
		}},
	})
	t.Run("stale-binding", func(t *testing.T) {
		a3Reject(t, a3Load(t, "D01", a2Path("D01")), "unobserved provision UID", func(tr *Trace, c []CausalCut) []CausalCut {
			tr.Checkpoints[0].States[0].Epoch.Needs[0].ProvisionUID = float64(99)
			return c
		})
	})
	t.Run("content-changes-one-sided", func(t *testing.T) {
		a3OneSided(t, "D01", func(t *testing.T, tr *Trace) {
			tr.Events[a2At(t, tr, "output", "to", "service")].Data["value"] = "other"
		})
	})
}

func TestCausal3SelfDisposeAndLateEffectNegatives(t *testing.T) {
	a3Table(t, "D25", map[string]a3Mut{
		"teardown-before-apply-returned": {"ended with 1 of 2 events", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "mark", "label", "apply-returned"), a2At(t, tr, "status", "to", "UNLOADING"))
			return c
		}},
		"request-missing": {"without its selfDispose request", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "selfDispose", "fiber", "self"))
		}},
		"request-duplicated": {"invalid selfDispose request", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "selfDispose", "fiber", "self"))
		}},
		"cleanup-before-request": {"outside UNLOADING", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "cleanup", "fiber", "self"), a2At(t, tr, "selfDispose", "fiber", "self"))
			return c
		}},
		"await-flag-forged": {"invalid selfDispose request", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "selfDispose", "fiber", "self")].Data["await"] = true
			return c
		}},
	})
	a3Table(t, "D24", map[string]a3Mut{
		"refusal-missing-on-both-sides": {"breaks program order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "error", "op", "lateEffect"))
		}},
		"refusal-while-target-live": {"outside the frame", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "error", "op", "lateEffect"), a2At(t, tr, "plugin", "fiber", "f", "disposed", true))
			return c
		}},
		"refusal-other-code": {"INACTIVE_EFFECT", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "error", "op", "lateEffect")].Data["error"] = map[string]any{"name": "Error", "message": "x", "code": "OTHER"}
			return c
		}},
		"error-output-swapped": {"breaks program order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "output", "to", "unload-error"), a2At(t, tr, "error", "op", "lateEffect"))
			return c
		}},
		"cleanup-program-runs-twice": {"breaks program order or repeats", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			i := a2At(t, tr, "output", "to", "unload-error")
			c = a2Dup(tr, c, i-1)    // second error
			return a2Dup(tr, c, i+1) // and its output
		}},
		"root-refusal-before-disposal-finished": {"breaks program order or repeats", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			j := a2At(t, tr, "error", "op", "lateEffect") // first (fiber) one
			_ = j
			last := len(tr.Events) - 2
			a2Move(tr, last, a2At(t, tr, "status", "to", "DISPOSED"))
			return c
		}},
	})
}

func a3PolicyUpdateAt(tr *Trace, cuts []CausalCut, after int, fiber string) []CausalCut {
	return a3Insert(tr, cuts, after, "status", a3Data("fiber", fiber, "from", "ACTIVE", "to", "UNLOADING"))
}

func TestCausal3UpdatePolicyNegatives(t *testing.T) {
	a3Table(t, "D28", map[string]a3Mut{
		"retain-then-restart": {"without a pending update request", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3PolicyUpdateAt(tr, c, a2At(t, tr, "update", "mode", "retain")+1, "f")
		}},
		"hook-event-wrong-mode": {"has mode", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "update", "mode", "retain")].Data["mode"] = "unchanged"
			return c
		}},
		"hook-event-missing": {"no complete event bundle", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "update", "mode", "retain"))
		}},
		"commit-under-noSave": {"command bundle", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Insert(tr, c, a2At(t, tr, "config", "config", map[string]any{"v": float64(2)})+1, "commit", a3Data("fiber", "f", "config", map[string]any{"v": float64(2)}))
		}},
		"resolve-publishes": {"command bundle", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Insert(tr, c, a2At(t, tr, "output", "to", "candidate"), "commit", a3Data("fiber", "f", "config", map[string]any{"v": float64(99)}))
		}},
	})
	a3Table(t, "D30", map[string]a3Mut{
		"veto-ignored-restart": {"without a pending update request", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3PolicyUpdateAt(tr, c, a2At(t, tr, "update", "mode", "unchanged")+1, "f")
		}},
		"changed-target-not-restarted": {"not served by a reload", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			i := a2At(t, tr, "status", "to", "UNLOADING")
			for k := 0; k < 5; k++ { // UNLOADING, LOADING, config, apply, ACTIVE
				c = a2Drop(tr, c, i)
			}
			return c
		}},
		"restart-applies-other-config": {"other than its update hook", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "apply", "fiber", "f", "config", map[string]any{"v": float64(2), "resolved": true})].Data["config"] = map[string]any{"v": float64(7), "resolved": true}
			return c
		}},
	})
	t.Run("effective-config-stale", func(t *testing.T) {
		a3Reject(t, a3Load(t, "D30", a2Path("D30")), "captured effective config", func(tr *Trace, c []CausalCut) []CausalCut {
			tr.Checkpoints[2].States[0].EffectiveConfig = map[string]any{"v": float64(1), "resolved": true}
			return c
		})
	})
}

func TestCausal3AsyncUpdateNegatives(t *testing.T) {
	a3Table(t, "D17", map[string]a3Mut{
		"two-reloads-for-coalesced-requests": {"without a pending update request", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3PolicyUpdateAt(tr, c, a2At(t, tr, "status", "fiber", "f", "from", "LOADING", "to", "ACTIVE")+1, "f")
		}},
		"stale-candidate-applied": {"stale target", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			stale := map[string]any{"v": float64(2)}
			tr.Events[a2At(t, tr, "status", "fiber", "f", "from", "UNLOADING", "to", "LOADING")+1].Data["config"] = stale
			return c
		}},
		"command-event-before-waitMark": {"before arrival", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "config", "config", map[string]any{"v": float64(3)}), a2At(t, tr, "gate", "label", "teardown"))
			return c
		}},
		"continuation-overtakes-parked-gate": {"before its release", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Cut(c, "initial", a2At(t, tr, "status", "fiber", "f", "from", "UNLOADING", "to", "LOADING")+1)
		}},
		"request-never-served": {"not served by a reload", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			i := a2At(t, tr, "status", "fiber", "f", "from", "UNLOADING", "to", "LOADING")
			for k := 0; k < 6; k++ { // LOADING, config, apply, mark, output, ACTIVE
				c = a2Drop(tr, c, i)
			}
			return c
		}},
		"revision-miscounts-coalescing": {"revision", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Checkpoints[1].States[0].Revision = 1
			tr.Checkpoints[1].States[0].Epoch.Revision = 1
			return c
		}},
		"commit-missing": {"no complete event bundle", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "commit", "config", map[string]any{"v": float64(3)}))
		}},
		"commit-duplicated": {"extra command-driven", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "commit", "config", map[string]any{"v": float64(3)}))
		}},
	})
	a3Table(t, "D20", map[string]a3Mut{
		"superseded-load-error-fails-the-latest-target": {"newer update request is pending", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Events[a2At(t, tr, "status", "fiber", "f", "from", "LOADING", "to", "UNLOADING")].Data["to"] = "FAILED"
			return c
		}},
		"command-event-before-gate-arrival": {"before arrival", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "config", "config", map[string]any{"v": float64(2), "cleanup": "new-cleanup"}), a2At(t, tr, "gate", "label", "old-load"))
			return c
		}},
	})
}

func TestCausal3UpdateListenerNegatives(t *testing.T) {
	a3Table(t, "D29", map[string]a3Mut{
		"awaitTask-resolves-before-its-task": {"before arrival", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "error", "op", "awaitTask"), a2At(t, tr, "gate", "label", "reject-update"))
			return c
		}},
		"listener-missing-on-both-sides": {"is repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "listener", "label", "update-owner"))
		}},
		"listener-before-its-command": {"started before its command", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "listener", "label", "update-owner"), a2At(t, tr, "config", "config", map[string]any{"v": float64(1)}))
			return c
		}},
		"second-gate-arrival": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "gate", "label", "old-update"))
		}},
		"rejection-event-without-rejection": {"extra root awaitTask error", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			i := a2At(t, tr, "output", "to", "rejection")
			return a3Insert(tr, c, i+1, "error", a3Data("op", "awaitTask", "error", map[string]any{"name": "Error", "message": "x"}))
		}},
		"stale-effective-config-invented": {"effective config", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			tr.Checkpoints[2].States[0].EffectiveConfig = map[string]any{"v": float64(42)}
			return c
		}},
	})
	// A hand-built script isolates the liveness rule: the chain's gate arrives
	// after its fiber was disposed (no waitMark relates the two).
	script := `{"schema":"s0-script/v1","id":"D98","title":"chain after disposal","covers":["x"],
		"plugins":[{"id":"p","apply":[{"op":"mark","label":"m"},{"op":"listen","key":"internal/update","label":"L","program":[{"op":"gate","label":"g"}]}]}],
		"steps":[{"op":"mount","fiber":"f","plugin":"p","config":{"v":0}},{"op":"settle","label":"a"},
		{"op":"update","fiber":"f","config":{"v":1},"await":false,"task":"t"},{"op":"waitMark","label":"m"},
		{"op":"dispose","fiber":"f","await":false},{"op":"release","label":"g"},{"op":"settle","label":"b"}]}`
	cfg0, cfg1 := map[string]any{"v": float64(0)}, map[string]any{"v": float64(1)}
	build := func(gateAfterDisposal bool) (*Trace, []CausalCut) {
		ev := []Event{
			{Kind: "plugin", Data: a3Data("fiber", "f", "disposed", false)},
			{Kind: "status", Data: a3Data("fiber", "f", "from", "PENDING", "to", "LOADING")},
			{Kind: "config", Data: a3Data("fiber", "f", "config", cfg0)},
			{Kind: "apply", Data: a3Data("fiber", "f", "config", cfg0)},
			{Kind: "mark", Data: a3Data("label", "m", "value", map[string]any{"$undefined": true})},
			{Kind: "status", Data: a3Data("fiber", "f", "from", "LOADING", "to", "ACTIVE")},
			{Kind: "config", Data: a3Data("fiber", "f", "config", cfg1)},
			{Kind: "commit", Data: a3Data("fiber", "f", "config", cfg1)},
			{Kind: "listener", Data: a3Data("label", "L")},
			{Kind: "gate", Data: a3Data("label", "g", "value", map[string]any{"$undefined": true})},
			{Kind: "plugin", Data: a3Data("fiber", "f", "disposed", true)},
			{Kind: "status", Data: a3Data("fiber", "f", "from", "ACTIVE", "to", "UNLOADING")},
			{Kind: "status", Data: a3Data("fiber", "f", "from", "UNLOADING", "to", "DISPOSED")},
		}
		if gateAfterDisposal {
			gate := ev[9]
			ev = append(append(append([]Event(nil), ev[:9]...), ev[10:]...), gate)
		}
		tr := &Trace{Schema: "s0-trace/v1", Scenario: "D98", Events: ev}
		for i := range tr.Events {
			tr.Events[i].Seq = i + 1
		}
		fiber := func(state string, uid any, epoch *Epoch, raw, eff any) FiberSnapshot {
			return FiberSnapshot{Fiber: "f", State: state, FiberUID: uid, Epoch: epoch, RawConfig: raw, EffectiveConfig: eff}
		}
		tr.Checkpoints = []Checkpoint{
			{Label: "a", States: []FiberSnapshot{fiber("ACTIVE", float64(1), &Epoch{Needs: []Binding{}}, cfg0, cfg0)}, Provisions: []ProvisionSnapshot{}, Outputs: map[string]any{}},
			{Label: "b", States: []FiberSnapshot{fiber("DISPOSED", nil, nil, cfg1, cfg0)}, Provisions: []ProvisionSnapshot{}, Outputs: map[string]any{}},
		}
		return tr, []CausalCut{{Label: "a", AfterEvent: 6}, {Label: "b", AfterEvent: len(ev)}}
	}
	t.Run("valid-control", func(t *testing.T) {
		l, lc := build(false)
		r, rc := build(false)
		if err := a2Cmp(t, []byte(script), l, lc, r, rc); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("continuation-after-its-fiber-left", func(t *testing.T) {
		l, lc := build(true)
		r, rc := build(true)
		err := a2Cmp(t, []byte(script), l, lc, r, rc)
		if err == nil || errors.Is(err, ErrCausalUnsupported) || !strings.Contains(err.Error(), "left its activation") {
			t.Fatalf("hook continuation after its fiber left not rejected: %v", err)
		}
	})
}

func TestCausal3HookDispatchNegatives(t *testing.T) {
	a3Table(t, "D31", map[string]a3Mut{
		"second-listener-starts-before-first-prefix-done": {"occurred before lane", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "listener", "label", "sync-second"), a2At(t, tr, "gate", "label", "emit-listener"))
			return c
		}},
		"completion-before-a-listener-prefix": {"occurred before lane", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "output", "to", "emit-result"), a2At(t, tr, "mark", "label", "second-called"))
			return c
		}},
		"listeners-out-of-order": {"occurred before lane", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "listener", "label", "sync-second"), 0)
			return c
		}},
		"continuation-before-release": {"before its release", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Cut(c, "returned", len(tr.Events))
		}},
		"listener-run-twice": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "listener", "label", "sync-second"))
		}},
	})
	a3Table(t, "D32", map[string]a3Mut{
		"awaitTask-result-before-the-task-finished": {"overtook an awaited", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "error", "op", "awaitTask"), a2At(t, tr, "mark", "label", "parallel-complete"))
			return c
		}},
		"slow-listener-run-twice": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "listener", "label", "slow"))
		}},
		"aggregate-output-missing": {"output-event prefix", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "output", "to", "aggregate"))
		}},
	})
	a3Table(t, "D33", map[string]a3Mut{
		"serial-listener-before-predecessor-finished": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "listener", "label", "zero"), a2At(t, tr, "gate", "label", "serial-gate"))
			return c
		}},
		"continuation-before-release": {"before its release", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Cut(c, "serial-waiting", a2At(t, tr, "output", "to", "zero-result")+1)
		}},
		"bail-ignored-after-zero": {"does not run", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Insert(tr, c, a2At(t, tr, "output", "to", "zero-result"), "listener", a3Data("label", "not-called"))
		}},
		"error-did-not-stop-the-chain": {"does not run", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Insert(tr, c, a2At(t, tr, "error", "op", "dispatch"), "mark", a3Data("label", "wrong-after-error", "value", map[string]any{"$undefined": true}))
		}},
	})
	a3Table(t, "D34", map[string]a3Mut{
		"bail-continues-after-zero": {"does not run", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Insert(tr, c, a2At(t, tr, "output", "to", "zero"), "listener", a3Data("label", "not-after-zero"))
		}},
		"empty-string-not-a-bail": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "listener", "label", "empty"))
		}},
		"listener-missing-on-both-sides": {"is repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Drop(tr, c, a2At(t, tr, "listener", "label", "zero"))
		}},
	})
	a3Table(t, "D35", map[string]a3Mut{
		"veto-ignored": {"does not run", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Insert(tr, c, a2At(t, tr, "listener", "label", "veto")+1, "listener", a3Data("label", "not-after-veto"))
		}},
		"inner-after-veto": {"inner", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a3Insert(tr, c, a2At(t, tr, "listener", "label", "veto")+1, "inner", a3Data("key", "waterfall-veto"))
		}},
		"outer-after-before-inner-chain": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			a2Move(tr, a2At(t, tr, "mark", "label", "outer-after"), a2At(t, tr, "listener", "label", "veto"))
			return c
		}},
		"duplicate-listener-event": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "listener", "label", "delegates"))
		}},
	})
	a3Table(t, "D36", map[string]a3Mut{
		"next-twice-without-the-error": {"ended after", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			c = a2Drop(tr, c, a2At(t, tr, "output", "to", "repeated-error"))
			c = a2Drop(tr, c, a2At(t, tr, "error", "op", "dispatch"))
			delete(tr.Checkpoints[0].Outputs, "repeated-error")
			return c
		}},
		"inner-called-twice": {"repeated, late or out of its planned order", func(t *testing.T, tr *Trace, c []CausalCut) []CausalCut {
			return a2Dup(tr, c, a2At(t, tr, "inner", "key", "repeated"))
		}},
	})
}

// Content changes inside planned events are caught by comparison with the oracle
// trace, one-sided.
func TestCausal3ContentChangesAreRejected(t *testing.T) {
	for id, mutate := range map[string]func(t *testing.T, tr *Trace){
		"D31": func(t *testing.T, tr *Trace) {
			tr.Events[a2At(t, tr, "gate", "label", "emit-listener")].Data["value"] = 1
		},
		"D32": func(t *testing.T, tr *Trace) {
			tr.Events[a2At(t, tr, "error", "op", "awaitTask")].Data["error"] = map[string]any{"name": "AggregateError", "message": "", "errors": []any{}}
		},
		"D33": func(t *testing.T, tr *Trace) {
			tr.Events[a2At(t, tr, "output", "to", "zero-result")].Data["value"] = false
		},
		"D34": func(t *testing.T, tr *Trace) { tr.Events[a2At(t, tr, "output", "to", "empty")].Data["value"] = nil },
		"D35": func(t *testing.T, tr *Trace) { tr.Events[a2At(t, tr, "output", "to", "delegated")].Data["value"] = "x" },
		"D36": func(t *testing.T, tr *Trace) {
			tr.Events[a2At(t, tr, "error", "op", "dispatch")].Data["error"] = map[string]any{"name": "Error", "message": "other"}
		},
		"D24": func(t *testing.T, tr *Trace) {
			tr.Events[a2At(t, tr, "output", "to", "disposed-error")].Data["value"] = map[string]any{"name": "Error", "message": "x"}
		},
		"D28": func(t *testing.T, tr *Trace) {
			tr.Events[a2At(t, tr, "update", "mode", "retain")].Data["config"] = map[string]any{"v": float64(1)}
		},
		"D29": func(t *testing.T, tr *Trace) {
			tr.Checkpoints[2].States[0].EffectiveConfig = map[string]any{"v": float64(1)}
		},
	} {
		t.Run(id, func(t *testing.T) { a3OneSided(t, id, mutate) })
	}
}

// Constructs without a rule stay unsupported, each in a form no amendment-3 rule
// covers (see the amendment's "Capabilities not added").
func TestCausal3StillUnsupportedForms(t *testing.T) {
	cases := map[string]struct {
		id   string
		edit func(doc map[string]any)
	}{
		"awaited-self-dispose": {"D25", func(d map[string]any) {
			a3Idx(d, "plugins", 0)["apply"].([]any)[0].(map[string]any)["await"] = true
		}},
		"reject-policy":    {"D28", func(d map[string]any) { a3Idx(d, "plugins", 0)["update"] = "reject" }},
		"listener-prepend": {"D34", func(d map[string]any) { a3Idx(d, "steps", 0)["prepend"] = true }},
		"gate-in-bail-listener": {"D34", func(d map[string]any) {
			a3Idx(d, "steps", 0)["program"] = []any{map[string]any{"op": "gate", "label": "g"}}
		}},
		"key-dispatched-twice": {"D34", func(d map[string]any) { a3Idx(d, "steps", 7)["key"] = "bail-zero" }},
		"restart-async": {"D17", func(d map[string]any) {
			s := a3Idx(d, "steps", 4)
			s["op"], s["await"] = "restart", false
			delete(s, "config")
		}},
		"awaitTask-restart-task": {"D17", func(d map[string]any) {
			steps := d["steps"].([]any)
			d["steps"] = append(steps[:6:6], append([]any{map[string]any{"op": "awaitTask", "task": "u2"}}, steps[6:]...)...)
		}},
		"meta-runtime-member": {"D01", func(d map[string]any) {
			a3Idx(d, "plugins", 0)["apply"].([]any)[1].(map[string]any)["value"] = map[string]any{"$meta": "fiber.uid"}
		}},
		"root-provide-with-service": {"D01", func(d map[string]any) { a3Idx(d, "steps", 0)["mode"] = "service" }},
		"update-listener-next": {"D29", func(d map[string]any) {
			a3Idx(d, "plugins", 0)["apply"].([]any)[0].(map[string]any)["program"] = []any{map[string]any{"op": "next"}}
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := compileCausalModel(a3Script(t, c.id, func(map[string]any) {})); err != nil {
				t.Fatalf("control script must compile: %v", err)
			}
			if _, err := compileCausalModel(a3Script(t, c.id, c.edit)); !errors.Is(err, ErrCausalUnsupported) {
				t.Fatalf("want explicit unsupported, got %v", err)
			}
		})
	}
}

// a3Script decodes a corpus script, lets edit change it structurally and encodes
// it again, so a mutation cannot silently miss its target text.
func a3Script(t *testing.T, id string, edit func(doc map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile(a2Path(id))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func a3Idx(doc map[string]any, list string, i int) map[string]any {
	return doc[list].([]any)[i].(map[string]any)
}
