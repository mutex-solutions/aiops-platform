// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/dist/harness"
)

// running starts aiops-test on a fresh home that holds files, with the profile dev and
// patches, and waits until it runs.
func running(t *testing.T, files map[string]string, patches ...string) *harness.Proc {
	t.Helper()
	p, err := harness.Start(t, harness.Binary(t), harness.Options{Profile: "dev", Context: t.Context(), Patches: patches,
		Install: func(home string) error { return writeFiles(home, files) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Stop(10 * time.Second); _ = os.RemoveAll(p.Home) })
	if s, ok := p.Until(time.Minute, harness.Snapshot.Running); !ok {
		t.Fatalf("aiops-test did not run dev: %+v\n%s", s.State.Rows, p.Output())
	}
	return p
}

// generation returns the generation of the passed final result that r prints, 0 for
// none.
func generation(r result) uint64 {
	var g uint64
	if _, err := fmt.Sscanf(r.out, "result passed, generation %d,", &g); err != nil {
		return 0
	}
	return g
}

// restarted fails t unless test-state reports, of the generation of the passed result
// that r prints, that the loader restarted row.
func restarted(t *testing.T, p *harness.Proc, r result, row string) {
	t.Helper()
	if s, ok := p.Until(30*time.Second, func(s harness.Snapshot) bool {
		return s.State.Reload != nil && s.State.Reload.Generation == generation(r) && slices.Contains(s.State.Reload.Applied["restarted"], row)
	}); !ok {
		t.Errorf("the generation of %q did not restart %s: test-state reports %+v", r.out, row, s.State.Reload)
	}
}

// The built-binary cases of the submission (ADR-062 decision 18), through the aiopsctl
// of the test distribution as a process of its own, on aiops-test running dev with the
// reload-socket of aiops-core and, in a bundle of the home, test-state, the rows of the
// required keys and the rows extra and flaky, disabled. Enabling extra answers passed
// with the generation that test-state reports, and the row is ACTIVE. Enabling flaky,
// whose first applies fail, answers rolled back, exits 1 and restores the profile
// patch, and the tree that runs is the one that the loader mounted. A generation that
// changes the row-level inject of reload-socket, which restarts it during the apply,
// answers passed. With a site layer that disables extra, enabling extra answers passed
// without the row, exits 1 and keeps the files. A generation that changes the history
// of reload, which restarts reload, answers passed. Disabling reload-socket or reload
// writes nothing and exits 1. On a process that runs dev with reload-socket disabled,
// enable answers restart-required and exits 1; with no status.json it exits 0 and
// changes the files only. s1-suites runs it.
func TestEnableOutcomesOnARunningProcess(t *testing.T) {
	files := map[string]string{
		"profiles/dev/profile.yaml": "bundles: [aiops-core, extras]\n",
		"bundles/extras/bundle.yaml": "- insert:\n    - {id: state, plugin: aiops-test/test-state, roles: [api]}\n" +
			"    - {id: spine, plugin: aiops-test/toy-spine, roles: [api], config: {workers: 2}}\n" +
			"    - {id: store, plugin: aiops-test/toy-provider, roles: [api]}\n" +
			"    - {id: extra, plugin: aiops-test/toy-consumer, roles: [api], enabled: false, config: {note: extra}}\n" +
			"    - {id: flaky, plugin: aiops-test/fail-apply, roles: [api], enabled: false, config: {failTimes: 2}}\n",
		// aiopsctl boots cli, whose required keys of the test distribution no row of
		// aiops-core provides.
		"profiles/cli/profile.yaml": "bundles: [aiops-core, aiops-cli, toys]\n",
		"bundles/toys/bundle.yaml":  "- insert:\n    - {id: spine, plugin: aiops-test/toy-spine, config: {workers: 2}}\n    - {id: store, plugin: aiops-test/toy-provider}\n",
	}
	p := running(t, files)
	if s, ok := p.Until(time.Minute, func(s harness.Snapshot) bool { return s.RowState("reload-socket") == "ACTIVE" }); !ok {
		t.Fatalf("reload-socket is not ACTIVE: %+v", s.State.Rows)
	}
	env := map[string]string{"PWD": t.TempDir(), "AIOPS_HOME": p.Home}
	patch, site := filepath.Join(p.Home, "profiles", "dev", "aiops.patch.yml"), filepath.Join(p.Home, "aiops.patch.yml")
	read := func(f string) string { b, _ := os.ReadFile(f); return string(b) }
	edit := func(f, line string) {
		if err := os.WriteFile(f, []byte(read(f)+line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	say := func(r result) string {
		return fmt.Sprintf("exit %d, stdout %q; stderr:\n%s\nthe process:\n%s", r.code, r.out, r.errOut, p.Output())
	}

	r := ctlRun(t, env, "plugin", "enable", "extra", "--profile", "dev")
	if s, ok := p.Until(30*time.Second, func(s harness.Snapshot) bool {
		return s.RowState("extra") == "ACTIVE" && s.State.Reload != nil && s.State.Reload.Generation == generation(r)
	}); r.code != 0 || !ok {
		t.Fatalf("enable extra: %s; test-state reports %+v", say(r), s.State.Reload)
	}

	before := read(patch)
	r = ctlRun(t, env, "plugin", "enable", "flaky", "--profile", "dev")
	if r.code != 1 || !strings.HasPrefix(r.out, "result rolled back, generation ") || read(patch) != before {
		t.Errorf("enable flaky: %s; want rolled back, exit 1 and the profile patch restored to %q, not %q", say(r), before, read(patch))
	}
	if s, ok := p.Until(30*time.Second, func(s harness.Snapshot) bool { return s.RowState("flaky") != "ACTIVE" && len(s.TreeProblems()) == 0 }); !ok {
		t.Errorf("after the rollback the tree that runs is not the one the loader mounted: %v, %+v", s.TreeProblems(), s.State.Rows)
	}

	edit(patch, "- {id: reload-socket, inject: [clock]}\n")
	if r = ctlRun(t, env, "plugin", "disable", "extra", "--profile", "dev"); r.code != 0 || !strings.HasPrefix(r.out, "result passed, generation ") {
		t.Errorf("disable extra in a generation that restarts reload-socket: %s", say(r))
	}
	restarted(t, p, r, "reload-socket")

	edit(site, "- {id: extra, enabled: false}\n")
	before = read(patch)
	r = ctlRun(t, env, "plugin", "enable", "extra", "--profile", "dev")
	if r.code != 1 || !strings.HasPrefix(r.out, "result passed, generation ") || !strings.Contains(r.errOut, "does not show") || read(patch) == before {
		t.Errorf("enable extra under a site layer that disables it: %s; want passed, exit 1 and the files kept", say(r))
	}
	if err := os.Remove(site); err != nil {
		t.Fatal(err)
	}

	edit(patch, "- {id: reload, config: {queue: 8, history: 64}}\n")
	if r = ctlRun(t, env, "plugin", "disable", "extra", "--profile", "dev"); r.code != 0 || !strings.HasPrefix(r.out, "result passed, generation ") {
		t.Errorf("disable extra in a generation that restarts reload: %s", say(r))
	}
	restarted(t, p, r, "reload")

	for _, row := range []string{"reload-socket", "reload"} {
		before := homeTree(t, p.Home)
		if r = ctlRun(t, env, "plugin", "disable", row, "--profile", "dev"); r.code != 1 || !maps.Equal(homeTree(t, p.Home), before) {
			t.Errorf("disable %s: %s; want exit 1 and nothing written", row, say(r))
		}
	}

	off := filepath.Join(t.TempDir(), "no-socket.yml")
	if err := os.WriteFile(off, []byte("- {id: reload-socket, enabled: false}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := running(t, files, off)
	env["AIOPS_HOME"] = q.Home
	patch = filepath.Join(q.Home, "profiles", "dev", "aiops.patch.yml")
	r = ctlRun(t, env, "plugin", "enable", "extra", "--profile", "dev", "--connect-wait", "1s")
	if r.code != 1 || !strings.Contains(r.out, "restart-required: no-answer") || !strings.Contains(read(patch), "id: extra") {
		t.Errorf("enable extra on a process whose reload-socket is disabled: %s; want restart-required, exit 1 and the files kept", say(r))
	}
	q.Stop(10 * time.Second)
	if err := os.Remove(filepath.Join(q.RunDir(), "status.json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	before = read(patch)
	if r = ctlRun(t, env, "plugin", "disable", "extra", "--profile", "dev"); r.code != 0 || read(patch) == before || strings.Contains(r.out, "result") {
		t.Errorf("disable extra with no status.json: %s; want exit 0 and the files changed only", say(r))
	}
}
