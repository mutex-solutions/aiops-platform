// SPDX-License-Identifier: Apache-2.0

package boot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The toolbox of launch 0.2 (ADR-061 decision 21): boot's parts on what the process
// runs, and the Tools that aiopsctl hands in.

// tree serves each file system under the directory of its name, at the root of Files.
type tree map[string]fs.FS

func (t tree) Open(name string) (fs.File, error) {
	dir, rest, _ := strings.Cut(name, "/")
	if f, ok := t[dir]; ok {
		return f.Open(cmpOr(rest, "."))
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

const boxBundle = happyBundle + "    - {id: probe, plugin: t/probe, roles: [api]}\n"

// boxFixture is a run whose probe holds the launch keys. Files serves the fixture's home
// at /h and extra at /x; Shipped holds the profile "shipped" of one bundle.
type boxFixture struct {
	*fixture
	l     launched
	extra fstest.MapFS
}

func newBox(t *testing.T) *boxFixture {
	b := &boxFixture{extra: fstest.MapFS{}}
	b.fixture = newFixture(t, boxBundle, probe(&b.l))
	bindLaunch(b.fixture, "t/probe")
	b.opt.Files = tree{"h": b.files, "x": b.extra}
	b.opt.Shipped = fstest.MapFS{
		"profiles/shipped/profile.yaml": {Data: []byte("bundles: [s]")},
		"bundles/s/bundle.yaml":         {Data: []byte("- insert:\n    - {id: only, plugin: t/store, roles: [api]}\n")},
	}
	return b
}

// start boots the run with signed, the fixture's own when nil, which the home trusts,
// and returns the toolbox; the run stops when the test ends.
func (b *boxFixture) start(t *testing.T, signed func([]byte) []byte) launch.Toolbox {
	b.sign()
	if signed != nil {
		b.opt.Signed = signed(b.opt.Signed)
		home, _ := trustedHome(b.opt.Signed)
		maps.Copy(b.files, home)
	}
	done := make(chan int, 1)
	go func() { done <- Run(t.Context(), b.opt) }()
	b.waitReady(t, done)
	t.Cleanup(func() { stopAndWait(t, b.fixture, done) })
	return b.toolbox(t)
}

// toolbox returns the toolbox that the probe's profileContext gives.
func (b *boxFixture) toolbox(t *testing.T) launch.Toolbox {
	t.Helper()
	b.l.mu.Lock()
	defer b.l.mu.Unlock()
	tb := b.l.profile.Toolbox()
	if tb == nil {
		t.Fatal("Toolbox = nil")
	}
	return tb
}

func rowIDs(c launch.Composition) []string {
	var out []string
	for _, r := range c.Rows {
		out = append(out, r.ID)
	}
	return out
}

func errorTexts(ds []launch.Diagnostic) []string {
	var out []string
	for _, d := range ds {
		if !d.Warning {
			out = append(out, d.Text)
		}
	}
	return out
}

func TestToolboxRender(t *testing.T) {
	b := newBox(t)
	b.files["patch.yml"] = &fstest.MapFile{Data: []byte("- {id: user, enabled: false}\n- {id: nobody, enabled: false}\n")}
	b.extra["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte("- insert:\n    - {id: store, plugin: t/store, roles: [api]}\n")}
	// A raise that --allow-bump admitted and a hash checked against nothing are departures
	// from the release too (ADR-044 decisions 8 and 10).
	pins := "pins:\n  - {path: example.test/dep, version: v0.1.0, gomod: \"h1:" + strings.Repeat("A", 43) + "=\", unchecked: true}\n" +
		"  - {path: golang.org/x/text, version: v0.44.0, sum: \"h1:" + strings.Repeat("B", 43) + "=\", release: v0.43.0}\n"
	tb := b.start(t, func(s []byte) []byte {
		s = bytes.Replace(s, []byte("pins: []\n"), []byte(pins), 1)
		return bytes.Replace(s, []byte("serves:"), []byte("site: {consumers: {profileContext: [t/probe]}}\nserves:"), 1)
	})
	ctx := t.Context()

	r, err := tb.Render(ctx, launch.RenderRequest{Home: "/h", Profile: "p", Roles: []string{"api", "all"}, Server: true,
		Dumps: []string{"--dump", "--dump-hooks", "--dump-config-schema"}})
	if err != nil {
		t.Fatal(err)
	}
	if ids := rowIDs(r.Composition); !slices.Equal(ids, []string{"store", "user", "probe"}) || r.Distribution != digestOf(b.opt.Signed) {
		t.Errorf("rows %v, distribution %s", ids, r.Distribution)
	}
	if len(r.Dumps) != 3 || !strings.Contains(r.Dumps["--dump"], "store") || r.Dumps["--dump-hooks"] == "" || !strings.Contains(r.Dumps["--dump-config-schema"], "{") {
		t.Errorf("dumps %v", r.Dumps)
	}
	store := slices.IndexFunc(r.Keys, func(k launch.KeyProviders) bool { return k.Name == "store" })
	if store < 0 || r.Keys[store].Provider != "open" || r.Keys[store].Consumer != "open" || !slices.Equal(r.Keys[store].Rows, []string{"store"}) {
		t.Errorf("keys %+v", r.Keys)
	}
	if slices.Contains(r.Required.Rows, LoaderRowID) || len(errorTexts(r.Diagnostics)) > 0 || len(r.Admission.Denied) > 0 {
		t.Errorf("required %v, errors %v, denied %v", r.Required, errorTexts(r.Diagnostics), r.Admission.Denied)
	}
	if !slices.Equal(r.Departures, []string{"the site adds consumers of profileContext: t/probe",
		"example.test/dep v0.1.0 was checked against nothing: no source recorded its hash (ADR-044 decision 8)",
		"the site raises golang.org/x/text from v0.43.0, the release's pin, to v0.44.0, which --allow-bump admitted (ADR-044 decision 10)"}) ||
		!slices.ContainsFunc(r.Diagnostics, func(d launch.Diagnostic) bool { return d.Warning && strings.HasPrefix(d.Text, "trust: ") }) {
		t.Errorf("departures %q, diagnostics %+v", r.Departures, r.Diagnostics)
	}

	// No home: the shipped profiles alone. Bundles of their own, patches, and a warning
	// of Compose with its place.
	if r, err := tb.Render(ctx, launch.RenderRequest{Profile: "shipped"}); err != nil || !slices.Equal(rowIDs(r.Composition), []string{"only"}) {
		t.Errorf("shipped: %v, %v", rowIDs(r.Composition), err)
	}
	r, err = tb.Render(ctx, launch.RenderRequest{Home: "/h", Profile: "p", Bundles: "/x", Patches: []string{"/h/patch.yml"}})
	if err != nil || !slices.Equal(rowIDs(r.Composition), []string{"store"}) ||
		!slices.ContainsFunc(r.Diagnostics, func(d launch.Diagnostic) bool { return d.Warning && d.File == "h/patch.yml" && d.Line == 2 }) {
		t.Errorf("bundles and patches: %v, %+v, %v", rowIDs(r.Composition), r.Diagnostics, err)
	}
	// A patch relative to Dir keeps the name the operator wrote; one that leaves Dir is
	// read by its path from the root, as S1's profile render read it.
	for p, file := range map[string]string{"patch.yml": "patch.yml", "./patch.yml": "patch.yml", "../h/patch.yml": "h/patch.yml"} {
		r, err = tb.Render(ctx, launch.RenderRequest{Home: "/h", Profile: "p", Patches: []string{p}, Dir: "/h"})
		if err != nil || !slices.ContainsFunc(r.Diagnostics, func(d launch.Diagnostic) bool { return d.Warning && d.File == file && d.Line == 2 }) {
			t.Errorf("patch %s in /h: %+v, %v; want a warning on %s", p, r.Diagnostics, err, file)
		}
	}

	// A refused profile is a rendered one with an error diagnostic and its denial.
	b.files["ghost.yml"] = &fstest.MapFile{Data: []byte("- insert:\n    - {id: ghost, plugin: t/none, required: true, roles: [api]}\n")}
	r, err = tb.Render(ctx, launch.RenderRequest{Home: "/h", Profile: "p", Patches: []string{"/h/ghost.yml"}})
	if err != nil || len(errorTexts(r.Diagnostics)) == 0 || len(r.Admission.Denied) != 1 || r.Admission.Denied[0].Row != "ghost" {
		t.Errorf("refused: %v, %+v, %v", errorTexts(r.Diagnostics), r.Admission, err)
	}

	// The bundle of an artifact appended as install appends it: its rows mount under the
	// group the artifact names, with their origin.
	r, err = tb.Render(ctx, launch.RenderRequest{Home: "/h", Profile: "p",
		Append: launch.Bundle{ID: "acme/x", Data: []byte("- insert:\n    - {id: x, plugin: acme/x, enabled: false, roles: [api]}\n")}})
	if ids := rowIDs(r.Composition); err != nil || !slices.Equal(ids, []string{"store", "user", "probe", "x", "x:x"}) || r.Composition.Rows[4].Origin != "acme/x" {
		t.Errorf("append: %v, %v", ids, err)
	}
}

func TestToolboxRenderRefuses(t *testing.T) {
	b := newBox(t)
	b.files["reserved.yml"] = &fstest.MapFile{Data: []byte("- insert:\n    - {id: loader, plugin: t/store, roles: [api]}\n")}
	tb := b.start(t, nil)
	other, _ := trustedHome([]byte("another manifest"))
	b.extra["other/verified/current"] = other["verified/current"]
	b.extra["torn/verified/current"] = &fstest.MapFile{Data: []byte("torn")}
	for name, tc := range map[string]struct {
		q    launch.RenderRequest
		want string
	}{
		"relative home":    {launch.RenderRequest{Home: "h", Profile: "p"}, "not an absolute path"},
		"relative bundles": {launch.RenderRequest{Home: "/h", Profile: "p", Bundles: "x"}, "not an absolute path"},
		"relative patch":   {launch.RenderRequest{Home: "/h", Profile: "p", Patches: []string{"patch.yml"}}, "not an absolute path"},
		"relative dir":     {launch.RenderRequest{Home: "/h", Profile: "p", Patches: []string{"patch.yml"}, Dir: "w"}, "not an absolute path"},
		"unknown dump":     {launch.RenderRequest{Home: "/h", Profile: "p", Dumps: []string{"--dump-all"}}, "not a dump"},
		"bad role":         {launch.RenderRequest{Home: "/h", Profile: "p", Roles: []string{"root"}}, `role "root"`},
		"reserved row":     {launch.RenderRequest{Home: "/h", Profile: "p", Patches: []string{"/h/reserved.yml"}}, "reserved"},
		"another current":  {launch.RenderRequest{Home: "/x/other", Profile: "p"}, "ADR-061 decision 26"},
		"torn current":     {launch.RenderRequest{Home: "/x/torn", Profile: "p"}, "verified/current"},
	} {
		if _, err := tb.Render(t.Context(), tc.q); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// A composition that stops on a file is a refused render whose diagnostic names the file
// and the line, so profile render prints it and profile sanitize finds the layer file of
// the home that stops a boot (ADR-061 decision 41).
func TestToolboxRenderNamesTheFileThatStopsComposition(t *testing.T) {
	b := newBox(t)
	tb := b.start(t, nil)
	for name, f := range b.files {
		b.extra["broken/"+name] = f
	}
	b.extra["broken/profiles/p/aiops.patch.yml"] = &fstest.MapFile{Data: []byte("- {id: user,\n  enabled: [\n")}
	for _, c := range []struct {
		q    launch.RenderRequest
		file string
	}{
		{launch.RenderRequest{Home: "/x/broken", Profile: "p"}, "profiles/p/aiops.patch.yml"},
		{launch.RenderRequest{Home: "/h", Profile: "nope"}, "profiles/nope/profile.yaml"},
	} {
		// The file that stopped the composition, set aside, is read as absent, as profile
		// sanitize leaves it.
		if c.q.Home == "/x/broken" {
			aside := c.q
			aside.Aside = []string{c.file}
			if r, err := tb.Render(t.Context(), aside); err != nil || len(errorTexts(r.Diagnostics)) > 0 || len(r.Composition.Rows) == 0 {
				t.Errorf("%s with %s aside: %+v, %v; want the profile composed", c.q.Home, c.file, r, err)
			}
		}
		r, err := tb.Render(t.Context(), c.q)
		i := slices.IndexFunc(r.Diagnostics, func(d launch.Diagnostic) bool { return !d.Warning && d.File == c.file })
		if err != nil || i < 0 || r.Diagnostics[i].Check != "compose" || len(r.Composition.Rows) > 0 || r.Distribution != digestOf(b.opt.Signed) {
			t.Errorf("%s: %+v, %v; want an error diagnostic of compose on %s", c.q.Home, r, err, c.file)
		}
	}
}

// A home that holds the snapshot of a stopped write renders with a warning that names
// it, so profile render shows that its files may hold part of that change (ADR-061
// decision 17).
func TestToolboxRenderWarnsOfAStoppedWrite(t *testing.T) {
	b := newBox(t)
	tb := b.start(t, nil)
	for name, f := range b.files {
		b.extra["stopped/"+name] = f
	}
	b.extra["stopped/snapshot/paths.json"] = &fstest.MapFile{Data: []byte(`{"Replaced":["profiles/p/aiops.patch.yml"]}`)}
	r, err := tb.Render(t.Context(), launch.RenderRequest{Home: "/x/stopped", Profile: "p"})
	if err != nil || !slices.ContainsFunc(r.Diagnostics, func(d launch.Diagnostic) bool { return d.Warning && strings.Contains(d.Text, "snapshot/") }) {
		t.Errorf("a render of a home with a snapshot: %+v, %v; want a warning that names snapshot/", r.Diagnostics, err)
	}
}

// With Against a render reads the module table and the bindings from that signed
// manifest, as `aiopsctl profile render --dist` does, and holds a home to that manifest's
// digest instead of the embedded one (ADR-061 decision 26). Its dump holds the lines that
// boot prints at every boot, as the dump of Render does.
func TestToolboxRenderAgainst(t *testing.T) {
	b := newBox(t)
	tb := b.start(t, nil)
	against := bytes.Replace(b.opt.Signed, []byte("serves:"), []byte("site: {consumers: {profileContext: [t/probe]}}\nserves:"), 1)
	b.extra["dist/distribution.yaml"] = &fstest.MapFile{Data: against}
	b.extra["bad.yaml"] = &fstest.MapFile{Data: []byte("x: [")}
	for name, f := range b.files {
		if !strings.HasPrefix(name, "verified/") && name != "trust.yaml" {
			b.extra["other/"+name] = f
		}
	}
	other, _ := trustedHome(against)
	for name, f := range other {
		b.extra["other/"+name] = f
	}
	ctx := t.Context()
	r, err := tb.Render(ctx, launch.RenderRequest{Profile: "shipped", Against: "/x/dist/distribution.yaml"})
	if err != nil || r.Distribution != digestOf(against) || !slices.Equal(r.Departures, []string{"the site adds consumers of profileContext: t/probe"}) {
		t.Errorf("no home: distribution %s, departures %q, %v", r.Distribution, r.Departures, err)
	}
	r, err = tb.Render(ctx, launch.RenderRequest{Home: "/x/other", Profile: "p", Against: "/x/dist/distribution.yaml", Dumps: []string{"--dump"}})
	if err != nil || len(errorTexts(r.Diagnostics)) > 0 || !slices.Equal(rowIDs(r.Composition), []string{"store", "user", "probe"}) ||
		!strings.Contains(r.Dumps["--dump"], "\n## distribution\ntrust: ") {
		t.Errorf("a home that runs it: rows %v, errors %v, %v", rowIDs(r.Composition), errorTexts(r.Diagnostics), err)
	}
	for name, tc := range map[string]struct {
		q    launch.RenderRequest
		want string
	}{
		"a home that runs another": {launch.RenderRequest{Home: "/h", Profile: "p", Against: "/x/dist/distribution.yaml"}, "ADR-061 decision 26"},
		"relative":                 {launch.RenderRequest{Profile: "p", Against: "dist/distribution.yaml"}, "not an absolute path"},
		"missing":                  {launch.RenderRequest{Profile: "p", Against: "/x/none.yaml"}, "none.yaml"},
		"malformed":                {launch.RenderRequest{Profile: "p", Against: "/x/bad.yaml"}, "/x/bad.yaml"},
	} {
		if _, err := tb.Render(ctx, tc.q); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// Every boot part refuses a call whose context has ended, and every call once the
// launcher is released.
func TestToolboxRefusesAfterRelease(t *testing.T) {
	b := newBox(t)
	b.sign()
	done := make(chan int, 1)
	go func() { done <- Run(t.Context(), b.opt) }()
	b.waitReady(t, done)
	tb := b.toolbox(t)
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := tb.Render(ended, launch.RenderRequest{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Render with an ended context = %v", err)
	}
	stopAndWait(t, b.fixture, done)
	ctx := t.Context()
	_, errRender := tb.Render(ctx, launch.RenderRequest{})
	_, errGate := tb.Gate(ctx, launch.GateRequest{})
	_, _, errRead := tb.ReadManifest(ctx, launch.ManifestRequest{})
	_, errShipped := tb.Shipped(ctx)
	_, errRelease := tb.Release(ctx, "")
	for name, err := range map[string]error{"Render": errRender, "Gate": errGate, "ReadManifest": errRead,
		"Shipped": errShipped, "Release": errRelease} {
		if !errors.Is(err, kernel.ErrInactive) {
			t.Errorf("%s after release = %v, want ErrInactive", name, err)
		}
	}
}

// artifact is the plugin.yaml, bundle and statement of an artifact whose OCI manifest
// has digest.
func artifact(id string, set ...string) (manifest, bundle, statement []byte, digest string) {
	m := manifestText(id, set...)
	digest = digestOf([]byte("the OCI manifest of " + id))
	statement = []byte(strings.Replace(attest(m, ""), strings.TrimPrefix(digestOf([]byte(m)), "sha256:"), strings.TrimPrefix(digest, "sha256:"), 1))
	return []byte(m), nil, statement, digest
}

func clauseOutcomes(v launch.Verdict) map[string]string {
	out := map[string]string{}
	for _, c := range v.Clauses {
		k := fmt.Sprintf("%d %s", c.Check, c.Clause)
		if out[k] == "" || c.Outcome == launch.OutcomeDeny {
			out[k] = c.Outcome
		}
	}
	return out
}

func TestToolboxGate(t *testing.T) {
	b := newBox(t)
	b.files["profiles/q/profile.yaml"] = &fstest.MapFile{Data: []byte("bundles: [b]")}
	// Two homes on which dist use recorded nothing, one that allows the artifact unsigned.
	for _, h := range []string{"clean", "allowed"} {
		b.extra[h+"/profiles/p/profile.yaml"] = &fstest.MapFile{Data: []byte("bundles: [b]")}
		b.extra[h+"/bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(boxBundle)}
	}
	b.extra["allowed/trust.yaml"] = &fstest.MapFile{Data: []byte("allowUnsigned: [acme/echo]\n")}
	tb := b.start(t, nil)
	ctx := t.Context()
	man, _, att, digest := artifact("acme/echo")
	own := []byte("- insert:\n    - {id: echo, plugin: acme/echo, enabled: false, roles: [api]}\n")

	// Install of in-process code: the home's trust.yaml lists no signer for it.
	q := launch.GateRequest{Entry: string(EntryInstall), Manifest: man, Bundle: own, Attestation: att, Digest: digest,
		Signer: Unsigned, Home: "/x/clean", Profile: "p"}
	v, err := tb.Gate(ctx, q)
	if got := clauseOutcomes(v); err != nil || v.Entry != string(EntryInstall) || got["5 signer"] != launch.OutcomeDeny || v.RebuildRequired {
		t.Errorf("unsigned install: %+v, %v", v, err)
	}
	// Listed in allowUnsigned, every evaluated clause passes: it takes a rebuild.
	q.Home = "/x/allowed"
	v, err = tb.Gate(ctx, q)
	if got := clauseOutcomes(v); err != nil || !v.RebuildRequired || got["5 module"] != launch.OutcomeNotEvaluated || got["6 "] != launch.OutcomeNotEvaluated {
		t.Errorf("allowed install: %+v, %v", v, err)
	}
	// After dist use every clause is evaluated: the module of in-process code is not in
	// the recorded distribution.
	q.Home = "/h"
	v, err = tb.Gate(ctx, q)
	if got := clauseOutcomes(v); err != nil || v.Entry != string(EntryInstallUsed) || got["5 module"] != launch.OutcomeDeny || v.RebuildRequired {
		t.Errorf("install after dist use: %+v, %v", v, err)
	}

	// A pack whose row is of a plugin that is not instantiable, gated offline against the
	// published manifest and at plugin check against the embedded one.
	pack, _, patt, pdigest := artifact("acme/pack", "runtimes: []", "uses: -", "provides: -", "registers: -", "hooks: -", "configSchema: -", "durable: -", "requires: {definitions: {}}")
	rows := []byte("- insert:\n    - {id: echo, plugin: t/store, enabled: false, roles: [api]}\n")
	for _, q := range []launch.GateRequest{
		{Entry: string(EntryOffline), Manifest: pack, Bundle: rows, Attestation: patt, Digest: pdigest, Against: b.opt.Signed},
		{Entry: string(EntryCheck), Manifest: pack, Bundle: rows, Attestation: patt, Digest: pdigest, Signer: "throwaway"},
	} {
		v, err := tb.Gate(ctx, q)
		if got := clauseOutcomes(v); err != nil || got["8 "+string(RuleInstalled)] != launch.OutcomeDeny || v.RebuildRequired {
			t.Errorf("%s: %+v, %v", q.Entry, v, err)
		}
	}
	// A bundle that does not read is a denial, not an error.
	v, err = tb.Gate(ctx, launch.GateRequest{Entry: string(EntryCheck), Manifest: pack, Bundle: []byte("- insert: ["), Attestation: patt, Digest: pdigest})
	if err != nil || !slices.ContainsFunc(v.Clauses, func(c launch.Clause) bool { return c.Outcome == launch.OutcomeDeny }) {
		t.Errorf("an unread bundle: %+v, %v", v, err)
	}

	// A row of the profile that stops it is a denial of every install into it.
	b.files["profiles/q/aiops.patch.yml"] = &fstest.MapFile{Data: []byte("- insert:\n    - {id: ghost, plugin: t/none, required: true, roles: [api]}\n")}
	q.Profile = "q"
	if v, err := tb.Gate(ctx, q); err != nil || clauseOutcomes(v)["0 "+string(RuleModuleMissing)] != launch.OutcomeDeny || v.RebuildRequired {
		t.Errorf("a profile that stops: %+v, %v", v, err)
	}

	for name, tc := range map[string]struct {
		q    launch.GateRequest
		want string
	}{
		"entry":         {launch.GateRequest{Entry: string(EntryBoot), Manifest: man}, "not an entry point"},
		"manifest":      {launch.GateRequest{Entry: string(EntryCheck), Manifest: []byte("id: [")}, "the manifest of the artifact"},
		"no home":       {launch.GateRequest{Entry: string(EntryInstall), Manifest: man, Bundle: own}, "names the home"},
		"no profile":    {launch.GateRequest{Entry: string(EntryInstall), Manifest: man, Bundle: own, Home: "/h", Profile: "nope"}, "profile.yaml"},
		"against":       {launch.GateRequest{Entry: string(EntryOffline), Manifest: man, Bundle: own, Against: []byte("distVersion: 2")}, "--against"},
		"relative home": {launch.GateRequest{Entry: string(EntryInstall), Manifest: man, Bundle: own, Home: "h", Profile: "p"}, "absolute"},
	} {
		if _, err := tb.Gate(ctx, tc.q); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
}

// After dist use the install of in-process code judges the module that module.json of
// the artifact records, as boot judges the record that install writes from it: check 5
// holds it to the pins of the trusted manifest and check 10 to the module the binary
// runs, so an artifact of another module is denied at install, not at the next boot
// (ADR-047 decision 17; ADR-020 decision 12).
func TestToolboxGateJudgesTheModuleAtInstall(t *testing.T) {
	b := newBox(t)
	b.files["profiles/r/profile.yaml"] = &fstest.MapFile{Data: []byte("bundles: []")}
	tb := b.start(t, nil)
	pub, id := testKey()
	trust := b.files["trust.yaml"].Data
	b.files["trust.yaml"] = &fstest.MapFile{Data: append([]byte("signers:\n  - key: |\n"+indent(pub, 6)+"\n    ids: [t/*]\n    runtimes: [inproc]\n"), trust...)}
	man, _, att, digest := artifact("t/store")
	q := launch.GateRequest{Entry: string(EntryInstall), Manifest: man, Bundle: []byte("- insert:\n    - {id: own, plugin: t/store, enabled: false, roles: [api]}\n"),
		Attestation: att, Digest: digest, Signer: id, Home: "/h", Profile: "r",
		Module: &launch.Module{Path: "example.test/other", Version: "v0.2.0", Sum: "h1:" + strings.Repeat("C", 43) + "="}}
	v, err := tb.Gate(t.Context(), q)
	if got := clauseOutcomes(v); err != nil || v.Entry != string(EntryInstallUsed) || got["5 module"] != launch.OutcomeDeny || got["10 attestation"] != launch.OutcomeDeny {
		t.Errorf("install of an artifact of another module after dist use: %+v, %v; want checks 5 and 10 denying its module", v, err)
	}
	q.Module = &launch.Module{Path: testModule}
	v, err = tb.Gate(t.Context(), q)
	if got := clauseOutcomes(v); err != nil || got["5 module"] == launch.OutcomeDeny || got["10 attestation"] == launch.OutcomeDeny {
		t.Errorf("install of an artifact of the module the binary runs: %+v, %v; want neither check 5 nor 10 denying its module", v, err)
	}
}

func TestToolboxReadManifest(t *testing.T) {
	b := newBox(t)
	b.extra["plug/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/echo", "registers: [{key: echoers, major: 0}]",
		"provides: [{key: store, major: 0}]", "requires: {pluginApi: \"0.1\", definitions: {echoers: [\"0.1\"]}}"))}
	b.extra["bad/plugin.yaml"] = &fstest.MapFile{Data: []byte("manifestVersion: \"1.0\"\nid: [\n")}
	tb := b.start(t, nil)
	ctx := t.Context()
	s, ds, err := tb.ReadManifest(ctx, launch.ManifestRequest{Dir: "/x/plug"})
	want := launch.ManifestSummary{ID: "acme/echo", Version: "0.1.0", Runtimes: []string{"inproc"}, Provides: []string{"store"},
		Registers: []string{"echoers"}, Definitions: map[string][]string{"echoers": {"0.1"}}}
	if err != nil || len(ds) != 0 || s.ID != want.ID || s.Version != want.Version || !slices.Equal(s.Runtimes, want.Runtimes) ||
		!slices.Equal(s.Provides, want.Provides) || !slices.Equal(s.Registers, want.Registers) || !slices.Equal(s.Definitions["echoers"], want.Definitions["echoers"]) {
		t.Errorf("ReadManifest = %+v, %+v, %v", s, ds, err)
	}
	if _, ds, err := tb.ReadManifest(ctx, launch.ManifestRequest{Dir: "/x/bad"}); err != nil || len(ds) != 1 || ds[0].Check != "1" || ds[0].File != "/x/bad/plugin.yaml" || ds[0].Line == 0 {
		t.Errorf("a malformed manifest = %+v, %v", ds, err)
	}
	for _, dir := range []string{"/x/none", "x/plug"} {
		if _, _, err := tb.ReadManifest(ctx, launch.ManifestRequest{Dir: dir}); err == nil {
			t.Errorf("ReadManifest(%s) = nil error", dir)
		}
	}
}

// The lints of step 1 of plugin check on the data that aiopsctl hands in (ADR-061
// decision 21; ADR-048 decisions 15, 19 and 23; 07-c4-plugin-sdk.md:1567): the reserved
// owners by the module path of the go.mod at or above the plugin, each id of license and
// integrates[].license against the SPDX list, the marks in the id, the title, a content
// path and a command path, and the forms of T7 for a third party that integrates.
func TestToolboxReadManifestLints(t *testing.T) {
	b := newBox(t)
	b.opt.Lint = ManifestLint{Licenses: map[string]bool{"Apache-2.0": true, "MIT": true, "GPL-2.0-only": true, "Classpath-exception-2.0": true},
		Marks: []string{"grafana"}, Tiers: []string{"official", "certified"}}
	mod := func(dir, module string) {
		b.extra[dir+"/go.mod"] = &fstest.MapFile{Data: []byte("module " + module + "\n\ngo 1.26.0\n")}
	}
	mod("m", "example.com/acme")
	mod("repo", "github.com/mutex-solutions/aiops-platform")
	mod("repo/contrib/zed", "github.com/mutex-solutions/aiops-platform/contrib/zed")
	b.extra["m/ok/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/ok", "license: (Apache-2.0 OR MIT+) AND GPL-2.0-only WITH Classpath-exception-2.0 OR LicenseRef-x"))}
	b.extra["m/lic/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/lic-zeta", "license: Apache-2.0 OR GPL-9.9",
		"integrates: [{system: zeta, versions: any, tested: [\"1\"], credential: read-only, license: MIT WITH Nope-exception}]",
		"title: Lic connector for Zeta"))}
	b.extra["m/marked/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/grafana-echo", "title: Echo for Grafana"))}
	b.extra["m/marked/content/dash/Grafana_board.json"] = &fstest.MapFile{Data: []byte("{}")}
	b.extra["m/marked/fixtures/commands/paths.json"] = &fstest.MapFile{Data: []byte(`[{"path": "sync grafana"}]`)}
	b.extra["m/badpaths/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/badpaths"))}
	b.extra["m/badpaths/fixtures/commands/paths.json"] = &fstest.MapFile{Data: []byte(`{`)}
	b.extra["m/first/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("aiops/first"))}
	b.extra["m/contrib/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("contrib/x"))}
	b.extra["repo/plugins/first/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("aiops/first"))}
	b.extra["repo/contrib/zed/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("contrib/zed"))}
	b.extra["repo/contrib/pack/plugin.yaml"] = &fstest.MapFile{Data: []byte(packManifestOf("contrib/pack"))}
	b.extra["repo/plugins/pack/plugin.yaml"] = &fstest.MapFile{Data: []byte(packManifestOf("contrib/pack"))}
	b.extra["nomod/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("aiops/nomod"))}
	b.extra["m/t7ok/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/connector-zeta", "title: Acme connector for Zeta",
		"integrates: [{system: zeta, versions: any, tested: [\"1\"], credential: read-only}]"))}
	b.extra["m/t7bad/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/official-zeta-tools", "title: Certified tools",
		"integrates: [{system: zeta, versions: any, tested: [\"1\"], credential: read-only}]"))}
	b.extra["m/t7twice/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/zeta-connector-zeta", "title: Connector for Zeta",
		"integrates: [{system: zeta, versions: any, tested: [\"1\"], credential: read-only}]"))}
	tb := b.start(t, nil)
	lint := func(dir string) []string {
		t.Helper()
		_, ds, err := tb.ReadManifest(t.Context(), launch.ManifestRequest{Dir: "/x/" + dir})
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		var out []string
		for _, d := range ds {
			if d.Check != "1" || d.File == "" {
				t.Errorf("%s: a diagnostic of no step 1 or no file: %+v", dir, d)
			}
			out = append(out, d.Text)
		}
		return out
	}
	for dir, want := range map[string][]string{
		"m/ok":               {},
		"m/lic":              {"GPL-9.9 is not an id of the SPDX license list", "Nope-exception is not an id of the SPDX license list"},
		"m/marked":           {`the id "acme/grafana-echo" holds the mark grafana`, `the title "Echo for Grafana" holds the mark grafana`, `the content path "dash/Grafana_board.json" holds the mark grafana`, `the command path "sync grafana" holds the mark grafana`},
		"m/badpaths":         {"fixtures/commands/paths.json"},
		"m/first":            {"the owner aiops is reserved to the release pipeline"},
		"m/contrib":          {"the owner contrib is reserved to the release pipeline"},
		"repo/plugins/first": {},
		"repo/contrib/zed":   {},
		"repo/contrib/pack":  {},
		"repo/plugins/pack":  {"the owner contrib is reserved to the release pipeline"},
		"nomod":              {"no go.mod"},
		"m/t7ok":             {},
		"m/t7bad":            {`the name "official-zeta-tools" holds "official"`, `the title "Certified tools" holds "certified"`, `the name "official-zeta-tools" of a plugin that integrates zeta is not <contribution noun>-zeta or connector-zeta`, `the title "Certified tools" does not read "... for zeta"`},
		"m/t7twice":          {`the name "zeta-connector-zeta" of a plugin that integrates zeta is not <contribution noun>-zeta or connector-zeta`},
	} {
		got := lint(dir)
		if len(got) != len(want) {
			t.Errorf("%s: %d diagnostics, want %d:\n%s", dir, len(got), len(want), strings.Join(got, "\n"))
			continue
		}
		for _, w := range want {
			if !slices.ContainsFunc(got, func(g string) bool { return strings.Contains(g, w) }) {
				t.Errorf("%s: no diagnostic %q in:\n%s", dir, w, strings.Join(got, "\n"))
			}
		}
	}
	// With no lint data, as in aiops, only the reader and the reserved owners run.
	b2 := newBox(t)
	b2.extra["m/go.mod"] = &fstest.MapFile{Data: []byte("module example.com/acme\n")}
	b2.extra["m/lic/plugin.yaml"] = &fstest.MapFile{Data: []byte(manifestText("acme/grafana", "license: GPL-9.9"))}
	if _, ds, err := b2.start(t, nil).ReadManifest(t.Context(), launch.ManifestRequest{Dir: "/x/m/lic"}); err != nil || len(ds) != 0 {
		t.Errorf("no lint data: %+v, %v", ds, err)
	}
}

// packManifestOf is a pack with no code under id.
func packManifestOf(id string) string {
	owner, _, _ := strings.Cut(id, "/")
	return "manifestVersion: \"1.0\"\nid: " + id + "\nversion: 0.1.0\ntitle: Pack\nlicense: Apache-2.0\npublisher: {id: " + owner +
		"}\nstability: experimental\nscope: [process]\nruntimes: []\nrequires:\n  definitions: {echoers: [\"0.1\"]}\n"
}

func TestToolboxShipped(t *testing.T) {
	b := newBox(t)
	tb := b.start(t, nil)
	got, err := tb.Shipped(t.Context())
	if err != nil || len(got) != 2 || string(got["profiles/shipped/profile.yaml"]) != "bundles: [s]" {
		t.Errorf("Shipped = %q, %v", got, err)
	}
	b = newBox(t)
	b.opt.Shipped = nil
	if got, err := b.start(t, nil).Shipped(t.Context()); err != nil || len(got) != 0 {
		t.Errorf("Shipped of a binary that ships nothing = %q, %v", got, err)
	}
}

func TestToolboxRelease(t *testing.T) {
	b := newBox(t)
	d := newTestDist().
		add(manifestText("aiops/echo", "instantiable: true", "registers: [{key: echoers, major: 0}]", "configSchema: {$schema: \"https://json-schema.org/draft/2020-12/schema\", examples: [{greeting: hi}]}",
			"uses: [{key: clock, major: 0, as: need}]", "requires: {pluginApi: \"0.1\", definitions: {clock: [\"0.1\"], echoers: [\"0.1\"]}}"), "").
		add(manifestText("aiops/bare", "instantiable: true", "registers: [{key: echoers, major: 0}]"), "").
		add(manifestText("aiops/plain"), "")
	b.extra["dist.yaml"] = &fstest.MapFile{Data: d.text()}
	b.extra["bad.yaml"] = &fstest.MapFile{Data: []byte("distVersion: 2\n")}
	tb := b.start(t, nil)
	ctx := t.Context()
	r, err := tb.Release(ctx, "")
	if err != nil || r.Digest != digestOf(b.opt.Signed) || !bytes.Equal(r.Manifest, b.opt.Signed) || len(r.Plugins) != 0 {
		t.Errorf("the embedded release = %s, %d plugins, %v", r.Digest, len(r.Plugins), err)
	}
	r, err = tb.Release(ctx, "/x/dist.yaml")
	if err != nil || len(r.Plugins) != 2 || r.Plugins[0].ID != "aiops/bare" || r.Plugins[0].Example != nil ||
		r.Plugins[1].ID != "aiops/echo" || fmt.Sprint(r.Plugins[1].Example) != "map[greeting:hi]" ||
		fmt.Sprint(r.Plugins[1].Feeds) != "map[echoers:[0.1]]" || len(r.Plugins[0].Feeds) != 0 {
		t.Errorf("the release of --against = %+v, %v", r.Plugins, err)
	}
	for _, against := range []string{"x/dist.yaml", "/x/none.yaml", "/x/bad.yaml"} {
		if _, err := tb.Release(ctx, against); err == nil {
			t.Errorf("Release(%s) = nil error", against)
		}
	}
	// A release binary hands dist use the release public key it carries, its default
	// distribution root, with the manifest it embeds and with no other (ADR-020
	// decision 13).
	b = newBox(t)
	b.opt.Release = &Release{KeyID: "K", Key: []byte("the release key"), Bundle: []byte("its bundle")}
	tb = b.start(t, nil)
	if r, err := tb.Release(ctx, ""); err != nil || string(r.Key) != "the release key" || len(r.Manifest) == 0 {
		t.Errorf("the release of a release binary = key %q, %v", r.Key, err)
	}
	b.extra["dist.yaml"] = &fstest.MapFile{Data: d.text()}
	if r, err := tb.Release(ctx, "/x/dist.yaml"); err != nil || r.Key != nil {
		t.Errorf("the release of --against = key %q, %v", r.Key, err)
	}
}

// fakeTools are the Tools that a test hands in, as aiopsctl hands in its own.
type fakeTools struct{ launch.NoTools }

func (fakeTools) Build(context.Context, launch.BuildRequest) (launch.Built, error) {
	return launch.Built{Image: "built"}, nil
}

func TestToolboxHandsOnTheTools(t *testing.T) {
	b := newBox(t)
	b.opt.Tools = fakeTools{}
	tb := b.start(t, nil)
	if got, err := tb.Build(t.Context(), launch.BuildRequest{}); err != nil || got.Image != "built" {
		t.Errorf("Build = %+v, %v", got, err)
	}
	if _, err := tb.Vet(t.Context(), "/x"); !errors.Is(err, launch.ErrNotCarried) {
		t.Errorf("Vet = %v", err)
	}
}
