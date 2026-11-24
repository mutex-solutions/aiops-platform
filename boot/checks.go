package boot

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The checks of S2a (ADR-060 decisions 14 to 17): 1, 2, 4, 5, 6 and 10, the signed and
// fake parts of check 7 and the installed-artifact clause of check 8. Each reads data
// only: manifests, served data, the statements, the records of install and trust.yaml.

// GateInput is what an entry point of the gate holds (ADR-060 decision 17). A field
// the entry point does not hold is zero, and the clauses that read it are the ones
// notEvaluated names for the entry.
type GateInput struct {
	Entry Entry
	// Mods is the module table, with the Go value of each plugin where the caller
	// links it.
	Mods ModuleTable
	// Bindings are the binding maps and the required set: the release's with the
	// site's entries (Distribution.Bindings).
	Bindings DistManifest
	// Dist is the signed distribution manifest: the module table's manifests and
	// statements, its modules, packages, licences and serves. Nil where none is given,
	// and the clauses that read it are then not evaluated (notEvaluated).
	Dist *Distribution
	// Trusted is nil when boot trusts Dist, and otherwise says why not; check 5 then
	// denies every Go module (ADR-020 decision 12). BuiltIn is true when Dist is the
	// built-in record of a release binary (ADR-020 decision 13).
	Trusted error
	BuiltIn bool
	// ReleaseKey is the id of the release key that the binary carries, "" for none
	// (Release; ADR-060 decision 50).
	ReleaseKey string
	// Tie is, by plugin id of Dist, the module that the binary runs for it and the
	// result of the module tie (ADR-020 decision 12). Nil where no binary runs.
	Tie map[string]Tie
	// Trust is trust.yaml; nil where the entry point has none.
	Trust *Trust
	// Artifacts are the installed artifacts by plugin id.
	Artifacts map[string]Artifact
}

// Tie is the module that boot resolved from the build information for a plugin of
// the module table, and why it does not tie to the trusted manifest, nil when it does.
type Tie struct {
	Module GoModule
	Err    error
}

// Artifact is an installed artifact: the digest of its OCI manifest, from plugins.lock
// or for in-process code from the artifact.json that install kept (ADR-047 decision
// 18), the lock's content digest, what install kept and the record of the digest. Err
// says why the kept files or the record cannot be read (RuleKept, RuleRecord).
type Artifact struct {
	ID, Digest, Content string
	Kept                Kept
	Record              Record
	Err                 error
}

// subject is what checks 1, 2, 4, 5, 6 and 10 read for a row: the plugin of the
// module table, or the installed artifact that inserted the row or holds its plugin.
type subject struct {
	id     string
	man    Manifest
	err    error  // the manifest is absent or malformed (check 1)
	raw    []byte // the manifest
	att    []byte // the statement, nil when there is none
	digest string // the digest a statement names: of raw, or of the artifact
	art    *Artifact
}

// clauseOf names the clause of a failure as notEvaluated keys it.
func clauseOf(f RowResult) string {
	switch f.Rule {
	case RuleSigner, RuleModule, RuleSignedClass, RuleFake, RuleInstalled:
		return strconv.Itoa(f.Check) + map[Rule]string{RuleSigner: " signer", RuleModule: " module",
			RuleSignedClass: " signed", RuleFake: " fake", RuleInstalled: " installed"}[f.Rule]
	}
	return strconv.Itoa(f.Check)
}

// installedBundle returns the plugin id of the installed artifact whose bundle
// inserted r, or "": such a bundle is named by a plugin id (ADR-047 decision 5).
func installedBundle(r ComposedRow) string {
	o, _ := r.Provenance.Of(FieldID)
	if id, ok := strings.CutPrefix(o.Source, "bundle:"); ok && strings.Contains(id, "/") {
		return id
	}
	return ""
}

// servedDefinitions returns every served major of d, the kernel part first.
func servedDefinitions(d *Distribution) []ServedDefinition {
	if d == nil {
		return nil
	}
	out := []ServedDefinition{d.Serves.Kernel}
	for _, n := range slices.Sorted(maps.Keys(d.Serves.Definitions)) {
		out = append(out, d.Serves.Definitions[n]...)
	}
	return out
}

// signedOf returns the keys and hooks that served data puts in the signed class
// (ADR-060 decision 15).
func signedOf(d *Distribution) map[string]bool {
	out := map[string]bool{}
	for _, sd := range servedDefinitions(d) {
		for k, c := range sd.Classes {
			out[k] = out[k] || c.Consumer == "signed"
		}
		for h, sv := range sd.Hooks {
			out[h] = out[h] || sv.Access == "signed"
		}
	}
	return out
}

// subject returns the plugin id of the module table, or with artifact the installed
// artifact, as a subject, read once.
func (g *gate) subject(id string, artifact bool) *subject {
	key := id
	if artifact {
		key = "artifact " + id
	}
	if s, ok := g.subjects[key]; ok {
		return s
	}
	s := &subject{id: id}
	switch {
	case artifact:
		a, ok := g.in.Artifacts[id]
		if !ok {
			a.Err = fmt.Errorf("no artifact of %s is installed: plugins.lock names none and install kept none", id)
		}
		s.art, s.raw, s.att, s.digest, s.err = &a, a.Kept.Manifest, a.Record.Attestation, a.Digest, a.Err
	case g.in.Dist == nil:
		s.err = errors.New("no signed distribution manifest")
	default:
		p, ok := g.in.Dist.Plugins[id]
		s.raw, s.att, s.digest = p.Manifest, p.Attestation, digestOf(p.Manifest)
		if !ok {
			s.err = fmt.Errorf("plugin %s has no manifest in the signed distribution manifest", id)
		}
	}
	if s.err == nil {
		s.man, s.err = ReadManifest("plugin.yaml of "+id, s.raw, ManifestLenient)
	}
	if s.err == nil && s.man.ID != id { // an entry is the plugin of its key (07-c4-plugin-sdk.md:571)
		s.err = fmt.Errorf("it declares the id %s", s.man.ID)
	}
	g.subjects[key] = s
	return s
}

// checks returns the S2a failures of one enabled row whose plugin resolved, on each of
// its subjects.
func (g *gate) checks(r ComposedRow) []RowResult {
	var out []RowResult
	f := func(check int, rule Rule, format string, a ...any) {
		out = append(out, g.fail(check, r.Row, rule, false, format, a...))
	}
	// Before the rebuild a row of the artifact's own in-process plugin has the artifact
	// as its one subject, and check 5 finds no module in the table (ADR-060 decision 17).
	art := installedBundle(r)
	if _, ok := g.in.Artifacts[r.Plugin]; art == "" && ok {
		art = r.Plugin // a row of an installed plugin that no artifact bundle inserted (ADR-020 decision 12)
	}
	var subs []*subject
	if _, built := g.in.Mods[r.Plugin]; built || art != r.Plugin {
		subs = append(subs, g.subject(r.Plugin, false))
	} else {
		f(CheckTrust, RuleModule, "row %q: plugin %s is not in the module table the binary runs: its module takes a rebuild", r.ID, r.Plugin)
	}
	if art != "" {
		subs = append(subs, g.subject(art, true))
	}
	for _, s := range subs {
		what := "plugin " + s.id
		if s.art != nil {
			what = "the installed artifact " + s.id
		}
		if s.art != nil && s.art.Err != nil {
			f(CheckTrust, artRule(s.art.Err), "row %q: %s: %v", r.ID, what, s.art.Err)
			continue
		}
		out = append(out, g.trust(r, s, what)...)
		if s.err != nil {
			f(CheckManifest, RuleManifest, "row %q: the manifest of %s: %v", r.ID, what, s.err)
			continue
		}
		out = append(out, g.versions(r, s, what)...)
		if rank(s.man.Stability) < rank(g.min) {
			f(CheckStability, RuleStability, "row %q: %s is %s, below the profile's minStability %s", r.ID, what, s.man.Stability, g.min)
		}
		if s.art == nil && s.att == nil && art == r.Plugin {
			continue // the install record of the plugin's artifact is its attestation (ADR-020 decision 10)
		}
		out = append(out, g.attested(r, s, what)...)
	}
	if subs[0].err == nil {
		out = append(out, g.signedUse(r, subs)...)
	}
	for _, b := range g.faults {
		f(b.check, b.rule, "row %q: %s", r.ID, b.why)
	}
	return out
}

// artRule is the rule of check 5 that an installed artifact which cannot be read
// breaks: its kept files, its record, or else its signer.
func artRule(err error) Rule {
	var e *Error
	if errors.As(err, &e) && (e.Rule == RuleKept || e.Rule == RuleRecord) {
		return e.Rule
	}
	return RuleSigner
}

// rank orders the stabilities, experimental < preview < ga; "" is below all, so a
// profile without minStability sets no minimum (07-c4-plugin-sdk.md:93, :127).
func rank(s string) int { return slices.Index([]string{"", "experimental", "preview", "ga"}, s) }

// versions is check 1 past the reader and check 2 for one subject.
func (g *gate) versions(r ComposedRow, s *subject, what string) []RowResult {
	var out []RowResult
	f := func(check int, rule Rule, format string, a ...any) {
		out = append(out, g.fail(check, r.Row, rule, false, "row %q: %s: "+format, append([]any{r.ID, what}, a...)...))
	}
	serves := Serves{}
	if g.in.Dist != nil {
		serves = g.in.Dist.Serves
	}
	q := s.man.Requires
	if slices.Contains(s.man.Runtimes, "wasm") {
		f(CheckManifest, RuleAuthorAPI, "runtimes lists wasm, a runtime this schema version does not serve (ADR-060 decision 58)")
	}
	if q.PluginAPI != "" && !minorServed(q.PluginAPI, serves.PluginAPI, "") {
		f(CheckManifest, RuleAuthorAPI, "requires.pluginApi %s is not the author API %s that the distribution serves", q.PluginAPI, serves.PluginAPI)
	}
	for _, name := range slices.Sorted(maps.Keys(q.Definitions)) {
		for _, v := range q.Definitions[name] {
			if !slices.ContainsFunc(serves.Definitions[name], func(sd ServedDefinition) bool { return minorServed(v, sd.Version, sd.CompatibleFrom) }) {
				f(CheckDefinitions, RuleUnserved, "requires %s %s, and the distribution serves %s", name, v, servedList(serves.Definitions[name]))
			}
		}
	}
	return out
}

// minorServed is the minor rule of check 2 (08-profiles-roadmap-adrs.md:498): the same
// major; on a stable major a served minor at least the one needed; at major 0 the same
// minor, or one from compatibleFrom up to the served minor.
func minorServed(need, served, from string) bool {
	nM, nm, _ := majorMinor(need)
	sM, sm, ok := majorMinor(served)
	fM, fm, hasFrom := majorMinor(from)
	switch {
	case !ok || nM != sM:
		return false
	case nM > 0:
		return sm >= nm
	}
	return nm == sm || hasFrom && fM == 0 && fm <= nm && nm <= sm
}

func servedList(sds []ServedDefinition) string {
	var vs []string
	for _, sd := range sds {
		vs = append(vs, sd.Version)
	}
	return "[" + strings.Join(vs, ", ") + "]"
}

// trust is check 5 for one subject: a plugin of the module table passes through the
// signer of the trusted manifest, and its module must tie to that manifest (ADR-020
// decisions 12 and 13); an installed artifact needs its kept files to match the
// digest and a signer that trust.yaml lists for its id and runtimes, or allowUnsigned
// (ADR-020 decisions 6 and 8; ADR-047 decision 18), and one with no module whose id is
// under a reserved owner needs the release key the binary carries as its signer
// (ADR-048 decision 14).
func (g *gate) trust(r ComposedRow, s *subject, what string) []RowResult {
	var out []RowResult
	f := func(rule Rule, format string, a ...any) {
		out = append(out, g.fail(CheckTrust, r.Row, rule, false, "row %q: %s: "+format, append([]any{r.ID, what}, a...)...))
	}
	in := g.in
	if s.art == nil {
		switch t := in.Tie[s.id]; {
		case in.Trusted != nil:
			f(RuleModule, "every Go module is absent from a trusted distribution manifest: %v", in.Trusted)
		case !g.inDist(s.id):
			f(RuleModule, "its module is absent from the trusted distribution manifest")
		case t.Err != nil:
			f(RuleModule, "%v", t.Err)
		}
		return out
	}
	a, man := s.art, s.man
	if a.Content != a.Kept.Content {
		f(RuleKept, "the content kept hashes to %q and plugins.lock names %q (ADR-047 decision 18)", a.Kept.Content, a.Content)
	}
	if rec := a.Record; s.err == nil && (rec.ID != man.ID || rec.Version != man.Version || !slices.Equal(rec.Runtimes, man.Runtimes)) {
		f(RuleRecord, "the record of %s vouches for %s@%s %v, and the kept manifest is %s@%s %v", a.Digest, rec.ID, rec.Version, rec.Runtimes, man.ID, man.Version, man.Runtimes)
	}
	switch owner, _, _ := strings.Cut(s.id, "/"); {
	case a.Record.Module == nil && (owner == "aiops" || owner == "contrib") && (in.ReleaseKey == "" || a.Record.Signer != in.ReleaseKey):
		f(RuleSigner, "it has no module, its id is under the reserved owner %s, and its signer %s is not the release key the binary carries (ADR-048 decision 14)", owner, a.Record.Signer)
	case a.Record.Signer == Unsigned && in.Trust != nil && slices.Contains(in.Trust.AllowUnsigned, s.id):
	case a.Record.Signer == Unsigned:
		f(RuleSigner, "no verified signer is recorded beside %s, and allowUnsigned does not list %s", a.Digest, s.id)
	case len(g.covering(s.id, a.Record.Signer, man.Runtimes)) == 0:
		f(RuleSigner, "its signer %s is outside trust.yaml for %s and the runtimes %v", a.Record.Signer, s.id, man.Runtimes)
	}
	if m := a.Record.Module; m != nil && in.Dist != nil && !slices.ContainsFunc(in.Dist.Modules, func(d DistModule) bool { return d.GoModule == *m && d.Replace == nil }) {
		f(RuleModule, "it pins %s %s, which the trusted distribution manifest does not list", m.Path, m.Version)
	}
	return out
}

// inDist reports whether the module table of the signed manifest lists id.
func (g *gate) inDist(id string) bool {
	if g.in.Dist == nil {
		return false
	}
	_, ok := g.in.Dist.Plugins[id]
	return ok
}

// covering returns the signers of trust.yaml with key id key that cover the plugin id
// and every runtime (ADR-020 decision 6).
func (g *gate) covering(id, key string, runtimes []string) []TrustSigner {
	var out []TrustSigner
	if g.in.Trust == nil {
		return nil
	}
	owner, _, _ := strings.Cut(id, "/")
	for _, s := range g.in.Trust.Signers {
		match := slices.ContainsFunc(s.IDs, func(p string) bool { return p == id || p == owner+"/*" })
		if s.KeyID == key && match && !slices.ContainsFunc(runtimes, func(rt string) bool { return !slices.Contains(s.Runtimes, rt) }) {
			out = append(out, s)
		}
	}
	return out
}

// signedUse is the signed part of check 7 (ADR-020 decision 7): a row that provides or
// uses a key, or listens on a hook, of the signed class passes when its plugin is
// first-party, or belongs to an installed artifact whose signer has a covering entry
// that names the key, for in-process code only, or the hook.
func (g *gate) signedUse(r ComposedRow, subs []*subject) []RowResult {
	m, a := subs[0].man, subs[len(subs)-1] // the row's plugin, and its installed artifact
	var names []string
	for _, k := range m.Provides {
		names = append(names, k.Key)
	}
	for _, u := range m.Uses {
		names = append(names, u.Key)
	}
	key := len(names)
	for _, h := range m.Hooks {
		names = append(names, h.Hook)
	}
	var out []RowResult
	for i, n := range names {
		if !g.signed[n] || strings.HasPrefix(r.Plugin, "aiops/") {
			continue
		}
		trusted := false
		if a.art != nil && a.art.Err == nil && a.id == r.Plugin {
			for _, s := range g.covering(r.Plugin, a.art.Record.Signer, a.man.Runtimes) {
				trusted = trusted || slices.Contains(s.Signed, n) && (i >= key || slices.Contains(a.man.Runtimes, "inproc"))
			}
		}
		if !trusted {
			out = append(out, g.fail(CheckKeyHookClass, r.Row, RuleSignedClass, false,
				"row %q uses %s of the signed class, and plugin %s is neither first-party nor of an installed artifact whose signer trust.yaml trusts for it", r.ID, n, r.Plugin))
		}
	}
	return out
}

type binaryFault struct {
	check int
	rule  Rule
	why   string
}

// binaryFaults are the denials of every row of the module table that the signed
// manifest decides for the whole binary: a fake or sdk/testkit package in its package
// graph (check 7; ADR-060 decision 58), and a module whose detected licence is outside
// the L7 allow-list and not accepted, or an acceptance in the release (check 6;
// 08-profiles-roadmap-adrs.md:1337, :1346).
func (g *gate) binaryFaults() []binaryFault {
	d := g.in.Dist
	if d == nil {
		return nil
	}
	var out []binaryFault
	for _, p := range d.Packages {
		if seg := strings.Split(p, "/"); slices.Contains(seg, "fake") || strings.Contains(p, "/sdk/testkit") {
			out = append(out, binaryFault{CheckKeyHookClass, RuleFake, "the binary links " + p + ", a fake or sdk/testkit package (ADR-060 decision 58)"})
			break
		}
	}
	for _, m := range d.Modules {
		if !Licensed(m.Licence, d.AcceptLicenses) {
			out = append(out, binaryFault{CheckLicence, RuleLicence, fmt.Sprintf("module %s is under %s, outside the L7 allow-list and not in acceptLicenses", m.Path, m.Licence)})
			break
		}
	}
	if g.in.BuiltIn && len(d.AcceptLicenses) > 0 {
		out = append(out, binaryFault{CheckLicence, RuleLicence, "the release records acceptLicenses " + strings.Join(d.AcceptLicenses, ", ") + ", and the release takes no acceptance (L16)"})
	}
	return out
}

// Licensed reports whether the SPDX expression e passes L7 with the accepted ids: A OR
// B when one side does, A AND B when both do, an id on the allow-list or accepted, and
// an id WITH an exception as the id. The licences L7 allows with a recorded condition
// are allowed here, since the register records the condition.
//
// ponytail: the list leaves out one SPDX id that L7 allows, the one that is the name
// of the register entry of the database behind sql, because the name lint U1 refuses
// that name in boot/ and no item of the register's exceptions gives it this place;
// until the owner adds one, a module under that licence alone needs acceptLicenses.
// No module of a distribution is under it.
func Licensed(e string, accepted []string) bool {
	toks := strings.Fields(strings.NewReplacer("(", " ( ", ")", " ) ").Replace(e))
	i := 0
	var or func() bool
	term := func() bool {
		t := toks[i]
		i++
		if t == "(" {
			v := or()
			i++ // ")"
			return v
		}
		if i < len(toks) && toks[i] == "WITH" {
			i += 2
		}
		return slices.Contains(accepted, t) || slices.Contains([]string{"Apache-2.0", "MIT", "BSD-2-Clause", "BSD-3-Clause", "ISC", "0BSD",
			"Zlib", "Unicode-DFS-2016", "Unicode-3.0", "BlueOak-1.0.0", "MPL-2.0", "CC0-1.0"}, t)
	}
	and := func() bool {
		v := term()
		for i < len(toks) && toks[i] == "AND" {
			i++
			v = term() && v
		}
		return v
	}
	or = func() bool {
		v := and()
		for i < len(toks) && toks[i] == "OR" {
			i++
			v = and() || v
		}
		return v
	}
	return e != "" && okSPDX(e) && or()
}

// attested is check 10 for one subject (08-profiles-roadmap-adrs.md:506; ADR-060
// decisions 55 and 56; ADR-020 decisions 9 and 12).
func (g *gate) attested(r ComposedRow, s *subject, what string) []RowResult {
	var out []RowResult
	f := func(format string, a ...any) {
		out = append(out, g.fail(CheckAttestation, r.Row, RuleAttestation, false, "row %q: %s: "+format, append([]any{r.ID, what}, a...)...))
	}
	m := s.man
	if s.att == nil {
		f("the conformance attestation is missing")
		return out
	}
	at, err := ReadAttestation("the attestation of "+s.id, s.att)
	switch {
	case err != nil:
		f("%v", err)
		return out
	case at.Name != m.ID+"@"+m.Version || at.Digest != s.digest:
		f("the attestation names another subject, %s at %s, and the subject is %s@%s at %s", at.Name, at.Digest, m.ID, m.Version, s.digest)
		return out
	}
	for _, name := range slices.Sorted(maps.Keys(m.Requires.Definitions)) {
		if v := at.Definitions[name].Version; !slices.Contains(m.Requires.Definitions[name], v) {
			f("the attestation names %s at %q, and requires names %v", name, v, m.Requires.Definitions[name])
		}
	}
	suites := g.suites(s)
	for _, name := range slices.Sorted(maps.Keys(suites)) {
		switch n := at.Definitions[name].Suite; {
		case n == nil:
			f("the attestation lacks the suite revision of %s, which the plugin provides, contributes to or feeds", name)
		case *n != suites[name]:
			f("the attestation names suite revision %d of %s, and the distribution serves revision %d (ADR-060 decision 55)", *n, name, suites[name])
		}
	}
	if len(m.Runtimes) == 0 && at.Scenario == nil {
		f("the attestation of a pack names no scenario version")
	}
	if t, ok := g.in.Tie[r.Plugin]; s.art != nil && s.art.Record.Module != nil && ok && *s.art.Record.Module != t.Module {
		f("the artifact pins %s and the binary runs %s, so its attestation names another subject (ADR-020 decision 12)",
			showModule(DistModule{GoModule: *s.art.Record.Module}), showModule(DistModule{GoModule: t.Module}))
	}
	return out
}

// suites returns, by name, the served suite revision of each definition whose revision
// the statement of s must name: those of the keys it provides or registers, and for a
// pack those of the keys that the plugins of its rows register (ADR-060 decision 56).
func (g *gate) suites(s *subject) map[string]int {
	keys := slices.Concat(s.man.Provides, s.man.Registers)
	if len(s.man.Runtimes) == 0 {
		for _, id := range g.feeds[s.id] {
			keys = append(keys, g.subject(id, false).man.Registers...)
		}
	}
	out := map[string]int{}
	if g.in.Dist != nil {
		for name, sds := range g.in.Dist.Serves.Definitions {
			for _, sd := range sds {
				major, _, _ := majorMinor(sd.Version)
				if slices.ContainsFunc(keys, func(k ManifestKey) bool { _, ok := sd.Classes[k.Key]; return ok && k.Major == major }) {
					out[name] = sd.Suite
				}
			}
		}
	}
	return out
}

// installed is the installed-artifact clause of check 8 (ADR-047 decisions 6, 11 and
// 19; ADR-060 decision 57), on the bundle that install kept: it holds insert items only;
// each row is of the artifact's own plugin or of a first-party plugin whose manifest
// says instantiable, sets only id, plugin, roles, enabled: false and config, and holds
// no ${env:} or ${file:} reference. A breach denies every row the bundle inserted, under
// the group that Compose adds. An artifact whose group id was taken (Composition.Clashes)
// or whose kept bundle does not read (Composition.Unread) is denied by its plugin id,
// since none of its rows was inserted.
func (g *gate) installed(c Composition) []RowResult {
	var out []RowResult
	for _, id := range slices.Sorted(maps.Keys(g.feeds)) {
		_, name, _ := strings.Cut(id, "/")
		if slices.Contains(g.in.Bindings.RequiredRows, name) {
			out = append(out, g.fail(CheckLayerRules, Row{ID: name}, RuleGroupClash, false,
				"the group %q of the installed artifact %s takes the id of a required row, which no artifact stands in for (ADR-047 decision 6)", name, id))
		}
		a, ok := g.in.Artifacts[id]
		if !ok || a.Err != nil {
			continue
		}
		why := g.bundleRule(id, a.Kept.Bundle)
		for _, r := range c.Rows {
			if why != "" && installedBundle(r) == id && r.ID != name {
				out = append(out, g.fail(CheckLayerRules, r.Row, RuleInstalled, false, "row %q: the bundle of the installed artifact %s %s", r.ID, id, why))
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(c.Unread)) {
		if s := g.subject(id, true); s.art.Err != nil {
			out = append(out, g.fail(CheckTrust, Row{ID: id}, artRule(s.art.Err), false, "the installed artifact %s: %v", id, s.art.Err))
		} else {
			out = append(out, g.fail(CheckLayerRules, Row{ID: id}, RuleInstalled, false,
				"the bundle of the installed artifact %s does not compose, so none of its rows is inserted: %s", id, c.Unread[id]))
		}
	}
	for _, id := range c.Clashes {
		_, name, _ := strings.Cut(id, "/")
		out = append(out, g.fail(CheckLayerRules, Row{ID: id}, RuleGroupClash, false,
			"the group %q of the installed artifact %s takes the id of a row composed before it or of the loader's row, so none of its rows is inserted (ADR-047 decision 6)", name, id))
	}
	return out
}

func (g *gate) bundleRule(id string, data []byte) string {
	file := "bundles/" + id + "/bundle.yaml"
	root, err := parseDocument(file, data)
	var items []Item
	if err == nil {
		items, err = (&parser{file: file, installed: true}).layerItems(root)
	}
	if err != nil {
		return err.Error()
	}
	for _, it := range items {
		if it.Patch != nil {
			return fmt.Sprintf("holds a patch of %q: it holds insert items only", it.Patch.ID)
		}
		for _, r := range it.Insert {
			own := r.Plugin == id
			if s := g.subject(r.Plugin, false); !own && (s.err != nil || !s.man.Instantiable || !strings.HasPrefix(r.Plugin, "aiops/")) {
				return fmt.Sprintf("inserts row %q of plugin %s, which is neither its own nor a first-party plugin marked instantiable", r.ID, r.Plugin)
			}
			if r.Enabled {
				return fmt.Sprintf("inserts row %q enabled: it inserts each row with enabled: false", r.ID)
			}
			for _, u := range collectRefs(r.Config, "config", nil) {
				if u.Ref.Scheme == RefEnv || u.Ref.Scheme == RefFile {
					return fmt.Sprintf("holds the reference ${%s:%s} at %s of row %q, and an installed artifact holds no ${env:} or ${file:} reference (ADR-060 decision 57)",
						u.Ref.Scheme, u.Ref.Name, u.Path, r.ID)
				}
			}
		}
	}
	return ""
}
