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
	tb := b.start(t, func(s []byte) []byte {
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
	if !slices.Equal(r.Departures, []string{"the site adds consumers of profileContext: t/probe"}) ||
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
		"unknown dump":     {launch.RenderRequest{Home: "/h", Profile: "p", Dumps: []string{"--dump-all"}}, "not a dump"},
		"bad role":         {launch.RenderRequest{Home: "/h", Profile: "p", Roles: []string{"root"}}, `role "root"`},
		"no profile":       {launch.RenderRequest{Home: "/h", Profile: "nope"}, "profile.yaml"},
		"reserved row":     {launch.RenderRequest{Home: "/h", Profile: "p", Patches: []string{"/h/reserved.yml"}}, "reserved"},
		"another current":  {launch.RenderRequest{Home: "/x/other", Profile: "p"}, "ADR-061 decision 26"},
		"torn current":     {launch.RenderRequest{Home: "/x/torn", Profile: "p"}, "verified/current"},
	} {
		if _, err := tb.Render(t.Context(), tc.q); err == nil || !strings.Contains(err.Error(), tc.want) {
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
	tb := b.start(t, nil)
	ctx := t.Context()
	man, _, att, digest := artifact("acme/echo")
	own := []byte("- insert:\n    - {id: echo, plugin: acme/echo, enabled: false, roles: [api]}\n")

	// Install of in-process code: the home's trust.yaml lists no signer for it.
	q := launch.GateRequest{Entry: string(EntryInstall), Manifest: man, Bundle: own, Attestation: att, Digest: digest,
		Signer: Unsigned, Home: "/h", Profile: "p"}
	v, err := tb.Gate(ctx, q)
	if got := clauseOutcomes(v); err != nil || v.Entry != string(EntryInstall) || got["5 signer"] != launch.OutcomeDeny || v.RebuildRequired {
		t.Errorf("unsigned install: %+v, %v", v, err)
	}
	// Listed in allowUnsigned, every evaluated clause passes: it takes a rebuild.
	b.files["trust.yaml"].Data = append(slices.Clone(b.files["trust.yaml"].Data), []byte("allowUnsigned: [acme/echo]\n")...)
	v, err = tb.Gate(ctx, q)
	if got := clauseOutcomes(v); err != nil || !v.RebuildRequired || got["5 module"] != launch.OutcomeNotEvaluated || got["6 "] != launch.OutcomeNotEvaluated {
		t.Errorf("allowed install: %+v, %v", v, err)
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
		add(manifestText("aiops/echo", "instantiable: true", "registers: [{key: echoers, major: 0}]", "configSchema: {$schema: \"https://json-schema.org/draft/2020-12/schema\", examples: [{greeting: hi}]}"), "").
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
		r.Plugins[1].ID != "aiops/echo" || fmt.Sprint(r.Plugins[1].Example) != "map[greeting:hi]" {
		t.Errorf("the release of --against = %+v, %v", r.Plugins, err)
	}
	for _, against := range []string{"x/dist.yaml", "/x/none.yaml", "/x/bad.yaml"} {
		if _, err := tb.Release(ctx, against); err == nil {
			t.Errorf("Release(%s) = nil error", against)
		}
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
