// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/templates"
)

// The template job (ADR-060 decisions 24 and 36; 08-profiles-roadmap-adrs.md:1263;
// 07-c4-plugin-sdk.md:1563): a plugin generated from the template of each registry
// compiles, passes T0 and the generated tests, and fails conformance with exactly one
// line per obligation of the registry's suite. The module of each plugin reaches the
// SDK modules of this repository through replace lines that the job adds, as the exit
// reaches them through its module proxy; the template itself holds none. It builds and
// runs Go in a module of its own, so -short skips it: make templates and the CI job
// templates run it.
func TestTemplateJob(t *testing.T) {
	if testing.Short() {
		t.Skip("the template job builds three modules; make templates runs it")
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	obligations := map[string][]string{"commands": commands.Obligations, "echoers": echoers.Obligations, "stampers": stampers.Obligations}
	if got := slices.Sorted(maps.Keys(obligations)); !slices.Equal(got, templates.Registries) {
		t.Fatalf("the job covers %v, and the templates are %v", got, templates.Registries)
	}
	tl := Tools{SDK: "v0.0.0"}
	for _, reg := range templates.Registries {
		t.Run(reg, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), reg)
			files, err := tl.Template(t.Context(), launch.TemplateRequest{Registry: reg, ID: "acme/job-" + reg, Lang: "go"})
			if err != nil {
				t.Fatal(err)
			}
			for name, b := range files {
				p := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if name == "go.mod" {
					for _, m := range []string{"sdk", "sdk/kernel", "sdk/testkit"} {
						b = append(b, []byte("\nreplace github.com/mutex-solutions/aiops-platform/"+m+" => "+filepath.Join(repo, m)+"\n")...)
					}
				}
				if err := os.WriteFile(p, b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
				t.Fatal(err)
			}
			// plugin check --write: the generated half and the generated tests.
			if ds, err := tl.Generate(t.Context(), launch.GenerateRequest{Dir: dir, Write: true}); err != nil || len(ds) != 0 {
				t.Fatalf("Generate: %+v, %v", ds, err)
			}
			for _, f := range []string{"zz_cycle_test.go", "zz_tenants_test.go"} {
				if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
					t.Errorf("plugin check --write wrote no %s: %v", f, err)
				}
			}
			if ds, err := tl.Generate(t.Context(), launch.GenerateRequest{Dir: dir}); err != nil || len(ds) != 0 {
				t.Errorf("step 2 after --write: %+v, %v", ds, err)
			}
			// It compiles, and passes T0.
			for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
				cmd := exec.Command("go", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
				}
			}
			if ds, err := tl.Vet(t.Context(), dir); err != nil || len(ds) != 0 {
				t.Errorf("T0: %+v, %v", ds, err)
			}
			// It passes the generated tests and its own, under -race; the two-tenant test
			// skips until S4a and is reported as not run.
			rep, err := tl.Suite(t.Context(), launch.SuiteRequest{Dir: dir})
			if err != nil || len(rep.Definitions) != 1 || len(rep.Definitions[0].Failed) != 0 || !slices.Equal(rep.Definitions[0].Skipped, []string{"TestTwoTenants"}) {
				t.Errorf("step 4: %+v, %v", rep, err)
			}
			// It fails conformance with exactly one line per obligation.
			rep, err = tl.Suite(t.Context(), launch.SuiteRequest{Dir: dir, Definitions: map[string]string{reg: "0.1"}, Halves: []string{"obligations"}, Fixtures: filepath.Join(dir, "fixtures")})
			if err != nil || len(rep.Definitions) != 1 {
				t.Fatalf("step 5: %+v, %v", rep, err)
			}
			if r := rep.Definitions[0]; r.Name != reg || r.Revision < 1 || !slices.Equal(r.Halves, []string{"obligations"}) || !slices.Equal(r.Failed, obligations[reg]) {
				t.Errorf("step 5: %+v; want suite %s failing each of %v once", r, reg, obligations[reg])
			}
		})
	}
}
