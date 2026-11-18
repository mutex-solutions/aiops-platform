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
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/differential"
)

// causalRateScenarios lists the 36 corpus scenarios with the latest fixture
// revision where one exists. D05 and D26 are explicit not-carried boundaries; the
// candidates cannot execute them, so they are recorded as not run, not as a result.
func causalRateScenarios() []causalScenario {
	var out []causalScenario
	for i := 1; i <= 36; i++ {
		id := fmt.Sprintf("D%02d", i)
		path := corpusPath(id)
		switch id {
		case "D13":
			path = "../../../differential/corpus/revision-3/D13.json"
		case "D22":
			path = "../../../differential/corpus/revision-4/D22.json"
		}
		out = append(out, causalScenario{id, path, id != "D10" && id != "D12" && id != "D16"})
	}
	return out
}

type causalRate struct {
	Scenario        string         `json:"scenario"`
	Fixture         string         `json:"fixture"`
	Backend         string         `json:"backend"`
	Runs            int            `json:"runs"`
	StrictMatch     int            `json:"strictMatch"`
	CausalMatch     int            `json:"causalMatch"`
	CausalMismatch  int            `json:"causalMismatch"`
	CausalNotProved int            `json:"causalUnsupported"`
	HarnessErrors   int            `json:"harnessErrors"`
	NotRun          string         `json:"notRun,omitempty"`
	Verdicts        map[string]int `json:"verdicts"`
}

// freshOracle executes the pinned oracle once for a script, with its witness.
func freshOracle(t *testing.T, scriptPath, dir string) (expected []byte, witness differential.CausalWitness) {
	t.Helper()
	oracle, err := filepath.Abs("../../../differential/oracle")
	if err != nil {
		t.Fatal(err)
	}
	expectedPath, witnessPath := filepath.Join(dir, "expected.trace.json"), filepath.Join(dir, "expected.witness.json")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, causalNode, "--import", filepath.Join(oracle, "register.mjs"), filepath.Join(oracle, "replay.mjs"), scriptPath, expectedPath)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "NODE_OPTIONS=") && !strings.HasPrefix(entry, "NODE_PATH=") && !strings.HasPrefix(entry, "CORDIS_SHARED=") && !strings.HasPrefix(entry, "S0_CAUSAL_WITNESS_PATH=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "S0_CAUSAL_WITNESS_PATH="+witnessPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fresh oracle failed: %v; %s", err, output)
	}
	expected = loadFixture(t, expectedPath)
	if err := json.Unmarshal(loadFixture(t, witnessPath), &witness); err != nil {
		t.Fatal(err)
	}
	return expected, witness
}

// TestCausalRates replays every scenario N times on each candidate against one
// fresh oracle execution and tallies strict and causal verdicts. It is the
// recorder for docs/design/s0-causal-coverage.md and never changes a verdict.
// S0_CAUSAL_RATE=<runs> and S0_CAUSAL_RATE_OUT=<fresh file> opt in.
func TestCausalRates(t *testing.T) {
	requireCausalLive(t)
	runsText := os.Getenv("S0_CAUSAL_RATE")
	if runsText == "" {
		t.Skip("set S0_CAUSAL_RATE=<runs> and S0_CAUSAL_RATE_OUT=<new file>")
	}
	runs, err := strconv.Atoi(runsText)
	if err != nil || runs < 1 {
		t.Fatalf("bad S0_CAUSAL_RATE %q", runsText)
	}
	only := os.Getenv("S0_CAUSAL_ONLY")
	var table []causalRate
	for _, sc := range causalRateScenarios() {
		if only != "" && !strings.Contains(only, sc.id) {
			continue
		}
		if sc.id == "D05" || sc.id == "D26" {
			for _, b := range causalBackends {
				table = append(table, causalRate{Scenario: sc.id, Fixture: strings.TrimPrefix(sc.path, "../../../differential/"), Backend: b.name, NotRun: "explicit not-carried boundary (decision record section 15)"})
			}
			continue
		}
		scriptPath, err := filepath.Abs(sc.path)
		if err != nil {
			t.Fatal(err)
		}
		script := loadFixture(t, scriptPath)
		expected, expectedWitness := freshOracle(t, scriptPath, t.TempDir())
		for _, backend := range causalBackends {
			row := causalRate{Scenario: sc.id, Fixture: strings.TrimPrefix(sc.path, "../../../differential/"), Backend: backend.name, Verdicts: map[string]int{}}
			for i := 0; i < runs; i++ {
				result, runErr, closeErr := captureCausalActual(backend.make, script, sc.composed)
				row.Runs++
				if err := errors.Join(runErr, closeErr, result.ComparisonError); err != nil {
					row.HarnessErrors++
					row.Verdicts["harness: "+err.Error()]++
					continue
				}
				actual := jsonBytes(t, result.Trace)
				sidecar := actualWitness(script, actual, result.CheckpointAfterEvent)
				if differential.Compare(expected, actual) == nil {
					row.StrictMatch++
				}
				verdict := differential.CompareCausal(script, expected, actual, expectedWitness, sidecar)
				switch {
				case verdict == nil:
					row.CausalMatch++
					row.Verdicts["match"]++
				case errors.Is(verdict, differential.ErrCausalUnsupported):
					row.CausalNotProved++
					row.Verdicts[verdict.Error()]++
				default:
					row.CausalMismatch++
					row.Verdicts[verdict.Error()]++
				}
			}
			table = append(table, row)
			t.Logf("%s %s: causal match %d/%d strict %d/%d", sc.id, backend.name, row.CausalMatch, row.Runs, row.StrictMatch, row.Runs)
		}
	}
	sort.SliceStable(table, func(i, j int) bool { return table[i].Scenario < table[j].Scenario })
	if out := os.Getenv("S0_CAUSAL_RATE_OUT"); out != "" {
		file, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.MarshalIndent(table, "", " ")
		_, writeErr := file.Write(append(encoded, '\n'))
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Fatal(err)
		}
	}
}
