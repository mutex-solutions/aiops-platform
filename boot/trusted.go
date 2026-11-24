package boot

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"runtime/debug"
	"slices"
	"strings"
)

// What the gate reads at boot and at every live generation (ADR-060 decision 17, its
// last row): the signed distribution manifest the binary embeds, tied to the module
// table it runs (ADR-020 decision 12); trust.yaml, the record of the distribution that
// dist use made current, or the built-in record of a release binary (ADR-020 decisions
// 10 and 13); plugins.lock and what install kept and recorded beside each digest
// (ADR-047 decisions 16 and 18). Boot verifies no signature: it reads what aiopsctl
// verified and recorded (08-profiles-roadmap-adrs.md:404).

// Release is what a release binary carries beside the manifest it embeds: the id of
// the release key, its default distribution root, with which the embedded manifest is
// its built-in record (ADR-020 decision 13; ADR-060 decision 50). A binary built
// without a release key carries none.
type Release struct{ KeyID string }

// Bindings returns the binding maps and the required set of d: the release's, with
// each entry the site added (ADR-044 decisions 13 and 22).
func (d Distribution) Bindings() DistManifest {
	merge := func(a, b []Binding) []Binding {
		m := map[string][]string{}
		for _, x := range slices.Concat(a, b) {
			m[x.Key] = union(m[x.Key], x.Plugins)
		}
		var out []Binding
		for _, k := range slices.Sorted(maps.Keys(m)) {
			out = append(out, Binding{Key: k, Plugins: m[k]})
		}
		return out
	}
	hooks := func(d DistManifest) []Binding {
		var out []Binding
		for _, h := range d.SealedHooks {
			out = append(out, Binding{Key: h.Hook, Plugins: h.Plugins})
		}
		return out
	}
	r, s := d.Release, d.Site
	out := DistManifest{Providers: merge(r.Providers, s.Providers), Consumers: merge(r.Consumers, s.Consumers),
		RequiredKeys: union(r.RequiredKeys, s.RequiredKeys), RequiredRows: union(r.RequiredRows, s.RequiredRows)}
	for _, b := range merge(hooks(r), hooks(s)) {
		out.SealedHooks = append(out.SealedHooks, HookListeners{Hook: b.Key, Plugins: b.Plugins})
	}
	return out
}

// tieModules ties each plugin of d's module table to the module that the binary runs
// for its package, the main module or the dependency whose path is the longest prefix
// of it, with its replacement (ADR-020 decision 12). The trusted manifest lists that
// module with the path, version and sum the build information records; for the main
// module and a replaced module it lists the path and the replacement, with no version
// and no sum, and the tie accepts a missing sum only where the build information
// records none (ADR-020 decision 28). It returns a line for each module tied without a
// sum, which boot prints at every boot.
func tieModules(d Distribution, bi *debug.BuildInfo) (map[string]Tie, []string) {
	if bi == nil {
		bi = &debug.BuildInfo{}
	}
	all := append([]*debug.Module{&bi.Main}, bi.Deps...)
	out, bare := map[string]Tie{}, map[string]bool{}
	for id, p := range d.Plugins {
		var m *debug.Module
		for _, x := range all {
			if x.Path != "" && (p.Package == x.Path || strings.HasPrefix(p.Package, x.Path+"/")) && (m == nil || len(x.Path) > len(m.Path)) {
				m = x
			}
		}
		if m == nil {
			out[id] = Tie{Err: fmt.Errorf("its package %s is in no module of the binary's build information", p.Package)}
			continue
		}
		got := DistModule{GoModule: GoModule{Path: m.Path, Version: m.Version, Sum: m.Sum}}
		if m == all[0] {
			got.Version = "" // "(devel)", or a version derived from version control
		}
		t := Tie{Module: got.GoModule}
		if m.Replace != nil {
			got.Version, got.Sum, got.Replace = "", "", &GoModule{Path: m.Replace.Path, Version: m.Replace.Version, Sum: m.Replace.Sum}
		}
		i := slices.IndexFunc(d.Modules, func(x DistModule) bool { return x.Path == m.Path })
		switch {
		case i < 0:
			t.Err = fmt.Errorf("its module %s is absent from the trusted distribution manifest", m.Path)
		case d.Modules[i].GoModule != got.GoModule || (d.Modules[i].Replace == nil) != (got.Replace == nil) ||
			got.Replace != nil && *d.Modules[i].Replace != *got.Replace:
			t.Err = fmt.Errorf("the binary runs %s, and the trusted distribution manifest lists %s", showModule(got), showModule(d.Modules[i]))
		case got.Sum == "" && (got.Replace == nil || got.Replace.Sum == ""):
			bare[m.Path] = true
		}
		out[id] = t
	}
	var lines []string
	for _, p := range slices.Sorted(maps.Keys(bare)) {
		lines = append(lines, "trust: module "+p+" is tied by its path, with no sum, which the build information does not record (ADR-020 decision 28)")
	}
	return out, lines
}

func showModule(m DistModule) string {
	s := strings.TrimSpace(m.Path + " " + m.Version + " " + m.Sum)
	if m.Replace != nil {
		s += " => " + strings.TrimSpace(m.Replace.Path+" "+m.Replace.Version+" "+m.Replace.Sum)
	}
	return s
}

// lockDigests returns the locks section of the generation digest from plugins.lock of
// the profile: per entry the pair (id, digest), and (content/<id>, content digest)
// where the entry has content (ADR-047 decision 8). A lock that cannot be read adds
// nothing; boot denies the rows of its artifacts (bootInput).
func lockDigests(home fs.FS, profile string) []LockDigest {
	lock, _ := ReadLock(home, profile)
	var out []LockDigest
	for _, e := range lock {
		out = append(out, LockDigest{Name: e.ID, Digest: e.Digest})
		if e.Content != "" {
			out = append(out, LockDigest{Name: "content/" + e.ID, Digest: e.Content})
		}
	}
	return out
}

// bootInput reads what boot and every live generation give the gate besides the
// composition and the module table, and the lines boot prints at every boot: the
// state of the default release root, allowUnsigned, the accepted licences, each
// departure from the release bindings (08-profiles-roadmap-adrs.md:403, :404, :1346)
// and a warning on each enabled row whose plugin declares credential: broader
// (08-profiles-roadmap-adrs.md:391). A file that cannot be read denies what it vouches
// for: every Go module for trust.yaml and the records of the distribution, the rows of
// an artifact for its lock entry, kept files and record.
func bootInput(home fs.FS, profile string, c Composition, mods ModuleTable, d Distribution, rel *Release, tie map[string]Tie) (GateInput, []string) {
	in := GateInput{Entry: EntryBoot, Mods: mods, Bindings: d.Bindings(), Dist: &d, Tie: tie, Artifacts: map[string]Artifact{}}
	trust, err := ReadTrust(home)
	in.Trust = &trust
	// The built-in record is a record of its own, so current is read only where it does
	// not apply (ADR-020 decision 30, reading 5).
	current, cerr := ReadCurrent(home)
	switch {
	case err != nil:
		in.Trusted = err
	case rel != nil && slices.Contains(trust.Distributions, ReleaseRoot):
		in.BuiltIn = true
	case cerr != nil:
		in.Trusted = cerr
	case current != d.Digest:
		in.Trusted = fmt.Errorf("the embedded distribution manifest %s is not the current distribution %q that dist use recorded; run aiopsctl dist use <image>", d.Digest, current)
	default:
		rec, err := ReadRecord(home, current)
		switch {
		case err != nil:
			in.Trusted = err
		case rec.Kind != "distribution" || !slices.Contains(trust.Distributions, rec.Signer):
			in.Trusted = fmt.Errorf("the record of %s is of kind %s with the signer %s, which distributions of trust.yaml does not list", current, rec.Kind, rec.Signer)
		}
	}
	if in.Trusted != nil {
		in.Trusted = fmt.Errorf("%w (to recover: `- key: release` under distributions restores the default root of a release binary, and an aiopsctl built from the current distribution boots on its record, ADR-020 decision 13)", in.Trusted)
	}
	lines := []string{"trust: no release key and no built-in record: the distribution that dist use made current is trusted (ADR-020 decision 13)"}
	if rel != nil {
		in.ReleaseKey = rel.KeyID
		state, use := "removed in trust.yaml", "not in use"
		if slices.Contains(trust.Distributions, ReleaseRoot) {
			state = "trusted"
		}
		if in.BuiltIn {
			use = "in use"
		}
		lines[0] = fmt.Sprintf("trust: the default release root, key %s, is %s; the built-in record of %s is %s (ADR-020 decision 13)", rel.KeyID, state, d.Digest, use)
	}
	if in.BuiltIn && cerr != nil {
		lines = append(lines, "warning: "+cerr.Error())
	}
	if len(trust.AllowUnsigned) > 0 {
		lines = append(lines, "trust: allowUnsigned lists "+strings.Join(trust.AllowUnsigned, ", "))
	}
	if len(d.AcceptLicenses) > 0 {
		lines = append(lines, "licence: the distribution accepts "+strings.Join(d.AcceptLicenses, ", "))
	}
	site := d.Site
	for _, side := range []struct {
		what string
		bs   []Binding
	}{{"providers", site.Providers}, {"consumers", site.Consumers}} {
		for _, b := range side.bs {
			lines = append(lines, fmt.Sprintf("bindings: the site adds %s of %s: %s", side.what, b.Key, strings.Join(b.Plugins, ", ")))
		}
	}
	for _, h := range site.SealedHooks {
		lines = append(lines, fmt.Sprintf("bindings: the site adds listeners of the sealed hook %s: %s", h.Hook, strings.Join(h.Plugins, ", ")))
	}
	if req := slices.Concat(site.RequiredKeys, site.RequiredRows); len(req) > 0 {
		lines = append(lines, "bindings: the site raises the required set by "+strings.Join(req, ", "))
	}
	// The pins depart from the release where a site's build raised one with --allow-bump
	// or took a hash that no source recorded (ADR-044 decisions 8 and 10).
	for _, p := range d.Pins {
		if p.Unchecked {
			lines = append(lines, fmt.Sprintf("pins: %s %s was checked against nothing: no source recorded its hash (ADR-044 decision 8)", p.Path, p.Version))
		}
		if p.Release != "" {
			lines = append(lines, fmt.Sprintf("pins: the site raises %s from %s, the release's pin, to %s, which --allow-bump admitted (ADR-044 decision 10)", p.Path, p.Release, p.Version))
		}
	}
	lock, lerr := ReadLock(home, profile)
	for id := range c.Unread {
		in.Artifacts[id] = installedArtifact(home, profile, id, lock, lerr)
	}
	for _, r := range c.Rows {
		// The artifact of a row's origin, or of its plugin for a row of an installed
		// plugin that no artifact bundle inserted (ADR-020 decision 12).
		ids := []string{installedBundle(r)}
		if _, err := fs.Stat(home, path.Join("bundles", r.Plugin, "artifact.json")); err == nil {
			ids = append(ids, r.Plugin)
		}
		for _, id := range ids {
			if _, done := in.Artifacts[id]; id != "" && !done {
				in.Artifacts[id] = installedArtifact(home, profile, id, lock, lerr)
			}
		}
		// A pack states its credential class in its own manifest (08-profiles-roadmap-adrs.md:391).
		raws := [][]byte{d.Plugins[r.Plugin].Manifest}
		if id := installedBundle(r); id != r.Plugin {
			raws = append(raws, in.Artifacts[id].Kept.Manifest)
		}
		for _, raw := range raws {
			man, err := ReadManifest("plugin.yaml", raw, ManifestLenient)
			for _, i := range man.Integrates {
				if err == nil && r.Enabled && i.Credential == "broader" {
					lines = append(lines, fmt.Sprintf("warning: row %q: plugin %s declares credential: broader for %s (08-profiles-roadmap-adrs.md:391)", r.ID, man.ID, i.System))
				}
			}
		}
	}
	return in, lines
}

// installedArtifact reads one installed artifact: the digest that plugins.lock names,
// or for in-process code, which has no entry, that of the artifact.json install kept
// (ADR-047 decisions 8 and 18); then the kept files against it and the record beside
// it. An artifact the lock names no digest for is in-process code by its record, or it
// is denied.
func installedArtifact(home fs.FS, profile, id string, lock []LockEntry, lerr error) Artifact {
	a := Artifact{ID: id, Err: lerr}
	i := slices.IndexFunc(lock, func(e LockEntry) bool { return e.ID == id })
	if i >= 0 {
		a.Digest, a.Content = lock[i].Digest, lock[i].Content
	} else if a.Err == nil {
		art, err := readFile(File{FS: home, Path: path.Join("bundles", id, "artifact.json")})
		a.Digest, a.Err = digestOf(art), err
	}
	if a.Err == nil {
		a.Kept, a.Err = ReadKept(home, id, a.Digest)
	}
	if a.Err == nil {
		a.Record, a.Err = ReadRecord(home, a.Digest)
	}
	if a.Err == nil && i < 0 && a.Record.Module == nil {
		a.Err = &Error{File: path.Join("profiles", profile, "plugins.lock"), Rule: RuleKept, Msg: "names no digest for " + id + ", and only in-process code has no entry"}
	}
	return a
}
