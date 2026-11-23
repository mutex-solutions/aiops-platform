package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// The golden dumps of the S1 exit (08-profiles-roadmap-adrs.md:693): the three dumps of
// `aiopsctl profile render` for the profile "smoke" of the test distribution, for the
// process that runs every role and for the role api, which is where every row of the
// profile runs. The files are in testdata/test-dist and `go test -update` rewrites them.
//
// The data is the one the test binary links: the signed distribution manifest it
// embeds, test/dist/distribution.yaml, with the manifest of each plugin and served
// data, and the profile and the bundle it embeds. The home trusts the manifest as dist
// use records it (test/trusted; ADR-060 decision 28), so the gate part is the
// evaluated checks. aiopsctl itself is run, as it runs in main, on a file system that
// holds those files.
//
// Not covered by golden files, and said so: the rows of the roles edge and worker,
// which the profile does not have. The volatile paths config.note and config.tag of
// aiops-test/toy-consumer are the marks of its plugin.Volatile fields in the
// configSchema of its manifest (ADR-048 decision 9), and the stage of each hook
// listener is its ordinal in the stages that serves holds (ADR-060 decision 7).

var update = flag.Bool("update", false, "rewrite the golden files")

// testDistFS is a file system with the test distribution as aiopsctl reads it, with
// signed as its signed manifest: dist/ with distribution.yaml, and home/ with the
// profile, the bundle and the records by which the home trusts signed.
func testDistFS(t *testing.T, signed []byte) fstest.MapFS {
	t.Helper()
	d, err := testdist.Distribution()
	if err != nil {
		t.Fatal(err)
	}
	m := fstest.MapFS{"dist/distribution.yaml": {Data: signed}}
	files, err := trusted.Files(signed)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		m["home/"+name] = &fstest.MapFile{Data: data}
	}
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
	w := testDistFS(t, testdist.Signed)
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
// what the server would compose"), though render holds no Go value: it reads the
// classes of the definitions from serves of the signed manifest, as the server reads
// them from the Go values (ADR-059 decision 20; ADR-060 decision 14). The variants of
// the manifest drop a binding of a key that a definition binds, so only the definition
// says that the key is bound.
func TestRenderDetachesWhatTheServerDetaches(t *testing.T) {
	d, err := testdist.Distribution()
	if err != nil {
		t.Fatal(err)
	}
	for _, drop := range [][]string{
		{"    reload:\n      - aiops-test/test-state\n      - aiops-test/test-submit\n"},
		{"    reload:\n      - aiops-test/test-state\n      - aiops-test/test-submit\n", "    toy.policy:\n      - aiops-test/toy-provider\n"},
	} {
		signed := string(testdist.Signed)
		for _, b := range drop {
			if !strings.Contains(signed, b) {
				t.Fatalf("the signed manifest has no binding %q", b)
			}
			signed = strings.Replace(signed, b, "", 1)
		}
		man, err := boot.ReadDistribution("distribution.yaml", []byte(signed))
		if err != nil {
			t.Fatal(err)
		}
		w := testDistFS(t, []byte(signed))
		home, err := fs.Sub(w, "home")
		if err != nil {
			t.Fatal(err)
		}
		comp, _, err := boot.Compose(boot.Sources{Profile: "smoke", Home: home, Bundles: home},
			boot.Options{Server: true, Modules: d.Registry.Modules})
		if err != nil {
			t.Fatal(err)
		}
		// The server's class rules on the Go values, given no manifest to read classes from.
		in := boot.GateInput{Entry: boot.EntryBoot, Mods: d.Registry.Modules, Bindings: man.Bindings()}
		srv := boot.AdmitWith(boot.WithLoader(comp), in, nil).Gate.Detached
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
