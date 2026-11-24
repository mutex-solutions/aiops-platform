package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// aiopsctl is the launcher of the cli profile: it boots aiops-core and aiops-cli in
// Command mode with the module table of aiops, and the cli row dispatches the command
// (ADR-061 decisions 24 and 25).

// spec is every command of the CLI spec (ADR-061 decision 30).
var spec = []string{"dist build", "dist sign", "dist use", "key generate", "plugin check", "plugin disable", "plugin enable",
	"plugin gate", "plugin inspect", "plugin install", "plugin new", "plugin pack", "plugin push", "plugin remove", "plugin sign",
	"plugin smoke", "plugin verify", "profile init", "profile render", "profile sanitize", "profile verify", "trust add"}

// trustedHome is a home, under home/ of a file system, whose current distribution is the
// release manifest, as dist use records it (test/trusted; ADR-060 decision 28).
func trustedHome(t *testing.T) fstest.MapFS {
	_, signed, _, err := dist.Load()
	if err != nil {
		t.Fatal(err)
	}
	files := trusted.Files(t, signed)
	m := fstest.MapFS{}
	for name, data := range files {
		m["home/"+name] = &fstest.MapFile{Data: data}
	}
	return m
}

// T3: aiopsctl boots the cli profile, and help exits 0 and lists every command of the CLI
// spec, which needs every row of aiops-cli ACTIVE (ADR-061 decision 31).
func TestHelpListsEveryCommand(t *testing.T) {
	r := do(t, fstest.MapFS{}, nil, "help")
	if r.code != 0 || strings.Contains(r.out, "not ACTIVE") {
		t.Fatalf("exit %d; stdout:\n%s\nstderr:\n%s", r.code, r.out, r.errOut)
	}
	for _, name := range spec {
		if !strings.Contains(r.out, "\n  "+name+" ") {
			t.Errorf("help does not list %s:\n%s", name, r.out)
		}
	}
	if strings.Contains(r.out, "aiops: booted") {
		t.Errorf("the launcher wrote on stdout:\n%s", r.out)
	}
}

// The exit codes of ADR-061 decision 10: no argument and an unknown path are usage
// errors, a failed command exits 1 with its reason on stderr and nothing on stdout.
func TestExitCodes(t *testing.T) {
	for _, c := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{nil, 2, "usage: aiopsctl <command>"},
		{[]string{"nope"}, 2, "no such command"},
		{[]string{"key", "generate", "k"}, 1, "aiopsctl: key generate: key generate is not built yet"},
		{[]string{"profile", "init", "cli"}, 2, "AIOPS_HOME is not set"},
		{[]string{"profile", "render", "--profile", "cli", "--dump", "--role", "api"}, 0, ""},
	} {
		r := do(t, fstest.MapFS{}, nil, c.args...)
		if r.code != c.code || !strings.Contains(r.errOut, c.stderr) || r.code != 0 && r.out != "" {
			t.Errorf("%q: exit %d, stdout %q, stderr:\n%s", c.args, r.code, r.out, r.errOut)
		}
	}
}

// The launcher's code is mapped to the three of aiopsctl: 0 and 2 are the command's, 8 is
// a signal, and every other code is a failure of the launcher (ADR-061 decisions 10 and
// 13).
func TestExitCodeOfTheLauncher(t *testing.T) {
	for launcher, want := range map[int]int{boot.ExitOK: 0, boot.ExitRefused: 1, boot.ExitUsage: 2, boot.ExitRequired: 1,
		boot.ExitLeak: 1, boot.ExitGaveUp: 1, boot.ExitInternal: 1, boot.ExitRestore: 1, boot.ExitInterrupted: 1} {
		var b bytes.Buffer
		if got := exitCode(launcher, &b); got != want || (launcher == boot.ExitInterrupted) != strings.Contains(b.String(), "interrupted") {
			t.Errorf("launcher %d: exit %d, stderr %q; want %d", launcher, got, b.String(), want)
		}
	}
}

// A binary built without a release key boots on a home whose current distribution is the
// one it embeds, as dist use records it, and refuses to boot with no such home
// (ADR-020 decisions 12 and 13; ADR-061 decision 26).
func TestBootWithoutAReleaseKey(t *testing.T) {
	m := trustedHome(t)
	if r := doWith(t, m, map[string]string{"AIOPS_HOME": "/home"}, nil, "help"); r.code != 0 {
		t.Errorf("a trusted home: exit %d; stderr:\n%s", r.code, r.errOut)
	}
	if r := doWith(t, m, nil, nil, "help"); r.code != 1 || r.out != "" || !strings.Contains(r.errOut, "error: ") {
		t.Errorf("no home: exit %d, stdout %q; stderr:\n%s", r.code, r.out, r.errOut)
	}
}

// A command of a row that a layer of the home disables, as the gate detaches a denied row,
// exits 1 and names that row (ADR-061 decisions 10 and 31).
func TestCommandOfADisabledRow(t *testing.T) {
	m := trustedHome(t)
	m["home/aiops.patch.yml"] = &fstest.MapFile{Data: []byte("- {id: command-key, enabled: false}\n")}
	r := do(t, m, map[string]string{"AIOPS_HOME": "/home"}, "key", "generate", "k")
	if r.code != 1 || !strings.Contains(r.errOut, "aiops/command-key") {
		t.Fatalf("exit %d; stderr:\n%s", r.code, r.errOut)
	}
}

// A layer file of the home that stops the boot of the cli profile: aiopsctl boots once
// more with the shipped cli profile alone and the home still passed, says which file
// stopped the first boot, and dispatches; profile sanitize sets the file aside, and the
// next run boots on the home (ADR-061 decision 41).
func TestABrokenSiteLayer(t *testing.T) {
	home := t.TempDir()
	_, signed, _, err := dist.Load()
	if err != nil {
		t.Fatal(err)
	}
	trusted.Write(t, home, signed)
	layer := filepath.Join(home, "aiops.patch.yml")
	if err := os.WriteFile(layer, []byte("- {id: clock,\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"AIOPS_HOME": home}
	r := doWith(t, os.DirFS("/"), env, nil, "profile", "sanitize")
	if r.code != 0 || !strings.Contains(r.errOut, "aiops.patch.yml") || !strings.Contains(r.errOut, "shipped cli profile alone") ||
		!strings.Contains(r.errOut, "set aside") {
		t.Fatalf("sanitize: exit %d; stderr:\n%s", r.code, r.errOut)
	}
	if baks, _ := filepath.Glob(layer + ".bak-*"); len(baks) != 1 {
		t.Fatalf("set aside as %v", baks)
	}
	if r := doWith(t, os.DirFS("/"), env, nil, "help"); r.code != 0 || strings.Contains(r.errOut, "shipped cli profile alone") {
		t.Fatalf("after sanitize: exit %d; stderr:\n%s", r.code, r.errOut)
	}
}

// The release ships aiops-core, aiops-cli and the cli profile; a file of the home wins
// over a shipped one (ADR-061 decision 29).
func TestAHomeProfileWins(t *testing.T) {
	m := trustedHome(t)
	m["home/profiles/cli/profile.yaml"] = &fstest.MapFile{Data: []byte("bundles: [aiops-core, aiops-cli, extra]\n")}
	r := do(t, m, map[string]string{"AIOPS_HOME": "/home"}, "help")
	if r.code != 1 || !strings.Contains(r.errOut, "bundles/extra/bundle.yaml") {
		t.Fatalf("exit %d; stderr:\n%s", r.code, r.errOut)
	}
}
