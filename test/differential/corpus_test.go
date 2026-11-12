// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func corpusIDs() []string {
	ids := make([]string, 36)
	for i := range ids {
		ids[i] = fmt.Sprintf("D%02d", i+1)
	}
	return ids
}

func digest(data []byte) string { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }

func TestCorpusCompleteAndRecordedExpectations(t *testing.T) {
	for _, id := range corpusIDs() {
		t.Run(id, func(t *testing.T) {
			scriptBytes := load(t, filepath.Join("corpus", id+".json"))
			script, err := ParseScript(scriptBytes)
			if err != nil {
				t.Fatal(err)
			}
			if script.ID != id || len(script.Expect) == 0 {
				t.Fatal("missing stable identity or independent assertion")
			}
			traceBytes := load(t, filepath.Join("results", "corpus-final", id+".trace.json"))
			trace, err := ParseTrace(traceBytes)
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckExpectations(*script, *trace); err != nil {
				t.Fatal(err)
			}
			var record RunRecord
			if err := decodeStrict(load(t, filepath.Join("results", "corpus-final", id+".trace.json.run.json")), &record); err != nil {
				t.Fatal(err)
			}
			if record.CriteriaSHA256 != CriteriaSHA256 || record.SourceCommit != SourceCommit || record.NodeVersion != "v24.22.1" || record.ExitCode != 0 || record.NativeAttempts != 0 {
				t.Fatal("invalid oracle provenance")
			}
			if record.ExecutableSHA256 != "e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb" {
				t.Fatal("wrong isolated executable")
			}
			if record.Scripts[id] != digest(scriptBytes) || record.TraceSHA256 != digest(traceBytes) || record.OracleSHA256 != digest(load(t, "oracle/replay.mjs")) || record.ResolverSHA256 != digest(load(t, "oracle/register.mjs")) || record.VendorManifestSHA256 != digest(load(t, "oracle/manifest.json")) {
				t.Fatal("recorded bytes changed: rerun under a new result identity")
			}
			if id == "D26" {
				if record.Result != "diagnostic" || trace.Diagnostic == nil {
					t.Fatal("awaited self-dispose was relabeled as success")
				}
			} else if record.Result != "pass" || trace.Diagnostic != nil {
				t.Fatal("unexpected diagnostic")
			}
		})
	}
}

func eventCount(trace *Trace, kind, field string, value any) int {
	count := 0
	for _, event := range trace.Events {
		if event.Kind == kind && event.Data[field] == value {
			count++
		}
	}
	return count
}

func recorded(t *testing.T, id string) *Trace {
	t.Helper()
	trace, err := ParseTrace(load(t, filepath.Join("results", "corpus-final", id+".trace.json")))
	if err != nil {
		t.Fatal(err)
	}
	return trace
}

func TestCausalTraceAssertions(t *testing.T) {
	// These event assertions supplement the script's checkpoint assertions; no
	// expected values participate in executing any plugin action.
	for _, id := range []string{"D33", "D34", "D35"} {
		for _, event := range recorded(t, id).Events {
			if event.Kind == "mark" {
				label, _ := event.Data["label"].(string)
				if len(label) >= 6 && label[:6] == "wrong-" {
					t.Fatalf("%s dispatched beyond a bail/veto", id)
				}
			}
		}
	}
	if eventCount(recorded(t, "D17"), "apply", "fiber", "f") != 2 {
		t.Fatal("coalesced intermediate target activated")
	}
	if eventCount(recorded(t, "D18"), "apply", "fiber", "c") != 1 {
		t.Fatal("sticky failure reactivated without Update")
	}
	if eventCount(recorded(t, "D20"), "mark", "label", "owned-cleanup") != 2 {
		t.Fatal("superseded/final cleanup not exactly once each")
	}
	if eventCount(recorded(t, "D23"), "mark", "label", "once") != 1 {
		t.Fatal("effect disposer is not single-shot")
	}
	if eventCount(recorded(t, "D30"), "apply", "fiber", "f") != 2 {
		t.Fatal("unchanged target restarted or changed target did not")
	}
	if eventCount(recorded(t, "D28"), "commit", "fiber", "f") != 1 {
		t.Fatal("noSave emitted commit or saved update omitted it")
	}
	if eventCount(recorded(t, "D35"), "inner", "key", "waterfall-veto") != 0 {
		t.Fatal("waterfall veto reached inner behavior")
	}
	if eventCount(recorded(t, "D36"), "inner", "key", "repeated") != 1 {
		t.Fatal("repeated next invoked inner twice")
	}
	trace := recorded(t, "D21")
	consumer, provider := 0, 0
	for _, event := range trace.Events {
		if event.Kind == "mark" && event.Data["label"] == "consumer-cleanup" {
			consumer = event.Seq
		}
		if event.Kind == "mark" && event.Data["label"] == "provider-cleanup" {
			provider = event.Seq
		}
	}
	if consumer == 0 || provider <= consumer {
		t.Fatal("provider released before consumer cleanup")
	}
	trace = recorded(t, "D14")
	before, after := trace.Checkpoints[0], trace.Checkpoints[1]
	oldUID, newUID := before.States[1].Epoch.Needs[0].ProvisionUID, after.States[1].Epoch.Needs[0].ProvisionUID
	if oldUID == newUID || before.States[0].FiberUID != after.States[0].FiberUID {
		t.Fatal("restart epoch relationship lost")
	}
}

func TestFailedInitialFixturesArePreservedAsControls(t *testing.T) {
	for _, id := range []string{"D05", "D06"} {
		script, err := ParseScript(load(t, filepath.Join("results", "expanded-initial", id+".script.json")))
		if err != nil {
			t.Fatal(err)
		}
		traceBytes := load(t, filepath.Join("results", "controls-v1", id+".trace.json"))
		trace, err := ParseTrace(traceBytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckExpectations(*script, *trace); err == nil {
			t.Fatal("invalid initial expectation no longer fails")
		}
		var receipt RunRecord
		if err := decodeStrict(load(t, filepath.Join("results", "controls-v1", id+".trace.json.run.json")), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.ExitCode != 1 || receipt.Result != "fail" || len(receipt.AssertionError) == 0 || receipt.TraceSHA256 != digest(traceBytes) {
			t.Fatal("failed control was misclassified")
		}
	}
}

func TestVendoredFilesRemainExact(t *testing.T) {
	var manifest struct {
		SourceCommit string `json:"sourceCommit"`
		Packages     []struct {
			Files []struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
				Bytes  int    `json:"bytes"`
			} `json:"files"`
		} `json:"packages"`
		AdditionalNotices []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Bytes  int    `json:"bytes"`
		} `json:"additionalNotices"`
	}
	if err := json.Unmarshal(load(t, "oracle/manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SourceCommit != SourceCommit || len(manifest.Packages) != 5 {
		t.Fatal("vendor identity changed")
	}
	count := 0
	for _, pkg := range manifest.Packages {
		for _, entry := range pkg.Files {
			data := load(t, filepath.Join("oracle", entry.Path))
			if len(data) != entry.Bytes || digest(data) != entry.SHA256 {
				t.Fatal(entry.Path)
			}
			count++
		}
	}
	for _, entry := range manifest.AdditionalNotices {
		data := load(t, filepath.Join("oracle", entry.Path))
		if len(data) != entry.Bytes || digest(data) != entry.SHA256 {
			t.Fatal(entry.Path)
		}
		count++
	}
	if count != 104 {
		t.Fatalf("copied byte manifest has %d files, want 104", count)
	}
}

func FuzzParseScript(f *testing.F) {
	data, err := os.ReadFile("corpus/D25.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		script, err := ParseScript(data)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(script)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseScript(encoded); err != nil {
			t.Fatal(err)
		}
	})
}
