// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/templates"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// result is how a command ended: its exit code, stdout and stderr.
type result struct {
	code        int
	out, errOut string
}

// checkReport is the report that plugin check --json prints, as the job reads it.
type checkReport struct {
	Steps []struct {
		N           int
		Outcome     string
		Diagnostics []struct{ Text string }
	}
}

// templateJob runs the aiopsctl built in the repository on a home that trusts it with a
// throwaway key, from the module cache alone.
type templateJob struct {
	t                  *testing.T
	bin, home, against string
}

func (j templateJob) run(dir, name string, args ...string) result {
	j.t.Helper()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(j.t.Context(), name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &out, &errOut
	cmd.Env = append(os.Environ(), "AIOPS_HOME="+j.home, "GOPROXY=off", "GOSUMDB=off")
	if err := cmd.Run(); err != nil && cmd.ProcessState == nil {
		j.t.Fatal(err)
	}
	return result{cmd.ProcessState.ExitCode(), out.String(), errOut.String()}
}

// check runs plugin check --json in dir against the signed manifest of the test
// distribution, which serves the toy registries, and returns its report, and per step
// the texts of its diagnostics.
func (j templateJob) check(dir string) (result, checkReport, map[int][]string) {
	j.t.Helper()
	r := j.run(dir, j.bin, "plugin", "check", "--json", "--against", j.against)
	var rep checkReport
	if err := json.Unmarshal([]byte(r.out), &rep); err != nil || len(rep.Steps) != 8 {
		j.t.Fatalf("plugin check --json in %s: %v; exit %d\n%s\n%s", dir, err, r.code, r.out, r.errOut)
	}
	texts := map[int][]string{}
	for _, s := range rep.Steps {
		for _, d := range s.Diagnostics {
			texts[s.N] = append(texts[s.N], d.Text)
		}
	}
	return r, rep, texts
}

// ids returns the obligation id of each text, or line, that ends with suffix.
func ids(texts []string, suffix string) []string {
	var out []string
	for _, s := range texts {
		if id, ok := strings.CutSuffix(strings.TrimSpace(s), ": "+suffix); ok {
			out = append(out, id)
		}
	}
	return out
}

// The template job (ADR-060 decisions 24 and 36; 08-profiles-roadmap-adrs.md:1263;
// 07-c4-plugin-sdk.md:1563), through the aiopsctl built in the repository on a home that
// trusts the manifest it embeds with a throwaway key (ADR-020 decision 13), as an
// author runs it: plugin new writes a plugin from the template of each registry, which
// compiles, passes T0 and the generated tests, and fails conformance with exactly one
// line per obligation of the registry's suite, each checked against the test
// distribution, whose serves hold the toy registries. The module of each plugin reaches the SDK
// modules of this repository through replace lines that the job adds, as the exit
// reaches them through its module proxy; the template itself holds none. The connector
// template writes a pack of the rows of the instantiable echoer of the test
// distribution, which plugin check holds to the suite binaries that this aiopsctl does
// not carry; the job that builds the test release, builder, runs its case against the
// aiopsctl of that release (TestTestRelease). It builds and runs Go in modules of their
// own, so -short skips it: make templates and the CI job templates run it.
func TestTemplateJob(t *testing.T) {
	if testing.Short() {
		t.Skip("the template job builds aiopsctl and three modules; make templates runs it")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
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
	// The aiopsctl of the repository, with the flags of every binary, and a throwaway home
	// that trusts the manifest it embeds: a key made in the test signs it with the code of
	// dist sign, and trust add and dist use write the home (ADR-060 decision 28).
	bin, home := filepath.Join(t.TempDir(), "aiopsctl"), t.TempDir()
	build := exec.Command("go", slices.Concat([]string{"build", "-trimpath"}, plugingen.BuildFlags, []string{"-o", bin, "./cmd/aiopsctl"})...)
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	signed, err := os.ReadFile(filepath.Join(repo, "dist", "distribution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	trusted.Write(t, home, signed)
	against := filepath.Join(repo, "test", "dist", "distribution.yaml")
	j := templateJob{t: t, bin: bin, home: home, against: against}
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
	t.Run(templates.Connector, func(t *testing.T) {
		ran[templates.Connector], j.t = true, t
		work := t.TempDir()
		if r := j.run(work, bin, "plugin", "new", templates.Connector, "acme/job-connector", "--against", against); r.code != 0 {
			t.Fatalf("plugin new connector: exit %d\n%s", r.code, r.errOut)
		}
		dir := filepath.Join(work, "job-connector")
		if b, err := os.ReadFile(filepath.Join(dir, "bundle.yaml")); err != nil || !strings.Contains(string(b), "plugin: aiops/example-echoer") {
			t.Errorf("the pack's bundle.yaml holds no row of the instantiable echoer: %s, %v", b, err)
		}
		r := j.run(dir, bin, "plugin", "check", "--against", against)
		if r.code != 1 || !strings.Contains(r.errOut, "this aiopsctl embeds no binary echoers.test") {
			t.Errorf("plugin check of the pack: exit %d; want the suite binary that this aiopsctl does not carry named\n%s", r.code, r.errOut)
		}
	})
	for _, reg := range templates.Registries {
		t.Run(reg, func(t *testing.T) {
			ran[reg], j.t = true, t
			work := t.TempDir()
			if r := j.run(work, bin, "plugin", "new", reg, "acme/job-"+reg, "--lang", "go"); r.code != 0 {
				t.Fatalf("plugin new %s: exit %d\n%s", reg, r.code, r.errOut)
			}
			dir := filepath.Join(work, "job-"+reg)
			mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			for _, m := range []string{"sdk", "sdk/kernel", "sdk/testkit"} {
				mod = append(mod, []byte("\nreplace github.com/mutex-solutions/aiops-platform/"+m+" => "+filepath.Join(repo, m)+"\n")...)
			}
			if err != nil || os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644) != nil || os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644) != nil {
				t.Fatalf("the go.mod and go.sum of the plugin: %v", err)
			}
			// The steps of the template's README: plugin check --write, then go mod tidy,
			// from the module cache alone (-e: the tests of goleak import a module the cache
			// need not hold).
			j.run(dir, bin, "plugin", "check", "--write", "--against", against)
			for _, f := range []string{"zz_cycle_test.go", "zz_tenants_test.go"} {
				if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
					t.Errorf("plugin check --write wrote no %s: %v", f, err)
				}
			}
			for _, args := range [][]string{{"mod", "tidy", "-e"}, {"build", "./..."}, {"vet", "./..."}} {
				if r := j.run(dir, "go", args...); r.code != 0 {
					t.Fatalf("go %s: exit %d\n%s", strings.Join(args, " "), r.code, r.errOut)
				}
			}
			// The parity of step 2, T0, and the generated tests and its own under -race,
			// where the two-tenant test skips until S4a and is reported as not run; then
			// conformance fails with exactly one line per obligation.
			r, rep, texts := j.check(dir)
			outcomes := []string{}
			for _, s := range rep.Steps {
				outcomes = append(outcomes, s.Outcome)
			}
			if want := []string{"pass", "pass", "pass", "pass", "fail", "skipped", "skipped", "skipped"}; r.code != 1 || !slices.Equal(outcomes, want) ||
				!slices.Contains(texts[4], "TestTwoTenants skipped and did not run") {
				t.Errorf("plugin check: exit %d, steps %v, step 4 %q; want %v and the two-tenant test reported\n%s", r.code, outcomes, texts[4], want, r.errOut)
			}
			if got := ids(texts[5], "the obligation failed"); !slices.Equal(got, obligations[reg]) || len(texts[5]) != len(got) {
				t.Errorf("step 5: %q; want one line per obligation of %v", texts[5], obligations[reg])
			}
			// A mount of the author's that skips each case after the first: step 5 names
			// each case that skipped, and no half is recorded, so nothing attests a half
			// that mostly did not run (ADR-060 decision 50).
			if reg == "echoers" {
				mount := "package jobechoers_test\n\nimport (\n\t\"strings\"\n\t\"testing\"\n\n\tplug \"example.com/acme/job-echoers\"\n" +
					"\t\"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance\"\n\t\"github.com/mutex-solutions/aiops-platform/sdk/kernel\"\n" +
					"\t\"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest\"\n)\n\n" +
					"func MountEchoers(t *testing.T) (conformance.Subject, *kernel.Root) {\n\tif strings.Contains(t.Name(), \"/\") {\n\t\tt.Skip(\"a case that this mount cannot set up\")\n\t}\n" +
					"\troot := kerneltest.New(t)\n\treturn conformance.Contribution(root.Plugin(plug.Plugin, nil)), root\n}\n"
				if err := os.WriteFile(filepath.Join(dir, "zz_mount_test.go"), []byte(mount), 0o644); err != nil {
					t.Fatal(err)
				}
				if _, _, texts := j.check(dir); !slices.Equal(ids(texts[5], "the case skipped and did not run"), obligations[reg][1:]) {
					t.Errorf("step 5 on a mount that skips each case after the first: %q; want %v skipped", texts[5], obligations[reg][1:])
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
				if _, _, texts := j.check(dir); !slices.Equal(ids(texts[5], "the obligation failed"), []string{"commands/paths"}) {
					t.Errorf("step 5 on a command that works: %q; want commands/paths failing alone", texts[5])
				}
			}
		})
	}
}
