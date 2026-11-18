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

// Revision 3 supersedes revision 2 as the executed D13 fixture: revision 2 had no
// causal barrier for fiber f, so a concurrent candidate could snapshot it LOADING.
// Revision 2 and its tests stay as the historical record of that racy fixture.
const (
	revision2Script = "b285c1deb72ccccefe14184cadfe8269684580e70c4c6ba8a0ecf39f3bd161c2" // revision-2 bytes
	currentOracle   = "5f6714acf1e568d6358c08614321bfe520357dd0aee6a44c438faa25296306c4"
)

func TestD13Revision3ExactCausalDelta(t *testing.T) {
	v2Bytes := load(t, "corpus/revision-2/D13.json")
	if digest(v2Bytes) != revision2Script {
		t.Fatal("revision-2 D13 was changed")
	}
	if string(v2Bytes) != string(load(t, "results/corpus-revision-3/D13-v2.script.json")) {
		t.Fatal("revision-2 snapshot differs")
	}
	if digest(load(t, "results/corpus-freeze-v2.json")) != "0f1be418539e35544e468e34a7ded084ff198cf6cc5c8e7a2b69f344fe51b82b" {
		t.Fatal("v2 freeze was changed")
	}
	script, err := ParseScript(v2Bytes)
	if err != nil {
		t.Fatal(err)
	}
	newBytes := load(t, "corpus/revision-3/D13.json")
	revised, err := ParseScript(newBytes)
	if err != nil {
		t.Fatal(err)
	}
	var steps []Action
	inserted := 0
	for _, action := range script.Steps {
		if action.Op == "snapshot" && action.Label == "six" {
			steps = append(steps, Action{Op: "awaitFiber", Fiber: "f", Catch: true})
			inserted++
		}
		steps = append(steps, action)
	}
	script.Steps = steps
	if inserted != 1 || !reflect.DeepEqual(script, revised) {
		t.Fatal("revision 3 differs by more than the one declared awaitFiber step")
	}
	var plan struct {
		NewScriptSHA256 string `json:"newScriptSHA256"`
		Executed        bool   `json:"executionStarted"`
		OracleSHA256    string `json:"oracleSHA256"`
	}
	if err := json.Unmarshal(load(t, "results/corpus-revision-3/preexecution.json"), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.NewScriptSHA256 != digest(newBytes) || plan.Executed || plan.OracleSHA256 != digest(load(t, "oracle/replay.mjs")) || plan.OracleSHA256 != currentOracle {
		t.Fatal("revision 3 script/oracle differs from its pre-execution record")
	}
}

func TestD13Revision3RecordedReplay(t *testing.T) {
	scriptBytes := load(t, "corpus/revision-3/D13.json")
	script, err := ParseScript(scriptBytes)
	if err != nil {
		t.Fatal(err)
	}
	var reference []byte
	for _, name := range []string{"D13.replay-1", "D13.replay-2", "D13.replay-3"} {
		path := filepath.Join("results", "corpus-revision-3", name+".trace.json")
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
		failed, awaited := 0, 0
		for _, event := range trace.Events {
			if event.Kind == "status" && event.Data["fiber"] == "f" && event.Data["to"] == "FAILED" {
				failed = event.Seq
			}
			if event.Kind == "error" && event.Data["op"] == "awaitFiber" {
				awaited = event.Seq
			}
		}
		if failed == 0 || awaited <= failed || len(trace.Checkpoints) == 0 || trace.Checkpoints[0].Label != "six" {
			t.Fatal("f FAILED is not causally before the awaitFiber barrier")
		}
	}
}

func TestD13Revision3FreezeBindsEvidence(t *testing.T) {
	var freeze struct {
		BaseFreeze struct {
			SHA256 string `json:"sha256"`
		} `json:"baseFreeze"`
		Preexecution struct {
			Plan, PlanSHA256, Record, RecordSHA256, DesignNote, DesignNoteSHA256 string
		} `json:"preexecution"`
		Delta struct {
			NewScript, NewScriptSHA256 string
		} `json:"delta"`
		Replays []struct{ Trace, TraceSHA256, Receipt, ReceiptSHA256 string } `json:"oracleReplays"`
	}
	if err := json.Unmarshal(load(t, "results/corpus-freeze-v3.json"), &freeze); err != nil {
		t.Fatal(err)
	}
	if freeze.BaseFreeze.SHA256 != digest(load(t, "results/corpus-freeze-v2.json")) || len(freeze.Replays) < 3 {
		t.Fatal("freeze base/replays invalid")
	}
	bind := map[string]string{
		freeze.Preexecution.Plan: freeze.Preexecution.PlanSHA256, freeze.Preexecution.Record: freeze.Preexecution.RecordSHA256,
		"../../docs/design/s0-corpus-revision-3.md": freeze.Preexecution.DesignNoteSHA256, freeze.Delta.NewScript: freeze.Delta.NewScriptSHA256,
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

func TestD13Revision3LiveExplicit(t *testing.T) {
	if os.Getenv("S0_ORACLE_RUN") != "1" {
		t.Skip("not a live oracle pass: explicitly set S0_ORACLE_RUN=1 and S0_NODE")
	}
	node := os.Getenv("S0_NODE")
	if node == "" {
		t.Fatal("explicit isolated S0_NODE required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	trace, err := RunOracle(ctx, node, "oracle", "corpus/revision-3/D13.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(load(t, "results/corpus-revision-3/D13.replay-1.trace.json"), data); err != nil {
		t.Fatal(err)
	}
}
