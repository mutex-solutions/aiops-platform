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

// Revision 4 supersedes the original D22 as the executed fixture: nothing ordered
// the child ACTIVE before the parent's disposal, so a concurrent candidate could
// snapshot it LOADING. The original D22 stays as the historical racy fixture.
const originalD22Script = "fda677fb41026d454b7d225b30cc64170bb6938d310c173ad77ffef9c66e7965"

var revision4Replays = []string{"D22.replay-1", "D22.replay-2", "D22.replay-3"}

func TestD22Revision4ExactCausalDelta(t *testing.T) {
	oldBytes := load(t, "corpus/D22.json")
	if digest(oldBytes) != originalD22Script {
		t.Fatal("original D22 was changed")
	}
	if string(oldBytes) != string(load(t, "results/corpus-revision-4/D22-v1.script.json")) {
		t.Fatal("original D22 snapshot differs")
	}
	script, err := ParseScript(oldBytes)
	if err != nil {
		t.Fatal(err)
	}
	newBytes := load(t, "corpus/revision-4/D22.json")
	revised, err := ParseScript(newBytes)
	if err != nil {
		t.Fatal(err)
	}
	var steps []Action
	inserted := 0
	for _, action := range script.Steps {
		if action.Op == "dispose" && action.Fiber == "parent" {
			steps = append(steps, Action{Op: "waitMark", Label: "child-mounted"}, Action{Op: "awaitFiber", Fiber: "child"}, Action{Op: "awaitFiber", Fiber: "parent"})
			inserted++
		}
		steps = append(steps, action)
	}
	script.Steps = steps
	marked := 0
	for i := range script.Plugins {
		if script.Plugins[i].ID != "parent" {
			continue
		}
		apply := script.Plugins[i].Apply
		if len(apply) != 1 || apply[0].Op != "child" {
			t.Fatal("unexpected original parent body")
		}
		script.Plugins[i].Apply = append(apply[:1:1], Action{Op: "mark", Label: "child-mounted"})
		marked++
	}
	if inserted != 1 || marked != 1 || !reflect.DeepEqual(script, revised) {
		t.Fatal("revision 4 differs by more than the declared mark, waitMark and awaitFiber steps")
	}
	var plan struct {
		NewScriptSHA256 string `json:"newScriptSHA256"`
		Executed        bool   `json:"executionStarted"`
		OracleSHA256    string `json:"oracleSHA256"`
	}
	if err := json.Unmarshal(load(t, "results/corpus-revision-4/preexecution.json"), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.NewScriptSHA256 != digest(newBytes) || plan.Executed || plan.OracleSHA256 != digest(load(t, "oracle/replay.mjs")) || plan.OracleSHA256 != currentOracle {
		t.Fatal("revision 4 script/oracle differs from its pre-execution record")
	}
}

func TestD22Revision4RecordedReplay(t *testing.T) {
	scriptBytes := load(t, "corpus/revision-4/D22.json")
	script, err := ParseScript(scriptBytes)
	if err != nil {
		t.Fatal(err)
	}
	var reference []byte
	for _, name := range revision4Replays {
		path := filepath.Join("results", "corpus-revision-4", name+".trace.json")
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
		if record.Scripts["D22"] != digest(scriptBytes) || record.TraceSHA256 != digest(data) || record.ExitCode != 0 || record.Result != "pass" || record.NativeAttempts != 0 {
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
		active, unloading := 0, 0
		for _, event := range trace.Events {
			if event.Kind != "status" || event.Data["fiber"] != "child" {
				continue
			}
			switch event.Data["to"] {
			case "ACTIVE":
				active = event.Seq
			case "UNLOADING":
				unloading = event.Seq
			}
		}
		if active == 0 || unloading <= active {
			t.Fatal("child ACTIVE is not causally before its unloading")
		}
	}
}

func TestD22Revision4FreezeBindsEvidence(t *testing.T) {
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
	if err := json.Unmarshal(load(t, "results/corpus-freeze-v4.json"), &freeze); err != nil {
		t.Fatal(err)
	}
	if freeze.BaseFreeze.SHA256 != digest(load(t, "results/corpus-freeze-v3.json")) || len(freeze.Replays) < 3 {
		t.Fatal("freeze base/replays invalid")
	}
	bind := map[string]string{
		freeze.Preexecution.Plan: freeze.Preexecution.PlanSHA256, freeze.Preexecution.Record: freeze.Preexecution.RecordSHA256,
		"../../docs/design/s0-corpus-revision-4.md": freeze.Preexecution.DesignNoteSHA256,
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

func TestD22Revision4LiveExplicit(t *testing.T) {
	if os.Getenv("S0_ORACLE_RUN") != "1" {
		t.Skip("not a live oracle pass: explicitly set S0_ORACLE_RUN=1 and S0_NODE")
	}
	node := os.Getenv("S0_NODE")
	if node == "" {
		t.Fatal("explicit isolated S0_NODE required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	trace, err := RunOracle(ctx, node, "oracle", "corpus/revision-4/D22.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(load(t, "results/corpus-revision-4/D22.replay-1.trace.json"), data); err != nil {
		t.Fatal(err)
	}
}
