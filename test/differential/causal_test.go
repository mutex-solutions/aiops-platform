// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// These are comparator unit records, not executions, golden candidate outputs,
// or evidence that either runtime implements this script. Integration records
// must obtain sidecars from the actual replay recorders.
const causalUnitScript = `{"schema":"s0-script/v1","id":"D16","title":"Synthetic static diamond comparator unit","covers":["comparator-unit"],"plugins":[{"id":"provider","apply":[{"op":"provide","key":"a","value":1}]},{"id":"left","needs":{"a":null},"apply":[{"op":"read","key":"a","to":"left"},{"op":"provide","key":"b","value":{"$output":"left"}}]},{"id":"right","needs":{"a":null},"apply":[{"op":"read","key":"a","to":"right"},{"op":"provide","key":"c","value":{"$output":"right"}}]},{"id":"leaf","needs":{"b":null,"c":null},"apply":[{"op":"read","key":"b","to":"leaf-left"},{"op":"read","key":"c","to":"leaf-right"}]}],"steps":[{"op":"mount","fiber":"p","plugin":"provider","config":{}},{"op":"mount","fiber":"l","plugin":"left","config":{}},{"op":"mount","fiber":"r","plugin":"right","config":{}},{"op":"mount","fiber":"leaf","plugin":"leaf","config":{}},{"op":"settle","label":"ready"}]}`

func causalUnitEvent(kind string, data map[string]any) Event { return Event{Kind: kind, Data: data} }
func causalUnitFiber(kind, fiber string) Event {
	return causalUnitEvent(kind, map[string]any{"fiber": fiber, "config": map[string]any{}})
}
func causalUnitStatus(fiber, from, to string) Event {
	return causalUnitEvent("status", map[string]any{"fiber": fiber, "from": from, "to": to})
}
func causalUnitService(key string, value any) Event {
	return causalUnitEvent("service", map[string]any{"key": key, "realm": "root:" + key, "value": value})
}
func causalUnitOutput(to string, value any) Event {
	return causalUnitEvent("output", map[string]any{"to": to, "value": value})
}
func causalUnitTrace() *Trace {
	trace := &Trace{Schema: TraceSchema, Scenario: "D16", Events: []Event{}, Checkpoints: []Checkpoint{}}
	for _, fiber := range []string{"p", "l", "r", "leaf"} {
		trace.Events = append(trace.Events, causalUnitEvent("plugin", map[string]any{"fiber": fiber, "disposed": false}))
	}
	trace.Events = append(trace.Events,
		causalUnitStatus("p", "PENDING", "LOADING"), causalUnitFiber("config", "p"), causalUnitFiber("apply", "p"), causalUnitStatus("p", "LOADING", "ACTIVE"),
		// The source contract allows consumer status before service notification;
		// this dependent pair's order is retained, not 'corrected' by comparison.
		causalUnitStatus("l", "PENDING", "LOADING"), causalUnitStatus("r", "PENDING", "LOADING"), causalUnitService("a", 1),
		causalUnitFiber("config", "l"), causalUnitFiber("apply", "l"), causalUnitOutput("left", 1),
		causalUnitFiber("config", "r"), causalUnitFiber("apply", "r"), causalUnitOutput("right", 1),
		causalUnitStatus("l", "LOADING", "ACTIVE"), causalUnitService("b", 1),
		causalUnitStatus("r", "LOADING", "ACTIVE"), causalUnitStatus("leaf", "PENDING", "LOADING"), causalUnitService("c", 1),
		causalUnitFiber("config", "leaf"), causalUnitFiber("apply", "leaf"),
		causalUnitOutput("leaf-left", 1), causalUnitOutput("leaf-right", 1), causalUnitStatus("leaf", "LOADING", "ACTIVE"),
	)
	cp := Checkpoint{Label: "ready", States: []FiberSnapshot{}, Provisions: []ProvisionSnapshot{}, Outputs: map[string]any{"left": 1, "right": 1, "leaf-left": 1, "leaf-right": 1}}
	for i, fiber := range []string{"p", "l", "r", "leaf"} {
		needs := []Binding{}
		if fiber == "l" || fiber == "r" {
			needs = append(needs, Binding{Key: "a", ProvisionUID: "a:old"})
		}
		if fiber == "leaf" {
			needs = append(needs, Binding{Key: "b", ProvisionUID: "b:old"}, Binding{Key: "c", ProvisionUID: "c:old"})
		}
		cp.States = append(cp.States, FiberSnapshot{Fiber: fiber, State: "ACTIVE", FiberUID: fmt.Sprintf("fiber:%d", i), Epoch: &Epoch{Needs: needs}, RawConfig: map[string]any{}, EffectiveConfig: map[string]any{}})
	}
	for i, key := range []string{"a", "b", "c"} {
		cp.Provisions = append(cp.Provisions, ProvisionSnapshot{Key: key, Realm: "root:" + key, Provider: []string{"p", "l", "r"}[i], ProvisionUID: key + ":old", Value: 1, Active: true})
	}
	trace.Checkpoints = append(trace.Checkpoints, cp)
	return trace
}
func causalUnitJSON(t *testing.T, trace *Trace) []byte {
	t.Helper()
	for i := range trace.Events {
		trace.Events[i].Seq = i + 1
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func causalUnitWitness(script, trace []byte, cuts ...CausalCut) CausalWitness {
	return CausalWitness{Schema: CausalWitnessSchema, ScriptSHA256: causalDigest(script), TraceSHA256: causalDigest(trace), Checkpoints: cuts}
}
func causalUnitCompare(t *testing.T, script []byte, expected, actual *Trace) error {
	t.Helper()
	left, right := causalUnitJSON(t, expected), causalUnitJSON(t, actual)
	lc, rc := []CausalCut{}, []CausalCut{}
	for _, cp := range expected.Checkpoints {
		lc = append(lc, CausalCut{Label: cp.Label, AfterEvent: len(expected.Events)})
	}
	for _, cp := range actual.Checkpoints {
		rc = append(rc, CausalCut{Label: cp.Label, AfterEvent: len(actual.Events)})
	}
	return CompareCausal(script, left, right, causalUnitWitness(script, left, lc...), causalUnitWitness(script, right, rc...))
}
func causalUnitSwapBranches(trace *Trace) {
	left := append([]Event(nil), trace.Events[11:14]...)
	right := append([]Event(nil), trace.Events[14:17]...)
	copy(trace.Events[11:14], right)
	copy(trace.Events[14:17], left)
}

func TestCausalIndependentBranchInterleaving(t *testing.T) {
	left, right := causalUnitTrace(), causalUnitTrace()
	causalUnitSwapBranches(right)
	beforeScript := []byte(causalUnitScript)
	leftRaw, rightRaw := causalUnitJSON(t, left), causalUnitJSON(t, right)
	leftCopy, rightCopy := bytes.Clone(leftRaw), bytes.Clone(rightRaw)
	wleft := causalUnitWitness(beforeScript, leftRaw, CausalCut{"ready", len(left.Events)})
	wright := causalUnitWitness(beforeScript, rightRaw, CausalCut{"ready", len(right.Events)})
	if err := Compare(leftRaw, rightRaw); err == nil {
		t.Fatal("strict comparison unexpectedly accepted branch reordering")
	}
	if err := CompareCausal(beforeScript, leftRaw, rightRaw, wleft, wright); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftRaw, leftCopy) || !bytes.Equal(rightRaw, rightCopy) || string(beforeScript) != causalUnitScript {
		t.Fatal("raw inputs were mutated")
	}
}

func causalUnitMoveEvent(trace *Trace, from, to int) {
	event := trace.Events[from]
	if from > to {
		copy(trace.Events[to+1:from+1], trace.Events[to:from])
	} else {
		copy(trace.Events[from:to], trace.Events[from+1:to+1])
	}
	trace.Events[to] = event
}

func TestCausalPureMountPublicationIsNotServiceRead(t *testing.T) {
	left, right := causalUnitTrace(), causalUnitTrace()
	// Publish l then allow p's complete body before r's root declaration.
	prefix := append([]Event(nil), right.Events[:11]...)
	for i, index := range []int{0, 1, 4, 5, 6, 7, 8, 10, 2, 9, 3} {
		right.Events[i] = prefix[index]
	}
	if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Trace){
		"root-mount-order":          func(tr *Trace) { tr.Events[1], tr.Events[2] = tr.Events[2], tr.Events[1] },
		"callback-before-own-mount": func(tr *Trace) { causalUnitMoveEvent(tr, 5, 0) },
		"status-before-own-mount":   func(tr *Trace) { causalUnitMoveEvent(tr, 9, 2) },
		"same-fiber-mount-config":   func(tr *Trace) { tr.Events[0], tr.Events[5] = tr.Events[5], tr.Events[0] },
	} {
		t.Run(name, func(t *testing.T) {
			tr := causalUnitTrace()
			mutate(tr)
			if err := causalUnitCompare(t, []byte(causalUnitScript), left, tr); err == nil {
				t.Fatal("mount exception erased an ordering obligation")
			}
		})
	}
	// A mount config using a shared output is not a pure declaration.
	script := []byte(strings.Replace(causalUnitScript, `"fiber":"r","plugin":"right","config":{}`, `"fiber":"r","plugin":"right","config":{"captured":{"$output":"left"}}`, 1))
	right = causalUnitTrace()
	causalUnitMoveEvent(right, 2, 14)
	if err := causalUnitCompare(t, script, left, right); err == nil {
		t.Fatal("mount output-cell dependency ignored")
	}
}

func TestCausalLastReadyProviderMayDiffer(t *testing.T) {
	left, right := causalUnitTrace(), causalUnitTrace()
	// Only the earlier and final ordinary publication swap their LOADING
	// orientation; provider ACTIVE transitions still precede consumer LOADING.
	right.Events[17], right.Events[19] = right.Events[19], right.Events[17]
	right.Events[18], right.Events[21] = right.Events[21], right.Events[18]
	if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err != nil {
		t.Fatal(err)
	}
	sharedScript := []byte(strings.Replace(causalUnitScript, `"op":"read","key":"b","to":"leaf-left"`, `"op":"output","to":"leaf-left","value":{"$output":"left"}`, 1))
	if err := causalUnitCompare(t, sharedScript, left, right); err == nil {
		t.Fatal("startup exception erased a separate shared output-cell conflict")
	}
	// A valid earlier provider's notification may itself be delayed across
	// consumer LOADING, independently of the final enabling provider bracket.
	right = causalUnitTrace()
	causalUnitMoveEvent(right, 18, 21)
	if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Trace){
		"last-publish-before-loading":     func(tr *Trace) { tr.Events[20], tr.Events[21] = tr.Events[21], tr.Events[20] },
		"loading-before-required-active":  func(tr *Trace) { tr.Events[19], tr.Events[20] = tr.Events[20], tr.Events[19] },
		"active-before-required-provider": func(tr *Trace) { causalUnitMoveEvent(tr, 26, 19) },
		"duplicate-active-publication": func(tr *Trace) {
			tr.Events = append(tr.Events[:22], append([]Event{causalUnitService("c", 1)}, tr.Events[22:]...)...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := causalUnitTrace()
			mutate(bad)
			if err := causalUnitCompare(t, []byte(causalUnitScript), left, bad); err == nil {
				t.Fatal("accepted invalid readiness bracket")
			}
			// Each stream validates independently, even two identical corruptions.
			other := causalUnitTrace()
			mutate(other)
			if err := causalUnitCompare(t, []byte(causalUnitScript), bad, other); err == nil {
				t.Fatal("identical malformed traces bypassed per-trace validation")
			}
		})
	}
}

func TestCausalMountExceptionNeverAppliesToDisposal(t *testing.T) {
	const script = `{"schema":"s0-script/v1","id":"D02","title":"Synthetic awaited disposal unit","covers":["unit"],"plugins":[{"id":"empty","apply":[]}],"steps":[{"op":"mount","fiber":"a","plugin":"empty","config":{}},{"op":"mount","fiber":"b","plugin":"empty","config":{}},{"op":"settle","label":"ready"},{"op":"dispose","fiber":"a"},{"op":"dispose","fiber":"b"},{"op":"settle","label":"disposed"}]}`
	makeTrace := func() *Trace {
		tr := &Trace{Schema: TraceSchema, Scenario: "D02", Events: []Event{}, Checkpoints: []Checkpoint{}}
		for _, actor := range []string{"a", "b"} {
			tr.Events = append(tr.Events, causalUnitEvent("plugin", map[string]any{"fiber": actor, "disposed": false}))
		}
		for _, actor := range []string{"a", "b"} {
			tr.Events = append(tr.Events, causalUnitStatus(actor, "PENDING", "LOADING"), causalUnitFiber("apply", actor), causalUnitStatus(actor, "LOADING", "ACTIVE"))
		}
		for _, actor := range []string{"a", "b"} {
			tr.Events = append(tr.Events, causalUnitEvent("plugin", map[string]any{"fiber": actor, "disposed": true}), causalUnitStatus(actor, "ACTIVE", "UNLOADING"), causalUnitStatus(actor, "UNLOADING", "DISPOSED"))
		}
		for _, label := range []string{"ready", "disposed"} {
			cp := Checkpoint{Label: label, States: []FiberSnapshot{}, Provisions: []ProvisionSnapshot{}, Outputs: map[string]any{}}
			for _, actor := range []string{"a", "b"} {
				s := FiberSnapshot{Fiber: actor, State: "ACTIVE", FiberUID: actor, Epoch: &Epoch{Needs: []Binding{}}, RawConfig: map[string]any{}, EffectiveConfig: map[string]any{}}
				if label == "disposed" {
					s.State = "DISPOSED"
					s.FiberUID = nil
					s.Epoch = nil
				}
				cp.States = append(cp.States, s)
			}
			tr.Checkpoints = append(tr.Checkpoints, cp)
		}
		return tr
	}
	compare := func(left, right *Trace) error {
		lb, rb := causalUnitJSON(t, left), causalUnitJSON(t, right)
		cuts := []CausalCut{{"ready", 8}, {"disposed", 14}}
		return CompareCausal([]byte(script), lb, rb, causalUnitWitness([]byte(script), lb, cuts...), causalUnitWitness([]byte(script), rb, cuts...))
	}
	if err := compare(makeTrace(), makeTrace()); err != nil {
		t.Fatal(err)
	}
	bad := makeTrace()
	bad.Events[10], bad.Events[11] = bad.Events[11], bad.Events[10]
	if err := compare(makeTrace(), bad); err == nil {
		t.Fatal("later disposal publication overtook awaited final state")
	}
	other := makeTrace()
	other.Events[10], other.Events[11] = other.Events[11], other.Events[10]
	if err := compare(bad, other); err == nil {
		t.Fatal("identical invalid disposal traces accepted")
	}
}

func TestCausalAdversarialSemanticChanges(t *testing.T) {
	for name, mutate := range map[string]func(*Trace){
		"dependent-provider-status": func(tr *Trace) { tr.Events[9], tr.Events[10] = tr.Events[10], tr.Events[9] },
		"dependent-config-prefix":   func(tr *Trace) { tr.Events[6], tr.Events[11] = tr.Events[11], tr.Events[6] },
		"same-fiber-order":          func(tr *Trace) { tr.Events[11], tr.Events[12] = tr.Events[12], tr.Events[11] },
		"event-dropped":             func(tr *Trace) { tr.Events = append(tr.Events[:12], tr.Events[13:]...) },
		"event-extra":               func(tr *Trace) { tr.Events = append(tr.Events, causalUnitFiber("config", "l")) },
		"event-duplicated":          func(tr *Trace) { tr.Events = append(tr.Events, tr.Events[13]) },
		"status-dropped":            func(tr *Trace) { tr.Events = tr.Events[:len(tr.Events)-1] },
		"state-changed":             func(tr *Trace) { tr.Checkpoints[0].States[1].State = "FAILED" },
		"state-order": func(tr *Trace) {
			tr.Checkpoints[0].States[1], tr.Checkpoints[0].States[2] = tr.Checkpoints[0].States[2], tr.Checkpoints[0].States[1]
		},
		"state-extra": func(tr *Trace) {
			tr.Checkpoints[0].States = append(tr.Checkpoints[0].States, FiberSnapshot{Fiber: "extra", State: "PENDING"})
		},
		"revision-erased":        func(tr *Trace) { tr.Checkpoints[0].States[1].Revision = 9 },
		"epoch-revision-changed": func(tr *Trace) { tr.Checkpoints[0].States[1].Epoch.Revision = 8 },
		"config-captured":        func(tr *Trace) { tr.Events[11].Data["config"] = map[string]any{"changed": true} },
		"apply-captured":         func(tr *Trace) { tr.Events[12].Data["config"] = map[string]any{"changed": true} },
		"raw-config":             func(tr *Trace) { tr.Checkpoints[0].States[1].RawConfig = map[string]any{"changed": true} },
		"effective-config":       func(tr *Trace) { tr.Checkpoints[0].States[1].EffectiveConfig = map[string]any{"changed": true} },
		"output-value":           func(tr *Trace) { tr.Events[13].Data["value"] = 2 },
		"checkpoint-value":       func(tr *Trace) { tr.Checkpoints[0].Outputs["left"] = 2 },
		"service-value":          func(tr *Trace) { tr.Events[10].Data["value"] = 2 },
		"provision-value":        func(tr *Trace) { tr.Checkpoints[0].Provisions[0].Value = 2 },
		"provision-inactive":     func(tr *Trace) { tr.Checkpoints[0].Provisions[0].Active = false },
		"binding-wrong-provider": func(tr *Trace) { tr.Checkpoints[0].States[1].Epoch.Needs[0].ProvisionUID = "b:old" },
		"binding-unobserved":     func(tr *Trace) { tr.Checkpoints[0].States[1].Epoch.Needs[0].ProvisionUID = "missing" },
		"fiber-alias":            func(tr *Trace) { tr.Checkpoints[0].States[2].FiberUID = tr.Checkpoints[0].States[1].FiberUID },
		"provision-alias":        func(tr *Trace) { tr.Checkpoints[0].Provisions[2].ProvisionUID = "b:old" },
		"needs-order":            func(tr *Trace) { n := tr.Checkpoints[0].States[3].Epoch.Needs; n[0], n[1] = n[1], n[0] },
		"root-plugin-order":      func(tr *Trace) { tr.Events[1], tr.Events[2] = tr.Events[2], tr.Events[1] },
	} {
		t.Run(name, func(t *testing.T) {
			left, right := causalUnitTrace(), causalUnitTrace()
			mutate(right)
			if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err == nil {
				t.Fatal("accepted semantic mismatch")
			}
		})
	}
}

func TestCausalWitnessFailClosed(t *testing.T) {
	script := []byte(causalUnitScript)
	trace := causalUnitTrace()
	raw := causalUnitJSON(t, trace)
	w := causalUnitWitness(script, raw, CausalCut{"ready", len(trace.Events)})
	for name, mutate := range map[string]func(*CausalWitness){
		"missing":               func(w *CausalWitness) { *w = CausalWitness{} },
		"schema":                func(w *CausalWitness) { w.Schema = "s0-causal-witness/v2" },
		"script-hash":           func(w *CausalWitness) { w.ScriptSHA256 = strings.Repeat("0", 64) },
		"trace-hash":            func(w *CausalWitness) { w.TraceSHA256 = strings.Repeat("f", 64) },
		"missing-cut":           func(w *CausalWitness) { w.Checkpoints = nil },
		"label":                 func(w *CausalWitness) { w.Checkpoints[0].Label = "forged" },
		"out-of-bounds":         func(w *CausalWitness) { w.Checkpoints[0].AfterEvent = len(trace.Events) + 1 },
		"negative":              func(w *CausalWitness) { w.Checkpoints[0].AfterEvent = -1 },
		"forged-valid-hash-cut": func(w *CausalWitness) { w.Checkpoints[0].AfterEvent = len(trace.Events) - 2 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := w
			bad.Checkpoints = append([]CausalCut(nil), w.Checkpoints...)
			mutate(&bad)
			if err := CompareCausal(script, raw, raw, w, bad); err == nil {
				t.Fatal("accepted invalid witness")
			}
		})
	}
	if err := CompareCausal(append(bytes.Clone(script), '\n'), raw, raw, w, w); err == nil {
		t.Fatal("accepted script bytes not bound by witness")
	}
	if err := CompareCausal(script, raw, append(bytes.Clone(raw), '\n'), w, w); err == nil {
		t.Fatal("accepted trace bytes not bound by witness")
	}
}

func TestCausalProvisionPermutationRequiresIndependence(t *testing.T) {
	left, right := causalUnitTrace(), causalUnitTrace()
	p := right.Checkpoints[0].Provisions
	p[1], p[2] = p[2], p[1]
	// Opaque names may follow local insertion order, not global expected order.
	p[0].ProvisionUID, p[1].ProvisionUID, p[2].ProvisionUID = 900, 901, 902
	for si := range right.Checkpoints[0].States {
		s := &right.Checkpoints[0].States[si]
		s.FiberUID = fmt.Sprintf("actual:%s", s.Fiber)
		for ni := range s.Epoch.Needs {
			b := &s.Epoch.Needs[ni]
			switch b.Key {
			case "a":
				b.ProvisionUID = 900
			case "b":
				b.ProvisionUID = 902
			case "c":
				b.ProvisionUID = 901
			}
		}
	}
	if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err != nil {
		t.Fatal(err)
	}
	right = causalUnitTrace()
	p = right.Checkpoints[0].Provisions
	p[0], p[1] = p[1], p[0]
	if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err == nil {
		t.Fatal("unconditionally treated provision array as a set")
	}
}

func TestCausalSharedOutputReadPreventsFalseIndependence(t *testing.T) {
	// No 'read' event announces this resource access. The compiler must still
	// conservatively link the whole activation reading left's output cell.
	script := []byte(strings.Replace(causalUnitScript, `"$output":"right"`, `"$output":"left"`, 1))
	left, right := causalUnitTrace(), causalUnitTrace()
	causalUnitSwapBranches(right)
	if err := causalUnitCompare(t, script, left, right); err == nil {
		t.Fatal("shared output-cell read/write conflict erased")
	}
	left, right = causalUnitTrace(), causalUnitTrace()
	p := right.Checkpoints[0].Provisions
	p[1], p[2] = p[2], p[1]
	if err := causalUnitCompare(t, script, left, right); err == nil {
		t.Fatal("shared output-cell providers treated as independent")
	}
}

func TestCausalUnknownSemanticsAreUnsupported(t *testing.T) {
	for _, op := range []string{"gate", "mark", "release", "waitMark", "notify", "child", "listen", "effect", "context", "selfDispose", "set"} {
		t.Run(op, func(t *testing.T) {
			var script map[string]any
			if err := json.Unmarshal([]byte(causalUnitScript), &script); err != nil {
				t.Fatal(err)
			}
			plugins := script["plugins"].([]any)
			p := plugins[0].(map[string]any)
			p["apply"] = []any{map[string]any{"op": op, "label": "control", "key": "a", "fiber": "child", "plugin": "left", "to": "ctx"}}
			raw, _ := json.Marshal(script)
			tr := causalUnitTrace()
			if err := causalUnitCompare(t, raw, tr, causalUnitTrace()); !errors.Is(err, ErrCausalUnsupported) {
				t.Fatalf("want ErrCausalUnsupported, got %v", err)
			}
		})
	}
	for name, mutate := range map[string]func(*Trace){
		"unknown-fiber":   func(tr *Trace) { tr.Events[11].Data["fiber"] = "unmapped" },
		"unknown-service": func(tr *Trace) { tr.Events[10].Data["realm"] = "elsewhere" },
		"unknown-output":  func(tr *Trace) { tr.Events[13].Data["to"] = "unknown" },
		"mark-event":      func(tr *Trace) { tr.Events[13] = causalUnitEvent("mark", map[string]any{"label": "unknown"}) },
		"unowned-error": func(tr *Trace) {
			tr.Events[13] = causalUnitEvent("error", map[string]any{"op": "read", "error": "failure"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr := causalUnitTrace()
			mutate(tr)
			if err := causalUnitCompare(t, []byte(causalUnitScript), causalUnitTrace(), tr); !errors.Is(err, ErrCausalUnsupported) {
				t.Fatalf("want ErrCausalUnsupported, got %v", err)
			}
		})
	}
}

func TestCausalUserPayloadAndErrorsRemainExact(t *testing.T) {
	payload := func() map[string]any {
		return map[string]any{"events": "not-hooks", "timestamp": 11, "provisionUID": 99}
	}
	for name, mutate := range map[string]func(map[string]any){
		"rename":    func(v map[string]any) { v["hooks"] = v["events"]; delete(v, "events") },
		"timestamp": func(v map[string]any) { v["timestamp"] = 12 },
		"uid":       func(v map[string]any) { v["provisionUID"] = 100 },
	} {
		t.Run(name, func(t *testing.T) {
			left, right := causalUnitTrace(), causalUnitTrace()
			l, r := payload(), payload()
			mutate(r)
			left.Events[13].Data["value"], right.Events[13].Data["value"] = l, r
			left.Checkpoints[0].Outputs["left"], right.Checkpoints[0].Outputs["left"] = l, r
			if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err == nil {
				t.Fatal("rewrote user payload")
			}
		})
	}
	for _, wrong := range []any{map[string]any{"name": "Error", "message": "cancelled"}, map[string]any{"name": "Error", "message": "deliberate", "code": "CANCELLED"}} {
		left, right := causalUnitTrace(), causalUnitTrace()
		left.Events = append(left.Events, causalUnitEvent("error", map[string]any{"fiber": "l", "error": map[string]any{"name": "Error", "message": "deliberate"}}))
		right.Events = append(right.Events, causalUnitEvent("error", map[string]any{"fiber": "l", "error": wrong}))
		if err := causalUnitCompare(t, []byte(causalUnitScript), left, right); err == nil {
			t.Fatal("masked exact error/cancellation distinction")
		}
	}
}

func TestCausalIdentityLifetimesAcrossAllCheckpoints(t *testing.T) {
	const script = `{"schema":"s0-script/v1","id":"D14","title":"Synthetic lifetime unit","covers":["unit"],"plugins":[{"id":"provider","apply":[{"op":"provide","key":"a","value":1}]},{"id":"consumer","needs":{"a":null},"apply":[]}],"steps":[{"op":"mount","fiber":"p","plugin":"provider","config":{}},{"op":"mount","fiber":"c","plugin":"consumer","config":{}},{"op":"settle","label":"old"},{"op":"update","fiber":"p","config":{}},{"op":"settle","label":"new"}]}`
	makeTrace := func() *Trace {
		tr := &Trace{Schema: TraceSchema, Scenario: "D14", Events: []Event{
			causalUnitEvent("plugin", map[string]any{"fiber": "p", "disposed": false}),
			causalUnitEvent("plugin", map[string]any{"fiber": "c", "disposed": false}),
			causalUnitStatus("p", "PENDING", "LOADING"), causalUnitFiber("apply", "p"), causalUnitStatus("p", "LOADING", "ACTIVE"),
			causalUnitStatus("c", "PENDING", "LOADING"), causalUnitFiber("apply", "c"), causalUnitStatus("c", "LOADING", "ACTIVE"), causalUnitService("a", 1),
			causalUnitFiber("commit", "p"), causalUnitStatus("p", "ACTIVE", "UNLOADING"), causalUnitStatus("c", "ACTIVE", "UNLOADING"), causalUnitStatus("c", "UNLOADING", "PENDING"),
			causalUnitService("a", map[string]any{"$undefined": true}), causalUnitStatus("p", "UNLOADING", "LOADING"), causalUnitFiber("apply", "p"), causalUnitStatus("p", "LOADING", "ACTIVE"),
			causalUnitStatus("c", "PENDING", "LOADING"), causalUnitFiber("apply", "c"), causalUnitStatus("c", "LOADING", "ACTIVE"), causalUnitService("a", 1),
		}, Checkpoints: []Checkpoint{}}
		for i, label := range []string{"old", "new"} {
			uid := fmt.Sprintf("a:%d", i)
			tr.Checkpoints = append(tr.Checkpoints, Checkpoint{Label: label, States: []FiberSnapshot{
				{Fiber: "p", State: "ACTIVE", FiberUID: "p-id", Revision: i, Epoch: &Epoch{Revision: i, Needs: []Binding{}}, RawConfig: map[string]any{}, EffectiveConfig: map[string]any{}},
				{Fiber: "c", State: "ACTIVE", FiberUID: "c-id", Epoch: &Epoch{Needs: []Binding{{Key: "a", ProvisionUID: uid}}}, RawConfig: map[string]any{}, EffectiveConfig: map[string]any{}},
			}, Provisions: []ProvisionSnapshot{{Key: "a", Realm: "root:a", Provider: "p", ProvisionUID: uid, Value: 1, Active: true}}, Outputs: map[string]any{}})
		}
		return tr
	}
	compare := func(left, right *Trace) error {
		lb, rb := causalUnitJSON(t, left), causalUnitJSON(t, right)
		cuts := []CausalCut{{"old", 9}, {"new", 21}}
		return CompareCausal([]byte(script), lb, rb, causalUnitWitness([]byte(script), lb, cuts...), causalUnitWitness([]byte(script), rb, cuts...))
	}
	if err := compare(makeTrace(), makeTrace()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Trace){
		"early-provider-removal": func(tr *Trace) { tr.Events[12], tr.Events[13] = tr.Events[13], tr.Events[12] },
		"epoch-reuse": func(tr *Trace) {
			tr.Checkpoints[1].Provisions[0].ProvisionUID = "a:0"
			tr.Checkpoints[1].States[1].Epoch.Needs[0].ProvisionUID = "a:0"
		},
		"stale-binding":           func(tr *Trace) { tr.Checkpoints[1].States[1].Epoch.Needs[0].ProvisionUID = "a:0" },
		"fiber-change":            func(tr *Trace) { tr.Checkpoints[1].States[0].FiberUID = "new-fiber" },
		"old-uid-alias-new-owner": func(tr *Trace) { tr.Checkpoints[1].States[1].FiberUID = "p-id" },
		"restart-revision-erased": func(tr *Trace) {
			tr.Checkpoints[1].States[0].Revision = 0
			tr.Checkpoints[1].States[0].Epoch.Revision = 0
		},
		"missing-restart-status": func(tr *Trace) { tr.Events = append(tr.Events[:10], tr.Events[11:]...) },
	} {
		t.Run(name, func(t *testing.T) {
			tr := makeTrace()
			mutate(tr)
			if err := compare(makeTrace(), tr); err == nil {
				t.Fatal("accepted lifetime/epoch corruption")
			}
		})
	}
	// Bijective opaque renaming across every checkpoint remains allowed.
	right := makeTrace()
	for ci := range right.Checkpoints {
		cp := &right.Checkpoints[ci]
		cp.States[0].FiberUID = 101
		cp.States[1].FiberUID = 102
		cp.Provisions[0].ProvisionUID = 200 + ci
		cp.States[1].Epoch.Needs[0].ProvisionUID = 200 + ci
	}
	if err := compare(makeTrace(), right); err != nil {
		t.Fatal(err)
	}
}

func TestCausalRejectsUIDChangeWithoutActivation(t *testing.T) {
	script := []byte(strings.Replace(causalUnitScript, `{"op":"settle","label":"ready"}`, `{"op":"settle","label":"ready"},{"op":"snapshot","label":"same"}`, 1))
	makeTrace := func() *Trace {
		tr := causalUnitTrace()
		data, _ := json.Marshal(tr.Checkpoints[0])
		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			t.Fatal(err)
		}
		cp.Label = "same"
		tr.Checkpoints = append(tr.Checkpoints, cp)
		return tr
	}
	left, right := makeTrace(), makeTrace()
	if err := causalUnitCompare(t, script, left, right); err != nil {
		t.Fatal(err)
	}
	right.Checkpoints[1].Provisions[0].ProvisionUID = "silently-new"
	for si := range right.Checkpoints[1].States {
		s := &right.Checkpoints[1].States[si]
		for ni := range s.Epoch.Needs {
			if s.Epoch.Needs[ni].Key == "a" {
				s.Epoch.Needs[ni].ProvisionUID = "silently-new"
			}
		}
	}
	if err := causalUnitCompare(t, script, left, right); err == nil {
		t.Fatal("opaque UID change without activation accepted")
	}
}

func TestCausalDoesNotUseScriptExpectations(t *testing.T) {
	script := []byte(strings.TrimSuffix(causalUnitScript, "}") + `,"expect":[{"at":"ready","path":"/outputs/left","equals":"deliberately-wrong"}]}`)
	if err := causalUnitCompare(t, script, causalUnitTrace(), causalUnitTrace()); err != nil {
		t.Fatalf("assertions influenced comparator: %v", err)
	}
}

func TestCausalStaticRealmsAndCrossRealmBindings(t *testing.T) {
	const script = `{"schema":"s0-script/v1","id":"D02","title":"Synthetic ordinary realm unit","covers":["unit"],"contexts":[{"id":"outer","parent":"root","isolate":{"key":"svc","label":"group"}},{"id":"a","parent":"outer"},{"id":"b","parent":"root","isolate":{"key":"svc"}}],"plugins":[{"id":"provider","apply":[{"op":"provide","key":"svc","value":1}]},{"id":"consumer","needs":{"svc":null},"apply":[]}],"steps":[{"op":"mount","context":"a","fiber":"pa","plugin":"provider","config":{}},{"op":"mount","context":"b","fiber":"pb","plugin":"provider","config":{}},{"op":"mount","context":"a","fiber":"ca","plugin":"consumer","config":{}},{"op":"settle","label":"ready"}]}`
	makeTrace := func() *Trace {
		tr := &Trace{Schema: TraceSchema, Scenario: "D02", Events: []Event{}, Checkpoints: []Checkpoint{}}
		for _, fiber := range []string{"pa", "pb", "ca"} {
			tr.Events = append(tr.Events, causalUnitEvent("plugin", map[string]any{"fiber": fiber, "disposed": false}))
		}
		for _, fiber := range []string{"pa", "pb", "ca"} {
			tr.Events = append(tr.Events, causalUnitStatus(fiber, "PENDING", "LOADING"), causalUnitFiber("apply", fiber), causalUnitStatus(fiber, "LOADING", "ACTIVE"))
		}
		tr.Events = append(tr.Events, causalUnitEvent("service", map[string]any{"key": "svc", "realm": "svc:group", "value": 1}), causalUnitEvent("service", map[string]any{"key": "svc", "realm": "svc:@b", "value": 1}))
		cp := Checkpoint{Label: "ready", States: []FiberSnapshot{}, Provisions: []ProvisionSnapshot{
			{Key: "svc", Realm: "svc:group", Provider: "pa", ProvisionUID: "ua", Value: 1, Active: true}, {Key: "svc", Realm: "svc:@b", Provider: "pb", ProvisionUID: "ub", Value: 1, Active: true},
		}, Outputs: map[string]any{}}
		for _, fiber := range []string{"pa", "pb", "ca"} {
			needs := []Binding{}
			if fiber == "ca" {
				needs = append(needs, Binding{Key: "svc", ProvisionUID: "ua"})
			}
			cp.States = append(cp.States, FiberSnapshot{Fiber: fiber, State: "ACTIVE", FiberUID: fiber, Epoch: &Epoch{Needs: needs}, RawConfig: map[string]any{}, EffectiveConfig: map[string]any{}})
		}
		tr.Checkpoints = append(tr.Checkpoints, cp)
		return tr
	}
	left, right := makeTrace(), makeTrace()
	p := right.Checkpoints[0].Provisions
	p[0], p[1] = p[1], p[0]
	if err := causalUnitCompare(t, []byte(script), left, right); err != nil {
		t.Fatal(err)
	}
	right = makeTrace()
	right.Checkpoints[0].States[2].Epoch.Needs[0].ProvisionUID = "ub"
	if err := causalUnitCompare(t, []byte(script), left, right); err == nil {
		t.Fatal("equal values hid cross-realm binding")
	}
}

func TestCausalOrderedNeedsECMAScriptKeys(t *testing.T) {
	keys, err := causalOrderedNeeds([]byte(`{"plugins":[{"id":"p","needs":{"z":null,"10":null,"2":null,"01":null,"a":null}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !causalEqual(keys["p"], []string{"2", "10", "z", "01", "a"}) {
		t.Fatalf("lost JSON/ECMAScript key order: %v", keys["p"])
	}
}

func TestCausalStructuralBuiltinRenameOnly(t *testing.T) {
	script := []byte(strings.ReplaceAll(causalUnitScript, `"a"`, `"events"`))
	left, right := causalUnitTrace(), causalUnitTrace()
	left.Events[10].Data["key"], left.Events[10].Data["realm"] = "events", "root:events"
	right.Events[10].Data["key"], right.Events[10].Data["realm"] = "hooks", "root:hooks"
	for _, tr := range []*Trace{left, right} {
		for si := range tr.Checkpoints[0].States {
			for ni := range tr.Checkpoints[0].States[si].Epoch.Needs {
				b := &tr.Checkpoints[0].States[si].Epoch.Needs[ni]
				if b.Key == "a" {
					b.Key = "events"
				}
			}
		}
		tr.Checkpoints[0].Provisions[0].Key = "events"
		tr.Checkpoints[0].Provisions[0].Realm = "root:events"
		payload := map[string]any{"events": "unchanged", "timestamp": 1, "provisionUID": 5}
		tr.Events[13].Data["value"] = payload
		tr.Checkpoints[0].Outputs["left"] = payload
	}
	right.Checkpoints[0].Provisions[0].Key = "hooks"
	right.Checkpoints[0].Provisions[0].Realm = "root:hooks"
	if err := causalUnitCompare(t, script, left, right); err != nil {
		t.Fatal(err)
	}
}

func TestCausalRequiresProtocolSequenceAndWholeStream(t *testing.T) {
	script := []byte(causalUnitScript)
	trace := causalUnitTrace()
	raw := causalUnitJSON(t, trace)
	w := causalUnitWitness(script, raw, CausalCut{"ready", len(trace.Events)})
	trace.Events[1].Seq = 90
	bad, _ := json.Marshal(trace)
	if err := CompareCausal(script, raw, bad, w, causalUnitWitness(script, bad, CausalCut{"ready", len(trace.Events)})); err == nil {
		t.Fatal("ignored raw sequence counter")
	}
	trace = causalUnitTrace()
	trace.Events = append(trace.Events, causalUnitFiber("config", "l"))
	bad = causalUnitJSON(t, trace)
	if err := CompareCausal(script, raw, bad, w, causalUnitWitness(script, bad, CausalCut{"ready", len(trace.Events) - 1})); err == nil {
		t.Fatal("discarded event tail after last checkpoint")
	}
}

func TestCausalRejectsUnprovedControlsAndStaticCycles(t *testing.T) {
	for name, script := range map[string]string{
		"async-update":       strings.Replace(causalUnitScript, `{"op":"settle","label":"ready"}`, `{"op":"update","fiber":"p","await":false,"task":"u"},{"op":"settle","label":"ready"}`, 1),
		"count-metadata":     strings.Replace(causalUnitScript, `"op":"provide"`, `"op":"provide","count":2`, 1),
		"available-service":  strings.Replace(causalUnitScript, `"op":"provide"`, `"op":"provide","mode":"available"`, 1),
		"dynamic-context":    strings.Replace(causalUnitScript, `"steps":[`, `"steps":[{"op":"context","to":"dynamic"},`, 1),
		"unused-gate":        strings.Replace(causalUnitScript, `"plugins":[`, `"plugins":[{"id":"unused","apply":[{"op":"gate","label":"x"}]},`, 1),
		"root-snapshot-race": strings.Replace(causalUnitScript, `"op":"settle"`, `"op":"snapshot"`, 1),
		"duplicate-provider": strings.Replace(causalUnitScript, `"key":"c","value"`, `"key":"b","value"`, 1),
		"needs-cycle":        strings.Replace(causalUnitScript, `"id":"provider","apply"`, `"id":"provider","needs":{"b":null},"apply"`, 1),
		"runtime-meta":       strings.Replace(causalUnitScript, `"$output":"right"`, `"$meta":"fiber.uid"`, 1),
		"ambiguous-output":   strings.Replace(causalUnitScript, `"to":"right"`, `"to":"left"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := compileCausalModel([]byte(script)); !errors.Is(err, ErrCausalUnsupported) {
				t.Fatalf("want explicit unsupported, got %v", err)
			}
		})
	}
}

func causalUnitMinimalLifecycle(op string) ([]byte, *Trace, []CausalCut) {
	steps := `{"op":"mount","fiber":"f","plugin":"empty","config":{}},{"op":"settle","label":"old"}`
	if op != "" {
		steps += `,{"op":"` + op + `","fiber":"f","config":{}},{"op":"settle","label":"new"}`
	}
	script := []byte(`{"schema":"s0-script/v1","id":"D14","title":"Malformed identical stream control","covers":["unit"],"plugins":[{"id":"empty","apply":[]}],"steps":[` + steps + `]}`)
	tr := &Trace{Schema: TraceSchema, Scenario: "D14", Events: []Event{
		causalUnitEvent("plugin", map[string]any{"fiber": "f", "disposed": false}), causalUnitStatus("f", "PENDING", "LOADING"), causalUnitFiber("apply", "f"), causalUnitStatus("f", "LOADING", "ACTIVE"),
	}, Checkpoints: []Checkpoint{}}
	labels := []string{"old"}
	if op != "" {
		labels = append(labels, "new")
	}
	cuts := []CausalCut{}
	for _, label := range labels {
		tr.Checkpoints = append(tr.Checkpoints, Checkpoint{Label: label, States: []FiberSnapshot{{Fiber: "f", State: "ACTIVE", FiberUID: "f-id", Epoch: &Epoch{Needs: []Binding{}}, RawConfig: map[string]any{}, EffectiveConfig: map[string]any{}}}, Provisions: []ProvisionSnapshot{}, Outputs: map[string]any{}})
		cuts = append(cuts, CausalCut{label, 4})
	}
	return script, tr, cuts
}

func TestCausalIdenticalMalformedApplyBeforeLoadingRejected(t *testing.T) {
	script, tr, cuts := causalUnitMinimalLifecycle("")
	tr.Events[1], tr.Events[2] = tr.Events[2], tr.Events[1]
	raw := causalUnitJSON(t, tr)
	w := causalUnitWitness(script, raw, cuts...)
	if err := CompareCausal(script, raw, raw, w, w); err == nil {
		t.Fatal("FALSE GREEN: identical Apply-before-LOADING traces accepted")
	}
}

func TestCausalLoadingFrameFailureWithoutApply(t *testing.T) {
	script, tr, cuts := causalUnitMinimalLifecycle("")
	tr.Events = []Event{tr.Events[0], tr.Events[1], causalUnitEvent("error", map[string]any{"fiber": "f", "error": map[string]any{"name": "Error", "message": "config-resolution-failed"}}), causalUnitStatus("f", "LOADING", "UNLOADING"), causalUnitStatus("f", "UNLOADING", "FAILED")}
	tr.Checkpoints[0].States[0].State = "FAILED"
	tr.Checkpoints[0].States[0].Epoch = nil
	cuts[0].AfterEvent = len(tr.Events)
	raw := causalUnitJSON(t, tr)
	w := causalUnitWitness(script, raw, cuts...)
	if err := CompareCausal(script, raw, raw, w, w); err != nil {
		t.Fatalf("captured config failure before Apply must not require Apply: %v", err)
	}
	tr.Events = append(tr.Events[:2], tr.Events[3:]...)
	cuts[0].AfterEvent = len(tr.Events)
	raw = causalUnitJSON(t, tr)
	w = causalUnitWitness(script, raw, cuts...)
	if err := CompareCausal(script, raw, raw, w, w); err == nil {
		t.Fatal("FAILED without observed error accepted")
	}
	_, tr, cuts = causalUnitMinimalLifecycle("")
	tr.Events = append(tr.Events[:3], append([]Event{causalUnitFiber("apply", "f")}, tr.Events[3:]...)...)
	cuts[0].AfterEvent = len(tr.Events)
	raw = causalUnitJSON(t, tr)
	w = causalUnitWitness(script, raw, cuts...)
	if err := CompareCausal(script, raw, raw, w, w); err == nil {
		t.Fatal("duplicate Apply within one loading frame accepted")
	}
}

func TestCausalIdenticalMissingRootMutationRejected(t *testing.T) {
	for _, op := range []string{"update", "restart"} {
		t.Run(op, func(t *testing.T) {
			script, tr, cuts := causalUnitMinimalLifecycle(op)
			raw := causalUnitJSON(t, tr)
			w := causalUnitWitness(script, raw, cuts...)
			if err := CompareCausal(script, raw, raw, w, w); err == nil {
				t.Fatal("FALSE GREEN: root mutation omitted in both streams")
			}
		})
	}
}

func TestCausalIdenticalPrematureRemovalRejected(t *testing.T) {
	script := []byte(strings.Replace(causalUnitScript, `{"op":"settle","label":"ready"}`, `{"op":"settle","label":"ready"},{"op":"dispose","fiber":"p"},{"op":"settle","label":"pending"}`, 1))
	tr := causalUnitTrace()
	first := len(tr.Events)
	tr.Events = append(tr.Events, causalUnitEvent("plugin", map[string]any{"fiber": "p", "disposed": true}), causalUnitStatus("p", "ACTIVE", "UNLOADING"),
		causalUnitStatus("l", "ACTIVE", "UNLOADING"), causalUnitStatus("r", "ACTIVE", "UNLOADING"), causalUnitStatus("leaf", "ACTIVE", "UNLOADING"),
		causalUnitService("a", 1), causalUnitService("a", map[string]any{"$undefined": true}), // deletion overtakes retained consumer bindings
		causalUnitStatus("leaf", "UNLOADING", "PENDING"), causalUnitStatus("l", "UNLOADING", "PENDING"), causalUnitStatus("r", "UNLOADING", "PENDING"), causalUnitStatus("p", "UNLOADING", "DISPOSED"))
	data, _ := json.Marshal(tr.Checkpoints[0])
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		t.Fatal(err)
	}
	cp.Label = "pending"
	cp.Provisions = []ProvisionSnapshot{}
	for i := range cp.States {
		s := &cp.States[i]
		s.State = "PENDING"
		s.Epoch = nil
		if s.Fiber == "p" {
			s.State = "DISPOSED"
			s.FiberUID = nil
		}
	}
	tr.Checkpoints = append(tr.Checkpoints, cp)
	raw := causalUnitJSON(t, tr)
	w := causalUnitWitness(script, raw, CausalCut{"ready", first}, CausalCut{"pending", len(tr.Events)})
	if err := CompareCausal(script, raw, raw, w, w); err == nil {
		t.Fatal("FALSE GREEN: provider deletion before retained consumers settle accepted")
	}
}

func TestCausalIdenticalPublishedStoreMismatchRejected(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprint("missing=", missing), func(t *testing.T) {
			script, tr, cuts := causalUnitMinimalLifecycle("")
			script = []byte(strings.Replace(string(script), `"apply":[]`, `"apply":[{"op":"provide","key":"a","value":1}]`, 1))
			tr.Events = append(tr.Events, causalUnitService("a", 1))
			cuts[0].AfterEvent++
			if !missing {
				tr.Checkpoints[0].Provisions = []ProvisionSnapshot{{Key: "a", Realm: "root:a", Provider: "f", ProvisionUID: "p-id", Value: 2, Active: true}}
			}
			raw := causalUnitJSON(t, tr)
			w := causalUnitWitness(script, raw, cuts...)
			if err := CompareCausal(script, raw, raw, w, w); err == nil {
				t.Fatal("FALSE GREEN: published provision/store prefix contradiction accepted")
			}
		})
	}
}

func TestCausalFirstInvalidatingProviderMayDiffer(t *testing.T) {
	script := []byte(strings.Replace(causalUnitScript, `{"op":"settle","label":"ready"}`, `{"op":"settle","label":"ready"},{"op":"dispose","fiber":"p"},{"op":"settle","label":"pending"}`, 1))
	makeTrace := func(rightFirst bool) *Trace {
		tr := causalUnitTrace()
		first, second, key1, key2 := "l", "r", "b", "c"
		if rightFirst {
			first, second, key1, key2 = "r", "l", "c", "b"
		}
		tr.Events = append(tr.Events, causalUnitEvent("plugin", map[string]any{"fiber": "p", "disposed": true}), causalUnitStatus("p", "ACTIVE", "UNLOADING"),
			causalUnitStatus(first, "ACTIVE", "UNLOADING"), causalUnitStatus("leaf", "ACTIVE", "UNLOADING"), causalUnitService(key1, 1), causalUnitStatus(second, "ACTIVE", "UNLOADING"), causalUnitService(key2, 1), causalUnitService("a", 1),
			causalUnitStatus("leaf", "UNLOADING", "PENDING"), causalUnitService("b", map[string]any{"$undefined": true}), causalUnitService("c", map[string]any{"$undefined": true}),
			causalUnitStatus("l", "UNLOADING", "PENDING"), causalUnitStatus("r", "UNLOADING", "PENDING"), causalUnitService("a", map[string]any{"$undefined": true}), causalUnitStatus("p", "UNLOADING", "DISPOSED"))
		data, _ := json.Marshal(tr.Checkpoints[0])
		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			t.Fatal(err)
		}
		cp.Label = "pending"
		cp.Provisions = []ProvisionSnapshot{}
		for i := range cp.States {
			s := &cp.States[i]
			s.State = "PENDING"
			s.Epoch = nil
			if s.Fiber == "p" {
				s.State = "DISPOSED"
				s.FiberUID = nil
			}
		}
		tr.Checkpoints = append(tr.Checkpoints, cp)
		return tr
	}
	compare := func(script []byte, left, right *Trace) error {
		lb, rb := causalUnitJSON(t, left), causalUnitJSON(t, right)
		return CompareCausal(script, lb, rb, causalUnitWitness(script, lb, CausalCut{"ready", 27}, CausalCut{"pending", len(left.Events)}), causalUnitWitness(script, rb, CausalCut{"ready", 27}, CausalCut{"pending", len(right.Events)}))
	}
	if err := compare(script, makeTrace(false), makeTrace(true)); err != nil {
		t.Fatal(err)
	}
	shared := []byte(strings.Replace(string(script), `"op":"read","key":"c","to":"leaf-right"`, `"op":"output","to":"leaf-right","value":{"$output":"right"}`, 1))
	if err := compare(shared, makeTrace(false), makeTrace(true)); err == nil {
		t.Fatal("teardown exception erased separate shared output conflict")
	}
	for name, mutate := range map[string]func(*Trace){
		"consumer-before-exact-bound-invalidator":   func(tr *Trace) { tr.Events[29], tr.Events[30] = tr.Events[30], tr.Events[29] },
		"retained-notification-before-consumer":     func(tr *Trace) { tr.Events[30], tr.Events[31] = tr.Events[31], tr.Events[30] },
		"old-removal-before-stable-consumer":        func(tr *Trace) { tr.Events[35], tr.Events[36] = tr.Events[36], tr.Events[35] },
		"wrong-provider-cause":                      func(tr *Trace) { causalUnitMoveEvent(tr, 30, 29); causalUnitMoveEvent(tr, 29, 28) },
		"missing-own-root-publication":              func(tr *Trace) { tr.Events = append(tr.Events[:27], tr.Events[28:]...) },
		"unmatched-command-cannot-explain-teardown": func(tr *Trace) { tr.Events[27] = causalUnitFiber("commit", "p") },
	} {
		t.Run(name, func(t *testing.T) {
			bad, other := makeTrace(false), makeTrace(false)
			mutate(bad)
			mutate(other)
			if err := compare(script, makeTrace(false), bad); err == nil {
				t.Fatal("one-sided invalid teardown accepted")
			}
			if err := compare(script, bad, other); err == nil {
				t.Fatal("identical invalid teardown accepted")
			}
		})
	}
}

func TestCausalRecordedStaticModelScope(t *testing.T) {
	// Compile scripts only: these checks do not invent historical checkpoint
	// positions or upgrade frozen raw traces into causal execution evidence.
	for _, id := range []string{"D02", "D10", "D12", "D14", "D16", "D19", "D27"} {
		t.Run(id, func(t *testing.T) {
			raw, err := os.ReadFile("corpus/" + id + ".json")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := compileCausalModel(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, id := range []string{"D15", "D17", "D18", "D20", "D25", "D26", "D28", "D29", "D30"} {
		t.Run(id, func(t *testing.T) {
			raw, err := os.ReadFile("corpus/" + id + ".json")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := compileCausalModel(raw); !errors.Is(err, ErrCausalUnsupported) {
				t.Fatalf("want explicit unsupported, got %v", err)
			}
		})
	}
}
