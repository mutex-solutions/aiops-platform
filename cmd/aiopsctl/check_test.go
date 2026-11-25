// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	"github.com/mutex-solutions/aiops-platform/test/dist/harness"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// standingRule is why the cases of standing rule 2 skip under -short: make check and the
// CI step that names them run them without it, and fail when they skip.
const standingRule = "builds aiopsctl and the test binary and runs go test inside plugin check on every plugin; make check and the CI step plugin check and T3 boot run it"

// checkReport is the report that plugin check --json prints, as this package reads it:
// a package under cmd/ imports no operations definition (ADR-061 decision 28).
type checkReport struct {
	Steps []struct {
		N             int
		Name, Outcome string
		Diagnostics   []struct {
			Text    string
			Warning bool
		}
	}
	Attestation []byte
}

// The outcomes of a step.
const (
	stepPass          = "pass"
	stepNotApplicable = "not applicable"
)

// plugins returns, by slash path from the repository root, each directory under plugins/
// and examples/ that holds a plugin.yaml: the plugins that standing rule 2 holds to plugin
// check and to a T3 boot of their row (ADR-060 decision 32).
func plugins(t *testing.T, repo string) []string {
	t.Helper()
	var out []string
	for _, tree := range []string{"plugins", "examples"} {
		found, err := filepath.Glob(filepath.Join(repo, tree, "*", "plugin.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			out = append(out, tree+"/"+filepath.Base(filepath.Dir(f)))
		}
	}
	return out
}

// builtAiopsctl builds the aiopsctl of the repository with the flags of every binary into a
// temporary directory, and returns it with a throwaway home that trusts the manifest it
// embeds: a key made in the test signs it with the code of dist sign, and trust add and
// dist use write trust.yaml, the record and current (ADR-020 decision 13; ADR-060
// decision 28).
func builtAiopsctl(t *testing.T, repo string) (bin, home string) {
	t.Helper()
	bin = filepath.Join(t.TempDir(), "aiopsctl")
	cmd := exec.Command("go", slices.Concat([]string{"build", "-trimpath"}, plugingen.BuildFlags, []string{"-o", bin, "."})...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	signed, err := os.ReadFile(filepath.Join(repo, "dist", "distribution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	home = t.TempDir()
	trusted.Write(t, home, signed)
	return bin, home
}

// Standing rule 2 (ADR-060 decisions 32 and 36; ADR-061 decision 23): the aiopsctl
// built in the repository, on a home that trusts it with a throwaway key, runs plugin
// check on every plugin under plugins/ and examples/, the five core plugins of S1
// included, and each passes every step that applies to it. An example is checked
// against the signed manifest of the test distribution, which serves the toy
// definitions; a plugin of plugins/ against the release this aiopsctl embeds. The echoer
// of examples/ is the filled-in echoer of step A2 of the exit (ADR-060 decision 30):
// steps 1 to 5 and 7 pass, step 4 reports the generated two-tenant case that did not
// run, which the attestation names (question Q7), steps 6 and 8 are not applicable, and
// the report that --json prints is kept, with the attestation it would sign.
func TestPluginCheckOnEveryPlugin(t *testing.T) {
	if testing.Short() {
		t.Skip(standingRule)
	}
	repo := abs(t, filepath.Join("..", ".."))
	bin, home := builtAiopsctl(t, repo)
	against := filepath.Join(repo, "test", "dist", "distribution.yaml")
	for _, dir := range plugins(t, repo) {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()
			args := []string{"plugin", "check", "--json"}
			if strings.HasPrefix(dir, "examples/") {
				args = append(args, "--against", against)
			}
			cmd := exec.CommandContext(t.Context(), bin, args...)
			var out, errOut bytes.Buffer
			cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = filepath.Join(repo, filepath.FromSlash(dir)), append(os.Environ(), "AIOPS_HOME="+home), &out, &errOut
			err := cmd.Run()
			var rep checkReport
			if jerr := json.Unmarshal(out.Bytes(), &rep); err != nil || jerr != nil || len(rep.Steps) != 8 {
				t.Fatalf("aiopsctl plugin check in %s: %v, %v; report %s\nstderr:\n%s", dir, err, jerr, out.Bytes(), errOut.Bytes())
			}
			outcomes := map[int]string{}
			for _, s := range rep.Steps {
				if outcomes[s.N] = s.Outcome; s.Outcome != stepPass && s.Outcome != stepNotApplicable {
					t.Errorf("%s: step %d %s: %s", dir, s.N, s.Name, s.Outcome)
				}
			}
			st, err := boot.ReadAttestation(dir, rep.Attestation)
			if err != nil {
				t.Fatalf("%s: the attestation it would sign: %v", dir, err)
			}
			if dir != "examples/echoer" {
				return
			}
			for n, want := range map[int]string{1: stepPass, 2: stepPass, 3: stepPass, 4: stepPass, 5: stepPass, 6: stepNotApplicable, 7: stepPass, 8: stepNotApplicable} {
				if outcomes[n] != want {
					t.Errorf("step A2: step %d is %s, want %s", n, outcomes[n], want)
				}
			}
			var q7 []string
			for _, d := range rep.Steps[3].Diagnostics {
				if d.Warning && strings.HasSuffix(d.Text, "skipped and did not run") {
					q7 = append(q7, strings.Fields(d.Text)[0])
				}
			}
			var pred struct{ Predicate struct{ NotRun []string } }
			if e := st.Definitions["echoers"]; e.Suite == nil || *e.Suite != 1 || !slices.Equal(e.Halves, []string{"obligations"}) ||
				json.Unmarshal(rep.Attestation, &pred) != nil || !slices.Contains(q7, "TestTwoTenants") || !slices.Equal(pred.Predicate.NotRun, q7) {
				t.Errorf("step A2: echoers %+v, the cases of Q7 that step 4 reports %v, the attestation's notRun %v", st.Definitions["echoers"], q7, pred.Predicate.NotRun)
			}
			kept := filepath.Join(t.TempDir(), "check.json")
			var again checkReport
			if err := os.WriteFile(kept, out.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			if b, err := os.ReadFile(kept); err != nil || json.Unmarshal(b, &again) != nil || !bytes.Equal(again.Attestation, rep.Attestation) {
				t.Errorf("step A2: the kept --json report does not read back: %v", err)
			}
		})
	}
}

// The T3 boot of each plugin's row (ADR-060 decisions 32 and 36; 07-c4-plugin-sdk.md
// T3): the test binary, which links every plugin of plugins/ and examples/, boots on a
// home that trusts its manifest with a composition that holds a row of each plugin that
// a server runs, and every row is ACTIVE in the state that test-state writes; the built
// aiopsctl boots the cli profile, and aiopsctl help exits 0 and lists every command of
// each command row, which needs every row of aiops-cli ACTIVE, the cli row and the
// operations row among them (ADR-061 decision 31). Every plugin under plugins/ and
// examples/ is a row of one of the two.
func TestBootOfEachPluginsRow(t *testing.T) {
	if testing.Short() {
		t.Skip(standingRule)
	}
	repo := abs(t, filepath.Join("..", ".."))
	server := map[string]string{"plugins/clock": "clock", "plugins/config-refs": "refs", "plugins/readiness": "readiness",
		"plugins/reload": "reload", "plugins/reload-socket": "reload-socket", "plugins/restart-policy": "restart-policy",
		"examples/dispatch": "dispatch", "examples/echoer": "echoer", "examples/stamper": "stamper", "examples/caller": "caller"}
	cli := []string{"plugins/cli", "plugins/operations", "plugins/command-dist", "plugins/command-key", "plugins/command-plugin",
		"plugins/command-profile", "plugins/command-trust"}
	for _, dir := range plugins(t, repo) {
		if _, ok := server[dir]; !ok && !slices.Contains(cli, dir) {
			t.Errorf("%s has no row in either T3 boot of this case", dir)
		}
	}
	p, err := harness.Start(t, harness.Binary(t), harness.Options{Profile: "t3", Context: t.Context(), Install: func(home string) error {
		return writeFiles(home, map[string]string{
			"profiles/t3/profile.yaml": "bundles: [smoke, t3]\n",
			"bundles/t3/bundle.yaml": "- insert:\n    - {id: reload-socket, plugin: aiops/reload-socket, roles: [api]}\n" +
				"    - {id: dispatch, plugin: aiops/example-dispatch, roles: [api]}\n    - {id: echoer, plugin: aiops/example-echoer, roles: [api]}\n" +
				"    - {id: stamper, plugin: aiops/example-stamper, roles: [api]}\n    - {id: caller, plugin: aiops/example-caller, roles: [api]}\n",
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(10 * time.Second); _ = os.RemoveAll(p.Home) })
	s, ok := p.Until(time.Minute, func(s harness.Snapshot) bool {
		for _, row := range server {
			if s.RowState(row) != "ACTIVE" {
				return false
			}
		}
		return s.Running()
	})
	if !ok {
		t.Errorf("aiops-test did not run t3 with a row of each plugin ACTIVE: %+v\n%s", s.State.Rows, p.Output())
	}

	bin, home := builtAiopsctl(t, repo)
	cmd := exec.CommandContext(t.Context(), bin, "help")
	cmd.Env = append(os.Environ(), "AIOPS_HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("aiopsctl help: %v\n%s", err, out)
	}
	for _, dir := range cli {
		var paths []struct{ Path string }
		if b, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(dir), "fixtures", "commands", "paths.json")); err == nil {
			if err := json.Unmarshal(b, &paths); err != nil {
				t.Fatal(err)
			}
		}
		for _, c := range paths {
			if !strings.Contains(string(out), c.Path) {
				t.Errorf("aiopsctl help does not list %s of %s:\n%s", c.Path, dir, out)
			}
		}
	}
}
