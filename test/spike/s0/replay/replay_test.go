// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
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
	for _, id := range []string{"D10", "D12", "D14", "D15", "D16", "D17", "D18", "D19", "D25"} {
		t.Run(id, func(t *testing.T) {
			script := corpus(t, id)
			eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
				result, err := Run(ctx, runtime, script)
				if err != nil {
					t.Fatal(err)
				}
				if !errors.Is(result.ComparisonError, ErrUnsupported) {
					t.Fatal("partial observations must not advertise oracle equivalence")
				}
				if err := differential.CheckExpectations(script, result.Trace); err != nil {
					t.Fatal(err)
				}
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
		// Explicit revision-2 fixture barrier; never silently replace D13 v1.
		script := corpus(t, "revision-2/D13")
		result, err := Run(ctx, runtime, script)
		if _, ok := runtime.(FiberAwaiter); ok {
			if err != nil {
				t.Fatal(err)
			}
			if err := differential.CheckExpectations(script, result.Trace); err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("held unrelated load gate requires explicit missing Await: %v", err)
		}
	})
}

func TestExpectationsNeverDriveExecution(t *testing.T) {
	script := corpus(t, "D19")
	script.Expect = []differential.Assertion{{At: "recovered", Path: "/outputs/version", Equals: "injected-value"}}
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, script)
		if err != nil {
			t.Fatal(err)
		}
		if differential.CheckExpectations(script, result.Trace) == nil {
			t.Fatal("deliberately false expectation influenced execution")
		}
		observed := result.Trace.Checkpoints[1].Outputs["version"]
		if observed != json.Number("2") {
			t.Fatalf("observed %v instead of fixture-produced version 2", observed)
		}
	})
}

// This negative check deliberately compares actual partial observations with a
// saved oracle only AFTER execution. It is expected to mismatch, never a gate.
func TestPartialTraceDoesNotClaimOracleEquality(t *testing.T) {
	expected, err := os.ReadFile("../../../differential/results/corpus-v1/D14.trace.json")
	if err != nil {
		t.Fatal(err)
	}
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, corpus(t, "D14"))
		if err != nil {
			t.Fatal(err)
		}
		actual, err := json.Marshal(result.Trace)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := differential.ParseTrace(actual); err != nil {
			t.Fatalf("candidate trace malformed, not a meaningful mismatch: %v", err)
		}
		if differential.Compare(expected, actual) == nil {
			t.Fatal("partial trace unexpectedly matched; review missing native APIs")
		}
		if !errors.Is(result.ComparisonError, ErrUnsupported) {
			t.Fatal("negative comparison not explicitly marked unsupported")
		}
	})
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
			case "apply", "error", "output":
				projected = append(projected, differential.Event{Kind: event.Kind, Data: event.Data})
			}
		}
		if !reflect.DeepEqual(actual, projected) {
			t.Fatalf("fixture order changed between ledger and projection: %v != %v", actual, projected)
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
