package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
)

// The golden dumps of the S1 exit (08-profiles-roadmap-adrs.md:693): the three dumps of
// `aiopsctl profile render` for the profile "smoke" of the test distribution, for the
// process that runs every role and for the role api, which is where every row of the
// profile runs. The files are in testdata/test-dist and `go test -update` rewrites them.
//
// The data is the one the test binary links. dist.yaml, the profile and the bundle are
// the files it embeds. The test distribution has no modules.yaml, since its module
// table is Go values (test/dist.Modules), so modulesYAML writes one from that table:
// the declared data (provides, needs, wants, hooks, volatile) is read from the linked
// plugins and is not written a second time by hand. aiopsctl itself is run, as it runs
// in main, on a file system that holds those files.
//
// Not covered by golden files, and said so: a stage number of a hook (the plugin value
// carries the name of its stage and not its ordinal until S2a generates the table, so
// the dump shows stage 0 for every listener) and the rows of the roles edge and worker,
// which the profile does not have.

var update = flag.Bool("update", false, "rewrite the golden files")

// modulesYAML renders a module table as modules.yaml.
func modulesYAML(mods boot.ModuleTable) []byte {
	var b strings.Builder
	ids := make([]string, 0, len(mods))
	for id := range mods {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	list := func(name string, v []string) {
		if len(v) > 0 {
			fmt.Fprintf(&b, "  %s: [%s]\n", name, strings.Join(v, ", "))
		}
	}
	for _, id := range ids {
		e := mods[id]
		fmt.Fprintf(&b, "%s:\n  module: %s\n  version: %s\n", id, e.Module, e.Version)
		list("provides", e.Provides)
		list("needs", e.Needs)
		list("wants", e.Wants)
		var vol []string
		for _, v := range e.Volatile {
			vol = append(vol, strings.TrimPrefix(v, "config."))
		}
		list("volatile", vol)
		if len(e.Hooks) > 0 {
			b.WriteString("  hooks:\n")
			for _, h := range e.Hooks {
				fmt.Fprintf(&b, "    - {hook: %s, stage: %d, name: %s}\n", h.Hook, h.Stage, h.StageName)
			}
		}
	}
	return []byte(b.String())
}

// testDistFS is a file system with the test distribution as aiopsctl reads it: dist/ with
// dist.yaml and modules.yaml, and home/ with the profile and the bundle.
func testDistFS(t *testing.T) fstest.MapFS {
	t.Helper()
	d, err := testdist.Distribution()
	if err != nil {
		t.Fatal(err)
	}
	m := fstest.MapFS{}
	dd, err := fs.ReadFile(d.Shipped, "dist.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m["dist/dist.yaml"] = &fstest.MapFile{Data: dd}
	m["dist/modules.yaml"] = &fstest.MapFile{Data: modulesYAML(d.Registry.Modules)}
	for _, dir := range []string{"bundles", "profiles"} {
		err := fs.WalkDir(d.Shipped, dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			data, err := fs.ReadFile(d.Shipped, p)
			m["home/"+p] = &fstest.MapFile{Data: data}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func TestGoldenDumpsOfTheTestDistribution(t *testing.T) {
	w := testDistFS(t)
	env := map[string]string{"PWD": "/"}
	for _, role := range []string{"", "api"} {
		for _, dump := range []struct{ flag, ext string }{
			{"--dump", "dump"}, {"--dump-hooks", "hooks"}, {"--dump-config-schema", "schema"},
		} {
			name := "smoke"
			args := []string{"profile", "render", "--profile", "smoke", "--home", "home", "--dist", "dist", "--server", dump.flag}
			if role != "" {
				name += "-" + role
				args = append(args, "--role", role)
			}
			t.Run(name+"."+dump.ext, func(t *testing.T) {
				r := do(t, w, env, args...)
				if r.code != 0 {
					t.Fatalf("exit %d; stderr:\n%s", r.code, r.errOut)
				}
				golden := filepath.Join("testdata", "test-dist", name+"."+dump.ext+".golden")
				if *update {
					if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, []byte(r.out), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatalf("%v (go test -update writes it)", err)
				}
				if string(want) != r.out {
					t.Errorf("%s differs from the dump (go test -update rewrites it):\n%s", golden, firstDiff(string(want), r.out))
				}
			})
		}
	}
}

// firstDiff names the first line where two texts differ.
func firstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < max(len(wl), len(gl)); i++ {
		var a, b string
		if i < len(wl) {
			a = wl[i]
		}
		if i < len(gl) {
			b = gl[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\n  golden: %s\n  dump:   %s", i+1, a, b)
		}
	}
	return "no difference"
}
