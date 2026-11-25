package main

import (
	"cmp"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/internal/authoring"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
	"github.com/mutex-solutions/aiops-platform/test/dist/harness"
)

func TestMain(m *testing.M) {
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

// The echo pack: a pack of acme whose bundle inserts one disabled row of the toy
// echoer of the test release, with content and a fixture (ADR-060 decision 38).
var echoPack = map[string]string{
	"plugin.yaml": "manifestVersion: \"1.0\"\nid: acme/echo-pack\nversion: 0.1.0\ntitle: Acme echo pack\nlicense: Apache-2.0\n" +
		"publisher: {id: acme}\nstability: experimental\nscope: [process]\nruntimes: []\nrequires: {definitions: {echoers: [\"0.1\"]}}\n",
	"bundle.yaml":        "- insert:\n    - {id: hello, plugin: aiops/example-echoer, roles: [api], enabled: false, config: {name: hello}}\n",
	"content/README.md":  "# Echo pack\n",
	"fixtures/echo.json": "{}\n",
}

// standIn is a stand-in for an embedded suite or scenario binary of the release build:
// a script that writes report as its JSON report, as internal/packsuite does.
func standIn(report string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("#!/bin/sh\nprintf '%s' '" + report + "' > \"$AIOPS_REPORT\"\n"), Mode: 0o755}
}

// The S2a exit's steps C3 and C4 on the test distribution (ADR-060 decision 30;
// ADR-062 decision 18): aiopsctl, over the test distribution's module table, packs
// the echo pack, signs it, pushes it to a registry inside the test, installs it into
// the profile of a running aiops-test, whose composition adds reload-socket and the toy
// dispatcher to smoke, and enables its row. Install and enable each submit over the
// socket of the process, which answers passed, and the row is ACTIVE in the state that
// test-state writes. plugin gate --against the test distribution's signed manifest
// passes the pack, and a copy whose row sets required, which the bundle of an installed
// artifact may not (08:395, check 8), is denied by the gate at pack. Stand-ins
// of the embedded binaries report the echoers suite and the echo scenario passing, as
// those the release build embeds do (ADR-060 decision 46).
func TestInstallAndEnableReachARunningProcess(t *testing.T) {
	bin := harness.Binary(t)
	p, err := harness.Start(t, bin, harness.Options{Profile: "echo", Context: t.Context(), Install: func(home string) error {
		return writeFiles(home, map[string]string{
			"profiles/echo/profile.yaml": "bundles: [smoke, socket]\n",
			"bundles/socket/bundle.yaml": "- insert:\n    - {id: reload-socket, plugin: aiops/reload-socket, roles: [api]}\n" +
				"    - {id: dispatch, plugin: aiops/example-dispatch, roles: [api]}\n",
			// The cli profile of the home meets the required set of the test distribution,
			// whose required keys toy.spine and toy.store no row of aiops-core provides.
			"profiles/cli/profile.yaml": "bundles: [aiops-core, aiops-cli, toys]\n",
			"bundles/toys/bundle.yaml": "- insert:\n    - {id: spine, plugin: aiops-test/toy-spine, config: {workers: 2}}\n" +
				"    - {id: store, plugin: aiops-test/toy-provider}\n",
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(10 * time.Second); _ = os.RemoveAll(p.Home) })
	if s, ok := p.Until(time.Minute, harness.Snapshot.Running); !ok || s.RowState("reload-socket") != "ACTIVE" {
		t.Fatalf("aiops-test did not run echo with reload-socket ACTIVE: %+v\n%s", s.State.Rows, p.Output())
	}
	work := t.TempDir()
	if err := writeFiles(work, echoPack); err != nil {
		t.Fatal(err)
	}
	reg := ocitest.New(t, ocitest.Options{})
	env := map[string]string{"PWD": work, "AIOPS_HOME": p.Home, "AIOPS_KEY_PASSWORD": "test"}
	tools := authoring.Tools{Suites: fstest.MapFS{
		"suites/echoers.test":       standIn(`{"Name":"echoers","Revision":1,"Halves":["obligations"]}`),
		"suites/scenario-echo.test": standIn(`{"Name":"echo","Version":"1","Halves":["kernel"]}`)}}
	var spec string
	against, err := filepath.Abs(filepath.Join("..", "..", "test", "dist", "distribution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"key", "generate", "author"}, {"trust", "add", "author.pub", "--ids", "acme/*"},
		{"plugin", "pack", "--out", "out"}, {"plugin", "sign", "--key", "author.key", "--out", "out"},
		{"plugin", "push", reg.Host + "/echo-pack", "--out", "out"}, {"plugin", "gate", "<spec>", "--against", against},
		{"plugin", "install", "<spec>", "--profile", "echo"},
		{"plugin", "enable", "echo-pack:hello", "--profile", "echo"}} {
		if args[2] == "<spec>" {
			args[2] = spec
		}
		r := doTest(t, env, tools, args...)
		if r.code != 0 {
			t.Fatalf("%q: exit %d, stdout %q; stderr:\n%s\nthe process:\n%s", args, r.code, r.out, r.errOut, p.Output())
		}
		t.Logf("aiopsctl %s: stdout %q; stderr:\n%s", strings.Join(args, " "), r.out, r.errOut)
		if args[1] == "push" {
			spec = strings.TrimSpace(r.out)
		}
		if (args[1] == "install" || args[1] == "enable") && !strings.HasPrefix(r.out, "result passed, generation ") {
			t.Errorf("%q answered %q; want the final result passed", args, r.out)
		}
	}
	if s, ok := p.Until(30*time.Second, func(s harness.Snapshot) bool { return s.RowState("echo-pack:hello") == "ACTIVE" }); !ok {
		t.Errorf("the row echo-pack:hello is not ACTIVE in the running process: %+v", s.State.Rows)
	}
	bad := t.TempDir()
	files := map[string]string{"bundle.yaml": strings.Replace(echoPack["bundle.yaml"], "enabled: false", "enabled: false, required: true", 1),
		"plugin.yaml": strings.Replace(echoPack["plugin.yaml"], "acme/echo-pack", "acme/bad-pack", 1)}
	if err := writeFiles(bad, files); err != nil {
		t.Fatal(err)
	}
	env["PWD"] = bad
	if r := doTest(t, env, tools, "plugin", "pack", "--out", "out"); r.code != 1 || !strings.Contains(r.errOut, "check 8") {
		t.Errorf("pack of a row that sets required: exit %d, stderr %q; want check 8", r.code, r.errOut)
	}
}

// doTest runs aiopsctl with the module table and the signed manifest of the test
// distribution, its shipped bundles and profiles over the release's, and tools.
func doTest(t *testing.T, env map[string]string, tools authoring.Tools, args ...string) result {
	t.Helper()
	d, err := testdist.Distribution()
	if err != nil {
		t.Fatal(err)
	}
	var environ []string
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}
	var out, errOut syncBuffer
	code := run(t.Context(), args, process{In: strings.NewReader(""), Out: &out, Err: &errOut, Environ: environ, Dir: cmp.Or(env["PWD"], "/"), Files: os.DirFS("/")},
		distribution{Registry: d.Registry, Signed: d.Signed, Limits: d.Limits, Shipped: overlay{First: d.Shipped, Second: dist.Shipped()}, Tools: tools})
	return result{code, out.String(), errOut.String()}
}

// writeFiles writes files, by slash path, under dir.
func writeFiles(dir string, files map[string]string) error {
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			return err
		}
	}
	return nil
}
