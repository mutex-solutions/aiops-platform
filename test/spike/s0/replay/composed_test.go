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
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// These assertions concern actual candidate observations. Passing an assertion is
// not a raw-oracle parity claim; the separate strict comparator records that.
func TestComposedContextCheckpointExecution(t *testing.T) {
	for _, id := range []string{"D01", "D02", "D03", "D04", "D07", "D08", "D09", "D20"} {
		t.Run(id, func(t *testing.T) {
			data := loadFixture(t, "../../../differential/corpus/"+id+".json")
			script := corpus(t, id)
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				result, err := RunComposedJSON(ctx, runtime, data)
				if err != nil {
					t.Fatal(err)
				}
				if result.ComparisonError != nil {
					t.Fatal(result.ComparisonError)
				}
				if err := differential.CheckExpectations(script, result.Trace); err != nil {
					t.Fatal(err)
				}
				assertComposedNativeObservations(t, result)
			})
		})
	}
}

func TestComposedActualGoldenTraceDiagnostics(t *testing.T) {
	for _, id := range []string{"D01", "D02", "D03", "D04", "D07", "D08", "D09", "D20"} {
		t.Run(id, func(t *testing.T) {
			data := loadFixture(t, "../../../differential/corpus/"+id+".json")
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				actual, err := RunComposedJSON(ctx, runtime, data)
				if err != nil {
					t.Fatal(err)
				}
				encoded := jsonBytes(t, actual.Trace)
				if _, err := differential.ParseTrace(encoded); err != nil {
					t.Fatalf("candidate observations are not a valid trace: %v", err)
				}
				// The golden is first read AFTER execution. Strict comparison does
				// not ignore candidate transitions, ordering, errors or identities.
				path := "../../../differential/results/corpus-final/" + id + ".trace.json"
				golden := loadFixture(t, path)
				var record differential.RunRecord
				if err := json.Unmarshal(loadFixture(t, path+".run.json"), &record); err != nil {
					t.Fatal(err)
				}
				if record.Scripts[actual.Trace.Scenario] != digest(data) || record.TraceSHA256 != digest(golden) ||
					record.SourceCommit != differential.SourceCommit || record.CriteriaSHA256 != differential.CriteriaSHA256 || record.ExitCode != 0 {
					t.Fatal("golden/script provenance changed")
				}
				comparison := differential.Compare(golden, encoded)
				if comparison != nil && !strings.Contains(comparison.Error(), "ordered semantic mismatch") {
					t.Fatal(comparison)
				}
				if actual.ComparisonError != nil {
					t.Fatalf("missing native observation capability: %v", actual.ComparisonError)
				}
				if os.Getenv("S0_COMPARE_RUN") == "1" && comparison != nil {
					t.Errorf("strict composed candidate parity failed: %v", comparison)
				}
				dir := t.TempDir()
				if base := os.Getenv("S0_REPLAY_EVIDENCE_DIR"); base != "" {
					dir = filepath.Join(base, t.Name())
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				// Native raw events contain typed provision records. They are
				// inspected directly below, not serialized as fabricated JSON.
				for name, bytes := range map[string][]byte{
					"actual.trace.json": encoded,
					"inspection.json":   jsonBytes(t, actual.RuntimeInspections),
					"comparison.txt":    []byte(fmt.Sprint(comparison)),
					"provenance.json": jsonBytes(t, map[string]any{
						"scriptSHA256": digest(data), "goldenSHA256": digest(golden), "actualSHA256": digest(encoded),
						"sourceCommit": record.SourceCommit, "criteriaSHA256": record.CriteriaSHA256,
						"oracleSHA256": record.OracleSHA256, "sources": composedSourceHashes(t),
						"comparisonError": fmt.Sprint(actual.ComparisonError),
						"projection":      "actual native provision observations and compiled addresses; no event sorting or filtering beyond the shared public projector",
					}),
				} {
					if err := os.WriteFile(filepath.Join(dir, name), bytes, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				goldenTrace, err := differential.ParseTrace(golden)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("strict actual comparison=%v; first difference=%s; actualSHA256=%s evidence=%s", comparison, firstEventDifference(*goldenTrace, actual.Trace), digest(encoded), dir)
				assertComposedNativeObservations(t, actual)
			})
		})
	}
}

func TestComposedExpectationsNeverDriveExecution(t *testing.T) {
	for _, id := range []string{"D02", "D07", "D08", "D09"} {
		t.Run(id, func(t *testing.T) {
			script := corpus(t, id)
			script.Expect = []differential.Assertion{{At: "impossible", Path: "/outputs/injected", Equals: "not-executed"}}
			data := jsonBytes(t, script)
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				actual, err := RunComposedJSON(ctx, runtime, data)
				if err != nil {
					t.Fatal(err)
				}
				if differential.CheckExpectations(script, actual.Trace) == nil {
					t.Fatal("an impossible assertion influenced execution")
				}
				for _, checkpoint := range actual.Trace.Checkpoints {
					if _, exists := checkpoint.Outputs["injected"]; exists {
						t.Fatal("expectation fabricated an output")
					}
				}
				assertComposedNativeObservations(t, actual)
			})
		})
	}
}

func TestComposedD06ExplicitRealmLocalAdaptation(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, loadFixture(t, "../../../differential/corpus/D06.json"))
		if err != nil {
			t.Fatal(err)
		}
		if len(actual.Trace.Checkpoints) != 2 {
			t.Fatalf("missing actual checkpoints: %+v", actual.Trace.Checkpoints)
		}
		checkpoint := actual.Trace.Checkpoints[0]
		if checkpoint.Outputs["inherited"] != "outer" || checkpoint.States[4].State != "PENDING" {
			t.Fatalf("ordinary inheritance or required new-realm scheduling failed: %+v", checkpoint)
		}
		// This one property intentionally differs from raw D06: contextmodel
		// disallows inherited injection across an isolation boundary. Keep the
		// actual native typed sentinel text, do not forge the source error class.
		want := map[string]any{"name": "Error", "message": cm.ErrUndeclared.Error()}
		if !reflect.DeepEqual(checkpoint.Outputs["retained-inherited"], want) {
			t.Fatalf("realm-local declaration rule did not refuse the real read: %+v", checkpoint.Outputs)
		}
		if differential.CheckExpectations(corpus(t, "D06"), actual.Trace) == nil {
			t.Fatal("stronger adapted property must not be reported as unchanged source parity")
		}
		assertComposedNativeObservations(t, actual)
	})
}

func TestComposedUnsupportedCapabilitiesStayExplicit(t *testing.T) {
	for _, id := range []string{"D05"} {
		t.Run(id, func(t *testing.T) {
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				_, err := RunComposedJSON(ctx, runtime, loadFixture(t, "../../../differential/corpus/"+id+".json"))
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("missing capability was not explicit: %v", err)
				}
			})
		})
	}
}

func TestComposedJSONNeedsOrderAndAllKeyDiscovery(t *testing.T) {
	data := []byte(`{"schema":"s0-script/v1","id":"D91","title":"typed multi-key declaration order","covers":["ordered-typed-keys"],"contexts":[{"id":"realm","parent":"root","isolate":{"key":"alpha","label":"joined"}}],"plugins":[{"id":"provider","apply":[{"op":"provide","key":"zebra","value":"Z"},{"op":"provide","key":"alpha","value":"A"}]},{"id":"consumer","needs":{"zebra":null,"alpha":{"mode":"typed"}},"apply":[{"op":"read","key":"zebra","to":"z"},{"op":"read","key":"alpha","to":"a"}]}],"steps":[{"op":"mount","context":"realm","fiber":"p","plugin":"provider","config":{}},{"op":"mount","context":"realm","fiber":"c","plugin":"consumer","config":{}},{"op":"settle","label":"active"},{"op":"dispose","fiber":"c"},{"op":"dispose","fiber":"p"},{"op":"settle","label":"disposed"}]}`)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		checkpoint := actual.Trace.Checkpoints[0]
		if checkpoint.Outputs["z"] != "Z" || checkpoint.Outputs["a"] != "A" {
			t.Fatalf("key registration did not discover ordinary arbitrary script names: %+v", checkpoint)
		}
		bindings := checkpoint.States[1].Epoch.Needs
		if len(bindings) != 2 || bindings[0].Key != "zebra" || bindings[1].Key != "alpha" {
			t.Fatalf("JSON declaration order lost in real native target: %+v", bindings)
		}
		assertComposedNativeObservations(t, actual)
	})
}

func TestComposedTypedUndeclaredReadIsNotNormalized(t *testing.T) {
	data := []byte(`{"schema":"s0-script/v1","id":"D92","title":"actual undeclared read refusal","covers":["typed-read-refusal"],"plugins":[{"id":"provider","apply":[{"op":"provide","key":"svc","value":"live"}]},{"id":"consumer","apply":[{"op":"read","key":"svc","to":"refused","catch":true}]}],"steps":[{"op":"mount","fiber":"p","plugin":"provider","config":{}},{"op":"settle","label":"provider"},{"op":"mount","fiber":"c","plugin":"consumer","config":{}},{"op":"settle","label":"checked"},{"op":"dispose","fiber":"c"},{"op":"dispose","fiber":"p"},{"op":"settle","label":"disposed"}]}`)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"name": "Error", "message": cm.ErrUndeclared.Error()}
		if got := actual.Trace.Checkpoints[1].Outputs["refused"]; !reflect.DeepEqual(got, want) {
			t.Fatalf("actual typed refusal was masked or source-normalized: %#v", got)
		}
	})
}

func TestComposedDynamicMetadataUsesRealContextTree(t *testing.T) {
	data := []byte(`{"schema":"s0-script/v1","id":"D93","title":"immutable metadata and dynamic caller derivation","covers":["actual-metadata-tree"],"contexts":[{"id":"outer","parent":"root","meta":{"audience":"A","entry":"spoof","nested":{"path":"outer"}}},{"id":"inner","parent":"outer","meta":{"audience":"B","nullable":null}}],"plugins":[{"id":"parent","apply":[{"op":"context","to":"dynamic","meta":{"audience":{"$config":"audience"},"copied":{"$meta":"nested.path"}}},{"op":"child","context":"dynamic","fiber":"child","plugin":"child","config":{"selectedAudience":{"$meta":"audience"}}}]},{"id":"child","apply":[{"op":"output","to":"config-audience","value":{"$config":"selectedAudience"}},{"op":"output","to":"audience","value":{"$meta":"audience"}},{"op":"output","to":"copied","value":{"$meta":"copied"}},{"op":"output","to":"nullable","value":{"$meta":"nullable"}},{"op":"output","to":"absent","value":{"$meta":"missing"}},{"op":"output","to":"claimed-entry","value":{"$meta":"entry"}}]}],"steps":[{"op":"mount","context":"inner","fiber":"parent","plugin":"parent","config":{"audience":"C"}},{"op":"settle","label":"children"},{"op":"output","context":"outer","to":"outer-audience","value":{"$meta":"audience"}},{"op":"output","context":"inner","to":"inner-audience","value":{"$meta":"audience"}},{"op":"snapshot","label":"unchanged"},{"op":"dispose","fiber":"parent"},{"op":"settle","label":"disposed"}]}`)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		got := actual.Trace.Checkpoints[1].Outputs
		want := map[string]any{"audience": "C", "config-audience": "C", "copied": "outer", "nullable": nil, "absent": undefined(), "claimed-entry": "spoof", "outer-audience": "A", "inner-audience": "B"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("metadata was not resolved from the actual selected immutable context: got=%#v want=%#v", got, want)
		}
		if actual.Trace.Checkpoints[0].States[1].State != "ACTIVE" || actual.Trace.Checkpoints[2].States[1].State != "DISPOSED" {
			t.Fatal("metadata derivation lost native child ownership")
		}
		assertComposedNativeObservations(t, actual)
	})
}

func TestComposedMetadataCannotMintEntryOrScopeAuthority(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		owner, intruder := cm.NewEntry("owner"), cm.NewEntry("intruder")
		key := cm.NewKey[any]("bound", 0, cm.Classes(cm.ProviderBound, cm.Open))
		authority, err := co.NewRuntime(runtime, cm.RootSpec{Entries: []cm.Plugin{owner, intruder}, Keys: []cm.AnyKey{key.Any()}, Bindings: []cm.Binding{{Key: key.Any(), Providers: []cm.Plugin{owner}}}})
		if err != nil {
			t.Fatal(err)
		}
		input := map[string]any{"entry": "owner", "frame": "owner", "scope": "tenant", "audience": "A"}
		derived := authority.Context().WithValues(input)
		input["audience"] = "MUTATED"
		if value, _ := derived.Metadata().Value("audience"); value != "A" || derived.Entry() != authority.Context().Entry() {
			t.Fatal("ordinary metadata aliases input or changes actual entry authority")
		}
		if _, err := authority.Start(ctx, derived, intruder, co.Spec{Metadata: cm.Spec{ID: "spoofed", Provides: []cm.AnyKey{key.Any()}}}); !errors.Is(err, cm.ErrBinding) {
			t.Fatalf("entry text conferred provider rights: %v", err)
		}
		scope := co.NewScope[string](key.Any())
		if _, err := scope.With(derived, "forged"); !errors.Is(err, cm.ErrScope) {
			t.Fatalf("metadata scope string conferred native provider ownership: %v", err)
		}
	})
}

func TestComposedAvailabilityWithdrawalRetainsActualBinding(t *testing.T) {
	data := []byte(`{"schema":"s0-script/v1","id":"D94","title":"availability gates target not teardown reads","covers":["consumer-availability-retained-binding"],"contexts":[{"id":"consumer","parent":"root","meta":{"audience":"A"}}],"plugins":[{"id":"provider","apply":[{"op":"provide","key":"svc","mode":"available","value":{"ready":"ready"}}]},{"id":"consumer","needs":{"svc":null},"apply":[{"op":"read","key":"svc","to":"first"}],"cleanup":[{"op":"read","key":"svc","to":"during-cleanup"}]}],"steps":[{"op":"output","to":"ready","value":{"A":true}},{"op":"mount","fiber":"p","plugin":"provider","config":{}},{"op":"settle","label":"provider"},{"op":"mount","context":"consumer","fiber":"c","plugin":"consumer","config":{}},{"op":"settle","label":"active"},{"op":"output","to":"ready","value":{"A":false}},{"op":"notify","keys":["svc"]},{"op":"settle","label":"withdrawn"},{"op":"output","to":"ready","value":{"A":true}},{"op":"notify","keys":["svc"]},{"op":"settle","label":"returned"},{"op":"dispose","fiber":"c"},{"op":"dispose","fiber":"p"},{"op":"settle","label":"disposed"}]}`)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		withdrawn, returned := actual.Trace.Checkpoints[2], actual.Trace.Checkpoints[3]
		want := map[string]any{"ready": "ready"}
		if withdrawn.States[1].State != "PENDING" || withdrawn.States[1].Epoch != nil || !reflect.DeepEqual(withdrawn.Outputs["during-cleanup"], want) {
			t.Fatalf("availability incorrectly invalidated a captured teardown binding: %+v", withdrawn)
		}
		if returned.States[1].State != "ACTIVE" || !reflect.DeepEqual(returned.Outputs["first"], want) {
			t.Fatalf("real Notify failed to restart the pending consumer: %+v", returned)
		}
		if withdrawn.Provisions[0].ProvisionUID != returned.Provisions[0].ProvisionUID {
			t.Fatal("availability notification replaced the actual provider")
		}
		assertComposedNativeObservations(t, actual)
	})
}

func assertComposedNativeObservations(t *testing.T, result Result) {
	t.Helper()
	for i, event := range result.RuntimeEvents {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("raw events lost native arrival order at %d: %+v", i, event)
		}
		if event.Kind == "service" || event.Kind == "service:publish" || event.Kind == "service:withdraw" {
			if _, ok := co.ProvisionObservation(event.Value); !ok {
				t.Fatalf("raw diagnostics replaced the native typed provision: %+v", event)
			}
		}
	}
	for _, checkpoint := range result.Trace.Checkpoints {
		native, exists := result.RuntimeInspections[checkpoint.Label]
		if !exists || len(native.Fibers) != len(checkpoint.States) || len(native.Provisions) != len(checkpoint.Provisions) {
			t.Fatalf("projection fabricated/dropped native observations: %+v native=%+v", checkpoint, native)
		}
		byName := make(map[string]cc.Snapshot)
		for _, state := range native.Fibers {
			byName[state.Name] = state
		}
		for _, state := range checkpoint.States {
			nativeState := byName[state.Fiber]
			if string(nativeState.State) != state.State || int(nativeState.Revision) != state.Revision || (nativeState.Target == nil) != (state.Epoch == nil) {
				t.Fatalf("projected lifecycle was inferred rather than observed: %+v native=%+v", state, nativeState)
			}
			if state.Epoch != nil {
				if len(state.Epoch.Needs) != len(nativeState.Target.UIDs) {
					t.Fatal("native target declaration order/length changed")
				}
				for i, binding := range state.Epoch.Needs {
					if binding.ProvisionUID != nativeState.Target.UIDs[i] {
						t.Fatal("native provision identity was not preserved")
					}
				}
			}
		}
		for i, provision := range checkpoint.Provisions {
			nativeProvision := native.Provisions[i]
			if !strings.HasPrefix(nativeProvision.Key, fmt.Sprintf("%q@0/realm:", provision.Key)) ||
				provision.ProvisionUID != nativeProvision.UID || provision.Provider != nativeProvision.Name ||
				provision.Active != nativeProvision.Active || !reflect.DeepEqual(provision.Value, nativeProvision.Value) {
				t.Fatalf("provision order/value/identity was fabricated: %+v native=%+v", provision, nativeProvision)
			}
			if provision.Realm == "" || strings.Contains(provision.Realm, "/realm:") {
				t.Fatalf("opaque native address was not decoded through its actual context: %+v", provision)
			}
		}
	}
}

func composedSourceHashes(t *testing.T) map[string]string {
	t.Helper()
	hashes := sourceHashes(t)
	for _, pattern := range []string{"../composition/*.go", "../contextmodel/*.go"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			hashes[path] = digest(loadFixture(t, path))
		}
	}
	return hashes
}
