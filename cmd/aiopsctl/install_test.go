package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
	if os.Getenv(asCtl) != "" {
		os.Exit(testCtl())
	}
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

// asCtl makes the test binary the aiopsctl of the test distribution: TestMain runs its
// arguments as main runs them, with the module table, the signed manifest and the
// shipped files of the test distribution, its own over the release's, and the stand-ins
// of the embedded binaries, so a case runs aiopsctl as a process of its own, with its
// signals and its exit code.
const asCtl = "AIOPSCTL_TEST_DIST"

func testCtl() int {
	d, err := testdist.Distribution()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aiopsctl: %v\n", err)
		return 1
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	dir, _ := os.Getwd()
	return run(context.Background(), os.Args[1:],
		process{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Environ: os.Environ(), Dir: dir, Files: os.DirFS("/"), Signals: sigs},
		distribution{Registry: d.Registry, Signed: d.Signed, Limits: d.Limits, Shipped: d.Shipped, Tools: standIns})
}

// ctl returns the command that runs the test binary as the aiopsctl of the test
// distribution with args, in the working directory and with the home of env.
func ctl(t *testing.T, env map[string]string, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), exe, args...)
	cmd.Dir, cmd.Env = env["PWD"], append(os.Environ(), asCtl+"=1", "AIOPS_HOME="+env["AIOPS_HOME"], "AIOPS_KEY_PASSWORD="+env["AIOPS_KEY_PASSWORD"])
	return cmd
}

// ctlRun runs ctl to its end and returns its exit code and output.
func ctlRun(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := ctl(t, env, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if ee := (*exec.ExitError)(nil); err != nil && !errors.As(err, &ee) {
		t.Fatal(err)
	}
	return result{cmd.ProcessState.ExitCode(), out.String(), errOut.String()}
}

// standIns are the stand-ins of the suite and scenario binaries that a release build
// embeds, each reporting a pass (ADR-060 decision 46).
var standIns = authoring.Tools{Suites: fstest.MapFS{
	"suites/echoers.test":       standIn(`{"Name":"echoers","Revision":1,"Halves":["obligations"]}`),
	"suites/scenario-echo.test": standIn(`{"Name":"echo","Version":"1","Halves":["kernel"]}`)}}

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
// the profile dev of a running aiops-test and enables its row. The home holds no
// profiles/dev/profile.yaml, so the process boots the shipped dev, whose aiops-core runs
// reload-socket (ADR-062 decision 2), with the test-state row, the toy rows that provide
// the test distribution's required keys toy.spine and toy.store, and the toy dispatcher
// in the profile layer of the home (ADR-060 decision 38). Install copies the shipped dev into the home and says
// so, appends the pack's bundle to it, writes plugins.lock by the pushed digest and
// unpacks the content (ADR-060 decision 27). Install and enable each submit over the
// socket of the process, which answers passed, and the row is ACTIVE in the state that
// test-state writes. plugin gate --against the test distribution's signed manifest
// passes the pack, and a copy whose row sets required, which the bundle of an installed
// artifact may not (08:395, check 8), is denied by the gate at pack. Stand-ins
// of the embedded binaries report the echoers suite and the echo scenario passing, as
// those the release build embeds do (ADR-060 decision 46).
func TestInstallAndEnableReachARunningProcess(t *testing.T) {
	bin := harness.Binary(t)
	p, err := harness.Start(t, bin, harness.Options{Profile: "dev", Context: t.Context(), Install: func(home string) error {
		return writeFiles(home, map[string]string{
			"profiles/dev/aiops.patch.yml": "- insert:\n    - {id: state, plugin: aiops-test/test-state, roles: [api]}\n" +
				"    - {id: spine, plugin: aiops-test/toy-spine, roles: [api], config: {workers: 2}}\n" +
				"    - {id: store, plugin: aiops-test/toy-provider, roles: [api]}\n" +
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
		t.Fatalf("aiops-test did not run the shipped dev with reload-socket ACTIVE: %+v\n%s", s.State.Rows, p.Output())
	}
	work := t.TempDir()
	if err := writeFiles(work, echoPack); err != nil {
		t.Fatal(err)
	}
	reg := ocitest.New(t, ocitest.Options{})
	env := map[string]string{"PWD": work, "AIOPS_HOME": p.Home, "AIOPS_KEY_PASSWORD": "test"}
	tools := standIns
	var spec string
	against, err := filepath.Abs(filepath.Join("..", "..", "test", "dist", "distribution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"key", "generate", "author"}, {"trust", "add", "author.pub", "--ids", "acme/*"},
		{"plugin", "pack", "--out", "out"}, {"plugin", "sign", "--key", "author.key", "--out", "out"},
		{"plugin", "push", reg.Host + "/echo-pack", "--out", "out"}, {"plugin", "gate", "<spec>", "--against", against},
		{"plugin", "install", "<spec>", "--profile", "dev"},
		{"plugin", "enable", "echo-pack:hello", "--profile", "dev"}} {
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
		if (args[1] == "install" || args[1] == "enable") && (r.out != "" || !strings.Contains(r.errOut, "result passed, generation ")) {
			t.Errorf("%q answered stdout %q, stderr %q; want the final result passed on stderr", args, r.out, r.errOut)
		}
		if args[1] == "install" && !strings.Contains(r.errOut, "copied the shipped profile dev to profiles/dev/profile.yaml") {
			t.Errorf("install into dev of a home with no profiles/dev/profile.yaml did not say that it copied the shipped one:\n%s", r.errOut)
		}
	}
	_, digest, _ := strings.Cut(spec, "@")
	dev, err := fs.ReadFile(dist.Shipped(), "profiles/dev/profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"profiles/dev/profile.yaml": strings.Replace(string(dev), "bundles: [aiops-core]\n", "bundles: [aiops-core, acme/echo-pack]\n", 1),
		"content/acme/echo-pack/README.md": echoPack["content/README.md"]} {
		if b, err := os.ReadFile(filepath.Join(p.Home, filepath.FromSlash(name))); err != nil || string(b) != want {
			t.Errorf("%s of the home: %q, %v; want %q", name, b, err, want)
		}
	}
	if b, err := os.ReadFile(filepath.Join(p.Home, "profiles", "dev", "plugins.lock")); err != nil || digest == "" ||
		!strings.Contains(string(b), "{id: acme/echo-pack, digest: "+digest+", content: sha256:") {
		t.Errorf("plugins.lock of dev: %q, %v; want acme/echo-pack at %s with its content", b, err, digest)
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

// doTest runs aiopsctl with the module table, the signed manifest and the shipped
// bundles and profiles of the test distribution, its own over the release's, and tools.
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
		distribution{Registry: d.Registry, Signed: d.Signed, Limits: d.Limits, Shipped: d.Shipped, Tools: tools})
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
