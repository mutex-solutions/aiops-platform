// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const witnessNodePath = "/tmp/aiops-s0-node-24.22.1-u4gdr6qr/bin/node"

// Local decoding types deliberately do not depend on the causal comparator API.
type oracleWitnessEvidence struct {
	Schema       string `json:"schema"`
	TraceSHA256  string `json:"traceSHA256"`
	ScriptSHA256 string `json:"scriptSHA256"`
	Checkpoints  []struct {
		Label      string `json:"label"`
		AfterEvent int    `json:"afterEvent"`
	} `json:"checkpoints"`
}

func witnessOracleNode(t *testing.T) string {
	t.Helper()
	if os.Getenv("S0_ORACLE_RUN") != "1" {
		t.Skip("set S0_ORACLE_RUN=1 and explicit pinned S0_NODE for live witness instrumentation tests")
	}
	if os.Getenv("S0_NODE") != witnessNodePath {
		t.Fatalf("witness tests require S0_NODE=%s; no host or mutable-cache fallback", witnessNodePath)
	}
	if digest(load(t, witnessNodePath)) != "e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb" {
		t.Fatal("isolated Node executable bytes changed")
	}
	return witnessNodePath
}

func invokeWitnessOracle(t *testing.T, node, scriptPath, tracePath, witnessPath string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--import", "./oracle/register.mjs", "./oracle/replay.mjs", scriptPath, tracePath)
	// Baseline runs must be genuinely opt-out, even if the outer test has opted in.
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "S0_CAUSAL_WITNESS_PATH=") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	if witnessPath != "" {
		cmd.Env = append(cmd.Env, "S0_CAUSAL_WITNESS_PATH="+witnessPath)
	}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("oracle command exceeded test bound: %v\n%s", ctx.Err(), output)
	}
	return output, err
}

func readOracleWitness(t *testing.T, path, scriptPath, tracePath string) oracleWitnessEvidence {
	t.Helper()
	var witness oracleWitnessEvidence
	decoder := json.NewDecoder(bytes.NewReader(load(t, path)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&witness); err != nil {
		t.Fatal(err)
	}
	if witness.Schema != "s0-causal-witness/v1" || witness.ScriptSHA256 != digest(load(t, scriptPath)) || witness.TraceSHA256 != digest(load(t, tracePath)) {
		t.Fatal("witness is not bound to the exact original script and written raw trace bytes")
	}
	trace, err := ParseTrace(load(t, tracePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(witness.Checkpoints) != len(trace.Checkpoints) {
		t.Fatal("witness checkpoint count differs from raw trace")
	}
	previous := 0
	seen := map[string]bool{}
	for i, cut := range witness.Checkpoints {
		if cut.Label == "" || seen[cut.Label] || cut.Label != trace.Checkpoints[i].Label || cut.AfterEvent < previous || cut.AfterEvent > len(trace.Events) {
			t.Fatalf("invalid checkpoint cut %d: %+v", i, cut)
		}
		seen[cut.Label] = true
		previous = cut.AfterEvent
	}
	return witness
}

func writeWitnessFixture(t *testing.T, directory, script string) string {
	t.Helper()
	path := filepath.Join(directory, "script.json")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOracleCausalWitnessD16RawBytesUnchanged(t *testing.T) {
	node := witnessOracleNode(t)
	directory := t.TempDir()
	baselinePath := filepath.Join(directory, "baseline.trace.json")
	tracePath := filepath.Join(directory, "witness.trace.json")
	witnessPath := filepath.Join(directory, "witness.json")
	if output, err := invokeWitnessOracle(t, node, "corpus/D16.json", baselinePath, ""); err != nil {
		t.Fatalf("baseline: %v\n%s", err, output)
	}
	if _, err := os.Stat(witnessPath); !os.IsNotExist(err) {
		t.Fatal("opt-out CLI created a witness")
	}
	if output, err := invokeWitnessOracle(t, node, "corpus/D16.json", tracePath, witnessPath); err != nil {
		t.Fatalf("witness: %v\n%s", err, output)
	}
	baseline, raw := load(t, baselinePath), load(t, tracePath)
	if !bytes.Equal(baseline, raw) {
		t.Fatal("enabling witness changed the complete raw D16 trace bytes")
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatal("raw trace no longer includes the CLI newline")
	}
	readOracleWitness(t, witnessPath, "corpus/D16.json", tracePath)
	var record RunRecord
	if err := json.Unmarshal(load(t, tracePath+".run.json"), &record); err != nil {
		t.Fatal(err)
	}
	if record.OracleSHA256 != digest(load(t, "oracle/replay.mjs")) || record.TraceSHA256 != digest(raw) || record.Scripts["D16"] != digest(load(t, "corpus/D16.json")) || record.NodeVersion != "v24.22.1" || record.ExecutableSHA256 != digest(load(t, node)) || record.NativeAttempts != 0 || record.Result != "pass" || record.ExitCode != 0 {
		t.Fatal("existing run record did not bind instrumented oracle source and exact replay bytes")
	}
}

func TestOracleCausalWitnessActualCheckpointCuts(t *testing.T) {
	node := witnessOracleNode(t)
	directory := t.TempDir()
	// Whitespace and trailing newlines are intentional: hash original bytes, not a
	// remarshal. The final event stays in the raw trace beyond the last checkpoint.
	scriptPath := writeWitnessFixture(t, directory, ` {
  "schema": "s0-script/v1", "id": "D99", "plugins": [],
  "steps": [
    {"op":"snapshot","label":"zero"},
    {"op":"output","to":"value","value":"one"},
    {"op":"snapshot","label":"one"},
    {"op":"snapshot","label":"same-cut"},
    {"op":"output","to":"value","value":"two"}
  ],
  "expect": [{"at":"one","path":"/outputs/value","equals":"one"}]
}

`)
	tracePath, witnessPath := filepath.Join(directory, "trace.json"), filepath.Join(directory, "witness.json")
	if output, err := invokeWitnessOracle(t, node, scriptPath, tracePath, witnessPath); err != nil {
		t.Fatalf("checkpoint fixture: %v\n%s", err, output)
	}
	witness := readOracleWitness(t, witnessPath, scriptPath, tracePath)
	var cuts []int
	for _, cut := range witness.Checkpoints {
		cuts = append(cuts, cut.AfterEvent)
	}
	if !reflect.DeepEqual(cuts, []int{0, 1, 1}) {
		t.Fatalf("checkpoint cuts were not observed at their real execution positions: %v", cuts)
	}
	trace, err := ParseTrace(load(t, tracePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Events) != 2 || trace.Events[1].Data["value"] != "two" || trace.Checkpoints[1].Outputs["value"] != "one" {
		t.Fatal("witness projected, truncated, or mutated the raw trace/checkpoint")
	}
}

func TestOracleCausalWitnessExportedReplayOptIn(t *testing.T) {
	node := witnessOracleNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := `import { createReplay } from './oracle/replay.mjs';
const script = { schema: 's0-script/v1', id: 'D99', steps: [{op:'snapshot',label:'first'}] };
const plain = createReplay(script), observed = createReplay(script, {causalWitness:true});
await Promise.all([plain.done, observed.done]);
process.stdout.write(JSON.stringify({plainKeys:Object.keys(plain),observedKeys:Object.keys(observed),cuts:observed.checkpointAfterEvent,sameRaw:JSON.stringify(plain.trace)===JSON.stringify(observed.trace)}));`
	cmd := exec.CommandContext(ctx, node, "--import", "./oracle/register.mjs", "--input-type=module", "--eval", code)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exported replay: %v\n%s", err, output)
	}
	var result struct {
		PlainKeys    []string `json:"plainKeys"`
		ObservedKeys []string `json:"observedKeys"`
		Cuts         []struct {
			Label      string `json:"label"`
			AfterEvent int    `json:"afterEvent"`
		} `json:"cuts"`
		SameRaw bool `json:"sameRaw"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.PlainKeys, []string{"trace", "done", "checkpoint"}) || !reflect.DeepEqual(result.ObservedKeys, []string{"trace", "done", "checkpoint", "checkpointAfterEvent"}) || !result.SameRaw || len(result.Cuts) != 1 || result.Cuts[0].Label != "first" || result.Cuts[0].AfterEvent != 0 {
		t.Fatalf("opt-in replay API changed raw trace or default API shape: %+v", result)
	}
}

func TestOracleCausalWitnessFailuresAndDiagnosticDoNotComplete(t *testing.T) {
	node := witnessOracleNode(t)
	for _, test := range []struct {
		name, script, wantError string
	}{
		{"duplicate-label", `{"schema":"s0-script/v1","id":"D99","steps":[{"op":"snapshot","label":"same"},{"op":"snapshot","label":"same"}]}`, "invalid checkpoint label"},
		{"action-failure", `{"schema":"s0-script/v1","id":"D99","steps":[{"op":"snapshot","label":"before"},{"op":"fail","error":"test-witness-failure"}]}`, "test-witness-failure"},
		{"assertion-failure", `{"schema":"s0-script/v1","id":"D99","steps":[{"op":"snapshot","label":"done"}],"expect":[{"at":"done","path":"/outputs/absent","equals":true}]}`, "assertion"},
		{"diagnostic", string(load(t, "corpus/D26.json")), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			scriptPath := writeWitnessFixture(t, directory, test.script)
			witnessPath := filepath.Join(directory, "witness.json")
			output, err := invokeWitnessOracle(t, node, scriptPath, filepath.Join(directory, "trace.json"), witnessPath)
			if test.wantError != "" && (err == nil || !strings.Contains(string(output), test.wantError)) {
				t.Fatalf("want failure %q, got %v\n%s", test.wantError, err, output)
			}
			if test.wantError == "" && err != nil {
				t.Fatalf("diagnostic: %v\n%s", err, output)
			}
			if _, err := os.Stat(witnessPath); !os.IsNotExist(err) {
				t.Fatal("failed or diagnostic run produced a complete witness")
			}
		})
	}
}

func TestOracleCausalWitnessExistingSidecarIsPreserved(t *testing.T) {
	node := witnessOracleNode(t)
	directory := t.TempDir()
	scriptPath := writeWitnessFixture(t, directory, `{"schema":"s0-script/v1","id":"D99","steps":[{"op":"snapshot","label":"done"}]}`)
	witnessPath := filepath.Join(directory, "witness.json")
	prior := []byte("pre-existing evidence must not be overwritten\n")
	if err := os.WriteFile(witnessPath, prior, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := invokeWitnessOracle(t, node, scriptPath, filepath.Join(directory, "trace.json"), witnessPath)
	if err == nil || !strings.Contains(string(output), "EEXIST") {
		t.Fatalf("existing sidecar did not fail closed: %v\n%s", err, output)
	}
	if !bytes.Equal(load(t, witnessPath), prior) {
		t.Fatal("oracle modified evidence owned by an earlier run")
	}
}
