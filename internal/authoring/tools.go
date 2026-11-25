// SPDX-License-Identifier: Apache-2.0

// Package authoring is the part of the launcher's toolbox that only aiopsctl links and
// hands to boot (ADR-061 decision 21): plugingen, the import analyzer and plugin vet of
// internal/vet, the suites, the templates of plugin new and the scenarios, the module zip
// of plugin pack, and the data of the lints of step 1 of plugin check. aiops links none
// of it.
//
// The suites of a Go plugin run in the author's module through the Go toolchain, so the
// attestation names the suites that plugin check ran and not those an author's test
// happens to call (07-c4-plugin-sdk.md:1512). The suites and the scenario of a pack run
// from the test binaries that the release build compiles with the first-party plugins
// they run on, which the release aiopsctl embeds under suites/ and hands in as
// Tools.Suites, each a child process with the pack as input (ADR-060 decision 46;
// internal/packsuite); a binary built without them, as every aiopsctl built in the
// repository, answers launch.ErrNotCarried.
package authoring

import (
	"bufio"
	"bytes"
	"context"
	_ "embed" // spdx.txt
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/builder"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	"github.com/mutex-solutions/aiops-platform/internal/vet"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
	"github.com/mutex-solutions/aiops-platform/templates"
)

const modPath = "github.com/mutex-solutions/aiops-platform"

// SPDXVersion is the version of the SPDX License List of spdx.txt.
const SPDXVersion = "3.29.0"

//go:embed spdx.txt
var spdxList string

// Tools are the parts of the toolbox that aiopsctl links.
type Tools struct {
	launch.NoTools
	SDK      string    // the version of the SDK modules that a template requires
	Suites   fs.FS     // the embedded binaries, suites/<definition>.test and suites/scenario-<name>.test; nil for none
	Signed   []byte    // the signed manifest this binary embeds, the release dist build builds on
	Progress io.Writer // where dist build prints, stderr in aiopsctl; nil drops it
}

// Build is dist build, the builder of internal/builder (ADR-044; ADR-060 decision 22),
// which writes into build/ beside dist.yaml. On the local path it builds on the
// release's signed manifest this binary embeds (ADR-044 decision 1), extracts the
// pinned toolchain under the user's cache directory, and refuses a proxy URL in GOPROXY
// until L14 clears. On the image path it runs the builder image that the release
// artifact names, whose aiopsctl takes the local path. With q.Plugin it is the build of
// plugin smoke, and with q.Module that of plugin verify of in-process code.
func (t Tools) Build(ctx context.Context, q launch.BuildRequest) (launch.Built, error) {
	switch {
	case q.Module != "":
		return t.verify(ctx, q)
	case q.Plugin != "":
		return t.smoke(ctx, q)
	}
	var r builder.Result
	var err error
	switch q.Builder {
	case "image":
		r, err = builder.RunImage(ctx, builder.ImageRequest{DistYAML: q.DistYAML, ModCache: q.ModCache, Mirrors: q.Mirrors, AllowBump: q.AllowBump, Progress: t.Progress})
	case "local":
		var cache string
		if cache, err = os.UserCacheDir(); err != nil {
			return launch.Built{}, err
		}
		r, err = builder.Build(ctx, builder.Request{DistYAML: q.DistYAML, Release: t.Signed, ModCache: q.ModCache, Mirrors: q.Mirrors,
			AllowBump: q.AllowBump, Proxy: os.Getenv("GOPROXY"), Out: filepath.Join(filepath.Dir(q.DistYAML), "build"),
			Cache: filepath.Join(cache, "aiopsctl"), Progress: t.Progress})
	default:
		return launch.Built{}, fmt.Errorf("the builder is image or local, not %q", q.Builder)
	}
	return launch.Built{Image: r.Image, Manifest: r.Manifest, Aiopsctl: r.Aiopsctl}, err
}

// New returns the Tools of this binary: the SDK version it was built with, and no
// suite binary, which the launcher of a release's aiopsctl hands in.
func New() Tools {
	t := Tools{SDK: "v0.0.0"}
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
// A fresh plugin has a go.mod and no go.sum, and plugingen builds the plugin's package,
// so with q.Write the modules of that package are first resolved into go.mod and
// go.sum through the author's GOPROXY, as -mod=mod resolves them; go mod tidy then
// adds those of the tests (ADR-060 decision 30, step A1 as edited on 2026-11-25).
func (Tools) Generate(ctx context.Context, q launch.GenerateRequest) ([]launch.Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.Write {
		cmd := exec.CommandContext(ctx, "go", "list", "-mod=mod", "-deps", ".")
		cmd.Dir = q.Dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return lines("2", fmt.Errorf("go list -mod=mod -deps . in %s, which resolves the modules of the plugin's package: %v: %s", q.Dir, err, bytes.TrimSpace(out))), nil
		}
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
		fs, err := vet.Module(dir, vet.Std{})
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			out = append(out, launch.Diagnostic{Check: "3", File: f.File, Line: f.Line, Text: f.Text})
		}
		out = append(out, examples(dir)...)
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

// examples holds each config example testdata/config/*.json of the plugin in dir to the
// configSchema of its manifest, read as the launcher reads a row config: the validator
// of sdk/plugin on the tree of boot.ParseConfig (07-c4-plugin-sdk.md:1569). A manifest
// that does not read is step 1's to report.
func examples(dir string) []launch.Diagnostic {
	files, _ := filepath.Glob(filepath.Join(dir, "testdata", "config", "*.json"))
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.yaml"))
	if len(files) == 0 || err != nil {
		return nil
	}
	m, err := boot.ReadManifest("plugin.yaml", raw, boot.ManifestLenient)
	if err != nil {
		return nil
	}
	schema, _ := json.Marshal(m.ConfigSchema)
	v, err := plugin.NewValidator(schema)
	if err != nil {
		return []launch.Diagnostic{{Check: "3", File: "plugin.yaml", Text: err.Error()}}
	}
	var out []launch.Diagnostic
	for _, f := range files {
		rel := "testdata/config/" + filepath.Base(f)
		src, err := os.ReadFile(f)
		var tree any
		if err == nil {
			tree, err = boot.ParseConfig(rel, src)
		}
		if err == nil {
			err = v.Validate(tree)
		}
		if err != nil {
			out = append(out, launch.Diagnostic{Check: "3", File: rel, Text: "a config example that does not validate against the derived schema: " + err.Error()})
		}
	}
	return out
}

// event is one line of go test -json.
type event struct {
	Action, Test, Output string
}

// goTest runs go test with args in dir, with env added to the environment, and returns
// its events.
func goTest(ctx context.Context, dir string, env []string, args ...string) ([]event, error) {
	cmd := exec.CommandContext(ctx, "go", append([]string{"test", "-count=1", "-json"}, args...)...)
	cmd.Dir, cmd.Env = dir, append(os.Environ(), env...)
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
	evs, err := goTest(ctx, dir, nil, "-race", "-skip", "^TestConformance$", "./...")
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

// conformance runs the suite of the definition name at version on the plugin in dir,
// through the toolchain in the author's module: a test written into a go build overlay,
// never onto disk, mounts the plugin's value as a contribution, or with the mount that
// the plugin's tests declare (mountOf). The halves are those that the suite records it
// ran (builder.RecordVar), and each failed obligation is its id.
func conformance(ctx context.Context, dir, name, version string) (launch.SuiteResult, error) {
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
	file, overlay, rec := filepath.Join(tmp, "suite_test.go"), filepath.Join(tmp, "overlay.json"), filepath.Join(tmp, "ran")
	ovl, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(dir, "zz_aiopsctl_"+name+"_test.go"): file}})
	test := builder.ConformanceTest(pkg.Name, pkg.ImportPath, name, version, "fixtures")
	if mount := mountOf(dir, pkg.Name, name); mount != "" {
		test = fmt.Appendf(nil, "package %s_test\n\nimport (\n\t\"testing\"\n\n\t%q\n)\n\nfunc TestAiopsctl(t *testing.T) { conformance.Run(t, %s) }\n",
			pkg.Name, def+"/conformance", mount)
	}
	if err := errors.Join(os.WriteFile(file, test, 0o600), os.WriteFile(overlay, ovl, 0o600), os.Mkdir(rec, 0o700)); err != nil {
		return launch.SuiteResult{}, err
	}
	evs, err := goTest(ctx, dir, []string{builder.RecordVar + "=" + rec}, "-overlay", overlay, "-run", "^TestAiopsctl$", ".")
	if err != nil {
		return launch.SuiteResult{}, err
	}
	r := launch.SuiteResult{Name: name, Revision: d.Suite, Halves: recorded(rec, name)}
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
	if !ran || len(r.Failed) == 0 && len(r.Halves) == 0 {
		return launch.SuiteResult{}, fmt.Errorf("the suite of %s did not run on %s: %s", name, pkg.ImportPath, strings.Join(out, "; "))
	}
	return r, nil
}

// mountOf returns Mount and the definition's name with its first letter in upper case,
// MountEchoers for echoers, where the external test package pkg_test of the plugin in
// dir declares that function: the mount of the suite's Mount type that step 5 runs the
// suite on, for a plugin that the generic mount of a contribution cannot set up, a
// provider or a contribution that needs more keys than the registry; "" otherwise.
// ponytail: a definition's name of one word, as every definition of S2a has.
func mountOf(dir, pkg, name string) string {
	want := "Mount" + strings.ToUpper(name[:1]) + name[1:]
	files, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	for _, f := range files {
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.SkipObjectResolution)
		for i := 0; err == nil && file.Name.Name == pkg+"_test" && i < len(file.Decls); i++ {
			if fd, ok := file.Decls[i].(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == want {
				return want
			}
		}
	}
	return ""
}

// recorded returns the halves of the suite of name that the records in dir say ran.
func recorded(dir, name string) []string {
	var have []string
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		var r []string // the package's directory, the definition and the halves
		if b, err := os.ReadFile(f); err == nil && json.Unmarshal(b, &r) == nil && len(r) > 1 && r[1] == name {
			have = append(have, r[2:]...)
		}
	}
	return slices.DeleteFunc([]string{"obligations", "guard"}, func(h string) bool { return !slices.Contains(have, h) })
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
		r, err := conformance(ctx, q.Dir, name, q.Definitions[name])
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
