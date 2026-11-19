// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
)

// Set with -ldflags=-X at compilation, AFTER hashing the final source closure.
// An environment variable alone cannot bind a stale executable to current source.
var diagnosticCompiledSourceSHA256, diagnosticCompiledBuildFlags string

const diagnosticLabel = "single-trial-NoSampleFloor-NoFinalParity"

var diagnosticSourceClasses = []string{
	"sdk/kernel", "test/spike/s0/concurrency", "test/spike/s0/composition",
	"test/spike/s0/contextmodel", "test/spike/s0/internal/eventqueue", "test/spike/s0/scale",
}

type diagnosticSource struct {
	SHA256 string `json:"sha256"`
	Text   string `json:"text"`
}

type diagnosticFence struct {
	SHA256        string                      `json:"sha256"`
	SourceClasses []string                    `json:"sourceClasses"`
	Files         map[string]diagnosticSource `json:"files"`
}

// Explicit package boundary: dependency production Go, all compiled scale test
// Go, root module files, original criteria, freeze receipt, and the amendment.
// The archive includes actual source (including native event/Inspect plumbing).
func diagnosticSources() (diagnosticFence, error) {
	root := filepath.Clean("../../../..") // go test executes in this package directory.
	paths := []string{"go.mod", "go.sum", "docs/design/s0-spike-criteria.md", "docs/design/s0-criteria-freeze.json", "docs/design/s0-causal-comparison-amendment.md"}
	for _, class := range diagnosticSourceClasses {
		matches, err := filepath.Glob(filepath.Join(root, class, "*.go"))
		if err != nil || len(matches) == 0 {
			return diagnosticFence{}, fmt.Errorf("source class %s: matches=%d: %w", class, len(matches), err)
		}
		for _, path := range matches {
			if class != "test/spike/s0/scale" && strings.HasSuffix(path, "_test.go") {
				continue // dependency tests are not compiled into this executable.
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return diagnosticFence{}, err
			}
			paths = append(paths, filepath.ToSlash(relative))
		}
	}
	slices.Sort(paths)
	fence := diagnosticFence{SourceClasses: slices.Clone(diagnosticSourceClasses), Files: make(map[string]diagnosticSource)}
	hash := sha256.New()
	for _, path := range paths {
		text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return fence, err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(text))
		if path == "docs/design/s0-spike-criteria.md" && digest != "628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c" {
			return fence, fmt.Errorf("original criteria hash changed: %s", digest)
		}
		fence.Files[path] = diagnosticSource{SHA256: digest, Text: string(text)}
		fmt.Fprintf(hash, "%s\x00%s\n", path, digest)
	}
	fence.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return fence, nil
}

func diagnosticJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	// Compact streaming JSON keeps every event/archive without constructing a
	// second indented 100MB string. Nothing truncates the native retained history.
	return errors.Join(json.NewEncoder(file).Encode(value), file.Close())
}

type diagnosticValue struct {
	Sequence uint64 `json:"eventSequence"`
	Fiber    uint64 `json:"fiber"`
	Key      string `json:"key"`
	UID      uint64 `json:"uid"`
	RawType  string `json:"rawType"`
	Observed bool   `json:"observed"`
	Method   string `json:"method"`
	Value    any    `json:"value"`
}

func diagnosticValues(result RunResult) []diagnosticValue {
	var values []diagnosticValue
	for _, event := range result.Events {
		if event.Key == "" {
			continue
		}
		value := diagnosticValue{Sequence: event.Sequence, Fiber: event.Fiber, Key: event.Key, UID: event.UID, RawType: fmt.Sprintf("%T", event.Value), Method: "no directly observed value"}
		if actual, ok := co.ProvisionObservation(event.Value); ok {
			value.Value, value.Observed, value.Method = actual, true, "composition.ProvisionObservation (immutable actual provision)"
		} else if actual, ok := event.Value.(int); ok {
			value.Value, value.Observed, value.Method = actual, true, "native actual int"
		}
		values = append(values, value)
	}
	return values
}

func diagnosticExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), errors.Join(err, file.Close())
}

// TestDiagnosticSourceFence performs no lifecycle work. Its digest is consumed
// by the parent's subsequent optimized, non-race compilation, not by acceptance.
func TestDiagnosticSourceFence(t *testing.T) {
	fence, err := diagnosticSources()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("DIAGNOSTIC_SOURCE_SHA256=%s sourceFiles=%d sourceClasses=%v", fence.SHA256, len(fence.Files), fence.SourceClasses)
}

func diagnosticCaseValid(selected string) bool {
	topology, backend, ok := strings.Cut(selected, "/")
	return ok && slices.Contains([]string{"independent", "chain", "fanout", "diamonds", "realms"}, topology) &&
		slices.Contains([]string{"reconciler", "graph-lock", "kernel"}, backend)
}

func TestDiagnosticCaseSelector(t *testing.T) {
	for _, topology := range []string{"independent", "chain", "fanout", "diamonds", "realms"} {
		for _, backend := range []string{"reconciler", "graph-lock", "kernel"} {
			if !diagnosticCaseValid(topology + "/" + backend) {
				t.Errorf("exact diagnostic case rejected: %s/%s", topology, backend)
			}
		}
	}
	for _, invalid := range []string{"", "all", "trees/reconciler", "chain/unknown", "chain/graph-lock/extra", " chain/graph-lock", "chain/graph-lock "} {
		if diagnosticCaseValid(invalid) {
			t.Errorf("invalid diagnostic selector accepted: %q", invalid)
		}
	}
}

// v2 requires ONE exact topology/backend per executable process. The parent
// orchestrates all ten fresh processes and retains every exit/result, including
// failures. v1's in-process ten-case mode is retired: unfinished native work from
// a failed root can contaminate later timing and goroutine ownership evidence.
// No trees, sample-floor waiver, percentiles, budget judgement, or parity claim.
func TestDiagnostic10k(t *testing.T) {
	if os.Getenv("S0_SCALE_DIAGNOSTIC") != "1" {
		t.Skip("diagnostic NOT acceptance: set S0_SCALE_DIAGNOSTIC=1 only after parent source freeze; " + diagnosticLabel)
	}
	if os.Getenv("S0_PROPERTY_FULL") == "1" || os.Getenv("S0_SCALE_RUN") == "1" {
		t.Fatal("refuse competing full property/default-scale authorization in this process; other host processes remain parent-owned")
	}
	selected := os.Getenv("S0_SCALE_DIAGNOSTIC_CASE")
	if !diagnosticCaseValid(selected) {
		t.Fatalf("no execution: S0_SCALE_DIAGNOSTIC_CASE must name one exact topology/backend; got %q", selected)
	}
	before, err := diagnosticSources()
	if err != nil {
		t.Fatal(err)
	}
	if !validDigest(diagnosticCompiledSourceSHA256) || diagnosticCompiledSourceSHA256 != before.SHA256 || diagnosticCompiledBuildFlags == "" {
		t.Fatalf("no execution: parent must recompile frozen closure with linker source/build receipts: compiled=%q current=%s flags=%q", diagnosticCompiledSourceSHA256, before.SHA256, diagnosticCompiledBuildFlags)
	}
	out := os.Getenv("S0_SCALE_DIAGNOSTIC_OUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), t.Name())
	}
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal("fresh exclusive diagnostic output directory required: ", err)
	}
	if err := diagnosticJSON(filepath.Join(out, "source-closure.json"), before); err != nil {
		t.Fatal(err)
	}
	executable, err := diagnosticExecutable()
	if err != nil {
		t.Fatal(err)
	}
	build, _ := debug.ReadBuildInfo()
	identity := map[string]any{
		"schema": "s0-scale-diagnostic/v2", "sourceGeneration": "process-isolated-case-selector-v2",
		"selectedCase": selected, "pid": os.Getpid(),
		"processIsolation": "one selected case per process; parent must collect all ten fresh-process exits/results",
		"label":            diagnosticLabel, "acceptance": false, "criteriaChanged": false,
		"compiledSourceSHA256": diagnosticCompiledSourceSHA256, "executableSHA256": executable,
		"compiledBuildFlags": diagnosticCompiledBuildFlags, "buildInfo": build,
		"goVersion": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH,
		"arguments": os.Args, "GOFLAGS": os.Getenv("GOFLAGS"), "GOMAXPROCS": runtime.GOMAXPROCS(0),
		"repetitions": 1, "trialsPerBackendGraph": 1, "expectedTrialsTotal": 1, "expectedOrchestratorTrials": 10,
		"quantiles":            "unavailable: n=1; NearestRank definition ceil(p*n), NOT a p99 estimate",
		"hostNoise":            "not controlled/accepted here; parent owns quiet-host evidence",
		"pendingObserverQueue": "ObserverStatus exists but existing RunT does not sample it; event history is not queue backlog",
		"rawValueEncoding":     "raw composition provision JSON retains {} private-field encoding; auxiliary values use only actual immutable ProvisionObservation, never expected fixture values; removal nil remains unavailable",
		"sourceClasses":        diagnosticSourceClasses,
	}
	if err := diagnosticJSON(filepath.Join(out, "identity.json"), identity); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s NOT acceptance; case=%s pid=%d sourceBefore=%s executable=%s evidence=%s", diagnosticLabel, selected, os.Getpid(), before.SHA256, executable, out)
	for _, topology := range []Topology{Independent, Chain, FanOut, Diamonds, Realms} {
		if !strings.HasPrefix(selected, string(topology)+"/") {
			continue
		}
		fixture, err := Graph(topology, GraphFibers)
		if err != nil || len(fixture.Nodes) != 10_000 {
			t.Fatalf("frozen topology %s: count=%d error=%v", topology, len(fixture.Nodes), err)
		}
		for _, backend := range supportedBackends(t) {
			if selected != string(topology)+"/"+backend.Name {
				continue
			}
			t.Run(selected, func(t *testing.T) {
				current, err := diagnosticSources()
				if err != nil || current.SHA256 != before.SHA256 {
					t.Fatalf("INVALID before execution: sourceBefore=%s current=%s error=%v", before.SHA256, current.SHA256, err)
				}
				trialDir := filepath.Join(out, string(topology)+"-"+backend.Name)
				if err := os.Mkdir(trialDir, 0o755); err != nil {
					t.Fatal(err)
				}
				result := RunT(t.Context(), backend, fixture) // existing 15s/op + fresh 15s final cleanup; no runner changes.
				result.Repetition, result.Trial = 0, 0        // one zero-based repetition/trial, never a floor-satisfying sample.
				after, fenceErr := diagnosticSources()
				classification := result.Classification
				if fenceErr != nil || after.SHA256 != before.SHA256 {
					classification = Invalid // raw candidate failures remain unchanged in result.Errors.
				}
				record := struct {
					Label          string            `json:"label"`
					Classification Classification    `json:"diagnosticClassification"`
					SourceBefore   string            `json:"sourceBeforeSHA256"`
					SourceAfter    string            `json:"sourceAfterSHA256"`
					SourceError    string            `json:"sourceError,omitempty"`
					Values         []diagnosticValue `json:"actualValueObservations"`
					Result         RunResult         `json:"rawRunTResult"`
				}{Label: diagnosticLabel, Classification: classification, SourceBefore: before.SHA256, SourceAfter: after.SHA256, Values: diagnosticValues(result), Result: result}
				if fenceErr != nil {
					record.SourceError = fenceErr.Error()
				}
				if err := diagnosticJSON(filepath.Join(trialDir, "result.json"), record); err != nil {
					t.Fatal(err)
				}
				for _, sample := range result.Samples {
					t.Logf("%s/%s %s n=1 wall=%dns error=%q", topology, backend.Name, sample.Operation, sample.LatencyNS, sample.Error)
				}
				t.Logf("%s/%s diagnostic=%s sourceBefore=%s sourceAfter=%s events=%d archives=%d errors=%d file=%s", topology, backend.Name, classification, before.SHA256, after.SHA256, len(result.Events), len(result.FinalFibers), len(result.Errors), filepath.Join(trialDir, "result.json"))
				if classification == Invalid || len(result.Errors) != 0 {
					t.Errorf("NOT acceptance; diagnostic=%s raw backend errors=%v sourceError=%v", classification, result.Errors, fenceErr)
				}
			}) // Parent launches every other case in its own fresh process, even after this failure.
		}
	}
}

// This tiny author smoke tests actual lifecycle JSON preservation, not scale.
// It is separate from the gated ten 10k roots and never raises sample counts.
func TestDiagnostic15FiberSmoke(t *testing.T) {
	fixture, err := RealmFixture(3, 4)
	if err != nil || len(fixture.Nodes) != 15 {
		t.Fatalf("15-fiber fixture: %v", err)
	}
	for _, backend := range supportedBackends(t) {
		t.Run(backend.Name, func(t *testing.T) {
			result := RunT(context.Background(), backend, fixture)
			path := filepath.Join(t.TempDir(), "result.json")
			values := diagnosticValues(result)
			if err := diagnosticJSON(path, map[string]any{"raw": result, "actualValueObservations": values}); err != nil {
				t.Fatal(err)
			}
			if len(result.Errors) != 0 || len(result.FinalFibers) != 15 || len(result.Events) == 0 {
				t.Errorf("tiny diagnostic only: fibers=%d events=%d errors=%v", len(result.FinalFibers), len(result.Events), result.Errors)
			}
			observed := 0
			for _, value := range values {
				if value.Observed {
					observed++
					if _, ok := value.Value.(int); !ok {
						t.Errorf("actual immutable provision type %T", value.Value)
					}
				}
			}
			if observed == 0 {
				t.Error("actual composition provider observations missing")
			}
			if err := diagnosticJSON(path, result); err == nil {
				t.Error("exclusive evidence file was overwritten")
			}
			if !reflect.DeepEqual(diagnosticValues(result), values) {
				t.Error("value projection changed original causal order")
			}
		})
	}
}
