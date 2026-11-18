// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"crypto/sha256"
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
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

func corpus(t *testing.T, id string) differential.Script {
	t.Helper()
	data, err := os.ReadFile("../../../differential/corpus/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	script, err := differential.ParseScript(data)
	if err != nil {
		t.Fatal(err)
	}
	return *script
}

func eachRuntime(t *testing.T, test func(*testing.T, context.Context, concurrency.Runtime)) {
	t.Helper()
	for name, makeRuntime := range map[string]func() concurrency.Runtime{
		"reconciler": concurrency.NewReconciler, "graph-lock": concurrency.NewGraphLock,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			runtime := makeRuntime()
			defer func() {
				closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer closeCancel()
				_ = runtime.Close(closeCtx) // Deliberate fixture failures can survive Close.
			}()
			test(t, ctx, runtime)
		})
	}
}

// These are candidate checkpoint assertions, NOT raw oracle-equivalence passes.
// The unchanged corpus drives real Apply/Read/Provide/Update/Dispose operations.
func TestLifecycleCheckpointExecution(t *testing.T) {
	for _, id := range []string{"D10", "D12", "D14", "D15", "D16", "D17", "D18", "D19", "D25", "D27", "D28", "D30"} {
		t.Run(id, func(t *testing.T) {
			script := corpus(t, id)
			data := loadFixture(t, "../../../differential/corpus/"+id+".json")
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
				result, err := RunJSON(ctx, runtime, data)
				if err != nil {
					t.Fatal(err)
				}
				if result.ComparisonError != nil {
					t.Fatalf("ordinary lifecycle observations lack a native prerequisite: %v", result.ComparisonError)
				}
				// Complete native observations are eligible for comparison, not a
				// claim that the ordered reference trace has matched.
				if err := differential.CheckExpectations(script, result.Trace); err != nil {
					t.Fatal(err)
				}
				assertNativeCheckpoints(t, result)
				if len(result.RuntimeEvents) == 0 {
					t.Fatal("real runtime produced no raw events")
				}
				for i, event := range result.RuntimeEvents {
					if event.Sequence != uint64(i+1) {
						t.Fatalf("lost or reordered raw event at %d: %+v", i, event)
					}
				}
			})
		})
	}
}

func TestUnsupportedFailsExplicitly(t *testing.T) {
	for _, id := range []string{"D09", "D11", "D26"} {
		t.Run(id, func(t *testing.T) {
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
				result, err := Run(ctx, runtime, corpus(t, id))
				if !errors.Is(err, ErrUnsupported) {
					t.Fatalf("wanted explicit unsupported, got %v", err)
				}
				if result.Trace.Diagnostic != nil {
					t.Fatal("adapter must not synthesize D26's oracle deadlock diagnostic")
				}
			})
		})
	}
}

func TestD13NeedsFiberScopedAwait(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		// Explicit revision-3 fixture barrier (awaitFiber f orders FAILED before the snapshot); revision-2 stays as the historical racy fixture.
		script := corpus(t, "revision-3/D13")
		result, err := RunJSON(ctx, runtime, loadFixture(t, "../../../differential/corpus/revision-3/D13.json"))
		if _, ok := runtime.(FiberAwaiter); ok {
			if err != nil {
				t.Fatal(err)
			}
			if err := differential.CheckExpectations(script, result.Trace); err != nil {
				t.Fatal(err)
			}
			assertNativeCheckpoints(t, result)
			for _, state := range result.Trace.Checkpoints[0].States {
				if state.Fiber == "u" && (state.FiberUID != nil || state.Epoch != nil) {
					t.Fatalf("requested disposal must clear Alive and desired target during UNLOADING: %+v", state)
				}
			}
		} else if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("held unrelated load gate requires explicit missing Await: %v", err)
		}
	})
}

func TestD29UpdateHooksRunNatively(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		script := corpus(t, "D29")
		result, err := RunComposedJSON(ctx, runtime, loadFixture(t, "../../../differential/corpus/D29.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := differential.CheckExpectations(script, result.Trace); err != nil {
			t.Fatal(err)
		}
		if result.ComparisonError != nil {
			t.Fatal(result.ComparisonError)
		}
	})
}

func TestExpectationsNeverDriveExecution(t *testing.T) {
	script := corpus(t, "D14")
	script.Expect = []differential.Assertion{
		{At: "updated", Path: "/outputs/seen", Equals: "injected-value"},
		{At: "updated", Path: "/provisions/0/value", Equals: "injected-provision"},
		{At: "updated", Path: "/states/1/epoch", Equals: "injected-target"},
	}
	data := jsonBytes(t, script)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := RunJSON(ctx, runtime, data)
		if err != nil {
			t.Fatal(err)
		}
		if differential.CheckExpectations(script, result.Trace) == nil {
			t.Fatal("deliberately false expectation influenced execution")
		}
		checkpoint := result.Trace.Checkpoints[1]
		want := map[string]any{"v": json.Number("2")}
		if !reflect.DeepEqual(checkpoint.Outputs["seen"], want) || !reflect.DeepEqual(checkpoint.Provisions[0].Value, want) {
			t.Fatalf("expectation injected into actual read/provision values: %+v", checkpoint)
		}
		assertNativeCheckpoints(t, result)
	})
}

// Full actual traces, including errors, provisions and all status/service events,
// are compared AFTER execution. A mismatch is diagnostic evidence, not a parity
// pass. Every evidence record binds the raw script/golden/candidate/source hashes.
func TestActualGoldenTraceDiagnostics(t *testing.T) {
	for _, id := range []string{"D10", "D12", "D14", "D15", "D16", "D17", "D18", "D19", "D25", "D27", "D28", "D30", "revision-2/D13", "revision-3/D13"} {
		t.Run(id, func(t *testing.T) {
			scriptData := loadFixture(t, "../../../differential/corpus/"+id+".json")
			goldenPath := "../../../differential/results/corpus-final/" + id + ".trace.json"
			if id == "revision-2/D13" {
				goldenPath = "../../../differential/results/corpus-revision-2/D13.replay-1.trace.json"
			}
			if id == "revision-3/D13" {
				goldenPath = "../../../differential/results/corpus-revision-3/D13.replay-1.trace.json"
			}
			// The oracle bytes are not read until each candidate has finished.
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
				result, err := RunJSON(ctx, runtime, scriptData)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := json.MarshalIndent(result.Trace, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := differential.ParseTrace(actual); err != nil {
					t.Fatalf("actual trace malformed, not meaningful mismatch evidence: %v", err)
				}
				golden := loadFixture(t, goldenPath)
				var record differential.RunRecord
				if err := json.Unmarshal(loadFixture(t, goldenPath+".run.json"), &record); err != nil {
					t.Fatal(err)
				}
				if record.Scripts[result.Trace.Scenario] != digest(scriptData) || record.TraceSHA256 != digest(golden) ||
					record.SourceCommit != differential.SourceCommit || record.CriteriaSHA256 != differential.CriteriaSHA256 || record.ExitCode != 0 {
					t.Fatal("golden/script provenance changed; not a supported source comparison")
				}
				comparison := differential.Compare(golden, actual) // Never gate this call on ComparisonError.
				if comparison != nil && !strings.Contains(comparison.Error(), "ordered semantic mismatch") {
					t.Fatalf("comparison was not a real semantic mismatch: %v", comparison)
				}
				if result.ComparisonError != nil {
					t.Fatalf("full trace observations are unavailable: %v", result.ComparisonError)
				}
				if os.Getenv("S0_COMPARE_RUN") == "1" && comparison != nil {
					t.Errorf("candidate did not satisfy strict ordered parity: %v", comparison)
				}
				dir := t.TempDir()
				if evidenceRoot := os.Getenv("S0_REPLAY_EVIDENCE_DIR"); evidenceRoot != "" {
					dir = filepath.Join(evidenceRoot, t.Name())
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				for name, data := range map[string][]byte{
					"actual.trace.json": actual,
					"comparison.txt":    []byte(fmt.Sprint(comparison)),
					"inspection.json":   jsonBytes(t, result.RuntimeInspections),
					"events.json":       jsonBytes(t, result.RuntimeEvents),
					"provenance.json": jsonBytes(t, map[string]any{
						"scriptSHA256": digest(scriptData), "goldenSHA256": digest(golden), "actualSHA256": digest(actual),
						"sourceCommit": record.SourceCommit, "criteriaSHA256": record.CriteriaSHA256,
						"oracleSHA256": record.OracleSHA256, "sources": sourceHashes(t), "comparisonError": fmt.Sprint(result.ComparisonError),
					}),
				} {
					if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if comparison == nil {
					t.Logf("FULL actual comparison matched, still unsupported as a full gate; actualSHA256=%s evidence=%s", digest(actual), dir)
				} else {
					goldenTrace, err := differential.ParseTrace(golden)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("FULL actual mismatch; scriptSHA256=%s goldenSHA256=%s actualSHA256=%s first difference=%s; evidence=%s", digest(scriptData), digest(golden), digest(actual), firstEventDifference(*goldenTrace, result.Trace), dir)
				}
			})
		})
	}
}

func loadFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func sourceHashes(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, pattern := range []string{"*.go", "../concurrency/*.go"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			out[path] = digest(loadFixture(t, path))
		}
	}
	return out
}
func firstEventDifference(expected, actual differential.Trace) string {
	for i := 0; i < len(expected.Events) && i < len(actual.Events); i++ {
		left, _ := json.Marshal(expected.Events[i])
		right, _ := json.Marshal(actual.Events[i])
		if string(left) != string(right) {
			return fmt.Sprintf("event[%d] expected=%s actual=%s", i, left, right)
		}
	}
	if len(expected.Events) != len(actual.Events) {
		return fmt.Sprintf("event count expected=%d actual=%d", len(expected.Events), len(actual.Events))
	}
	return "checkpoints (full Compare diagnostic retained)"
}

// This assertion compares only with the actual native observation captured at the
// same checkpoint. It does not derive desired epochs from lifecycle state, the
// committed binding, plugin declarations or expected fixture responses.
func assertNativeCheckpoints(t *testing.T, result Result) {
	t.Helper()
	for _, checkpoint := range result.Trace.Checkpoints {
		inspection, ok := result.RuntimeInspections[checkpoint.Label]
		if !ok {
			t.Fatalf("no real inspection for checkpoint %q", checkpoint.Label)
		}
		byName := map[string]concurrency.Snapshot{}
		for _, snapshot := range inspection.Fibers {
			byName[snapshot.Name] = snapshot
		}
		for _, state := range checkpoint.States {
			snapshot, ok := byName[state.Fiber]
			if !ok {
				t.Fatalf("invented fiber state %q", state.Fiber)
			}
			var uid any
			if snapshot.Alive {
				uid = snapshot.ID
			}
			if state.State != string(snapshot.State) || state.Revision != int(snapshot.Revision) || !reflect.DeepEqual(state.FiberUID, uid) {
				t.Fatalf("altered native state at %q: actual=%+v native=%+v", checkpoint.Label, state, snapshot)
			}
			if snapshot.Target == nil {
				if state.Epoch != nil {
					t.Fatalf("invented desired epoch from state or committed bindings: %+v", state)
				}
				continue
			}
			if state.Epoch == nil || state.Epoch.Revision != int(snapshot.Target.Revision) || len(state.Epoch.Needs) != len(snapshot.Target.UIDs) {
				t.Fatalf("lost actual desired target: actual=%+v native=%+v", state, snapshot)
			}
			for i, binding := range state.Epoch.Needs {
				if binding.ProvisionUID != snapshot.Target.UIDs[i] {
					t.Fatalf("replaced real target UID with committed/current binding: %+v != %+v", state.Epoch, snapshot.Target)
				}
			}
		}
		if len(checkpoint.Provisions) != len(inspection.Provisions) {
			t.Fatalf("fabricated or dropped provision observations: %+v != %+v", checkpoint.Provisions, inspection.Provisions)
		}
		for i, provision := range checkpoint.Provisions {
			native := inspection.Provisions[i]
			if provision.Key != native.Key || provision.Realm != "root:"+native.Key || provision.Provider != native.Name ||
				provision.ProvisionUID != native.UID || provision.Active != native.Active || !reflect.DeepEqual(provision.Value, native.Value) {
				t.Fatalf("altered/reordered provision at %q[%d]: %+v != %+v", checkpoint.Label, i, provision, native)
			}
		}
	}
}

func TestUnsupportedCannotBeCaughtAsFixtureSuccess(t *testing.T) {
	script := corpus(t, "D10")
	script.Steps = append([]differential.Action{{Op: "dispatch", Key: "missing", Mode: "emit", Catch: true}}, script.Steps...)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		_, err := Run(ctx, runtime, script)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unsupported operation was swallowed: %v", err)
		}
	})
}

func TestFailureIdentityDoesNotMaskOtherErrors(t *testing.T) {
	startup := errors.New("same-message")
	other := errors.New("same-message")
	if !onlyFailure(startup, startup) || !onlyFailure(errors.Join(startup), startup) {
		t.Fatal("lost original startup failure identity")
	}
	if onlyFailure(other, startup) || onlyFailure(errors.Join(startup, other), startup) ||
		onlyFailure(errors.Join(startup, context.DeadlineExceeded), startup) {
		t.Fatal("masked a different failure or deadline")
	}
}

func TestDisposedFailureAwaitAndTaskSemantics(t *testing.T) {
	script := corpus(t, "D19")
	yes, no := true, false
	script.Steps = []differential.Action{
		{Op: "mount", Fiber: "f", Plugin: "recoverable", Config: map[string]any{"fail": true}},
		{Op: "settle", Label: "failed"},
		{Op: "dispose", Fiber: "f", Await: &no, Task: "disposal"},
		{Op: "awaitTask", Task: "disposal"},
		{Op: "awaitFiber", Fiber: "f", Await: &yes, Catch: true, To: "await-error"},
		{Op: "settle", Label: "disposed"},
	}
	script.Expect = nil
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, script)
		if err != nil {
			t.Fatal(err)
		}
		last := result.Trace.Checkpoints[len(result.Trace.Checkpoints)-1]
		if last.States[0].State != "DISPOSED" {
			t.Fatalf("disposal did not complete: %v", last.States)
		}
		value, ok := last.Outputs["await-error"].(map[string]any)
		if !ok || value["message"] != "recoverable-startup" {
			t.Fatalf("disposed Fiber.Await failed to reject retained failure: %v", last.Outputs)
		}
	})
}

func TestUpdateAwaitDoesNotSuppressStartupFailure(t *testing.T) {
	script := corpus(t, "D19")
	script.Steps = []differential.Action{
		{Op: "mount", Fiber: "f", Plugin: "recoverable", Config: map[string]any{"fail": false}},
		{Op: "settle", Label: "active"},
		{Op: "update", Fiber: "f", Config: map[string]any{"fail": true}, Catch: true, To: "update-error"},
		{Op: "settle", Label: "failed"},
		{Op: "dispose", Fiber: "f"},
	}
	script.Expect = nil
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, script)
		if err != nil {
			t.Fatal(err)
		}
		value, ok := result.Trace.Checkpoints[1].Outputs["update-error"].(map[string]any)
		if !ok || value["message"] != "recoverable-startup" {
			t.Fatalf("update join silently suppressed startup failure: %v", result.Trace.Checkpoints[1].Outputs)
		}
	})
}

func TestProjectionPreservesStateAndFixtureLedgerOrder(t *testing.T) {
	raw := []concurrency.Event{
		{Sequence: 1, Name: "f", Kind: "state", From: concurrency.Pending, State: concurrency.Loading},
		{Sequence: 2, Kind: "fixture", Key: "apply", Data: map[string]any{"fiber": "f", "config": map[string]any{}}},
		{Sequence: 3, Kind: "fixture", Key: "mark", Data: map[string]any{"label": "body"}},
		{Sequence: 4, Name: "f", Kind: "state", From: concurrency.Loading, State: concurrency.Active},
		{Sequence: 5, Name: "f", Kind: "state", From: concurrency.Active, State: concurrency.Unloading},
		{Sequence: 6, Kind: "fixture", Key: "cleanup", Data: map[string]any{"fiber": "f"}},
		{Sequence: 7, Name: "f", Kind: "state", From: concurrency.Unloading, State: concurrency.Pending},
		{Sequence: 8, Name: "f", Kind: "state", From: concurrency.Pending, State: concurrency.Loading},
	}
	projected, err := project(raw)
	if err != nil {
		t.Fatal(err)
	}
	var kinds, states []string
	for i, event := range projected {
		if event.Seq != i+1 {
			t.Fatal("nonmonotonic projection")
		}
		kinds = append(kinds, event.Kind)
		if event.Kind == "status" {
			states = append(states, event.Data["to"].(string))
		}
	}
	if !reflect.DeepEqual(kinds, []string{"status", "apply", "mark", "status", "status", "cleanup", "status", "status"}) {
		t.Fatalf("reordered body and states: %v", kinds)
	}
	if !reflect.DeepEqual(states, []string{"LOADING", "ACTIVE", "UNLOADING", "PENDING", "LOADING"}) {
		t.Fatalf("filtered or merged actual states: %v", states)
	}
}

func TestProjectionUsesActualServiceValuesAndRemovalOperations(t *testing.T) {
	raw := []concurrency.Event{
		{Kind: "service", Key: "svc", State: concurrency.Active, Value: map[string]any{"v": 1}},
		{Kind: "service", Key: "svc", State: concurrency.Unloading, Value: map[string]any{"v": 1}},
		{Kind: "remove", Key: "svc", State: concurrency.Unloading},
		{Kind: "service:publish", Key: "nullable", State: concurrency.Active, Value: nil},
		{Kind: "service:remove", Key: "nullable", State: concurrency.Unloading, Value: "retired-debug-value"},
	}
	actual, err := project(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"v": json.Number("1")}, map[string]any{"v": json.Number("1")}, undefined(), nil, undefined()}
	if len(actual) != len(want) {
		t.Fatalf("dropped service notification/removal: %+v", actual)
	}
	for i, event := range actual {
		if !reflect.DeepEqual(event.Data["value"], want[i]) {
			t.Fatalf("service event %d value derived from state rather than actual operation: %v != %v", i, event.Data["value"], want[i])
		}
	}
}

func TestRecorderCapturesActualFixtureEvents(t *testing.T) {
	script := corpus(t, "D19")
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		if _, ok := runtime.(concurrency.Recorder); !ok {
			t.Log("Recorder not implemented yet: shared-ledger comparison remains unsupported")
			return
		}
		result, err := Run(ctx, runtime, script)
		if err != nil {
			t.Fatal(err)
		}
		var actual []differential.Event
		for _, event := range result.RuntimeEvents {
			if event.Kind == "fixture" {
				actual = append(actual, differential.Event{Kind: event.Key, Data: clone(event.Data).(map[string]any)})
			}
		}
		if len(actual) == 0 {
			t.Fatal("Recorder implemented but fixture events were not recorded")
		}
		var projected []differential.Event
		for _, event := range result.Trace.Events {
			switch event.Kind {
			case "config", "commit", "update", "apply", "cleanup", "error", "output":
				projected = append(projected, differential.Event{Kind: event.Kind, Data: event.Data})
			}
		}
		if !reflect.DeepEqual(actual, projected) {
			t.Fatalf("fixture order changed between ledger and projection: %v != %v", actual, projected)
		}
	})
}

func TestSnapshotRequiresNativeInspector(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		// Embed only Runtime so optional methods cannot leak through this adapter.
		withoutInspector := struct{ concurrency.Runtime }{runtime}
		script := corpus(t, "D10")
		script.Steps = []differential.Action{{Op: "snapshot", Label: "unavailable", Catch: true}}
		result, err := Run(ctx, withoutInspector, script)
		if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "native Inspector") {
			t.Fatalf("missing inspection must fail explicitly, including catch:true: %v", err)
		}
		if len(result.Trace.Checkpoints) != 0 || len(result.RuntimeInspections) != 0 {
			t.Fatalf("unavailable native inspection fabricated an empty provision store: %+v", result)
		}
	})
}

func TestLoadingReservationAndRetiringProvisionAreObserved(t *testing.T) {
	no := false
	script := differential.Script{Schema: differential.ScriptSchema, ID: "D14", Plugins: []differential.PluginSpec{
		{ID: "provider", Apply: []differential.Action{{Op: "provide", Key: "svc", Value: map[string]any{"actual": "reserved"}}, {Op: "gate", Label: "load"}},
			Cleanup: []differential.Action{{Op: "gate", Label: "teardown"}}},
	}, Steps: []differential.Action{
		{Op: "mount", Fiber: "p", Plugin: "provider", Config: map[string]any{}},
		{Op: "waitMark", Label: "load"}, {Op: "snapshot", Label: "loading"},
		{Op: "release", Label: "load"}, {Op: "settle", Label: "active"},
		{Op: "dispose", Fiber: "p", Await: &no}, {Op: "waitMark", Label: "teardown"}, {Op: "snapshot", Label: "retiring"},
		{Op: "release", Label: "teardown"}, {Op: "settle", Label: "disposed"},
	}, Expect: []differential.Assertion{{At: "loading", Path: "/provisions/0/value", Equals: "not-the-response"}}}
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, script)
		if err != nil {
			t.Fatal(err)
		}
		assertNativeCheckpoints(t, result)
		for i, label := range []string{"loading", "active", "retiring"} {
			checkpoint := result.Trace.Checkpoints[i]
			if checkpoint.Label != label {
				t.Fatalf("lost checkpoint %s: %+v", label, checkpoint)
			}
			if len(checkpoint.Provisions) != 1 {
				t.Fatalf("missing actual %s reservation: %+v", label, checkpoint)
			}
			p := checkpoint.Provisions[0]
			if p.Active != (label == "active") || !reflect.DeepEqual(p.Value, map[string]any{"actual": "reserved"}) {
				t.Fatalf("wrong actual reservation value/active flag at %s: %+v", label, p)
			}
		}
		retiring := result.Trace.Checkpoints[2].States[0]
		if retiring.State != string(concurrency.Unloading) || retiring.FiberUID != nil || retiring.Epoch != nil {
			t.Fatalf("disposed-in-flight UID/target inferred from UNLOADING state: %+v", retiring)
		}
		if len(result.Trace.Checkpoints[3].Provisions) != 0 {
			t.Fatal("released reservation remained in actual store")
		}
		// Mutating returned raw diagnostics cannot rewrite the public trace or
		// the separately retained raw snapshot of the same native observation.
		inspection := result.RuntimeInspections["loading"]
		inspection.Fibers[0].Target.Revision = 999
		inspection.Provisions[0].Value.(map[string]any)["actual"] = "mutated"
		if result.RuntimeSnapshots["loading"][0].Target.Revision == 999 ||
			result.Trace.Checkpoints[0].States[0].Epoch.Revision == 999 ||
			result.Trace.Checkpoints[0].Provisions[0].Value.(map[string]any)["actual"] != "reserved" {
			t.Fatal("returned diagnostic values alias another observation")
		}
	})
}

func TestUnloadingEpochUsesActualTargetNotCommittedActivation(t *testing.T) {
	script := corpus(t, "D17")
	steps := make([]differential.Action, 0, len(script.Steps)+1)
	for _, action := range script.Steps {
		if action.Op == "release" && action.Label == "teardown" {
			steps = append(steps, differential.Action{Op: "snapshot", Label: "coalescing"})
		}
		steps = append(steps, action)
	}
	script.Steps = steps
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, script)
		if err != nil {
			t.Fatal(err)
		}
		assertNativeCheckpoints(t, result)
		native := result.RuntimeInspections["coalescing"].Fibers[0]
		if native.State != concurrency.Unloading || native.Target == nil || native.Target.Revision != 2 {
			t.Fatalf("missing real latest target while old activation is unloading: %+v", native)
		}
		state := result.Trace.Checkpoints[1].States[0]
		if state.Epoch == nil || state.Epoch.Revision != int(native.Target.Revision) || state.FiberUID == nil {
			t.Fatalf("used committed revision/state instead of real desired target and Alive: %+v", state)
		}
	})
}

func TestConfigCapturedPerActivationAndCleanup(t *testing.T) {
	script := corpus(t, "D17")
	script.Plugins[0].Cleanup = []differential.Action{
		{Op: "mark", Label: "cleanup-config", Value: map[string]any{"$config": "v"}},
		{Op: "gate", Label: "teardown"},
	}
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, script)
		if err != nil {
			t.Fatal(err)
		}
		var got []any
		for _, event := range result.Trace.Events {
			if event.Kind == "mark" && event.Data["label"] == "cleanup-config" {
				got = append(got, event.Data["value"])
			}
		}
		if !reflect.DeepEqual(got, []any{json.Number("1"), json.Number("3")}) {
			t.Fatalf("cleanup captured mutable latest config: %v", got)
		}
	})
}
