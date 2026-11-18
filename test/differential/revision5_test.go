// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Revision 5 supersedes revision 3 as the executed D13 fixture: nothing ordered
// fibers a, d and u ACTIVE before the six snapshot or their disposal, so a
// concurrent candidate and the run to completion oracle recorded different event
// sets or activation positions. Revision 3 stays as the historical racy fixture.
const revision3D13Script = "07438cacef25b6cdf7b9f0d825645e41fa07ecd5bd64d29c9073a0d34c2d6381"

var revision5Replays = []string{"D13.replay-1", "D13.replay-2", "D13.replay-3"}

func TestD13Revision5ExactCausalDelta(t *testing.T) {
	oldBytes := load(t, "corpus/revision-3/D13.json")
	if digest(oldBytes) != revision3D13Script {
		t.Fatal("revision-3 D13 was changed")
	}
	if string(oldBytes) != string(load(t, "results/corpus-revision-5/D13-v3.script.json")) {
		t.Fatal("revision-3 snapshot differs")
	}
	if digest(load(t, "results/corpus-freeze-v3.json")) != "e47cc4ee64727da2e9b8d46dc340db91504b5d2cf5c94463c9ad6ee6da05385d" {
		t.Fatal("v3 freeze was changed")
	}
	script, err := ParseScript(oldBytes)
	if err != nil {
		t.Fatal(err)
	}
	newBytes := load(t, "corpus/revision-5/D13.json")
	revised, err := ParseScript(newBytes)
	if err != nil {
		t.Fatal(err)
	}
	var steps []Action
	inserted := 0
	for _, action := range script.Steps {
		if action.Op == "dispose" && (action.Fiber == "d" || action.Fiber == "u") {
			steps = append(steps, Action{Op: "awaitFiber", Fiber: action.Fiber})
			inserted++
		}
		steps = append(steps, action)
		if action.Op == "waitMark" && action.Label == "active-apply" {
			steps = append(steps, Action{Op: "awaitFiber", Fiber: "a"})
			inserted++
		}
	}
	script.Steps = steps
	if inserted != 3 || !reflect.DeepEqual(script, revised) {
		t.Fatal("revision 5 differs by more than the three declared awaitFiber steps")
	}
	var plan struct {
		NewScriptSHA256 string `json:"newScriptSHA256"`
		Executed        bool   `json:"executionStarted"`
		OracleSHA256    string `json:"oracleSHA256"`
	}
	if err := json.Unmarshal(load(t, "results/corpus-revision-5/preexecution.json"), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.NewScriptSHA256 != digest(newBytes) || plan.Executed || plan.OracleSHA256 != digest(load(t, "oracle/replay.mjs")) || plan.OracleSHA256 != currentOracle {
		t.Fatal("revision 5 script/oracle differs from its pre-execution record")
	}
}

func TestD13Revision5RecordedReplay(t *testing.T) {
	scriptBytes := load(t, "corpus/revision-5/D13.json")
	script, err := ParseScript(scriptBytes)
	if err != nil {
		t.Fatal(err)
	}
	var reference []byte
	for _, name := range revision5Replays {
		path := filepath.Join("results", "corpus-revision-5", name+".trace.json")
		data := load(t, path)
		trace, err := ParseTrace(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckExpectations(*script, *trace); err != nil {
			t.Fatal(err)
		}
		var record RunRecord
		if err := decodeStrict(load(t, path+".run.json"), &record); err != nil {
			t.Fatal(err)
		}
		if record.Scripts["D13"] != digest(scriptBytes) || record.TraceSHA256 != digest(data) || record.ExitCode != 0 || record.Result != "pass" || record.NativeAttempts != 0 {
			t.Fatal("invalid revised replay provenance")
		}
		if record.OracleSHA256 != currentOracle || record.OracleSHA256 != digest(load(t, "oracle/replay.mjs")) {
			t.Fatal("interpreter changed")
		}
		if record.ExecutableSHA256 != "e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb" || record.NodeVersion != "v24.22.1" || record.CriteriaSHA256 != CriteriaSHA256 || record.SourceCommit != SourceCommit {
			t.Fatal("pin changed")
		}
		if reference == nil {
			reference = data
		} else if err := Compare(reference, data); err != nil {
			t.Fatal(err)
		}
		for _, fiber := range []string{"a", "d", "u"} {
			active, disposed := 0, 0
			for _, event := range trace.Events {
				if event.Kind == "status" && event.Data["fiber"] == fiber && event.Data["to"] == "ACTIVE" {
					active = event.Seq
				}
				if event.Kind == "plugin" && event.Data["fiber"] == fiber && event.Data["disposed"] == true {
					disposed = event.Seq
				}
			}
			if active == 0 || disposed <= active {
				t.Fatalf("%s ACTIVE is not causally before its disposal", fiber)
			}
		}
	}
}

func TestD13Revision5FreezeBindsEvidence(t *testing.T) {
	var freeze struct {
		BaseFreeze struct {
			SHA256 string `json:"sha256"`
		} `json:"baseFreeze"`
		Preexecution struct {
			Plan, PlanSHA256, Record, RecordSHA256, DesignNote, DesignNoteSHA256 string
		} `json:"preexecution"`
		Delta struct {
			OldScript, OldScriptSHA256, NewScript, NewScriptSHA256 string
		} `json:"delta"`
		Replays []struct{ Trace, TraceSHA256, Receipt, ReceiptSHA256 string } `json:"oracleReplays"`
	}
	if err := json.Unmarshal(load(t, "results/corpus-freeze-v5.json"), &freeze); err != nil {
		t.Fatal(err)
	}
	if freeze.BaseFreeze.SHA256 != digest(load(t, "results/corpus-freeze-v4.json")) || len(freeze.Replays) < 3 {
		t.Fatal("freeze base/replays invalid")
	}
	bind := map[string]string{
		freeze.Preexecution.Plan: freeze.Preexecution.PlanSHA256, freeze.Preexecution.Record: freeze.Preexecution.RecordSHA256,
		"../../docs/design/s0-corpus-revision-5.md": freeze.Preexecution.DesignNoteSHA256,
		freeze.Delta.NewScript:                      freeze.Delta.NewScriptSHA256, freeze.Delta.OldScript: freeze.Delta.OldScriptSHA256,
	}
	for _, r := range freeze.Replays {
		bind[r.Trace], bind[r.Receipt] = r.TraceSHA256, r.ReceiptSHA256
	}
	for path, want := range bind {
		if digest(load(t, path)) != want {
			t.Fatalf("freeze digest mismatch for %s", path)
		}
	}
}

func TestD13Revision5LiveExplicit(t *testing.T) {
	if os.Getenv("S0_ORACLE_RUN") != "1" {
		t.Skip("not a live oracle pass: explicitly set S0_ORACLE_RUN=1 and S0_NODE")
	}
	node := os.Getenv("S0_NODE")
	if node == "" {
		t.Fatal("explicit isolated S0_NODE required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	trace, err := RunOracle(ctx, node, "oracle", "corpus/revision-5/D13.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(load(t, "results/corpus-revision-5/D13.replay-1.trace.json"), data); err != nil {
		t.Fatal(err)
	}
}
