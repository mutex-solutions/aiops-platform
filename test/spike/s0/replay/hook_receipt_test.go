// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	goruntime "runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/differential"
)

const (
	hookCriteriaPath         = "../../../../docs/design/s0-spike-criteria.md"
	hookEncodingMethodPath   = "../../../../docs/design/s0-hook-error-encoding.md"
	hookHistoricalOracleSHA  = "b0f4e4474e4ffba2b5c9316680fa21581c2032d699fb845d0a9c6c5d0d6488b1"
	hookHistoricalOraclePath = "../../../differential/oracle/history/replay-" + hookHistoricalOracleSHA + ".mjs"
)

func hookReceiptProof(before, after map[string]string, scriptBefore, scriptAfter []byte) error {
	var failures []error
	if before[hookCriteriaPath] != differential.CriteriaSHA256 || before[causalAmendment] != causalAmendmentSHA {
		failures = append(failures, errors.New("frozen criteria or owner-approved amendment identity mismatch"))
	}
	if !reflect.DeepEqual(before, after) {
		failures = append(failures, errors.New("candidate source manifest changed during semantic execution"))
	}
	if digest(scriptBefore) != digest(scriptAfter) {
		failures = append(failures, errors.New("executed script bytes changed during semantic execution"))
	}
	return errors.Join(failures...)
}

// The executable digest binds the running test binary. Local manifests and git
// HEAD are unsigned provenance, not proof that dirty source produced that binary;
// the parent retains the real outer go-test command/build log independently.
func hookCandidateIdentity(t *testing.T) map[string]any {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	head, headErr := exec.Command("git", "rev-parse", "--verify", "HEAD").Output()
	identity := map[string]any{
		"testExecutableSHA256":     digest(loadFixture(t, executable)),
		"actualTestExecutableArgs": append([]string(nil), os.Args...),
		"goRuntimeVersion":         goruntime.Version(), "goOS": goruntime.GOOS, "goArch": goruntime.GOARCH,
		"gomaxprocs": goruntime.GOMAXPROCS(0), "numCPU": goruntime.NumCPU(),
		"goCandidateBaseline": strings.TrimSpace(string(head)), "goCandidateBaselineError": fmt.Sprint(headErr),
		"raceInstrumentation": "not inferred from runtime; consult retained actual outer build/test command and build settings",
		"provenanceLimit":     "unsigned local hashes; baseline HEAD is not a clean-tree or build-source attestation; os.Args is the test process argv, not an invented go invocation; parent retains full outer command/environment log",
	}
	if build, ok := debug.ReadBuildInfo(); ok {
		identity["goBuildInfo"] = build // Actual embedded settings; no guessed flags.
	} else {
		identity["goBuildInfoUnavailable"] = true
	}
	return identity
}

func TestHookReceiptProofRejectsChangedOrUnboundSources(t *testing.T) {
	valid := map[string]string{hookCriteriaPath: differential.CriteriaSHA256, causalAmendment: causalAmendmentSHA, "hooks_adapter.go": "source-sha"}
	script := []byte("original script bytes\n")
	if err := hookReceiptProof(valid, valid, script, script); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"changed-source", "missing-source", "criteria", "amendment", "script"} {
		t.Run(mode, func(t *testing.T) {
			before, after := map[string]string{}, map[string]string{}
			for path, sha := range valid {
				before[path], after[path] = sha, sha
			}
			scriptAfter := script
			switch mode {
			case "changed-source":
				after["hooks_adapter.go"] = "different-source"
			case "missing-source":
				delete(after, "hooks_adapter.go")
			case "criteria":
				before[hookCriteriaPath], after[hookCriteriaPath] = "forged", "forged"
			case "amendment":
				before[causalAmendment], after[causalAmendment] = "forged", "forged"
			case "script":
				scriptAfter = append(append([]byte(nil), script...), '\n')
			}
			if hookReceiptProof(before, after, script, scriptAfter) == nil {
				t.Fatal("invalid provenance was eligible for a comparison pass")
			}
		})
	}
}
