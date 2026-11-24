// SPDX-License-Identifier: Apache-2.0

// Package authoring is the part of the launcher's toolbox that only aiopsctl links and
// hands to boot (ADR-061 decision 21): plugingen, the import analyzer and plugin vet of
// internal/vet, the suites, the templates of plugin new and the scenarios, and the data
// of the lints of step 1 of plugin check. aiops links none of it.
//
// The suites of a Go plugin run in the author's module through the Go toolchain, so the
// attestation names the suites that plugin check ran and not those an author's test
// happens to call (07-c4-plugin-sdk.md:1512). The suites and the scenario of a pack run
// from the test binaries that the release build compiles with the first-party plugins
// they run on and embeds under suites/, each a child process with the pack as input
// (ADR-060 decision 46); a binary built without them answers launch.ErrNotCarried.
package authoring

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	"github.com/mutex-solutions/aiops-platform/internal/vet"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/templates"
)

const modPath = "github.com/mutex-solutions/aiops-platform"

// SPDXVersion is the version of the SPDX License List of spdx.txt.
const SPDXVersion = "3.29.0"

//go:embed spdx.txt
var spdxList string

// suites holds the suite and scenario binaries that the release build embeds:
// suites/<definition>.test and suites/scenario-<name>.test.
//
//go:embed suites
var suites embed.FS

// Tools are the parts of the toolbox that aiopsctl links. Build stays launch.NoTools's
// until the builder of ADR-044 lands.
type Tools struct {
	launch.NoTools
	SDK    string // the version of the SDK modules that a template requires
	Suites fs.FS  // the embedded binaries, under suites/; nil for none
}

// New returns the Tools of this binary: the SDK version it was built with and the
// binaries it embeds.
func New() Tools {
	t := Tools{SDK: "v0.0.0", Suites: suites}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == modPath+"/sdk" {
				t.SDK = d.Version
			}
		}
	}
	return t
}

// marks are the marks of 00-decisions.md §8.1, which no id, title, content path or
// command path holds; tiers are the words that the name and title of a third-party
// plugin never hold (08-profiles-roadmap-adrs.md:1368, T7).
var (
	marks = []string{"grafana", "loki", "tempo", "mimir", "pyroscope", "logql", "traceql", "alloy", "beyla"}
	tiers = []string{"official", "certified", "enterprise", "premium", "pro"}
)

// Lint returns the data of the lints of step 1 that boot's ReadManifest runs: the ids of
// the SPDX License List of spdx.txt, licenses and exceptions, the marks and the tiers.
func Lint() boot.ManifestLint {
	l := boot.ManifestLint{Licenses: map[string]bool{}, Marks: marks, Tiers: tiers}
	for _, line := range strings.Split(spdxList, "\n") {
		if kind, id, ok := strings.Cut(line, " "); ok && (kind == "license" || kind == "exception") {
			l.Licenses[id] = true
		}
	}
	return l
}

// Template returns the files of the template of a registry, or of the connector pack,
// for the plugin q.ID (ADR-060 decision 24).
func (t Tools) Template(ctx context.Context, q launch.TemplateRequest) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.Registry != templates.Connector && q.Lang != "go" {
		return nil, fmt.Errorf("no template in %q: the Python template arrives with the Python SDK at S2b", q.Lang)
	}
	d, err := templates.NewData(q.ID, t.SDK, q.Contrib)
	if err != nil {
		return nil, err
	}
	return templates.Render(q.Registry, d)
}

// lines returns each line of err as a diagnostic of check, none for nil.
func lines(check string, err error) []launch.Diagnostic {
	var out []launch.Diagnostic
	if err != nil {
		for _, l := range strings.Split(err.Error(), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, launch.Diagnostic{Check: check, Text: l})
			}
		}
	}
	return out
}

// Generate is plugingen on the plugin in q.Dir: with q.Write it writes the generated
// half of plugin.yaml and the generated tests; otherwise it is step 2, the parity check.
func (Tools) Generate(ctx context.Context, q launch.GenerateRequest) ([]launch.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.Write {
		return lines("2", plugingen.Write(q.Dir)), nil
	}
	return lines("2", plugingen.Check(q.Dir)), nil
}

// hasGo reports whether dir holds a Go file outside testdata.
func hasGo(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		switch {
		case err != nil || found:
			return filepath.SkipAll
		case e.IsDir() && p != dir && (e.Name() == "testdata" || strings.HasPrefix(e.Name(), ".")):
			return filepath.SkipDir
		}
		found = strings.HasSuffix(p, ".go")
		return nil
	})
	return found
}

// Vet is step 3, T0 static: the import analyzer and plugin vet of internal/vet on Go
// code, and for every artifact the refusal of a plugin.json of the dashboard server,
// which no artifact carries (08-profiles-roadmap-adrs.md:391; licence record §7.6).
func (Tools) Vet(ctx context.Context, dir string) ([]launch.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []launch.Diagnostic
	if hasGo(dir) {
		fs, err := vet.Module(dir)
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			out = append(out, launch.Diagnostic{Check: "3", File: f.File, Line: f.Line, Text: f.Text})
		}
	}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && e.Name() == "plugin.json" {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, launch.Diagnostic{Check: "3", File: filepath.ToSlash(rel),
				Text: "a plugin.json of the dashboard server, which no artifact carries (08-profiles-roadmap-adrs.md:391)"})
		}
		return err
	})
	return out, err
}

// event is one line of go test -json.
type event struct {
	Action, Test, Output string
}

// goTest runs go test with args in dir and returns its events.
func goTest(ctx context.Context, dir string, args ...string) ([]event, error) {
	cmd := exec.CommandContext(ctx, "go", append([]string{"test", "-count=1", "-json"}, args...)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var evs []event
	s := bufio.NewScanner(bytes.NewReader(out))
	s.Buffer(nil, 1<<24)
	for s.Scan() {
		var e event
		if json.Unmarshal(s.Bytes(), &e) == nil {
			evs = append(evs, e)
		}
	}
	if len(evs) == 0 && err != nil {
		return nil, fmt.Errorf("go test in %s: %v: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return evs, nil
}

// own is step 4, T1 alone: the plugin's own tests under -race, the generated ones among
// them, but the suite that step 5 runs itself (07-c4-plugin-sdk.md:1570).
func own(ctx context.Context, dir string) (launch.SuiteReport, error) {
	evs, err := goTest(ctx, dir, "-race", "-skip", "^TestConformance$", "./...")
	if err != nil {
		return launch.SuiteReport{}, err
	}
	var r launch.SuiteResult
	var build []string
	for _, e := range evs {
		top := e.Test != "" && !strings.Contains(e.Test, "/")
		switch {
		case top && e.Action == "fail":
			r.Failed = append(r.Failed, e.Test)
		case top && e.Action == "skip":
			r.Skipped = append(r.Skipped, e.Test)
		case e.Test == "" && e.Action == "output":
			build = append(build, strings.TrimSpace(e.Output))
		case e.Test == "" && e.Action == "fail" && len(r.Failed) == 0:
			r.Failed = append(r.Failed, "the package: "+strings.Join(build, "; "))
		}
	}
	return launch.SuiteReport{Definitions: []launch.SuiteResult{r}}, nil
}

// contributionArgs are the arguments that the Contribution of a suite takes after the
// fiber: the fixtures directory of commands (ADR-061 decision 5).
// ponytail: one entry per suite whose Contribution takes more than the fiber.
var contributionArgs = map[string]string{"commands": `, "fixtures"`}

// conformance runs the obligations of the suite of the definition name at version on
// the plugin in dir, through the toolchain in the author's module: a test written into
// a go build overlay, never onto disk, mounts the plugin's value as a contribution.
// Each failed obligation is its id.
func conformance(ctx context.Context, dir, name, version string, halves []string) (launch.SuiteResult, error) {
	major, _, _ := strings.Cut(version, ".")
	def := modPath + "/sdk/def/" + name + "/v" + major
	var pkg struct{ ImportPath, Name string }
	var defDir struct{ Dir string }
	for _, l := range []struct {
		path string
		into any
	}{{".", &pkg}, {def, &defDir}} {
		cmd := exec.CommandContext(ctx, "go", "list", "-json", l.path)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err == nil {
			err = json.Unmarshal(out, l.into)
		}
		if err != nil {
			return launch.SuiteResult{}, fmt.Errorf("go list %s in %s: %w", l.path, dir, err)
		}
	}
	src, err := os.ReadFile(filepath.Join(defDir.Dir, "definition.yaml"))
	if err != nil {
		return launch.SuiteResult{}, err
	}
	d, err := plugingen.ParseDefinition(def, src)
	if err != nil {
		return launch.SuiteResult{}, err
	}
	tmp, err := os.MkdirTemp("", "aiopsctl-suite-")
	if err != nil {
		return launch.SuiteResult{}, err
	}
	defer os.RemoveAll(tmp)
	test := fmt.Sprintf(`package %s_test

import (
	"testing"

	zzplugin %q
	"%s/conformance"
	"%s/sdk/kernel"
	"%s/sdk/testkit/kerneltest"
)

func TestAiopsctl(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		return conformance.Contribution(root.Plugin(zzplugin.Plugin, nil)%s), root
	})
}
`, pkg.Name, pkg.ImportPath, def, modPath, modPath, contributionArgs[name])
	file, overlay := filepath.Join(tmp, "suite_test.go"), filepath.Join(tmp, "overlay.json")
	ovl, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(dir, "zz_aiopsctl_"+name+"_test.go"): file}})
	if err := errors.Join(os.WriteFile(file, []byte(test), 0o600), os.WriteFile(overlay, ovl, 0o600)); err != nil {
		return launch.SuiteResult{}, err
	}
	evs, err := goTest(ctx, dir, "-overlay", overlay, "-run", "^TestAiopsctl$", ".")
	if err != nil {
		return launch.SuiteResult{}, err
	}
	r := launch.SuiteResult{Name: name, Revision: d.Suite, Halves: halves}
	ran := false
	var out []string
	for _, e := range evs {
		sub, isSub := strings.CutPrefix(e.Test, "TestAiopsctl/")
		switch {
		case isSub && e.Action == "fail":
			r.Failed = append(r.Failed, name+"/"+sub)
		case e.Test == "TestAiopsctl" && e.Action == "run":
			ran = true
		case e.Action == "output":
			out = append(out, strings.TrimSpace(e.Output))
		}
	}
	if !ran {
		return launch.SuiteResult{}, fmt.Errorf("the suite of %s did not run on %s: %s", name, pkg.ImportPath, strings.Join(out, "; "))
	}
	return r, nil
}

// Suite runs step 4, with no definition, or step 5 on the definitions of q: in the
// author's module for Go code, and from the embedded binaries for a pack.
func (t Tools) Suite(ctx context.Context, q launch.SuiteRequest) (launch.SuiteReport, error) {
	if !hasGo(q.Dir) {
		var rep launch.SuiteReport
		for _, name := range slices.Sorted(maps.Keys(q.Definitions)) {
			var r launch.SuiteResult
			if err := t.embedded(ctx, name+".test", q.Dir, q.Fixtures, &r); err != nil {
				return launch.SuiteReport{}, err
			}
			rep.Definitions = append(rep.Definitions, r)
		}
		return rep, nil
	}
	if len(q.Definitions) == 0 {
		return own(ctx, q.Dir)
	}
	var rep launch.SuiteReport
	for _, name := range slices.Sorted(maps.Keys(q.Definitions)) {
		r, err := conformance(ctx, q.Dir, name, q.Definitions[name], q.Halves)
		if err != nil {
			return launch.SuiteReport{}, err
		}
		rep.Definitions = append(rep.Definitions, r)
	}
	return rep, nil
}

// Scenario runs the scenario q.Name on the pack in q.Dir, replayed from its fixtures,
// from the binary that the release build embeds (ADR-060 decision 49).
func (t Tools) Scenario(ctx context.Context, q launch.ScenarioRequest) (launch.ScenarioReport, error) {
	var r launch.ScenarioReport
	err := t.embedded(ctx, "scenario-"+q.Name+".test", q.Dir, filepath.Join(q.Dir, "fixtures"), &r)
	return r, err
}

// embedded runs the binary suites/name with the pack dir and its fixtures as input, as a
// child process, and decodes the JSON report it writes into out (ADR-060 decision 46).
func (t Tools) embedded(ctx context.Context, name, dir, fixtures string, out any) error {
	var bin []byte
	err := fs.ErrNotExist
	if t.Suites != nil {
		bin, err = fs.ReadFile(t.Suites, "suites/"+name)
	}
	if err != nil {
		return fmt.Errorf("%w: %s: this aiopsctl embeds no binary %s; the release build compiles each suite and scenario with the first-party plugins it runs on into aiopsctl (ADR-060 decision 46)",
			launch.ErrNotCarried, strings.TrimSuffix(name, ".test"), name)
	}
	tmp, err := os.MkdirTemp("", "aiopsctl-suite-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	exe, report := filepath.Join(tmp, name), filepath.Join(tmp, "report.json")
	if err := os.WriteFile(exe, bin, 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, "-test.count=1")
	cmd.Env = append(os.Environ(), "AIOPS_PACK="+dir, "AIOPS_FIXTURES="+fixtures, "AIOPS_REPORT="+report)
	if o, err := cmd.CombinedOutput(); err != nil && len(o) > 0 && !fileExists(report) {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(o)))
	}
	b, err := os.ReadFile(report)
	if err != nil {
		return fmt.Errorf("%s wrote no report: %w", name, err)
	}
	return json.Unmarshal(b, out)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
