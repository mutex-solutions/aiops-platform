// SPDX-License-Identifier: Apache-2.0

package boot

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"testing/fstest"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// The toolbox of launch 0.2, what profileContext.Toolbox returns (ADR-061 decisions 21
// and 33): boot's parts, compose, admission, the gate, the readers and the dumps, on
// what the process runs, and the Tools that aiopsctl hands in, which boot hands on
// without importing what implements them. Every path of a request is absolute and is
// read from RunOptions.Files.
type toolbox struct {
	launch.Tools
	p       *profileContext
	files   fs.FS
	shipped fs.FS
	signed  []byte
	lint    ManifestLint
}

var _ launch.Toolbox = (*toolbox)(nil)

// noFiles is a file system that holds nothing: the Files or the Shipped of a launcher
// that was given none.
type noFiles struct{}

func (noFiles) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func orNone(f fs.FS) fs.FS {
	if f == nil {
		return noFiles{}
	}
	return f
}

// overlay serves the files of first, and those of second where first has none: a file
// of the home wins over a shipped one.
type overlay struct{ first, second fs.FS }

func (o overlay) Open(name string) (fs.File, error) {
	f, err := o.first.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return o.second.Open(name)
	}
	return f, err
}

// aside serves the files of a home but those set aside, which read as absent.
type aside struct {
	fs.FS
	names []string
}

func (a aside) Open(name string) (fs.File, error) {
	if slices.Contains(a.names, name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return a.FS.Open(name)
}

func (t *toolbox) ready(ctx context.Context) error { return cmp.Or(ctx.Err(), t.p.released()) }

// rel returns the absolute path p as a path of Files.
func rel(p string) (string, error) {
	if !path.IsAbs(p) {
		return "", fmt.Errorf("%q is not an absolute path: a command resolves every path of a request (ADR-061 decision 16)", p)
	}
	return cmp.Or(strings.TrimPrefix(path.Clean(p), "/"), "."), nil
}

func (t *toolbox) dir(p string) (fs.FS, error) {
	r, err := rel(p)
	if err != nil {
		return nil, err
	}
	return fs.Sub(t.files, r)
}

// sources are the layers that a request composes. With no home they are the shipped
// files alone. Given a home, a home whose current distribution is not digest, the one
// this binary embeds or the one rendered against, is refused: that module table is not
// the one the server of that home runs, so what it composes is not what that server
// would (ADR-061 decision 26). Append is read from the home, as the bundle of an
// installed artifact is.
func (t *toolbox) sources(q launch.RenderRequest, digest string) (Sources, error) {
	src := Sources{Profile: q.Profile, Home: t.shipped}
	if q.Home != "" {
		h, err := t.dir(q.Home)
		if err != nil {
			return Sources{}, err
		}
		switch cur, err := ReadCurrent(h); {
		case err != nil:
			return Sources{}, err
		case cur != "" && cur != digest:
			return Sources{}, fmt.Errorf("the home runs the distribution %s, which dist use made current, and this render admits with %s: run the aiopsctl that dist build wrote for %s (ADR-061 decision 26)", cur, digest, cur)
		}
		if len(q.Aside) > 0 {
			h = aside{h, q.Aside}
		}
		src.Home = overlay{h, t.shipped}
	}
	src.Bundles = src.Home
	if q.Bundles != "" {
		b, err := t.dir(q.Bundles)
		if err != nil {
			return Sources{}, err
		}
		src.Bundles = overlay{b, t.shipped}
	}
	for _, p := range q.Patches {
		f, err := t.patch(p, q.Dir)
		if err != nil {
			return Sources{}, err
		}
		src.Patches = append(src.Patches, f)
	}
	if q.Append.ID != "" {
		src.Home = overlay{fstest.MapFS{path.Join("bundles", q.Append.ID, "bundle.yaml"): {Data: q.Append.Data}}, src.Home}
		src.Append = []string{q.Append.ID}
	}
	return src, nil
}

// patch returns the patch p of a request. A patch relative to dir, with no "..", is read
// from dir and keeps the name the operator wrote in provenance, as S1's profile render
// kept it, so a dump holds no absolute path the operator did not write; any other is
// read by its path from the root.
func (t *toolbox) patch(p, dir string) (File, error) {
	if c := path.Clean(p); !path.IsAbs(p) && dir != "" {
		if fs.ValidPath(c) {
			d, err := t.dir(dir)
			return File{FS: d, Path: c}, err
		}
		p = path.Join(dir, p)
	}
	r, err := rel(p)
	return File{FS: t.files, Path: r}, err
}

// dumpOf returns the dump of profile render for its flag, nil for any other flag.
func dumpOf(flag string) func(Admission) string {
	switch flag {
	case "--dump":
		return RenderDump
	case "--dump-hooks":
		return RenderHooks
	case "--dump-config-schema":
		return RenderConfigSchema
	}
	return nil
}

// Render composes and admits a profile as boot does, at EntryRender, with this binary's
// module table and the signed manifest it embeds, or with Against's, whose module table
// is read from its manifests, as Render reads it (08-profiles-roadmap-adrs.md:359,
// :366). A composition that stops on a file is a diagnostic of compose with the file and
// the line, not an error.
func (t *toolbox) Render(ctx context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	if err := t.ready(ctx); err != nil {
		return launch.Rendered{}, err
	}
	var roles []Role
	for _, s := range q.Roles {
		_, rs, err := parseRunRole(s)
		if err != nil {
			return launch.Rendered{}, err
		}
		roles = append(roles, rs...)
	}
	for _, d := range q.Dumps {
		if dumpOf(d) == nil {
			return launch.Rendered{}, fmt.Errorf("%q is not a dump of profile render", d)
		}
	}
	d, mods, release, tie := t.p.signed, t.p.mods, t.p.release, t.p.tie
	if q.Against != "" {
		var err error
		if d, _, err = t.against(q.Against); err != nil {
			return launch.Rendered{}, err
		}
		mods, release, tie = d.Table(), nil, nil
	}
	src, err := t.sources(q, d.Digest)
	if err != nil {
		return launch.Rendered{}, err
	}
	c, warns, err := Compose(src, Options{Server: q.Server, Modules: mods, Locks: lockDigests(src.Home, q.Profile)})
	var stop *Error
	if errors.As(err, &stop) {
		return launch.Rendered{Distribution: d.Digest,
			Diagnostics: []launch.Diagnostic{{Check: "compose", File: stop.File, Line: stop.Line, Text: err.Error()}}}, nil
	}
	if err == nil {
		err = CheckReserved(c)
	}
	if err != nil {
		return launch.Rendered{}, err
	}
	w := WithLoader(c)
	in, prints := bootInput(src.Home, q.Profile, w, mods, d, release, tie)
	in.Entry = EntryRender
	a := AdmitWith(w, in, roles)
	a.Prints = prints // the dump holds what boot prints at every boot, as Render's does
	out := launch.Rendered{Composition: launchComposition(c), Admission: launchAdmission(a), Dumps: map[string]string{},
		Required: launch.RequiredSet{Rows: slices.DeleteFunc(slices.Clone(a.Required.Rows), func(id string) bool { return id == LoaderRowID }),
			Keys: a.Required.Keys},
		Distribution: d.Digest}
	for _, d := range q.Dumps {
		out.Dumps[d] = dumpOf(d)(a)
	}
	for _, w := range warns {
		out.Diagnostics = append(out.Diagnostics, launch.Diagnostic{File: w.File, Line: w.Line, Text: w.String(), Warning: true})
	}
	// The lines that boot prints at every boot and the findings: a departure from the
	// release bindings is a line of its own (08-profiles-roadmap-adrs.md:366), and so is
	// one from the release pins (ADR-044 decision 10).
	for _, l := range append(prints, a.Diagnostics()...) {
		if d, ok := strings.CutPrefix(l, "bindings: "); ok {
			out.Departures = append(out.Departures, d)
			continue
		}
		if d, ok := strings.CutPrefix(l, "pins: "); ok {
			out.Departures = append(out.Departures, d)
			continue
		}
		text, failed := strings.CutPrefix(l, "error: ")
		out.Diagnostics = append(out.Diagnostics, launch.Diagnostic{Text: strings.TrimPrefix(text, "warning: "), Warning: !failed})
	}
	pb, cb, _ := classesOf(a.Modules, a.Dist, in.Dist)
	rows := map[string][]string{}
	for _, p := range a.Graph.Providers {
		rows[p.Key] = union(rows[p.Key], p.Rows)
	}
	class := map[bool]string{false: "open", true: "bound"}
	for _, k := range slices.Sorted(maps.Keys(rows)) {
		out.Keys = append(out.Keys, launch.KeyProviders{Name: k, Provider: class[pb[k]], Consumer: class[cb[k]], Rows: rows[k]})
	}
	return out, nil
}

// launchAdmission lists every denied row of a. A disabled row mounts no code, so only
// check 8, which runs on every row, denies it: disabling a row stays possible.
func launchAdmission(a Admission) launch.Admission {
	var out launch.Admission
	for _, id := range a.Gate.Rows {
		d, ok := a.Gate.Denial(id)
		if r, found := findRow(a.Comp, id); found && !r.Enabled {
			d = a.Gate.Result(CheckLayerRules, id)
			ok = d.Status == StatusFail
		}
		if ok {
			out.Denied = append(out.Denied, launch.Denial{Row: id, Check: checkName(d.Check), Reason: d.Reason})
		}
	}
	return out
}

// Gate evaluates the clauses of one entry point on an artifact (ADR-060 decision 17):
// at install, on the target profile of a home with the artifact's bundle appended; at
// plugin check and plugin gate --against, on the artifact's bundle alone, against the
// signed manifest this binary embeds or the published one of the request. The verdict
// holds the clauses that judge the artifact's rows and every clause that would stop the
// profile.
func (t *toolbox) Gate(ctx context.Context, q launch.GateRequest) (launch.Verdict, error) {
	if err := t.ready(ctx); err != nil {
		return launch.Verdict{}, err
	}
	entry := Entry(q.Entry)
	if !slices.Contains([]Entry{EntryCheck, EntryOffline, EntryInstall}, entry) {
		return launch.Verdict{}, fmt.Errorf("%q is not an entry point that gates an artifact", q.Entry)
	}
	man, err := ReadManifest("the manifest of the artifact", q.Manifest, ManifestLenient)
	if err != nil {
		return launch.Verdict{}, err
	}
	art := Artifact{ID: man.ID, Digest: q.Digest, Kept: Kept{Manifest: q.Manifest, Bundle: q.Bundle},
		Record: Record{Signer: q.Signer, ID: man.ID, Version: man.Version, Runtimes: man.Runtimes, Attestation: q.Attestation}}
	var in GateInput
	var c Composition
	if entry == EntryInstall {
		if q.Home == "" {
			return launch.Verdict{}, errors.New("install names the home it writes")
		}
		src, err := t.sources(launch.RenderRequest{Home: q.Home, Profile: q.Profile, Append: launch.Bundle{ID: man.ID, Data: q.Bundle}}, t.p.signed.Digest)
		if err == nil {
			c, _, err = Compose(src, Options{Modules: t.p.mods, Locks: lockDigests(src.Home, q.Profile)})
		}
		if err != nil {
			return launch.Verdict{}, err
		}
		in, _ = bootInput(src.Home, q.Profile, WithLoader(c), t.p.mods, t.p.signed, t.p.release, t.p.tie)
		// After dist use the module table is the recorded distribution's, which sources
		// holds equal to this binary's: every clause is evaluated.
		if cur, _ := ReadCurrent(src.Home); cur != "" {
			entry = EntryInstallUsed
		}
	} else {
		d, mods := t.p.signed, t.p.mods
		if q.Against != nil {
			if d, err = ReadDistribution("the signed manifest of --against", q.Against); err != nil {
				return launch.Verdict{}, err
			}
			mods = d.Table()
		}
		// The artifact's bundle alone, in a profile of its own. A bundle that does not read
		// is no error: Compose leaves the artifact out, and the gate denies it
		// (Composition.Unread).
		mem := fstest.MapFS{"profiles/gate/profile.yaml": {Data: []byte("bundles: []")},
			path.Join("bundles", man.ID, "bundle.yaml"): {Data: q.Bundle}}
		c, _, _ = Compose(Sources{Profile: "gate", Home: mem, Bundles: mem, Append: []string{man.ID}}, Options{Modules: mods})
		in = GateInput{Mods: mods, Bindings: d.Bindings(), Dist: &d, Artifacts: map[string]Artifact{}}
	}
	in.Entry, in.Artifacts[man.ID] = entry, art
	w := WithLoader(c)
	mine := map[string]bool{man.ID: true}
	for _, r := range w.Rows {
		mine[r.ID] = mine[r.ID] || installedBundle(r) == man.ID
	}
	return verdictOf(GateWith(w, in), mine, entry == EntryInstall && slices.Contains(man.Runtimes, "inproc")), nil
}

// verdictOf reads a gate result as the clauses of a verdict: each failure of a row of
// mine, or that stops the composition, is a denial; a check with none passes; what the
// entry point does not evaluate is listed with the reason. rebuild is set where nothing
// is denied: install of in-process code then answers rebuild-required.
func verdictOf(g GateResult, mine map[string]bool, rebuild bool) launch.Verdict {
	v := launch.Verdict{Entry: string(g.Entry)}
	add := func(n int, clause, outcome, reason string) {
		v.Clauses = append(v.Clauses, launch.Clause{Check: n, Clause: clause, Outcome: outcome, Reason: reason})
	}
	denied := false
	for _, f := range g.Unresolved {
		if mine[f.Row] || f.Stop {
			add(checkResolve, string(f.Rule), launch.OutcomeDeny, f.Reason)
			denied = true
		}
	}
	for _, s := range g.Slots {
		if !s.Evaluated {
			add(s.N, "", launch.OutcomeNotEvaluated, cmp.Or(s.Reason, reasonNotEvaluated+s.Until))
			continue
		}
		pass := true
		for _, f := range s.Fails {
			if mine[f.Row] || f.Stop {
				add(s.N, string(f.Rule), launch.OutcomeDeny, f.Reason)
				pass = false
			}
		}
		for _, c := range s.Clauses {
			name, why, _ := strings.Cut(c, ": ")
			add(s.N, name, launch.OutcomeNotEvaluated, why)
		}
		for _, f := range s.Open {
			if mine[f.Row] {
				add(s.N, string(f.Rule), launch.OutcomeNotEvaluated, f.Reason)
			}
		}
		if pass {
			add(s.N, "", launch.OutcomePass, "")
		}
		denied = denied || !pass
	}
	v.RebuildRequired = rebuild && !denied
	return v
}

// ReadManifest is boot's reader in strict mode on dir/plugin.yaml, then the lints of
// step 1 on the data that aiopsctl hands in (ADR-061 decision 21). A manifest the reader
// refuses, and each finding of a lint, is a diagnostic of step 1 of plugin check, not an
// error.
func (t *toolbox) ReadManifest(ctx context.Context, q launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	if err := t.ready(ctx); err != nil {
		return launch.ManifestSummary{}, nil, err
	}
	dir, err := rel(q.Dir)
	if err != nil {
		return launch.ManifestSummary{}, nil, err
	}
	file := path.Join(dir, "plugin.yaml")
	data, err := readFile(File{FS: t.files, Path: file})
	if err != nil {
		return launch.ManifestSummary{}, nil, err
	}
	m, err := ReadManifest("/"+file, data, ManifestStrict)
	if err != nil {
		d := launch.Diagnostic{Check: "1", Text: err.Error()}
		var e *Error
		if errors.As(err, &e) {
			d.File, d.Line = e.File, e.Line
		}
		return launch.ManifestSummary{}, []launch.Diagnostic{d}, nil
	}
	var ds []launch.Diagnostic
	for _, text := range t.lint.lint(t.files, dir, m) {
		ds = append(ds, launch.Diagnostic{Check: "1", File: "/" + file, Text: text})
	}
	return m.Summary(), ds, nil
}

// Summary returns the six fields of m that the toolbox's ReadManifest hands out.
func (m Manifest) Summary() launch.ManifestSummary {
	keys := func(ks []ManifestKey) []string {
		var out []string
		for _, k := range ks {
			out = append(out, k.Key)
		}
		return out
	}
	return launch.ManifestSummary{ID: m.ID, Version: m.Version, Runtimes: m.Runtimes, Provides: keys(m.Provides),
		Registers: keys(m.Registers), Definitions: m.Requires.Definitions}
}

// Shipped returns the files that the binary embeds, by path.
func (t *toolbox) Shipped(ctx context.Context) (map[string][]byte, error) {
	if err := t.ready(ctx); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	err := fs.WalkDir(t.shipped, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		out[p], err = fs.ReadFile(t.shipped, p)
		return err
	})
	if errors.Is(err, fs.ErrNotExist) { // a binary that ships nothing
		err = nil
	}
	return out, err
}

// against reads the signed manifest at the absolute path p, and returns it with its
// bytes.
func (t *toolbox) against(p string) (Distribution, []byte, error) {
	r, err := rel(p)
	if err != nil {
		return Distribution{}, nil, err
	}
	raw, err := fs.ReadFile(t.files, r)
	if err != nil {
		return Distribution{}, nil, err
	}
	d, err := ReadDistribution(p, raw)
	return d, raw, err
}

// Release returns the signed manifest that the binary embeds, or the one at against,
// with each instantiable plugin and the first example of its configSchema.
func (t *toolbox) Release(ctx context.Context, against string) (launch.Release, error) {
	if err := t.ready(ctx); err != nil {
		return launch.Release{}, err
	}
	d, raw := t.p.signed, t.signed
	if against != "" {
		var err error
		if d, raw, err = t.against(against); err != nil {
			return launch.Release{}, err
		}
	}
	out := launch.Release{Digest: d.Digest, Manifest: raw}
	if against == "" && t.p.release != nil {
		out.Key = t.p.release.Key // the default distribution root of a release binary
	}
	for _, id := range slices.Sorted(maps.Keys(d.Plugins)) {
		m, err := ReadManifest("plugin.yaml of "+id, d.Plugins[id].Manifest, ManifestLenient)
		if err != nil || !m.Instantiable {
			continue
		}
		p := launch.ReleasePlugin{ID: id, Feeds: map[string][]string{}}
		if xs, _ := m.ConfigSchema["examples"].([]any); len(xs) > 0 {
			p.Example = xs[0]
		}
		// ponytail: a registry maps to the definition of its name, as every registry of
		// S2a does.
		for _, k := range m.Registers {
			if vs, ok := m.Requires.Definitions[k.Key]; ok {
				p.Feeds[k.Key] = vs
			}
		}
		out.Plugins = append(out.Plugins, p)
	}
	return out, nil
}
