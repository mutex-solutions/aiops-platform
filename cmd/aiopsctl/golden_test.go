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
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
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
// the declared data (provides, needs, wants, hooks, volatile) is read from the
// manifests the test binary embeds and is not written a second time by hand. aiopsctl
// itself is run, as it runs in main, on a file system that holds those files.
//
// Not covered by golden files, and said so: a stage number of a hook (a manifest names
// the stage and not its ordinal, which is served data that boot reads with the signed
// distribution manifest, so the dump shows stage 0 for every listener); a volatile
// path (sdk/plugin marks no Volatile field in configSchema until the kernel can hand a
// value committed in place to a running field, so no plugin of the test distribution
// has one); and the rows of the roles edge and worker, which the profile does not have.

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
		// The classes, which the server reads from the Go values (ADR-059 decision 20).
		p := e.Plugin.(kernel.Plugin)
		keys := slices.Clone(p.Provides)
		for _, d := range p.Inject {
			keys = append(keys, d.Key())
		}
		var pb, cb, sh []string
		for _, k := range keys {
			bp, bc, _ := kernel.KeyClasses(k)
			if bp && !slices.Contains(pb, k.Name()) {
				pb = append(pb, k.Name())
			}
			if bc && !slices.Contains(cb, k.Name()) {
				cb = append(cb, k.Name())
			}
		}
		for _, u := range p.Hooks {
			if u.Sealed() {
				sh = append(sh, u.Hook())
			}
		}
		fmt.Fprintf(&b, "  classes: {providerBound: [%s], consumerBound: [%s], sealedHooks: [%s]}\n",
			strings.Join(pb, ", "), strings.Join(cb, ", "), strings.Join(sh, ", "))
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

// A render reports the rows that the server's gate check 7 detaches (08 §1.4, "exactly
// what the server would compose"), though modules.yaml holds no Go value: it carries
// the classes of the definitions, as the server reads them from the Go values (ADR-059
// decision 20). The dist.yaml variants drop a binding of a key that a definition binds,
// so only the definition says that the key is bound.
func TestRenderDetachesWhatTheServerDetaches(t *testing.T) {
	d, err := testdist.Distribution()
	if err != nil {
		t.Fatal(err)
	}
	for _, drop := range [][]string{
		{"  reload: [aiops-test/test-submit, aiops-test/test-state]\n"},
		{"  reload: [aiops-test/test-submit, aiops-test/test-state]\n", "  toy.policy: [aiops-test/toy-provider]\n"},
	} {
		w := testDistFS(t)
		dd := string(w["dist/dist.yaml"].Data)
		for _, line := range drop {
			if !strings.Contains(dd, line) {
				t.Fatalf("dist.yaml has no line %q", line)
			}
			dd = strings.Replace(dd, line, "", 1)
		}
		w["dist/dist.yaml"] = &fstest.MapFile{Data: []byte(dd)}
		man, err := boot.ParseDist("dist.yaml", []byte(dd))
		if err != nil {
			t.Fatal(err)
		}
		home, err := fs.Sub(w, "home")
		if err != nil {
			t.Fatal(err)
		}
		comp, _, err := boot.Compose(boot.Sources{Profile: "smoke", Home: home, Bundles: home},
			boot.Options{Server: true, Modules: d.Registry.Modules})
		if err != nil {
			t.Fatal(err)
		}
		srv := boot.Admit(boot.WithLoader(comp), d.Registry.Modules, man, nil).Gate.Detached
		if len(srv) == 0 {
			t.Fatalf("dropping %q detaches no row on the server", drop)
		}
		r := do(t, w, map[string]string{"PWD": "/"}, "profile", "render", "--profile", "smoke",
			"--home", "home", "--dist", "dist", "--server", "--dump")
		for _, row := range srv {
			if !strings.Contains(r.errOut, "row \""+row+"\" detached") {
				t.Errorf("dropping %q: the server detaches row %q and the render does not say so; stderr:\n%s", drop, row, r.errOut)
			}
		}
	}
}
