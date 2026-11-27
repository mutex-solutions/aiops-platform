// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// The test files that a release build compiles into the suite binaries of its aiopsctl
// (ADR-060 decisions 46 and 49), compiled here with go test -c in the repository's
// module, with the toy instantiable echoer of examples/ as the first-party plugin a
// pack's rows name, and run as aiopsctl runs them: a child process with the pack, its
// fixtures and the file of its report in its environment. The suite of echoers fails
// each obligation once whose recorded case is missing or which a row fails, and passes
// a row that meets them; the echo scenario calls each row through the dispatcher and
// fails a reply that differs from the recorded one, a row it does not call and a
// missing script. A row of a plugin that the binary does not carry ends the binary with
// no report.
func TestSuiteBinaries(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files := SuitesGo(map[string]int{"commands": 1, "echoers": 1, "stampers": 1},
		[]SuitePlugin{{Package: rootModule + "/examples/echoer", Accessor: "Plugin", Registers: []string{"echoers"}},
			{Package: rootModule + "/examples/stamper", Accessor: "Plugin", Registers: []string{"stampers"}}})
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, []string{"suites/commands/suite_test.go", "suites/echoers/suite_test.go", "suites/scenario-echo/scenario_test.go", "suites/stampers/suite_test.go"}) {
		t.Fatalf("the test files %v", got)
	}
	tmp := t.TempDir()
	bin := map[string]string{}
	for _, name := range []string{"echoers", "scenario-echo"} {
		var file string
		for p, src := range files {
			if strings.HasPrefix(p, "suites/"+name+"/") {
				file = filepath.Join(tmp, filepath.Base(p))
				if err := os.WriteFile(file, src, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		// The file enters the repository's module as an overlay, in a directory of its own.
		dir := filepath.Join(repo, "internal", "builder", "testdata", "zz-"+name)
		ovl, _ := json.Marshal(map[string]any{"Replace": map[string]string{filepath.Join(dir, filepath.Base(file)): file}})
		overlay := filepath.Join(tmp, name+".json")
		bin[name] = filepath.Join(tmp, name+".test")
		if err := os.WriteFile(overlay, ovl, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "test", "-c", "-vet=off", "-overlay", overlay, "-o", bin[name], "./internal/builder/testdata/zz-"+name)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go test -c of %s: %v\n%s", name, err, out)
		}
	}
	run := func(name string, pack map[string]string, report any) (string, error) {
		t.Helper()
		dir := t.TempDir()
		for p, data := range pack {
			f := filepath.Join(dir, filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		file := filepath.Join(dir, "report.json")
		cmd := exec.Command(bin[name], "-test.count=1")
		cmd.Env = append(os.Environ(), "AIOPS_PACK="+dir, "AIOPS_FIXTURES="+filepath.Join(dir, "fixtures"), "AIOPS_REPORT="+file)
		out, err := cmd.CombinedOutput()
		if b, rerr := os.ReadFile(file); rerr == nil {
			if err := json.Unmarshal(b, report); err != nil {
				t.Fatalf("the report of %s: %v: %s", name, err, b)
			}
		}
		return string(out), err
	}
	// The row of the stamper feeds another registry, so neither the suite of echoers nor
	// the echo scenario runs on it.
	loud := "- insert:\n    - {id: loud, plugin: aiops/example-echoer, enabled: false, config: {name: loud, prefix: 'LOUD '}}\n" +
		"    - {id: stamp, plugin: aiops/example-stamper, enabled: false, config: {name: stamp}}\n"
	cases := map[string]string{}
	for _, id := range conformance.Obligations {
		cases["fixtures/"+id+".json"] = "{}\n"
	}
	with := func(base map[string]string, kv ...string) map[string]string {
		out := maps.Clone(base)
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] == "" {
				delete(out, kv[i])
			} else {
				out[kv[i]] = kv[i+1]
			}
		}
		return out
	}

	for _, c := range []struct {
		name   string
		pack   map[string]string
		failed []string
	}{
		{"a row that meets every obligation", with(cases, "bundle.yaml", loud), nil},
		{"a case that is missing", with(cases, "bundle.yaml", loud, "fixtures/echoers/reply.json", ""), []string{"echoers/reply"}},
		{"a fresh pack", map[string]string{"bundle.yaml": "[]\n"}, conformance.Obligations},
		{"a row that does not start", with(cases, "bundle.yaml", strings.Replace(loud, "name: loud", "name: ''", 1)), conformance.Obligations},
	} {
		var r launch.SuiteResult
		out, err := run("echoers", c.pack, &r)
		if r.Name != "echoers" || r.Revision != 1 || !slices.Equal(r.Halves, []string{"obligations"}) || !slices.Equal(r.Failed, c.failed) {
			t.Errorf("%s: the report %+v (%v); want echoers at revision 1 failing %v\n%s", c.name, r, err, c.failed, out)
		}
	}
	var none launch.SuiteResult
	out, err := run("echoers", with(cases, "bundle.yaml", strings.Replace(loud, "aiops/example-echoer", "aiops/other", 1)), &none)
	if err == nil || none.Name != "" || !strings.Contains(out, "names aiops/other, which is not an instantiable first-party plugin of this release") {
		t.Errorf("a row of a plugin the binary does not carry: %v, %+v\n%s", err, none, out)
	}

	script := `{"calls": [{"row": "loud", "message": "hi", "reply": %q}]}`
	for _, c := range []struct {
		name     string
		pack     map[string]string
		failures []string
	}{
		{"the recorded replies", map[string]string{"bundle.yaml": loud, "fixtures/scenario/echo.json": strings.Replace(script, "%q", `"LOUD hi"`, 1)}, nil},
		{"a reply that differs", map[string]string{"bundle.yaml": loud, "fixtures/scenario/echo.json": strings.Replace(script, "%q", `"hi"`, 1)},
			[]string{`call 0: row loud answered "LOUD hi", <nil>; the recorded reply is "hi"`}},
		{"a row with no call", map[string]string{"bundle.yaml": loud, "fixtures/scenario/echo.json": `{"calls": []}`}, []string{"calls no echoer of row loud"}},
		{"no script", map[string]string{"bundle.yaml": loud}, []string{"no recorded scenario in "}},
	} {
		var r launch.ScenarioReport
		out, err := run("scenario-echo", c.pack, &r)
		ok := r.Name == "echo" && r.Version == "1" && slices.Equal(r.Halves, []string{"kernel"}) && len(r.Failures) == len(c.failures)
		for i := range c.failures {
			ok = ok && strings.Contains(r.Failures[i], c.failures[i])
		}
		if !ok {
			t.Errorf("%s: the report %+v (%v); want the scenario echo 1 of the kernel half failing %q\n%s", c.name, r, err, c.failures, out)
		}
	}
}
