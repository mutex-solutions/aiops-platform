// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

const (
	causalNode         = "/tmp/aiops-s0-node-24.22.1-u4gdr6qr/bin/node"
	causalNodeSHA      = "e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb"
	causalAmendment    = "../../../../docs/design/s0-causal-comparison-amendment.md"
	causalAmendmentSHA = "becf5c0634ec98c279d76db3ac3dc1c08cea70cd2d66301393c445b3df73559a"
)

var causalBackends = []struct {
	name string
	make func() concurrency.Runtime
}{{"reconciler", concurrency.NewReconciler}, {"graph-lock", concurrency.NewGraphLock}}

// actualWitness binds exact emitted bytes, not a remarshal or a desired cut.
func actualWitness(script, trace []byte, cuts []differential.CausalCut) differential.CausalWitness {
	return differential.CausalWitness{Schema: differential.CausalWitnessSchema,
		ScriptSHA256: digest(script), TraceSHA256: digest(trace),
		Checkpoints: append([]differential.CausalCut{}, cuts...)}
}

func checkCausalWitness(script, raw []byte, witness differential.CausalWitness) error {
	trace, err := differential.ParseTrace(raw)
	if err != nil {
		return err
	}
	if witness.Schema != differential.CausalWitnessSchema || witness.ScriptSHA256 != digest(script) || witness.TraceSHA256 != digest(raw) {
		return errors.New("witness does not bind exact script/trace bytes")
	}
	if witness.Checkpoints == nil || len(witness.Checkpoints) != len(trace.Checkpoints) {
		return errors.New("witness checkpoint count differs from actual trace")
	}
	previous, seen := 0, map[string]bool{}
	for i, cut := range witness.Checkpoints {
		if cut.Label == "" || seen[cut.Label] || cut.Label != trace.Checkpoints[i].Label || cut.AfterEvent < previous || cut.AfterEvent > len(trace.Events) {
			return fmt.Errorf("invalid witnessed cut %d: %+v", i, cut)
		}
		previous, seen[cut.Label] = cut.AfterEvent, true
	}
	return nil
}

func captureCausalActual(makeRuntime func() concurrency.Runtime, script []byte, composed bool) (Result, error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backend := makeRuntime() // Never reuse a backend across scripts or candidates.
	run := RunJSON
	if composed {
		run = RunComposedJSON
	}
	result, runErr := run(ctx, backend, script)
	// Freeze observations before Close; its administrative disposal is not input
	// to either comparator. The unchanged selected scripts dispose all fibers.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer closeCancel()
	return result, runErr, backend.Close(closeCtx)
}

// Ordinary tests establish actual witness plumbing only, never oracle parity.
func TestActualCausalWitnessObservations(t *testing.T) {
	for _, id := range []string{"D10", "D16"} {
		for _, backend := range causalBackends {
			t.Run(id+"/"+backend.name, func(t *testing.T) {
				script := loadFixture(t, "../../../differential/corpus/"+id+".json")
				result, runErr, closeErr := captureCausalActual(backend.make, script, false)
				if err := errors.Join(runErr, closeErr, result.ComparisonError); err != nil {
					t.Fatal(err)
				}
				raw := append(jsonBytes(t, result.Trace), '\n')
				witness := actualWitness(script, raw, result.CheckpointAfterEvent)
				if err := checkCausalWitness(script, raw, witness); err != nil {
					t.Fatal(err)
				}
				if witness.TraceSHA256 == digest(raw[:len(raw)-1]) {
					t.Fatal("witness hashed normalized JSON instead of exact bytes")
				}
				assertNativeCheckpoints(t, result)
				for i, event := range result.RuntimeEvents {
					if event.Sequence != uint64(i+1) {
						t.Fatal("native ledger lost sequence/multiplicity")
					}
				}
				for _, cut := range witness.Checkpoints {
					inspection := result.RuntimeInspections[cut.Label]
					var prefix []concurrency.Event
					for _, event := range result.RuntimeEvents {
						if event.Sequence > inspection.Sequence {
							break
						}
						prefix = append(prefix, event)
					}
					observed, err := project(prefix)
					if err != nil || cut.AfterEvent != len(observed) {
						t.Fatalf("cut %s did not use actual Inspect.Sequence=%d: %+v %v", cut.Label, inspection.Sequence, cut, err)
					}
				}
				witness.Checkpoints[0].AfterEvent++
				if reflect.DeepEqual(witness.Checkpoints, result.CheckpointAfterEvent) {
					t.Fatal("sidecar aliases mutable native result cuts")
				}
			})
		}
	}
}

func TestActualCausalWitnessZeroRepeatedCutAndTail(t *testing.T) {
	// No expected fixture values enter execution. The last output remains after
	// the last checkpoint; an empty and repeated cut are actual observations.
	script := []byte(` {"schema":"s0-script/v1","id":"D99","title":"Actual checkpoint positions","covers":["witness-cuts"],"plugins":[],"steps":[
		{"op":"snapshot","label":"zero"},{"op":"output","to":"v","value":1},
		{"op":"snapshot","label":"one"},{"op":"snapshot","label":"same"},
		{"op":"output","to":"v","value":2}]} ` + "\n")
	for _, backend := range causalBackends {
		t.Run(backend.name, func(t *testing.T) {
			result, runErr, closeErr := captureCausalActual(backend.make, script, false)
			if err := errors.Join(runErr, closeErr); err != nil {
				t.Fatal(err)
			}
			raw := jsonBytes(t, result.Trace)
			witness := actualWitness(script, raw, result.CheckpointAfterEvent)
			if err := checkCausalWitness(script, raw, witness); err != nil {
				t.Fatal(err)
			}
			want := []differential.CausalCut{{Label: "zero", AfterEvent: 0}, {Label: "one", AfterEvent: 1}, {Label: "same", AfterEvent: 1}}
			if !reflect.DeepEqual(witness.Checkpoints, want) || len(result.Trace.Events) != 2 {
				t.Fatalf("lost actual empty/repeated cuts or raw tail: %+v", result)
			}
		})
	}
}

func saveCausalEvidence(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
}

func causalSources(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	for _, pattern := range []string{"*.go", "../concurrency/*.go", "../composition/*.go", "../contextmodel/*.go", "../hooks/*.go", "../observers/*.go", "../internal/*/*.go", "../../../differential/*.go", "../../../differential/oracle/*.*", "../../../../go.mod", "../../../../go.sum", "../../../../docs/design/s0-spike-criteria.md", "../../../../docs/design/s0-reference-pins.md", causalAmendment, "../../../../docs/design/s0-causal-comparison-freeze.json"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
				continue
			}
			sources[filepath.ToSlash(path)] = digest(loadFixture(t, path))
		}
	}
	return sources
}

// A default SKIP is explicitly not acceptance. An opted-in mismatch or missing
// proof FAILS, including ErrCausalUnsupported for these selected supported cases.
func TestCausalReplayIntegration(t *testing.T) {
	if os.Getenv("S0_CAUSAL_RUN") != "1" {
		t.Skip("not acceptance: set S0_CAUSAL_RUN=1 for live, pinned oracle/candidate comparison")
	}
	nodeBytes, err := os.ReadFile(causalNode)
	if os.IsNotExist(err) {
		t.Skip("not acceptance: exact isolated Node 24.22.1 is absent; no fallback")
	}
	if err != nil || digest(nodeBytes) != causalNodeSHA {
		t.Fatalf("exact isolated Node identity failed: %v", err)
	}
	if digest(loadFixture(t, causalAmendment)) != causalAmendmentSHA {
		t.Fatal("owner-approved immutable comparison amendment changed")
	}
	for _, id := range []string{"D02", "D10", "D12", "D16"} {
		for _, backend := range causalBackends {
			t.Run(id+"/"+backend.name, func(t *testing.T) {
				dir := t.TempDir()
				if root := os.Getenv("S0_CAUSAL_EVIDENCE_DIR"); root != "" {
					dir = filepath.Join(root, t.Name())
					if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dir, 0o700); err != nil {
						t.Fatalf("evidence leaf must be fresh (never overwrite): %v", err)
					}
				}
				t.Logf("complete strict/causal evidence: %s", dir)
				scriptPath, err := filepath.Abs("../../../differential/corpus/" + id + ".json")
				if err != nil {
					t.Fatal(err)
				}
				script := loadFixture(t, scriptPath)
				sources := causalSources(t)
				saveCausalEvidence(t, dir, "script.json", script)
				saveCausalEvidence(t, dir, "sources-before.json", jsonBytes(t, sources))
				result, runErr, closeErr := captureCausalActual(backend.make, script, id == "D02")
				actual := jsonBytes(t, result.Trace)
				actualSidecar := actualWitness(script, actual, result.CheckpointAfterEvent)
				saveCausalEvidence(t, dir, "actual.trace.json", actual)
				saveCausalEvidence(t, dir, "actual.witness.json", jsonBytes(t, actualSidecar))
				saveCausalEvidence(t, dir, "actual.events.json", jsonBytes(t, result.RuntimeEvents))
				saveCausalEvidence(t, dir, "actual.inspections.json", jsonBytes(t, result.RuntimeInspections))
				// The expected trace/sidecar are freshly executed only AFTER actual
				// capture. Neither historical goldens nor expected values are read.
				oracle, err := filepath.Abs("../../../differential/oracle")
				if err != nil {
					t.Fatal(err)
				}
				expectedPath, witnessPath := filepath.Join(dir, "expected.trace.json"), filepath.Join(dir, "expected.witness.json")
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, causalNode, "--import", filepath.Join(oracle, "register.mjs"), filepath.Join(oracle, "replay.mjs"), scriptPath, expectedPath)
				for _, entry := range os.Environ() {
					if !strings.HasPrefix(entry, "NODE_OPTIONS=") && !strings.HasPrefix(entry, "NODE_PATH=") && !strings.HasPrefix(entry, "CORDIS_SHARED=") && !strings.HasPrefix(entry, "S0_CAUSAL_WITNESS_PATH=") {
						command.Env = append(command.Env, entry)
					}
				}
				command.Env = append(command.Env, "S0_CAUSAL_WITNESS_PATH="+witnessPath)
				output, oracleErr := command.CombinedOutput()
				saveCausalEvidence(t, dir, "oracle.output.txt", output)
				if oracleErr != nil {
					t.Fatalf("fresh oracle failed (actual retained): %v; %s", oracleErr, output)
				}
				expected := loadFixture(t, expectedPath) // Preserve CLI's newline.
				var expectedSidecar differential.CausalWitness
				if err := json.Unmarshal(loadFixture(t, witnessPath), &expectedSidecar); err != nil {
					t.Fatal(err)
				}
				strict := differential.Compare(expected, actual)
				causal := differential.CompareCausal(script, expected, actual, expectedSidecar, actualSidecar)
				var record differential.RunRecord
				if err := json.Unmarshal(loadFixture(t, expectedPath+".run.json"), &record); err != nil {
					t.Fatal(err)
				}
				proofErr := errors.Join(checkCausalWitness(script, expected, expectedSidecar), checkCausalWitness(script, actual, actualSidecar))
				if record.SourceCommit != differential.SourceCommit || record.CriteriaSHA256 != differential.CriteriaSHA256 || record.NodeVersion != "v24.22.1" || record.ExecutableSHA256 != causalNodeSHA || record.OracleSHA256 != sources["../../../differential/oracle/replay.mjs"] || record.ResolverSHA256 != sources["../../../differential/oracle/register.mjs"] || record.VendorManifestSHA256 != sources["../../../differential/oracle/manifest.json"] || record.TraceSHA256 != digest(expected) || record.Scripts[id] != digest(script) || record.ExitCode != 0 || record.NativeAttempts != 0 || record.Result != "pass" {
					proofErr = errors.Join(proofErr, errors.New("fresh oracle execution receipt identity mismatch"))
				}
				parsed, parseErr := differential.ParseScript(script)
				assertErr := parseErr
				if parseErr == nil {
					assertErr = differential.CheckExpectations(*parsed, result.Trace) // Post-run, independent assertions only.
				}
				after := causalSources(t)
				if !reflect.DeepEqual(sources, after) || digest(loadFixture(t, scriptPath)) != digest(script) {
					proofErr = errors.Join(proofErr, errors.New("source/script changed during execution; invalid receipt"))
				}
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				classification := "match"
				if causal != nil {
					classification = "mismatch-or-invalid"
					if errors.Is(causal, differential.ErrCausalUnsupported) {
						classification = "unsupported (selected-supported case: failure)"
					}
				}
				saveCausalEvidence(t, dir, "strict.txt", []byte(fmt.Sprint(strict)))
				saveCausalEvidence(t, dir, "causal.txt", []byte(fmt.Sprint(causal)))
				saveCausalEvidence(t, dir, "receipt.json", jsonBytes(t, map[string]any{
					"schema": "s0-causal-integration/v1", "scenario": id, "backend": backend.name, "command": command.Args,
					"scriptSHA256": digest(script), "expectedSHA256": digest(expected), "actualSHA256": digest(actual),
					"expectedWitnessSHA256": digest(loadFixture(t, witnessPath)), "actualWitnessSHA256": digest(jsonBytes(t, actualSidecar)),
					"nodeSHA256": causalNodeSHA, "testExecutableSHA256": digest(loadFixture(t, executable)),
					"criteriaSHA256": differential.CriteriaSHA256, "amendmentSHA256": causalAmendmentSHA, "sources": sources, "sourcesAfter": after,
					"strictError": fmt.Sprint(strict), "causalError": fmt.Sprint(causal), "causalClassification": classification,
					"runError": fmt.Sprint(runErr), "closeError": fmt.Sprint(closeErr), "observationError": fmt.Sprint(result.ComparisonError),
					"proofError": fmt.Sprint(proofErr), "assertionError": fmt.Sprint(assertErr), "runtimeErrors": fmt.Sprint(result.RuntimeErrors),
					"scope": "complete raw and strict retained; differential comparison only, not full S0 acceptance",
				}))
				t.Logf("strict=%v; causal=%v; classification=%s", strict, causal, classification)
				if err := errors.Join(runErr, closeErr, result.ComparisonError, proofErr, assertErr, causal); err != nil {
					t.Fatalf("causal candidate/source parity FAILED (strict diagnostic retained): %v; evidence=%s", err, dir)
				}
			})
		}
	}
}
