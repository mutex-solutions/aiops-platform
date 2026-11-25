// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"errors"
	"io/fs"
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
// templates run it. The case of the connector template is deferred, as the note of
// 2026-11-24 to ADR-060 decision 24 records.
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
	ran := map[string]bool{} // the templates the job ran a case of
	defer func() {
		// Every template is a case of the job: one left out is never generated.
		es, err := fs.ReadDir(templates.FS, ".")
		var all []string
		for _, e := range es {
			if e.IsDir() && e.Name() != "shared" && e.Name() != "go" {
				all = append(all, e.Name())
			}
		}
		if got := slices.Sorted(maps.Keys(ran)); err != nil || !slices.Equal(got, all) {
			t.Errorf("the job runs a case of the templates %v, and the templates are %v", got, all)
		}
	}()
	// The pack of the connector template passes T0, and step 5 and the echo scenario
	// answer that this aiopsctl carries no suite binary: until the release build embeds
	// them (ADR-060 decision 46) and the exit's test release serves an instantiable
	// plugin for a row to name, the case asserts that. When both land, it generates the
	// pack with plugin new connector against that release and asserts that step 5 fails
	// each obligation of the registries its rows feed once.
	t.Run(templates.Connector, func(t *testing.T) {
		ran[templates.Connector] = true
		dir := filepath.Join(t.TempDir(), templates.Connector)
		files, err := tl.Template(t.Context(), launch.TemplateRequest{Registry: templates.Connector, ID: "acme/job-connector"})
		if err != nil {
			t.Fatal(err)
		}
		files["bundle.yaml"] = []byte("[]\n")
		for name, b := range files {
			p := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if ds, err := tl.Vet(t.Context(), dir); err != nil || len(ds) != 0 {
			t.Errorf("T0: %+v, %v", ds, err)
		}
		carried := New()
		if _, err := carried.Suite(t.Context(), launch.SuiteRequest{Dir: dir, Definitions: map[string]string{"echoers": "0.1"}, Halves: []string{"obligations"},
			Fixtures: filepath.Join(dir, "fixtures")}); !errors.Is(err, launch.ErrNotCarried) {
			t.Errorf("step 5 of the pack: %v; want ErrNotCarried until the suite binaries are embedded, and then the case of ADR-060 decision 24", err)
		}
		if _, err := carried.Scenario(t.Context(), launch.ScenarioRequest{Dir: dir, Name: "echo"}); !errors.Is(err, launch.ErrNotCarried) {
			t.Errorf("the echo scenario: %v; want ErrNotCarried until the scenario binary is embedded, and then the case of ADR-060 decision 24", err)
		}
	})
	for _, reg := range templates.Registries {
		t.Run(reg, func(t *testing.T) {
			ran[reg] = true
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
			// The steps of the template's README: plugin check --write, then go mod tidy, from
			// the module cache alone.
			tidy := func() {
				cmd := exec.Command("go", "mod", "tidy", "-e") // -e: the tests of goleak import a module the cache need not hold
				cmd.Dir, cmd.Env = dir, append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOSUMDB=off")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("go mod tidy: %v\n%s", err, out)
				}
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
			tidy()
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
			// A mount of the author's that skips each case after the first: step 5 names each
			// case that skipped, and no half is recorded, so nothing attests a half that
			// mostly did not run (ADR-060 decision 50).
			if reg == "echoers" {
				mount := "package jobechoers_test\n\nimport (\n\t\"strings\"\n\t\"testing\"\n\n\tplug \"example.com/acme/job-echoers\"\n" +
					"\t\"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance\"\n\t\"github.com/mutex-solutions/aiops-platform/sdk/kernel\"\n" +
					"\t\"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest\"\n)\n\n" +
					"func MountEchoers(t *testing.T) (conformance.Subject, *kernel.Root) {\n\tif strings.Contains(t.Name(), \"/\") {\n\t\tt.Skip(\"a case that this mount cannot set up\")\n\t}\n" +
					"\troot := kerneltest.New(t)\n\treturn conformance.Contribution(root.Plugin(plug.Plugin, nil)), root\n}\n"
				if err := os.WriteFile(filepath.Join(dir, "zz_mount_test.go"), []byte(mount), 0o644); err != nil {
					t.Fatal(err)
				}
				rep, err = tl.Suite(t.Context(), launch.SuiteRequest{Dir: dir, Definitions: map[string]string{reg: "0.1"}, Halves: []string{"obligations"}, Fixtures: filepath.Join(dir, "fixtures")})
				if err != nil || len(rep.Definitions) != 1 || len(rep.Definitions[0].Halves) != 0 || !slices.Equal(rep.Definitions[0].Skipped, obligations[reg][1:]) {
					t.Errorf("step 5 on a mount that skips each case after the first: %+v, %v; want no half and %v skipped", rep, err, obligations[reg][1:])
				}
				if err := os.Remove(filepath.Join(dir, "zz_mount_test.go")); err != nil {
					t.Fatal(err)
				}
			}
			// The failures of commands come from its stub: a command that works passes the
			// cases of run.json and cancel.json that the template ships, and paths.json,
			// which the author writes, fails on its missing file (07-c4-plugin-sdk.md:1561).
			if reg == "commands" {
				file := filepath.Join(dir, "internal", "command", "command.go")
				src, err := os.ReadFile(file)
				if err == nil {
					err = os.WriteFile(file, []byte(strings.Replace(string(src), "return ErrNotImplemented", "return ctx.Err()", 1)), 0o644)
				}
				if err != nil {
					t.Fatal(err)
				}
				rep, err = tl.Suite(t.Context(), launch.SuiteRequest{Dir: dir, Definitions: map[string]string{reg: "0.1"}, Halves: []string{"obligations"}, Fixtures: filepath.Join(dir, "fixtures")})
				if err != nil || len(rep.Definitions) != 1 || !slices.Equal(rep.Definitions[0].Failed, []string{"commands/paths"}) {
					t.Errorf("step 5 on a command that works: %+v, %v; want commands/paths failing alone", rep, err)
				}
			}
		})
	}
}
