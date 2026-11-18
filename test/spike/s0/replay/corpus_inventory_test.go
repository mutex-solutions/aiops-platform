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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

// This opt-in recorder is NOT an acceptance gate. It executes all 36 original
// files, including unsupported/diagnostic cases, then the distinct D13 v2 file.
// Expected bytes are read only after candidate capture and never execute actions.
const inventoryMethod = "../../../../docs/design/s0-d23-request-mapping.md"

// Both actual backends implement these optional APIs. Forwarding preserves their
// real native behavior while retaining unmodified typed inspection envelopes.
// No fixture callback or scheduler operation runs in this observation sidecar.
type inventoryRuntime struct {
	cc.Runtime
	inspections []cc.Inspection
}

func (r *inventoryRuntime) Inspect(ctx context.Context) (cc.Inspection, error) {
	inspection, err := r.Runtime.(cc.Inspector).Inspect(ctx)
	if err == nil {
		r.inspections = append(r.inspections, inspection)
	}
	return inspection, err
}
func (r *inventoryRuntime) Record(ctx context.Context, kind string, data map[string]any) error {
	return r.Runtime.(cc.Recorder).Record(ctx, kind, data)
}
func (r *inventoryRuntime) RootContext(ctx context.Context) (*cc.Context, error) {
	return r.Runtime.(cc.RootProvider).RootContext(ctx)
}
func (r *inventoryRuntime) AwaitFiber(ctx context.Context, id uint64) error {
	return r.Runtime.(cc.Waiter).AwaitFiber(ctx, id)
}
func (r *inventoryRuntime) RequestUpdateConfig(ctx context.Context, id uint64, value any, noSave bool) (cc.ConfigRequest, error) {
	return r.Runtime.(cc.ConfigRequester).RequestUpdateConfig(ctx, id, value, noSave)
}
func (r *inventoryRuntime) Notify(ctx context.Context, keys ...string) error {
	return r.Runtime.(cc.Notifier).Notify(ctx, keys...)
}

func inventoryError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func inventorySources(t *testing.T) map[string]string {
	sources := causalSources(t) // Exact existing code/doc allowlist, not the whole tree.
	for _, path := range []string{inventoryMethod, hookEncodingMethodPath,
		"../../../../docs/design/s0-corpus-revision-2.md", hookHistoricalOraclePath} {
		sources[path] = digest(loadFixture(t, path))
	}
	return sources
}

func inventoryPointer(trace differential.Trace, assertion differential.Assertion) (any, error) {
	var checkpoint *differential.Checkpoint
	for i := range trace.Checkpoints {
		if trace.Checkpoints[i].Label == assertion.At {
			checkpoint = &trace.Checkpoints[i]
			break
		}
	}
	if checkpoint == nil {
		return nil, fmt.Errorf("missing checkpoint %q", assertion.At)
	}
	var current any
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&current); err != nil {
		return nil, err
	}
	if assertion.Path == "" {
		return current, nil
	}
	for _, key := range strings.Split(assertion.Path[1:], "/") {
		key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[key]
			if !ok {
				return nil, fmt.Errorf("missing assertion path %s", assertion.Path)
			}
		case []any:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(value) {
				return nil, errors.New("invalid assertion index")
			}
			current = value[index]
		default:
			return nil, errors.New("assertion traverses scalar")
		}
	}
	return current, nil
}

// Metadata identifies one original assertion, not a map patch or replacement
// payload. Even this approved realm-local difference remains a failed original
// assertion and cannot become a strict match or an unchanged-source pass.
func inventoryAdaptation(id string, assertion differential.Assertion) string {
	if id == "D06" && assertion.At == "children" && assertion.Path == "/outputs/retained-inherited" {
		return "checklist row 8: inherited declarations are realm-local; original expected outer retained unchanged; see TestComposedD06ExplicitRealmLocalAdaptation"
	}
	return ""
}

func inventoryOracleProof(script, golden, receipt []byte, id string, sources map[string]string) (differential.RunRecord, error) {
	var record differential.RunRecord
	if err := json.Unmarshal(receipt, &record); err != nil {
		return record, err
	}
	result := "pass"
	if id == "D26" {
		result = "diagnostic"
	}
	command := []string{"S0_NODE", "--import", "oracle/register.mjs", "oracle/replay.mjs", "corpus/" + id + ".json", "OUTPUT"}
	if record.Schema != "s0-oracle-run/v1" || record.SourceCommit != differential.SourceCommit ||
		record.CriteriaSHA256 != differential.CriteriaSHA256 || record.Scripts[id] != digest(script) ||
		record.TraceSHA256 != digest(golden) || record.OracleSHA256 != hookHistoricalOracleSHA ||
		sources[hookHistoricalOraclePath] != hookHistoricalOracleSHA ||
		record.ResolverSHA256 != sources["../../../differential/oracle/register.mjs"] ||
		record.VendorManifestSHA256 != sources["../../../differential/oracle/manifest.json"] ||
		record.NodeVersion != "v24.22.1" || record.ExecutableSHA256 != causalNodeSHA ||
		record.ExitCode != 0 || record.NativeAttempts != 0 || record.Result != result ||
		!reflect.DeepEqual(record.Command, command) ||
		!reflect.DeepEqual(record.ResolvedPackages, []string{"@deepseek-ai/cordis", "@deepseek-ai/cosmokit"}) {
		return record, errors.New("saved oracle receipt/script/trace/historical source route identity mismatch")
	}
	return record, nil
}

// Close may rethrow a real retained fixture failure. Preserve the exact error and
// classify only actual native error leaves matching retained RuntimeFailures;
// deadlines, cancellation, unsupported errors and extra leaves remain unexplained.
func inventoryCloseClass(closeErr error, failures []RuntimeFailure) string {
	if closeErr == nil {
		return "nil"
	}
	var explained func(error) bool
	explained = func(err error) bool {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, ErrUnsupported) {
			return false
		}
		if group, ok := err.(interface{ Unwrap() []error }); ok {
			if len(group.Unwrap()) == 0 {
				return false
			}
			for _, child := range group.Unwrap() {
				if !explained(child) {
					return false
				}
			}
			return true
		}
		if child := errors.Unwrap(err); child != nil {
			return explained(child)
		}
		for _, failure := range failures {
			if errors.Is(failure.Error, err) {
				return true
			}
		}
		return false
	}
	if explained(closeErr) {
		return "retained-fixture-FAILED (not a nil error or parity pass)"
	}
	return "unexplained-kernel-or-harness-error"
}

func TestCorpusInventory(t *testing.T) {
	if os.Getenv("S0_CORPUS_INVENTORY") != "1" {
		t.Skip("NOT ACCEPTANCE: set S0_CORPUS_INVENTORY=1 to record all original D01..D36 on both real backends")
	}
	root := os.Getenv("S0_CORPUS_OUT")
	if root == "" || !filepath.IsAbs(root) {
		t.Fatal("S0_CORPUS_OUT must name a fresh absolute evidence root")
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("evidence root must be fresh, never overwritten: %v", err)
	}
	t.Logf("NOT ACCEPTANCE: recording original corpus; evidence=%s", root)
	sourcesBefore, identityBefore := inventorySources(t), hookCandidateIdentity(t)
	saveCausalEvidence(t, root, "sources-before.json", jsonBytes(t, sourcesBefore))
	saveCausalEvidence(t, root, "candidate-before.json", jsonBytes(t, identityBefore))
	counts := map[string]int{"match": 0, "failed": 0, "incomplete": 0}
	rows := []map[string]any{}
	for number := 1; number <= 37; number++ {
		id, variant := fmt.Sprintf("D%02d", number), "original"
		if number == 37 {
			id, variant = "D13", "revision-2-overlay"
		}
		scriptPath := "../../../differential/corpus/" + id + ".json"
		goldenPath := "../../../differential/results/corpus-final/" + id + ".trace.json"
		if variant != "original" {
			scriptPath = "../../../differential/corpus/revision-2/D13.json"
			goldenPath = "../../../differential/results/corpus-revision-2/D13.replay-1.trace.json"
		}
		for _, backend := range causalBackends {
			name := id + "-" + variant + "-" + backend.name
			dir := filepath.Join(root, name)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			scriptBytes := loadFixture(t, scriptPath)
			saveCausalEvidence(t, dir, "script.json", scriptBytes)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			runtime := &inventoryRuntime{Runtime: backend.make()} // Fresh native backend for every file.
			result, executeErr := RunComposedJSON(ctx, runtime, scriptBytes)
			// Capture before administrative Close; neither Close nor expected data
			// may add semantic events to the executed candidate trace.
			actual := jsonBytes(t, result.Trace)
			saveCausalEvidence(t, dir, "actual.trace.json", actual)
			saveCausalEvidence(t, dir, "actual.events.json", jsonBytes(t, result.RuntimeEvents))
			saveCausalEvidence(t, dir, "raw-inspections.json", jsonBytes(t, runtime.inspections))
			saveCausalEvidence(t, dir, "observed-inspections.json", jsonBytes(t, result.RuntimeInspections))
			saveCausalEvidence(t, dir, "snapshots.json", jsonBytes(t, result.RuntimeSnapshots))
			saveCausalEvidence(t, dir, "actual.witness.json", jsonBytes(t, actualWitness(scriptBytes, actual, result.CheckpointAfterEvent)))
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			closeErr := runtime.Close(closeCtx)
			closeCancel()
			cancel()
			closeClass := inventoryCloseClass(closeErr, result.RuntimeErrors)
			// No saved golden, receipt or expectation has been consulted above.
			golden, receipt := loadFixture(t, goldenPath), loadFixture(t, goldenPath+".run.json")
			saveCausalEvidence(t, dir, "expected.trace.json", golden)
			saveCausalEvidence(t, dir, "expected.run.json", receipt)
			record, proofErr := inventoryOracleProof(scriptBytes, golden, receipt, id, sourcesBefore)
			strictErr := differential.Compare(golden, actual) // Always full trace; no capability bypass.
			saveCausalEvidence(t, dir, "strict.txt", []byte(inventoryError(strictErr)))
			script, parseErr := differential.ParseScript(scriptBytes)
			assertions := []map[string]any{}
			failedAssertions, unexplainedAssertions := 0, 0
			if parseErr == nil {
				for index, assertion := range script.Expect {
					one := *script
					one.Expect = []differential.Assertion{assertion}
					assertErr := differential.CheckExpectations(one, result.Trace)
					value, pathErr := inventoryPointer(result.Trace, assertion)
					adaptation := inventoryAdaptation(id, assertion)
					if assertErr != nil {
						failedAssertions++
						if adaptation == "" {
							unexplainedAssertions++
						}
					}
					assertions = append(assertions, map[string]any{"index": index, "at": assertion.At, "path": assertion.Path,
						"expected": assertion.Equals, "actual": value, "actualPathErr": inventoryError(pathErr),
						"problem": inventoryError(assertErr), "originalPassed": assertErr == nil, "adaptationMetadata": adaptation})
				}
			}
			runtimeErrors := []map[string]any{}
			for _, failure := range result.RuntimeErrors {
				runtimeErrors = append(runtimeErrors, map[string]any{"operation": failure.Operation, "error": inventoryError(failure.Error), "type": fmt.Sprintf("%T", failure.Error)})
			}
			knownBoundary := ""
			if id == "D05" {
				knownBoundary = "not-carried unchecked ctx.get; incomplete, never raw parity"
			} else if id == "D26" {
				knownBoundary = "not-carried awaited own-dispose; separate saved oracle diagnostic, never synthesized in Go"
			} else if id == "D29" {
				knownBoundary = "missing native integration: plugin internal/update hooks; unsupported remains a failure"
			} else if id == "D13" && variant == "original" {
				knownBoundary = "original missing causal activation barrier; nondeterministic timeout retained; revision-2 is a separate execution"
			}
			classification := "match"
			if strictErr != nil || failedAssertions != 0 || executeErr != nil || closeErr != nil {
				classification = "failed"
			}
			if executeErr != nil || result.ComparisonError != nil || parseErr != nil || proofErr != nil {
				classification = "incomplete"
			}
			counts[classification]++
			row := map[string]any{"scenario": id, "variant": variant, "backend": backend.name, "classification": classification,
				"executeErr": inventoryError(executeErr), "unsupportedSentinel": errors.Is(executeErr, ErrUnsupported),
				"comparisonCapabilityErr": inventoryError(result.ComparisonError), "closeErr": inventoryError(closeErr), "closeClassification": closeClass,
				"strictErr": inventoryError(strictErr), "oracleProofErr": inventoryError(proofErr), "parseErr": inventoryError(parseErr),
				"assertions": assertions, "failedOriginalAssertions": failedAssertions, "runtimeErrors": runtimeErrors,
				"scriptSHA256": digest(scriptBytes), "actualTraceDigest": digest(actual), "goldenSHA256": digest(golden),
				"oracleHistoricalSource": hookHistoricalOraclePath, "oracleReceipt": record, "knownBoundary": knownBoundary,
				"frontend":        "RunComposedJSON (same shared native configuration cell and transforms)",
				"candidateNewIDs": "see actual native IDs, addresses, targets and UIDs in raw-inspections.json; no invented identifiers",
				"nativeEffects":   "see full native EffectTree/Effects in raw-inspections.json and snapshots.json",
				"rawEnvelopeNote": "native co.provision wrappers have unexported fields and JSON encodes their value as {}; raw-inspections/events retain that envelope. observed-inspections.json contains the already-projected actual typed provision observations, not golden substitutions",
				"notAcceptance":   true}
			if id == "D26" {
				oracle, oracleErr := differential.ParseTrace(golden)
				if oracleErr == nil {
					row["separateOracleDiagnosticDescriptor"] = oracle.Diagnostic
				}
				row["goDiagnosticFabricated"] = result.Trace.Diagnostic != nil
			}
			saveCausalEvidence(t, dir, "record.json", jsonBytes(t, row))
			rows = append(rows, map[string]any{"case": name, "classification": classification, "assertionFailures": failedAssertions, "evidence": dir})
			t.Logf("NOT ACCEPTANCE %s class=%s assertions=%d/%d failed unsupported=%t close=%s actual=%s", name, classification, failedAssertions, len(assertions), errors.Is(executeErr, ErrUnsupported), closeClass, digest(actual))
			// Only explicit not-carried boundaries can explain missing continuation
			// checkpoints, never make them pass. D06 explains just the exact path.
			notCarried := id == "D05" || id == "D26"
			if proofErr != nil || parseErr != nil || result.ComparisonError != nil ||
				closeClass == "unexplained-kernel-or-harness-error" ||
				(!notCarried && (executeErr != nil || unexplainedAssertions != 0)) ||
				(notCarried && (!errors.Is(executeErr, ErrUnsupported) || result.Trace.Diagnostic != nil)) ||
				(os.Getenv("S0_CORPUS_REQUIRE_ASSERTIONS") == "1" && !notCarried && unexplainedAssertions != 0) {
				t.Errorf("inventory has unexplained/missing behavior in %s; evidence=%s (NOT ACCEPTANCE)", name, dir)
			}
		}
	}
	sourcesAfter, identityAfter := inventorySources(t), hookCandidateIdentity(t)
	proofErr := hookReceiptProof(sourcesBefore, sourcesAfter, nil, nil)
	if !reflect.DeepEqual(identityBefore, identityAfter) {
		proofErr = errors.Join(proofErr, errors.New("actual test executable/argv/runtime identity changed during inventory"))
	}
	saveCausalEvidence(t, root, "sources-after.json", jsonBytes(t, sourcesAfter))
	saveCausalEvidence(t, root, "candidate-after.json", jsonBytes(t, identityAfter))
	saveCausalEvidence(t, root, "inventory.json", jsonBytes(t, map[string]any{"schema": "s0-corpus-inventory/v1", "NOTACCEPTANCE": true,
		"originalScripts": 36, "backends": 2, "additionalD13Revision2Executions": 2, "recordedExecutions": len(rows),
		"counts": counts, "cases": rows, "sourceWitnessErr": inventoryError(proofErr), "oracleExecution": "none; frozen goldens read after candidate execution",
		"scope": "bounded diagnostic inventory only; match/failed/incomplete separate, no total-pass claim and no altered assertion payloads"}))
	t.Logf("NOT ACCEPTANCE SUMMARY: executions=%d match=%d failed=%d incomplete=%d evidence=%s", len(rows), counts["match"], counts["failed"], counts["incomplete"], root)
	if proofErr != nil {
		t.Errorf("inventory source witness invalid: %v", proofErr)
	}
}
