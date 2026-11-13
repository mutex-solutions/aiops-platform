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

func TestD13Revision2ExactCausalDelta(t *testing.T) {
	oldBytes := load(t, "corpus/D13.json")
	if digest(oldBytes) != "f798d978e0e775c91aa8cf26da0aadf2058a154e41a292d05a3c890effd77e79" {
		t.Fatal("v1 D13 was changed")
	}
	if string(oldBytes) != string(load(t, "results/corpus-revision-2/D13-v1.script.json")) {
		t.Fatal("v1 snapshot differs")
	}
	if digest(load(t, "results/corpus-freeze.json")) != "3b40b812ed6df7b98780d80f46396eb2c4bcb685393a5ba0464a04623ae72596" {
		t.Fatal("v1 freeze was changed")
	}
	oldScript, err := ParseScript(oldBytes)
	if err != nil {
		t.Fatal(err)
	}
	newBytes := load(t, "corpus/revision-2/D13.json")
	if digest(newBytes) != "b285c1deb72ccccefe14184cadfe8269684580e70c4c6ba8a0ecf39f3bd161c2" {
		t.Fatal("v2 script changed after preexecution plan")
	}
	newScript, err := ParseScript(newBytes)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range oldScript.Plugins {
		if oldScript.Plugins[i].ID == "unloading" {
			if len(oldScript.Plugins[i].Apply) != 0 {
				t.Fatal("v1 Apply was not empty")
			}
			oldScript.Plugins[i].Apply = []Action{{Op: "mark", Label: "u-ready"}}
			found = true
		}
	}
	if !found {
		t.Fatal("missing unloading plugin")
	}
	for i, action := range oldScript.Steps {
		if action.Op == "dispose" && action.Fiber == "u" {
			steps := append([]Action(nil), oldScript.Steps[:i]...)
			steps = append(steps, Action{Op: "waitMark", Label: "u-ready"})
			oldScript.Steps = append(steps, oldScript.Steps[i:]...)
			break
		}
	}
	if !reflect.DeepEqual(oldScript, newScript) {
		t.Fatal("v2 differs by more than the two declared causal actions")
	}
}

func TestD13Revision2RecordedReplay(t *testing.T) {
	scriptBytes := load(t, "corpus/revision-2/D13.json")
	script, err := ParseScript(scriptBytes)
	if err != nil {
		t.Fatal(err)
	}
	var reference []byte
	for _, name := range []string{"D13.replay-1", "D13.replay-2", "D13.replay-3"} {
		path := filepath.Join("results", "corpus-revision-2", name+".trace.json")
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
		if record.OracleSHA256 != "b0f4e4474e4ffba2b5c9316680fa21581c2032d699fb845d0a9c6c5d0d6488b1" || record.OracleSHA256 != historicalOracleDigest(t, record.OracleSHA256) {
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
		ready, disposing := 0, 0
		for _, event := range trace.Events {
			if event.Kind == "mark" && event.Data["label"] == "u-ready" {
				ready = event.Seq
			}
			if event.Kind == "plugin" && event.Data["fiber"] == "u" && event.Data["disposed"] == true {
				disposing = event.Seq
			}
		}
		if ready == 0 || disposing <= ready {
			t.Fatal("u disposal not causally after Apply readiness")
		}
	}
}

func TestD13Revision2LiveExplicit(t *testing.T) {
	if os.Getenv("S0_ORACLE_RUN") != "1" {
		t.Skip("not a live oracle pass: explicitly set S0_ORACLE_RUN=1 and S0_NODE")
	}
	node := os.Getenv("S0_NODE")
	if node == "" {
		t.Fatal("explicit isolated S0_NODE required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	trace, err := RunOracle(ctx, node, "oracle", "corpus/revision-2/D13.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(load(t, "results/corpus-revision-2/D13.replay-1.trace.json"), data); err != nil {
		t.Fatal(err)
	}
}
