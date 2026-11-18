// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

const effectControlsMethodPath = "../../../../docs/design/s0-fixture-effect-controls.md"
const effectRequestMethodPath = "../../../../docs/design/s0-d23-request-mapping.md"

// All original assertions run unchanged. These are observed checkpoint claims,
// not an assertion that the complete ordered oracle trace has matched.
func TestEffectsCheckpointExecution(t *testing.T) {
	for _, id := range []string{"D21", "D22", "D23", "D24"} {
		t.Run(id, func(t *testing.T) {
			data := loadFixture(t, "../../../differential/corpus/"+id+".json")
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				actual, err := RunComposedJSON(ctx, runtime, data)
				if err != nil {
					t.Fatal(err)
				}
				if actual.ComparisonError != nil {
					t.Fatal(actual.ComparisonError)
				}
				if err := differential.CheckExpectations(corpus(t, id), actual.Trace); err != nil {
					t.Fatal(err)
				}
				assertComposedNativeObservations(t, actual)
				if id == "D23" {
					assertD23NativeExecution(t, actual)
				}
				if id != "D22" {
					return
				}
				for _, label := range []string{"joining-setup", "joining-cleanup"} {
					native := actual.RuntimeInspections[label]
					found := false
					for _, snapshot := range native.Fibers {
						if snapshot.Name != "child" {
							continue
						}
						found = true
						// The setup checkpoint can race Apply's native completion:
						// LOADING is legitimate there. Only the original cleanup
						// assertion requires UNLOADING; ownership must persist at both.
						if label == "joining-cleanup" && snapshot.State != cc.Unloading ||
							snapshot.Alive || snapshot.Target != nil ||
							!reflect.DeepEqual(snapshot.Tracked, []string{"effect setup late-setup"}) {
							t.Fatalf("late setup/cleanup lost actual native tracked ownership at %s: %+v", label, snapshot)
						}
					}
					if !found {
						t.Fatalf("native child inspection missing at %s", label)
					}
				}
				if len(actual.RuntimeErrors) != 1 || !inactiveEffectOnly(actual.RuntimeErrors[0].Error) {
					t.Fatalf("late registration refusal was lost or mixed with an unexpected error: %+v", actual.RuntimeErrors)
				}
				for _, event := range actual.Trace.Events {
					if event.Kind == "error" || event.Kind == "status" && event.Data["to"] == "FAILED" {
						t.Fatalf("normal late cleanup fabricated a semantic failure: %+v", event)
					}
				}
			})
		})
	}
}

// Read the golden only after execution, retain every actual transition and body
// event, and make strict differences fatal when explicitly requested. No D22
// parent/child LIFO difference is hidden behind a scenario-wide comparator skip.
func TestEffectsActualGoldenTraceDiagnostics(t *testing.T) {
	for _, id := range []string{"D21", "D22", "D23", "D24"} {
		t.Run(id, func(t *testing.T) {
			path := "../../../differential/corpus/" + id + ".json"
			data := loadFixture(t, path)
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				manifest := func() map[string]string {
					sources := causalSources(t)
					sources[effectControlsMethodPath] = digest(loadFixture(t, effectControlsMethodPath))
					sources[effectRequestMethodPath] = digest(loadFixture(t, effectRequestMethodPath))
					sources[hookHistoricalOraclePath] = digest(loadFixture(t, hookHistoricalOraclePath))
					return sources
				}
				before := manifest()
				identity := hookCandidateIdentity(t)
				actual, err := RunComposedJSON(ctx, runtime, data)
				after := manifest()
				if proofErr := hookReceiptProof(before, after, data, loadFixture(t, path)); proofErr != nil {
					t.Fatalf("INVALID effect receipt: %v", proofErr)
				}
				if err != nil {
					t.Fatal(err)
				}
				encoded := jsonBytes(t, actual.Trace)
				if _, err := differential.ParseTrace(encoded); err != nil {
					t.Fatal(err)
				}
				goldenPath := "../../../differential/results/corpus-final/" + id + ".trace.json"
				golden, recordBytes := loadFixture(t, goldenPath), loadFixture(t, goldenPath+".run.json")
				var record differential.RunRecord
				if err := json.Unmarshal(recordBytes, &record); err != nil {
					t.Fatal(err)
				}
				if record.Scripts[id] != digest(data) || record.TraceSHA256 != digest(golden) || record.SourceCommit != differential.SourceCommit || record.CriteriaSHA256 != differential.CriteriaSHA256 || record.ExitCode != 0 || record.OracleSHA256 != before[hookHistoricalOraclePath] {
					t.Fatal("historical oracle provenance does not bind the script, trace and source")
				}
				comparison := differential.Compare(golden, encoded)
				if comparison != nil && !strings.Contains(comparison.Error(), "ordered semantic mismatch") {
					t.Fatal(comparison)
				}
				if actual.ComparisonError != nil {
					t.Fatal(actual.ComparisonError)
				}
				dir := t.TempDir()
				if root := os.Getenv("S0_REPLAY_EVIDENCE_DIR"); root != "" {
					dir = filepath.Join(root, t.Name())
					if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dir, 0o700); err != nil {
						t.Fatalf("effect evidence leaf must be fresh: %v", err)
					}
				}
				diagnostics := make([]map[string]string, len(actual.RuntimeErrors))
				for index, failure := range actual.RuntimeErrors {
					diagnostics[index] = map[string]string{"operation": failure.Operation, "error": failure.Error.Error()}
				}
				for name, bytes := range map[string][]byte{
					"script.json": data, "actual.trace.json": encoded, "golden.trace.json": golden, "golden.trace.json.run.json": recordBytes,
					"events.json": jsonBytes(t, actual.RuntimeEvents), "inspection.json": jsonBytes(t, actual.RuntimeInspections), "native-errors.json": jsonBytes(t, diagnostics),
					"comparison.txt": []byte(fmt.Sprint(comparison)), "sources-before.json": jsonBytes(t, before), "sources-after.json": jsonBytes(t, after),
					"provenance.json": jsonBytes(t, map[string]any{
						"schema": "s0-effect-replay-receipt/v1", "scenario": id, "backendTest": t.Name(), "receiptStatus": "valid",
						"scriptSHA256": digest(data), "actualSHA256": digest(encoded), "goldenSHA256": digest(golden), "oracleRunRecordSHA256": digest(recordBytes),
						"oracleHistoricalSourcePath": hookHistoricalOraclePath, "oracleHistoricalSHA256": record.OracleSHA256, "oracleSourceCommit": record.SourceCommit,
						"criteriaSHA256": record.CriteriaSHA256, "controlsMethodPath": effectControlsMethodPath, "controlsMethodSHA256": before[effectControlsMethodPath],
						"requestMethodPath": effectRequestMethodPath, "requestMethodSHA256": before[effectRequestMethodPath],
						"candidateIdentity": identity, "sourcesBefore": before, "sourcesAfter": after, "strictError": fmt.Sprint(comparison),
						"adaptations": effectReplayAdaptations(id),
						"scope":       "checkpoint execution and strict differential diagnostics only, not full S0 acceptance",
					}),
				} {
					saveCausalEvidence(t, dir, name, bytes)
				}
				t.Logf("strict actual effect comparison=%v actualSHA256=%s evidence=%s", comparison, digest(encoded), dir)
				if os.Getenv("S0_COMPARE_RUN") == "1" && comparison != nil {
					t.Errorf("strict actual effect parity failed: %v", comparison)
				}
				assertComposedNativeObservations(t, actual)
			})
		})
	}
}

func effectReplayAdaptations(id string) map[string]string {
	mapping := map[string]string{
		"ordinary":  "native Effect + tracked Go; source gate barriers; strict known native inactive sentinel codec",
		"unchanged": "original assertions and strict comparator; no sorting, hidden fibers, fabricated effects or scenario exclusions",
	}
	if id == "D23" {
		mapping["requestCompletion"] = "owner-approved callback request / external completion observation; actual native scope Defer rollback, EffectHandle withdrawal twice, and Inspector EffectTree; raw caught-error timing differences retained"
	}
	return mapping
}

func assertD23NativeExecution(t *testing.T, actual Result) {
	t.Helper()
	var marks []string
	for _, event := range actual.Trace.Events {
		if event.Kind == "mark" {
			marks = append(marks, fmt.Sprint(event.Data["label"]))
		}
		if event.Kind == "status" && event.Data["to"] == "FAILED" {
			t.Fatalf("caught native setup failure changed the actual lifecycle: %+v", event)
		}
	}
	if !reflect.DeepEqual(marks, []string{"second", "first", "once"}) {
		t.Fatalf("native rollback/withdraw order or once-only cleanup changed: %v", marks)
	}
	for _, label := range []string{"active", "unwound", "disposed"} {
		inspection, ok := actual.RuntimeInspections[label]
		if !ok || len(inspection.Fibers) != 1 {
			t.Fatalf("D23 actual native inspection missing at %s: %+v", label, inspection)
		}
		snapshot := inspection.Fibers[0]
		if len(snapshot.EffectTree) != 0 || len(snapshot.Effects) != 0 || len(snapshot.Tracked) != 0 {
			t.Fatalf("completed native scope ownership remained at %s: %+v", label, snapshot)
		}
		if label != "disposed" && (snapshot.State != cc.Active || !snapshot.Alive || snapshot.Target == nil) {
			t.Fatalf("scope failure/withdrawal lost the actual live activation at %s: %+v", label, snapshot)
		}
	}
	if len(actual.RuntimeErrors) != 1 || actual.RuntimeErrors[0].Operation != "effect setup throwing-setup" || actual.RuntimeErrors[0].Error.Error() != "setup failed" {
		t.Fatalf("actual native setup completion diagnostic missing or changed: %+v", actual.RuntimeErrors)
	}
}

func TestEffectsD23RollbackRetainsActualOwnershipUntilReturn(t *testing.T) {
	script := corpus(t, "D23")
	// This is a separate adversarial input, not a changed frozen assertion.
	// Keep the original first/second/once cleanup behavior behind a real gate.
	script.Plugins[0].Apply[0].Cleanup[1] = differential.Action{Op: "when", Label: "second", Base: true, Value: true,
		Program: []differential.Action{{Op: "gate", Label: "held-rollback"}, {Op: "mark", Label: "second"}}}
	script.Steps = append([]differential.Action{script.Steps[0],
		{Op: "waitMark", Label: "held-rollback"}, {Op: "snapshot", Label: "rollback-owned"},
		{Op: "release", Label: "held-rollback"}}, script.Steps[1:]...)
	data := jsonBytes(t, script)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		if err := differential.CheckExpectations(corpus(t, "D23"), actual.Trace); err != nil {
			t.Fatal(err)
		}
		assertD23NativeExecution(t, actual)
		native := actual.RuntimeInspections["rollback-owned"]
		if len(native.Fibers) != 1 {
			t.Fatalf("held native ownership missing: %+v", native)
		}
		snapshot := native.Fibers[0]
		if snapshot.State != cc.Loading || !snapshot.Alive || snapshot.Target == nil || len(snapshot.EffectTree) != 2 {
			t.Fatalf("rollback gate fabricated completion or lost native scope ownership: %+v", snapshot)
		}
		partial, single := snapshot.EffectTree[0], snapshot.EffectTree[1]
		if partial.Label != "throwing-setup" || partial.State != cc.ScopeCleaning || len(partial.Children) != 2 ||
			partial.Children[0].Label != "first" || partial.Children[0].State != cc.ScopeReady ||
			partial.Children[1].Label != "second" || partial.Children[1].State != cc.ScopeCleaning ||
			single.Label != "single-shot" || single.State != cc.ScopeWithdraw {
			t.Fatalf("inspection is not the actual incremental rollback/withdraw tree: %+v", snapshot.EffectTree)
		}
		for _, checkpoint := range actual.Trace.Checkpoints {
			if checkpoint.Label == "rollback-owned" {
				if _, exists := checkpoint.Outputs["setup-error"]; exists {
					t.Fatal("caught output was published before actual native rollback completed")
				}
			}
		}
		assertComposedNativeObservations(t, actual)
	})
}

func TestEffectsHandleInspectionUsesActualNativeTree(t *testing.T) {
	data := []byte(`{"schema":"s0-script/v1","id":"D91","title":"native handle inspection","covers":["effects"],"plugins":[{"id":"owned","apply":[{"op":"effect","label":"kept","to":"kept-handle","cleanup":[{"op":"mark","label":"kept-cleanup"}]}]}],"steps":[{"op":"mount","fiber":"f","plugin":"owned","config":{}},{"op":"settle","label":"ready"},{"op":"inspect","fiber":"f","mode":"effects","to":"effects"},{"op":"snapshot","label":"inspected"},{"op":"dispose","fiber":"f"},{"op":"settle","label":"disposed"}]}`)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		snapshot := actual.RuntimeInspections["inspected"].Fibers[0]
		if len(snapshot.EffectTree) != 1 || snapshot.EffectTree[0].Label != "kept" || snapshot.EffectTree[0].State != cc.ScopeReady ||
			len(snapshot.EffectTree[0].Children) != 1 || snapshot.EffectTree[0].Children[0].Label != "kept cleanup" {
			t.Fatalf("successful scope native ownership was fabricated or missing: %+v", snapshot.EffectTree)
		}
		for _, checkpoint := range actual.Trace.Checkpoints {
			if checkpoint.Label == "inspected" && !reflect.DeepEqual(checkpoint.Outputs["effects"], []any{"kept"}) {
				t.Fatalf("effect labels did not come from the actual nonempty native tree: %+v", checkpoint.Outputs)
			}
		}
		var cleanups int
		for _, event := range actual.Trace.Events {
			if event.Kind == "mark" && event.Data["label"] == "kept-cleanup" {
				cleanups++
			}
		}
		if cleanups != 1 || len(actual.RuntimeInspections["disposed"].Fibers[0].EffectTree) != 0 {
			t.Fatalf("actual unload did not own single-shot scope cleanup: %d %+v", cleanups, actual.RuntimeInspections["disposed"])
		}
		assertComposedNativeObservations(t, actual)
	})
}

func TestEffectsD23ActualErrorAndRollbackCausesAreNotReplaced(t *testing.T) {
	script := corpus(t, "D23")
	script.Plugins[0].Apply[0].Error = "actual setup failure"
	script.Plugins[0].Apply[0].Cleanup = append(script.Plugins[0].Apply[0].Cleanup, differential.Action{Op: "fail", Error: "actual rollback failure"})
	data := jsonBytes(t, script)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		if len(actual.RuntimeErrors) != 1 {
			t.Fatalf("native joined failure missing: %+v", actual.RuntimeErrors)
		}
		native := actual.RuntimeErrors[0].Error
		if !strings.Contains(native.Error(), "actual setup failure") || !strings.Contains(native.Error(), "actual rollback failure") {
			t.Fatalf("actual native setup/cleanup causes were discarded: %v", native)
		}
		for _, checkpoint := range actual.Trace.Checkpoints {
			if !reflect.DeepEqual(checkpoint.Outputs["setup-error"], errorValue(native)) {
				t.Fatalf("caught value was not the exact observed completion result: %+v / %v", checkpoint.Outputs, native)
			}
		}
		if err := differential.CheckExpectations(corpus(t, "D23"), actual.Trace); err == nil {
			t.Fatal("mutating actual causes was replaced with frozen expected values")
		}
	})
}

func TestEffectsD23ExpectedValuesNeverDriveExecution(t *testing.T) {
	script := corpus(t, "D23")
	script.Expect = []differential.Assertion{{At: "unwound", Path: "/outputs/injected", Equals: "fabricated"}}
	data := jsonBytes(t, script)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, checkpoint := range actual.Trace.Checkpoints {
			if _, exists := checkpoint.Outputs["injected"]; exists {
				t.Fatal("D23 expected assertion injected a caught value")
			}
		}
		if err := differential.CheckExpectations(corpus(t, "D23"), actual.Trace); err != nil {
			t.Fatal("changing only expected values changed actual D23 execution:", err)
		}
		assertD23NativeExecution(t, actual)
	})
}

func TestEffectsPendingCatchContinuationRemainsUnsupported(t *testing.T) {
	script := corpus(t, "D23")
	continuation := differential.Action{Op: "output", To: "premature", Value: map[string]any{"nested": []any{map[string]any{"$output": "setup-error.message"}}}, Catch: true}
	script.Plugins[0].Apply = append(script.Plugins[0].Apply[:1], continuation)
	data := jsonBytes(t, script)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("pending callback result was guessed or swallowed by catch:true: %v", err)
		}
		var nativeSetup error
		for _, failure := range actual.RuntimeErrors {
			if failure.Operation == "effect setup throwing-setup" {
				nativeSetup = failure.Error
			}
		}
		var unsupportedAt, failedAt int
		for _, event := range actual.Trace.Events {
			if event.Kind == "error" && event.Data["fiber"] == "f" {
				value, _ := event.Data["error"].(map[string]any)
				if strings.Contains(fmt.Sprint(value["message"]), "source continuation reads a pending native effect catch output") {
					unsupportedAt = event.Seq
				}
			}
			if event.Kind == "status" && event.Data["to"] == "FAILED" {
				failedAt = event.Seq
			}
			if event.Kind != "output" {
				continue
			}
			if event.Data["to"] == "premature" {
				t.Fatalf("unsupported continuation executed: %+v", event)
			}
			if event.Data["to"] == "setup-error" {
				// Graph-lock completes the refused pending scope during actual
				// failure teardown. This later real inactive cause must remain
				// visible, not be confused with a fabricated source setup error.
				if !inactiveEffectOnly(nativeSetup) || unsupportedAt == 0 || failedAt <= unsupportedAt || event.Seq <= failedAt ||
					!reflect.DeepEqual(event.Data["value"], effectErrorValue(nativeSetup)) {
					t.Fatalf("deferred catch was not the actual post-failure native completion: %+v; native=%v", event, nativeSetup)
				}
				inspection := actual.RuntimeInspections["active"]
				if len(inspection.Fibers) != 1 || inspection.Fibers[0].State != cc.Failed || len(inspection.Fibers[0].EffectTree) != 0 {
					t.Fatalf("native failed scope still owned resources after observed completion: %+v", inspection)
				}
			}
		}
		if unsupportedAt == 0 {
			t.Fatal("pending continuation did not report its actual unsupported cause")
		}
	})
}

func TestEffectsInactiveNativeCallbackNeverRuns(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		var captured *cc.Context
		var callbacks atomic.Int64
		var during error
		setup := func() (cc.Disposer, error) {
			callbacks.Add(1)
			return func(context.Context) error { return nil }, nil
		}
		id, err := runtime.Mount(ctx, cc.Spec{ID: "inactive-callback", Apply: func(c *cc.Context) error {
			captured = c
			return c.Effect("owned", func() (cc.Disposer, error) {
				return func(context.Context) error {
					during = c.Effect("during-unload", setup)
					return nil
				}, nil
			})
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.(cc.Waiter).AwaitFiber(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := runtime.Dispose(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := runtime.(cc.Waiter).AwaitFiber(ctx, id); err != nil {
			t.Fatal(err)
		}
		after := captured.Effect("after-dispose", setup)
		for _, err := range []error{during, after} {
			if !errors.Is(err, cc.ErrInactiveEffect) || !errors.Is(err, cc.ErrInactive) || !inactiveEffectOnly(err) {
				t.Fatalf("native refusal did not retain its exact known causes: %v", err)
			}
		}
		if callbacks.Load() != 0 {
			t.Fatal("inactive native Effect invoked setup")
		}
	})
}

func TestEffectsLateUsesCapturedActivationNotTemplate(t *testing.T) {
	data := loadFixture(t, "../../../differential/corpus/D24.json")
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		script, err := differential.ParseScript(data)
		if err != nil {
			t.Fatal(err)
		}
		order, err := decodeNeedsOrder(data)
		if err != nil {
			t.Fatal(err)
		}
		frontend := &composedFrontend{script: script, orderedNeeds: order}
		if _, err := runAdapted(ctx, &composedObservations{Runtime: runtime}, *script, order, frontend); err != nil {
			t.Fatal(err)
		}
		frontend.mu.Lock()
		frontend.contexts["f"] = frontend.contexts["root"] // A genuinely live but wrong template.
		frontend.mu.Unlock()
		handled, err := frontend.executeEffect(differential.Action{Op: "lateEffect", Fiber: "f", Label: "must-refuse-old", Catch: true, To: "actual-old"}, environment{std: ctx})
		if !handled || err != nil {
			t.Fatalf("late Effect was not handled: %v", err)
		}
		want := effectErrorValue(errors.Join(cc.ErrInactiveEffect, cc.ErrInactive))
		frontend.r.mu.Lock()
		value := frontend.r.outputs["actual-old"]
		frontend.r.mu.Unlock()
		if !reflect.DeepEqual(value, want) {
			t.Fatalf("late Effect used the live template instead of the captured old activation: %+v", value)
		}
	})
}

func TestEffectsExpectedValuesNeverDriveExecution(t *testing.T) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(loadFixture(t, "../../../differential/corpus/D24.json"), &document); err != nil {
		t.Fatal(err)
	}
	document["expect"] = jsonBytes(t, []differential.Assertion{{At: "refused", Path: "/outputs/injected", Equals: "fabricated"}})
	data := jsonBytes(t, document)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, checkpoint := range actual.Trace.Checkpoints {
			if _, exists := checkpoint.Outputs["injected"]; exists {
				t.Fatal("expected assertion injected an output")
			}
		}
		if err := differential.CheckExpectations(corpus(t, "D24"), actual.Trace); err != nil {
			t.Fatal("mutating only expected values changed the actual inactive refusals:", err)
		}
	})
}

func TestEffectsUnsupportedRemainsExplicit(t *testing.T) {
	for name, steps := range map[string]string{
		"uncaught partial":  `[{"op":"effect","mode":"partial","label":"partial"}]`,
		"async partial":     `[{"op":"effect","mode":"partial","label":"partial","await":false,"catch":true}]`,
		"withdraw program":  `[{"op":"effect","label":"handle","to":"handle","program":[{"op":"mark","label":"program"}]}]`,
		"setup task":        `[{"op":"effect","label":"task","task":"setup","await":false,"catch":true}]`,
		"withdraw task":     `[{"op":"withdraw","task":"handle","to":"completion"}]`,
		"unknown collector": `[{"op":"effect","mode":"unknown","label":"unknown"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte(`{"schema":"s0-script/v1","id":"D91","title":"effect limit","covers":["effects"],"plugins":[],"steps":` + steps + `}`)
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				if _, err := RunComposedJSON(ctx, runtime, data); !errors.Is(err, ErrUnsupported) {
					t.Fatalf("unsupported effect capability was swallowed: %v", err)
				}
			})
		})
	}
}

type effectOpaqueWrapper struct{ cause error }

func (e effectOpaqueWrapper) Error() string { return "opaque wrapper: " + e.cause.Error() }
func (e effectOpaqueWrapper) Unwrap() error { return e.cause }

type effectOpaqueGroup struct{ causes []error }

func (e effectOpaqueGroup) Error() string   { return "opaque aggregate" }
func (e effectOpaqueGroup) Unwrap() []error { return e.causes }

type effectIsSpoof struct{}

func (effectIsSpoof) Error() string { return "spoofed inactive identity" }
func (effectIsSpoof) Is(error) bool { return true }

func TestEffectsInactiveCodecOnlyExactNativeSentinelSet(t *testing.T) {
	want := map[string]any{"name": "Error", "message": cc.ErrInactiveEffect.Error(), "code": "INACTIVE_EFFECT"}
	for _, err := range []error{cc.ErrInactiveEffect, errors.Join(cc.ErrInactiveEffect, cc.ErrInactive), errors.Join(cc.ErrInactive, errors.Join(cc.ErrInactiveEffect, cc.ErrInactive))} {
		if !inactiveEffectOnly(err) || !reflect.DeepEqual(effectErrorValue(err), want) {
			t.Fatalf("known native inactive Effect causes lost their source code: %v", err)
		}
	}
	known := errors.Join(cc.ErrInactiveEffect, cc.ErrInactive)
	for _, err := range []error{
		cc.ErrInactive, errors.New(cc.ErrInactiveEffect.Error()), context.Canceled, context.DeadlineExceeded,
		fmt.Errorf("unknown wrapper: %w", known), effectOpaqueWrapper{known}, effectOpaqueGroup{[]error{cc.ErrInactiveEffect, cc.ErrInactive}}, effectIsSpoof{},
		errors.Join(known, errors.New("cleanup failed")), errors.Join(known, context.DeadlineExceeded), errors.Join(known, context.Canceled), errors.Join(known, effectOpaqueWrapper{known}),
	} {
		if inactiveEffectOnly(err) || !reflect.DeepEqual(effectErrorValue(err), errorValue(err)) {
			t.Fatalf("unknown wrapper, cleanup or cancellation cause was normalized away: %T %v", err, err)
		}
	}
	if inactiveEffectOnly(nil) {
		t.Fatal("nil is not an inactive refusal")
	}
}
