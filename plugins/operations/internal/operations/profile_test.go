// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// box is the launcher's toolbox of these tests. Render reads the home as boot would for
// the two layer files: one that holds "broken" and is not set aside stops the
// composition of the profile. It records each request. Shipped holds the profile cli.
type box struct {
	launch.NoTools
	mu   sync.Mutex
	reqs []launch.RenderRequest
}

func (b *box) Render(ctx context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	b.mu.Lock()
	b.reqs = append(b.reqs, q)
	b.mu.Unlock()
	for _, f := range []string{"profiles/" + q.Profile + "/aiops.patch.yml", "aiops.patch.yml"} {
		data, err := os.ReadFile(filepath.Join(q.Home, filepath.FromSlash(f)))
		if q.Home != "" && err == nil && strings.TrimSpace(string(data)) == "broken" && !slices.Contains(q.Aside, f) {
			return launch.Rendered{Diagnostics: []launch.Diagnostic{{Check: "compose", File: f, Line: 1, Text: f + ":1: syntax: broken"}}}, nil
		}
	}
	return launch.Rendered{Departures: []string{"a departure"}}, ctx.Err()
}

func (b *box) requests() []launch.RenderRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.reqs)
}

func (*box) Gate(context.Context, launch.GateRequest) (launch.Verdict, error) {
	return launch.Verdict{}, launch.ErrNotCarried
}

func (*box) ReadManifest(context.Context, launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	return launch.ManifestSummary{}, nil, launch.ErrNotCarried
}

func (*box) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{}, launch.ErrNotCarried
}

const cliProfile = "bundles: [aiops-core, aiops-cli]\n"

func (*box) Shipped(ctx context.Context) (map[string][]byte, error) {
	return map[string][]byte{"profiles/cli/profile.yaml": []byte(cliProfile)}, ctx.Err()
}

func profileRequest(home, p string) def.ProfileRequest {
	return def.ProfileRequest{Caller: caller(), Home: home, Profile: p}
}

// profile init writes through the write of the home (ADR-061 decision 17): it restores
// the snapshot that an interrupted write left before it writes, and says so, so the
// profile it copies outlives the next write; it writes the profile with mode 0600 and
// leaves no snapshot; it creates a missing home but not a missing parent; and it
// answers by run/status.json for the profile it wrote.
func TestInitProfileWritesThroughTheSnapshot(t *testing.T) {
	l := Library{LockWait: time.Second, Box: &box{}}
	h := newHome(t)
	put(t, h, "snapshot/paths.json", `{"Replaced":["trust.yaml"],"Created":["profiles"]}`)
	put(t, h, "snapshot/files/trust.yaml", "trusted\n")
	ch, err := l.InitProfile(t.Context(), profileRequest(h.Dir, "cli"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ch.Notes) < 2 || !strings.Contains(ch.Notes[0], "restored") || !strings.Contains(ch.Notes[len(ch.Notes)-1], "copied the shipped profile cli") {
		t.Errorf("notes %q; want the restore, then the copy", ch.Notes)
	}
	p := filepath.Join(h.Dir, "profiles", "cli", "profile.yaml")
	if !slices.Equal(ch.Written, []string{p}) || ch.Outcome != def.NotRunning {
		t.Errorf("change %+v; want %s written and not-running", ch, p)
	}
	if got := tree(t, h); !equal(got, map[string]string{"trust.yaml": "trusted\n", "profiles/cli/profile.yaml": cliProfile}) {
		t.Errorf("home %v; want trust.yaml restored and the profile copied", got)
	}
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("%s: %v, %v; want mode 0600", p, st, err)
	}
	// The next write keeps the profile.
	if _, err := h.Write(t.Context(), Write{Command: "next", Plan: plan([]Change{{Path: "trust.yaml", Data: []byte("x\n")}})}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("the next write removed the profile that init copied: %v", err)
	}
	// A process of the profile runs: the write is not submitted; one of another profile runs: not-running.
	_ = os.Remove(p)
	put(t, h, "run/status.json", `{"phase": "running", "profile": "cli"}`)
	if ch, err := l.InitProfile(t.Context(), profileRequest(h.Dir, "cli")); err != nil || ch.Outcome != def.RestartRequired || ch.Reason != def.ReasonNotSubmitted {
		t.Errorf("with cli running: %+v, %v; want restart-required, not-submitted", ch, err)
	}
	_ = os.Remove(p)
	put(t, h, "run/status.json", `{"phase": "running", "profile": "dev"}`)
	if ch, err := l.InitProfile(t.Context(), profileRequest(h.Dir, "cli")); err != nil || ch.Outcome != def.NotRunning {
		t.Errorf("with dev running: %+v, %v; want not-running", ch, err)
	}
	// A missing parent fails the write and creates nothing.
	missing := filepath.Join(t.TempDir(), "no", "such", "home")
	if _, err := l.InitProfile(t.Context(), profileRequest(missing, "cli")); err == nil {
		t.Error("profile init created a home whose parent is missing")
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(missing))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the missing parent: %v; want it still missing", err)
	}
}

// profile sanitize sets aside each layer file that stops the composition, the profile
// layer and then the site layer, renaming it to <name>.bak-<time> through the write of
// the home, and submits the profile; a profile that composes changes nothing; a
// refused write puts every file back (ADR-061 decisions 17 and 41).
func TestSanitizeProfileSetsAsideWhatStops(t *testing.T) {
	b := &box{}
	l := Library{LockWait: time.Second, Box: b}
	h := newHome(t)
	put(t, h, "profiles/cli/aiops.patch.yml", "broken\n")
	put(t, h, "aiops.patch.yml", "broken\n")
	ch, err := l.SanitizeProfile(t.Context(), profileRequest(h.Dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := tree(t, h)
	var aside []string
	for p, data := range got {
		if strings.Contains(p, ".bak-") && data == "broken\n" {
			aside = append(aside, p)
		}
	}
	slices.Sort(aside)
	if len(aside) != 2 || !strings.HasPrefix(aside[0], "aiops.patch.yml.bak-") || !strings.HasPrefix(aside[1], "profiles/cli/aiops.patch.yml.bak-") {
		t.Fatalf("home %v; want both layer files set aside", got)
	}
	if _, ok := got["aiops.patch.yml"]; ok {
		t.Errorf("the site layer is still in place")
	}
	if len(ch.Written) != 4 || len(ch.Notes) != 2 || ch.Outcome != def.NotRunning || !strings.Contains(ch.Notes[0], "set aside as aiops.patch.yml.bak-") {
		t.Errorf("change %+v; want each copy and each removal written, a note per file and not-running", ch)
	}
	for _, q := range b.requests() {
		if q.Home != h.Dir || q.Profile != "cli" {
			t.Errorf("render %+v; want the home and the profile cli", q)
		}
	}
	before := tree(t, h)
	if ch, err := l.SanitizeProfile(t.Context(), profileRequest(h.Dir, "cli")); err != nil || len(ch.Written) != 0 || ch.Outcome != "" ||
		len(ch.Notes) != 1 || !strings.Contains(ch.Notes[0], "no layer file of the home stops the composition of cli") {
		t.Errorf("a second sanitize: %+v, %v; want nothing written or submitted and a note", ch, err)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Errorf("a sanitize with nothing to set aside changed the home: %v", got)
	}
}

// Render and VerifyProfile hand the request to the toolbox and write nothing; a home
// that is not absolute and a profile name that is a path are usage errors.
func TestRenderHandsTheRequestOn(t *testing.T) {
	b := &box{}
	l := Library{LockWait: time.Second, Box: b}
	h := newHome(t)
	before := tree(t, h)
	q := launch.RenderRequest{Home: h.Dir, Profile: "p", Patches: []string{"/p.yml"}, Dumps: []string{"--dump"}}
	if r, err := l.Render(t.Context(), def.RenderRequest{Caller: caller(), RenderRequest: q}); err != nil || len(r.Departures) != 1 {
		t.Fatalf("Render = %+v, %v", r, err)
	}
	if v, err := l.VerifyProfile(t.Context(), def.RenderRequest{Caller: caller(), RenderRequest: launch.RenderRequest{Home: h.Dir, Dumps: []string{"--dump"}}}); err != nil || len(v.Departures) != 1 {
		t.Fatalf("VerifyProfile = %+v, %v", v, err)
	}
	reqs := b.requests()
	if len(reqs) != 2 || !slices.Equal(reqs[0].Patches, q.Patches) || reqs[1].Profile != "cli" || reqs[1].Dumps != nil {
		t.Errorf("requests %+v; want the first as given, the second for cli with no dump", reqs)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Errorf("a render changed the home: %v", got)
	}
	for _, r := range []def.RenderRequest{{RenderRequest: launch.RenderRequest{Home: "home", Profile: "p"}}, {RenderRequest: launch.RenderRequest{Profile: "../p"}}} {
		if _, err := l.Render(t.Context(), r); !errors.Is(err, def.ErrUsage) {
			t.Errorf("Render %+v: %v, want ErrUsage", r.RenderRequest, err)
		}
	}
	for _, p := range []string{"", "..", "a/b"} {
		if _, err := l.InitProfile(t.Context(), profileRequest(h.Dir, p)); !errors.Is(err, def.ErrUsage) {
			t.Errorf("profile init %q: %v, want ErrUsage", p, err)
		}
	}
}
