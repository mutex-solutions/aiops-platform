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
	"time"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	hk "github.com/mutex-solutions/aiops-platform/test/spike/s0/hooks"
)

func TestHooksCheckpointExecution(t *testing.T) {
	for _, id := range []string{"D31", "D32", "D33", "D34", "D35", "D36"} {
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
				// Original source assertions remain unchanged. Error representation
				// comes from actual typed native causes, not alternate expectations.
				if err := differential.CheckExpectations(script, result.Trace); err != nil {
					t.Fatal(err)
				}
				assertHooksNative(t, result)
			})
		})
	}
}

func TestHooksActualGoldenTraceDiagnostics(t *testing.T) {
	for _, id := range []string{"D31", "D32", "D33", "D34", "D35", "D36"} {
		t.Run(id, func(t *testing.T) {
			scriptPath := "../../../differential/corpus/" + id + ".json"
			data := loadFixture(t, scriptPath)
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				// Capture the compiled dependency closure and explicit approved docs
				// BEFORE semantic execution. No unrelated draft-document wildcard.
				sourcesBefore := causalSources(t)
				sourcesBefore[hookHistoricalOraclePath] = digest(loadFixture(t, hookHistoricalOraclePath))
				sourcesBefore[hookEncodingMethodPath] = digest(loadFixture(t, hookEncodingMethodPath))
				candidateIdentity := hookCandidateIdentity(t)
				if err := hookReceiptProof(sourcesBefore, sourcesBefore, data, data); err != nil {
					t.Fatalf("INVALID hook receipt before execution: %v", err)
				}
				actual, err := RunComposedJSON(ctx, runtime, data)
				// Close belongs to eachRuntime's cleanup, outside this semantic-run
				// fence. Its administrative events never enter the compared trace.
				sourcesAfter := causalSources(t)
				sourcesAfter[hookHistoricalOraclePath] = digest(loadFixture(t, hookHistoricalOraclePath))
				sourcesAfter[hookEncodingMethodPath] = digest(loadFixture(t, hookEncodingMethodPath))
				proofErr := hookReceiptProof(sourcesBefore, sourcesAfter, data, loadFixture(t, scriptPath))
				if proofErr != nil {
					t.Errorf("INVALID hook receipt: %v", proofErr)
				}
				if err != nil {
					t.Fatal(err)
				}
				encoded := jsonBytes(t, actual.Trace)
				if _, err := differential.ParseTrace(encoded); err != nil {
					t.Fatal(err)
				}
				// Read expected observations only AFTER actual execution; explicitly
				// route the saved oracle receipt to its immutable historical source.
				path := "../../../differential/results/corpus-final/" + id + ".trace.json"
				golden, oracleRecord := loadFixture(t, path), loadFixture(t, path+".run.json")
				var record differential.RunRecord
				if err := json.Unmarshal(oracleRecord, &record); err != nil {
					t.Fatal(err)
				}
				if record.Scripts[id] != digest(data) || record.TraceSHA256 != digest(golden) || record.SourceCommit != differential.SourceCommit || record.CriteriaSHA256 != differential.CriteriaSHA256 || record.ExitCode != 0 || record.OracleSHA256 != hookHistoricalOracleSHA || sourcesBefore[hookHistoricalOraclePath] != hookHistoricalOracleSHA {
					proofErr = errors.Join(proofErr, errors.New("historical oracle provenance does not bind the script/trace/source"))
					t.Errorf("INVALID hook receipt: historical oracle provenance mismatch")
				}
				comparison := differential.Compare(golden, encoded)
				if comparison != nil && !strings.Contains(comparison.Error(), "ordered semantic mismatch") {
					t.Fatal(comparison)
				}
				if actual.ComparisonError != nil {
					t.Fatal(actual.ComparisonError)
				}
				dir := t.TempDir()
				if base := os.Getenv("S0_REPLAY_EVIDENCE_DIR"); base != "" {
					dir = filepath.Join(base, t.Name())
					if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dir, 0o700); err != nil {
						t.Fatalf("hook evidence leaf must be fresh (never overwrite): %v", err)
					}
				}
				status := "valid"
				if proofErr != nil {
					status = "INVALID"
				}
				for name, bytes := range map[string][]byte{
					"script.json": data, "actual.trace.json": encoded, "golden.trace.json": golden, "golden.trace.json.run.json": oracleRecord,
					"events.json": jsonBytes(t, actual.RuntimeEvents), "inspection.json": jsonBytes(t, actual.RuntimeInspections),
					"actual.witness.json": jsonBytes(t, actualWitness(data, encoded, actual.CheckpointAfterEvent)), "comparison.txt": []byte(fmt.Sprint(comparison)),
					"sources-before.json": jsonBytes(t, sourcesBefore), "sources-after.json": jsonBytes(t, sourcesAfter),
					"provenance.json": jsonBytes(t, map[string]any{
						"schema": "s0-hook-replay-receipt/v1", "scenario": id, "backendTest": t.Name(), "receiptStatus": status, "proofError": fmt.Sprint(proofErr),
						"scriptSHA256": digest(data), "goldenSHA256": digest(golden), "actualSHA256": digest(encoded), "oracleRunRecordSHA256": digest(oracleRecord),
						"oracleSourceCommit": record.SourceCommit, "criteriaSHA256": record.CriteriaSHA256, "amendmentSHA256": causalAmendmentSHA,
						"hookErrorEncodingMethodSHA256": sourcesBefore[hookEncodingMethodPath], "hookErrorEncodingMethodPath": hookEncodingMethodPath,
						"oracleHistoricalSHA256": record.OracleSHA256, "oracleHistoricalSourcePath": hookHistoricalOraclePath,
						"oracleExecutableSHA256": record.ExecutableSHA256, "oracleNodeVersion": record.NodeVersion,
						"sourcesBefore": sourcesBefore, "sourcesAfter": sourcesAfter, "candidateIdentity": candidateIdentity,
						"comparisonError": fmt.Sprint(actual.ComparisonError), "strictError": fmt.Sprint(comparison),
						"adaptations": "actual typed ListenerError + ParallelAggregateError; original source assertions unchanged; unknown/panic/context errors remain diagnostic; no error text stripping or trace sorting",
						"scope":       "strict hook differential diagnostics only, not full S0 acceptance; no causal acceptance inferred from actual-only witness",
					}),
				} {
					saveCausalEvidence(t, dir, name, bytes)
				}
				t.Logf("strict actual hook comparison=%v receipt=%s actualSHA256=%s evidence=%s", comparison, status, digest(encoded), dir)
				if os.Getenv("S0_COMPARE_RUN") == "1" && comparison != nil {
					t.Errorf("strict actual hook parity failed: %v", comparison)
				}
				assertHooksNative(t, actual)
			})
		})
	}
}

func assertHooksNative(t *testing.T, result Result) {
	t.Helper()
	assertComposedNativeObservations(t, result)
	for _, checkpoint := range result.Trace.Checkpoints {
		if len(checkpoint.States) != 0 || len(checkpoint.Provisions) != 0 {
			t.Fatalf("hook builtin created a fake user fiber or provision row: %+v", checkpoint)
		}
	}
	fixtureCount := 0
	for _, event := range result.RuntimeEvents {
		if event.Kind == "fixture" {
			fixtureCount++
		}
	}
	if fixtureCount != len(result.Trace.Events) {
		t.Fatalf("listener/body events bypassed native Recorder: native=%d projected=%d", fixtureCount, len(result.Trace.Events))
	}
}

func TestHooksExpectedValuesNeverDriveExecution(t *testing.T) {
	script := corpus(t, "D34")
	script.Expect = []differential.Assertion{{At: "finished", Path: "/outputs/zero", Equals: "injected"}}
	// Keep original action tokens (including null versus omitted values) and the
	// empty plugins array: only the expectation data is deliberately changed.
	var document map[string]json.RawMessage
	if err := json.Unmarshal(loadFixture(t, "../../../differential/corpus/D34.json"), &document); err != nil {
		t.Fatal(err)
	}
	document["expect"] = jsonBytes(t, script.Expect)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, jsonBytes(t, document))
		if err != nil {
			t.Fatal(err)
		}
		if differential.CheckExpectations(script, actual.Trace) == nil || actual.Trace.Checkpoints[0].Outputs["zero"] != json.Number("0") {
			t.Fatal("mutated expectation altered actual bail execution")
		}
		assertHooksNative(t, actual)
	})
}

func TestHooksSymbolIdentityAndReturnStopsProgram(t *testing.T) {
	data := []byte(`{"schema":"s0-script/v1","id":"D91","title":"actual hook identities","covers":["hooks"],"plugins":[],"steps":[{"op":"listen","key":"same","label":"string","program":[{"op":"return","value":"string"},{"op":"fail","error":"after return"}]},{"op":"listen","key":"same","symbol":true,"label":"symbol","program":[{"op":"return","value":"symbol"}]},{"op":"dispatch","key":"same","mode":"bail","to":"string"},{"op":"dispatch","key":"same","symbol":true,"mode":"bail","to":"symbol"},{"op":"settle","label":"finished"}]}`)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		out := actual.Trace.Checkpoints[0].Outputs
		if out["string"] != "string" || out["symbol"] != "symbol" {
			t.Fatalf("symbol and string event identities collided: %+v", out)
		}
		assertHooksNative(t, actual)
	})
}

func TestHooksBailReturnsAlreadyStartedFutureWithoutAwaiting(t *testing.T) {
	data := []byte(`{"schema":"s0-script/v1","id":"D91","title":"bail returns future immediately","covers":["hooks"],"plugins":[],"steps":[{"op":"listen","key":"future","label":"async-first","program":[{"op":"gate","label":"held"},{"op":"mark","label":"finished"}]},{"op":"listen","key":"future","label":"must-not-run","program":[{"op":"fail","error":"bail ignored future"}]},{"op":"dispatch","key":"future","mode":"bail","to":"future"},{"op":"snapshot","label":"returned"},{"op":"release","label":"held"},{"op":"waitMark","label":"finished"},{"op":"settle","label":"finished"}]}`)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		actual, err := RunComposedJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		if got := actual.Trace.Checkpoints[0].Outputs["future"]; !reflect.DeepEqual(got, map[string]any{}) {
			t.Fatalf("already-started Future did not survive Bail's immediate result projection: %+v", got)
		}
		prefix := actual.Trace.Events[:actual.CheckpointAfterEvent[0].AfterEvent]
		if len(prefix) != 3 || prefix[0].Kind != "listener" || prefix[1].Kind != "gate" || prefix[2].Kind != "output" {
			t.Fatalf("Bail awaited future or called a later listener: %+v", prefix)
		}
		assertHooksNative(t, actual)
	})
}

func TestHooksHostBuiltinOwnsActualBundleAndDisposes(t *testing.T) {
	data := loadFixture(t, "../../../differential/corpus/D31.json")
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
		script, err := differential.ParseScript(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyMissingValues(data, script); err != nil {
			t.Fatal(err)
		}
		frontend := &composedFrontend{script: script}
		actual, err := runAdapted(ctx, &composedObservations{Runtime: runtime}, *script, map[string][]string{}, frontend)
		if err != nil {
			t.Fatal(err)
		}
		b := frontend.hooks
		if b == nil || b.owner == nil || !b.owner.Owns(b.key.Any()) || !b.binding.Owns(b.key.Any()) {
			t.Fatal("actual bundle is not owned by native root builtin receipt")
		}
		if got := b.owner.Metadata().Spec(); got.ID != "root" || len(got.Provides) != 1 || got.Provides[0] != b.key.Any() {
			t.Fatalf("fabricated host metadata: %+v", got)
		}
		if frontend.contexts["root"].context.Owns(b.key.Any()) {
			t.Fatal("ordinary root observer acquired private host ownership")
		}
		if reflect.TypeOf(b.key) == reflect.TypeOf(frontend.keys["hooks"]) {
			t.Fatal("typed builtin key collapsed into ordinary Key[any]")
		}
		assertHooksNative(t, actual)
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runtime.Close(closeCtx); err != nil {
			t.Fatal(err)
		}
		if !b.closed.Load() || b.owner.Owns(b.key.Any()) || b.binding.Runtime.Enter() {
			t.Fatal("native close did not drain and revoke actual builtin ownership")
		}
	})
}

func TestHooksUnsupportedRemainsExplicit(t *testing.T) {
	for name, steps := range map[string]string{
		"unknown mode":              `[{"op":"dispatch","key":"x","mode":"imaginary","catch":true}]`,
		"prepend":                   `[{"op":"listen","key":"x","prepend":true,"catch":true}]`,
		"global":                    `[{"op":"listen","key":"x","global":true,"catch":true}]`,
		"builtin service collision": `[{"op":"provide","key":"hooks","value":1},{"op":"dispatch","key":"x","mode":"emit"}]`,
		"duplicate task":            `[{"op":"dispatch","key":"x","mode":"serial","task":"same","await":false},{"op":"dispose","fiber":"absent","task":"same"}]`,
		"async waterfall":           `[{"op":"listen","key":"x","program":[{"op":"gate","label":"cannot-detach"}]},{"op":"dispatch","key":"x","mode":"waterfall","catch":true}]`,
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte(`{"schema":"s0-script/v1","id":"D91","title":"explicit limits","covers":["hooks"],"plugins":[],"steps":` + steps + `}`)
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime cc.Runtime) {
				_, err := RunComposedJSON(ctx, runtime, data)
				if name == "unknown mode" {
					if err == nil || err.Error() != "unknown dispatch mode" {
						t.Fatalf("protocol-invalid mode was accepted: %v", err)
					}
					return // The unchanged protocol parser rejects this before replay.
				}
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("unsupported capability was swallowed or faked: %v", err)
				}
			})
		})
	}
}

func TestHooksKnownFixtureErrorsOnly(t *testing.T) {
	fixture := &hookFixtureError{message: "known fixture"}
	if got := hookErrorValue(&hk.ListenerError{PluginID: "root", Cause: fixture}); !reflect.DeepEqual(got, map[string]any{"name": "Error", "message": fixture.message}) {
		t.Fatal(got)
	}
	if got := hookErrorValue(&hk.ListenerError{PluginID: "root", Cause: hk.ErrNext}); !reflect.DeepEqual(got, errorValue(hk.ErrNext)) {
		t.Fatal(got)
	}
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("arbitrary wrapper: %w", fixture), errors.New("listener root: unknown"), errors.Join(fixture, context.Canceled), fmt.Errorf("outer: %w", errors.Join(fixture, context.Canceled)), &hk.PanicError{Value: fixture}, &hk.PanicError{Value: errors.New("panic")}} {
		if got := hookErrorValue(err); !reflect.DeepEqual(got, errorValue(err)) {
			t.Fatalf("unknown error was normalized: %T %+v", err, got)
		}
	}
}
