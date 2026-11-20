package main

import (
	"bytes"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

// world is the S1 fixture distribution of the boot package, under /work of an
// in-memory file system, plus a rogue row and a patch for the refusal cases.
func world(t *testing.T) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	root := os.DirFS("../../boot/testdata/s1")
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(root, p)
		m["work/"+p] = &fstest.MapFile{Data: data}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	m["work/p/ok.yml"] = &fstest.MapFile{Data: []byte("- {id: metrics, config: {endpoint: 'http://p3.example.test'}}\n- {id: ghost, enabled: false}\n")}
	m["work/p/bad.yml"] = &fstest.MapFile{Data: []byte("- {id: metrics, plugin: aiops/x}\n")}
	m["work/p/rogue.yml"] = &fstest.MapFile{Data: []byte("- insert: [{id: rogue, plugin: aiops/kek-kms, roles: [api]}]\n")}
	m["abs/iso.yml"] = &fstest.MapFile{Data: []byte("- insert: [{id: iso, plugin: aiops/secrets, roles: [api], isolate: [approval]}]\n")}
	m["work/p/loader.yml"] = &fstest.MapFile{Data: []byte("- insert: [{id: loader, plugin: aiops/loader, roles: [api], required: true}]\n")}
	return m
}

type result struct {
	code        int
	out, errOut string
}

func do(t *testing.T, fsys fs.FS, env map[string]string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errOut, fsys, func(k string) string { return env[k] })
	return result{code, out.String(), errOut.String()}
}

var base = []string{"profile", "render", "--profile", "demo", "--home", "home", "--dist", "dist", "--server"}

func with(extra ...string) []string { return append(append([]string(nil), base...), extra...) }

func TestRunDumps(t *testing.T) {
	w := world(t)
	env := map[string]string{"PWD": "/work"}
	for _, tc := range []struct {
		flag, first string
	}{
		{"--dump", "# aiopsctl profile render --dump"},
		{"--dump-hooks", "# aiopsctl profile render --dump-hooks"},
		{"--dump-config-schema", "{"},
	} {
		r := do(t, w, env, with(tc.flag)...)
		if r.code != 0 || !strings.HasPrefix(r.out, tc.first+"\n") {
			t.Errorf("%s: exit %d, first line %q, stderr %q", tc.flag, r.code, firstLine(r.out), r.errOut)
		}
		if strings.Contains(r.out, "===") {
			t.Errorf("%s: a single dump has no section header", tc.flag)
		}
		if !strings.Contains(r.errOut, "detached as disabled") {
			t.Errorf("%s: the detached row is not on stderr: %q", tc.flag, r.errOut)
		}
	}
	all := do(t, w, env, with("--dump", "--dump-hooks", "--dump-config-schema")...)
	for _, h := range []string{"=== --dump ===\n", "=== --dump-hooks ===\n", "=== --dump-config-schema ===\n"} {
		if !strings.Contains(all.out, h) {
			t.Errorf("missing %q", h)
		}
	}
	// No dump flag: diagnostics only.
	if r := do(t, w, env, with()...); r.code != 0 || r.out != "" || r.errOut == "" {
		t.Errorf("no dump: %+v", r)
	}
	// The same input gives the same bytes.
	if again := do(t, w, env, with("--dump", "--dump-hooks", "--dump-config-schema")...); again != all {
		t.Error("two runs differ")
	}
}

func TestRunRolesPathsAndPatches(t *testing.T) {
	w := world(t)
	env := map[string]string{"PWD": "/work"}
	api := do(t, w, env, with("--role", "api", "--dump")...)
	apiList := do(t, w, env, with("--role", "api,api", "--dump")...)
	if api.code != 0 || !strings.Contains(api.out, "\nroles api\n") || !strings.Contains(apiList.out, "\nroles api,api\n") {
		t.Errorf("roles: %q", firstLine(api.out))
	}
	two := do(t, w, env, with("--role", "api", "--role", "worker", "--dump")...)
	if !strings.Contains(two.out, "\nroles api,worker\n") {
		t.Errorf("repeated roles:\n%s", two.out)
	}
	// A relative patch keeps its relative name in provenance, and an unknown id warns.
	r := do(t, w, env, with("--patch", "p/ok.yml", "--dump")...)
	if r.code != 0 || !strings.Contains(r.out, "(patch:p/ok.yml) p/ok.yml:1") || !strings.Contains(r.errOut, `unknown id "ghost"`) {
		t.Errorf("relative patch: %+v", r)
	}
	// An absolute patch path and one that leaves the working directory.
	for _, p := range []string{"/work/p/ok.yml", "../work/p/ok.yml", "./p/../p/ok.yml"} {
		if r := do(t, w, env, with("--patch", p, "--dump")...); r.code != 0 || strings.Contains(r.out, "/home/") {
			t.Errorf("patch %s: exit %d %s", p, r.code, r.errOut)
		}
	}
	// Home from the environment, bundles from another directory, an absolute dist.
	env2 := map[string]string{"PWD": "/work/p", "AIOPS_HOME": "/work/home"}
	r = do(t, w, env2, "profile", "render", "--profile", "demo", "--bundles", "../home", "--dist", "/work/dist", "--server", "--dump")
	if r.code != 0 {
		t.Errorf("env home: %+v", r)
	}
	// An empty or missing PWD means the root of the file system.
	rootEnv := map[string]string{"PWD": ""}
	r = do(t, w, rootEnv, "profile", "render", "--profile", "demo", "--home", "work/home", "--dist", "work/dist", "--server")
	if r.code != 0 {
		t.Errorf("root pwd: %+v", r)
	}
	r = do(t, w, rootEnv, "profile", "render", "--profile", "demo", "--home", "/", "--dist", "work/dist")
	if r.code != 1 || !strings.Contains(r.errOut, "profiles/demo/profile.yaml") {
		t.Errorf("home at the root: %+v", r)
	}
}

func TestRunRefusals(t *testing.T) {
	w := world(t)
	env := map[string]string{"PWD": "/work"}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"a second provider of kek in one realm", with("--patch", "p/rogue.yml", "--role", "api"), "two-providers"},
		{"patch that changes the plugin", with("--patch", "p/bad.yml"), "fixed-field"},
		{"missing patch file", with("--patch", "p/nope.yml"), "file-missing"},
		{"profile that does not exist", []string{"profile", "render", "--profile", "nope", "--home", "home", "--dist", "dist"}, "profiles/nope/profile.yaml"},
		{"isolate on a bound key", with("--patch", "/abs/iso.yml", "--dump"), "static graph"},
		// boot mounts the loader itself, so it refuses a composition that holds the row; render must too.
		{"a row with the reserved id of the loader", with("--patch", "p/loader.yml", "--dump"), `the row id "loader" is reserved`},
		{"no dist directory", []string{"profile", "render", "--profile", "demo", "--home", "home", "--dist", "none"}, "modules.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := do(t, w, env, tc.args...)
			if r.code != 1 || !strings.Contains(r.errOut, tc.want) {
				t.Errorf("exit %d, stderr %q, want %q", r.code, r.errOut, tc.want)
			}
		})
	}
	// A dist file that is read but malformed, in each of the two files.
	for _, file := range []string{"modules.yaml", "dist.yaml"} {
		bad := fstest.MapFS{}
		for k, v := range w {
			bad[k] = v
		}
		bad["work/dist/"+file] = &fstest.MapFile{Data: []byte("x: [")}
		if r := do(t, bad, env, with()...); r.code != 1 || !strings.Contains(r.errOut, file) {
			t.Errorf("%s: %+v", file, r)
		}
	}
	missing := fstest.MapFS{}
	for k, v := range w {
		missing[k] = v
	}
	delete(missing, "work/dist/dist.yaml")
	if r := do(t, missing, env, with()...); r.code != 1 || !strings.Contains(r.errOut, "dist.yaml") {
		t.Errorf("no dist.yaml: %+v", r)
	}
}

func TestRunUsage(t *testing.T) {
	w := world(t)
	env := map[string]string{"PWD": "/work"}
	for name, args := range map[string][]string{
		"no arguments":       nil,
		"one word":           {"profile"},
		"unknown subcommand": {"profile", "verify"},
		"unknown command":    {"plugin", "render"},
		"unknown flag":       with("--nope"),
		"flag without value": {"profile", "render", "--profile"},
		"no profile":         {"profile", "render", "--home", "home", "--dist", "dist"},
		"no dist":            {"profile", "render", "--profile", "demo", "--home", "home"},
		"no home":            {"profile", "render", "--profile", "demo", "--dist", "dist"},
		"stray argument":     with("extra"),
		"unknown role":       with("--role", "api,boss"),
	} {
		r := do(t, w, env, args...)
		if r.code != 2 || !strings.Contains(r.errOut, "usage: aiopsctl profile render") || r.out != "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", name, r.code, r.out, r.errOut)
		}
	}
}

func TestListFlag(t *testing.T) {
	var l list
	_ = l.Set("a")
	_ = l.Set("b")
	if l.String() != "a,b" {
		t.Errorf("String = %q", l.String())
	}
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }
