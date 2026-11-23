package boot

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The gate: admission of a composition (03-c3-kernel.md:357, "Admit"). It has
// one slot per check of the one gate text, named and ordered as its table is
// (08-profiles-roadmap-adrs.md:493-506; ADR-060 decision 16), and runs at each entry
// point of ADR-060 decision 17 on what that entry point holds (GateInput): the
// composition, the module table, the signed distribution manifest, trust.yaml and
// what install recorded, plus the keys the root provides and the classes of keys and
// hooks that their definitions declare (classesOf). It runs no plugin code. Before the
// ten checks it resolves each enabled row's plugin (03-c3-kernel.md:357).
//
// A result is data. Per check and per row it is pass, fail with a reason, or not
// evaluated, with the reason: the entry point does not hold the input the clause
// reads, or the step that builds the check is later. What a failure means follows the
// failure matrix (08-profiles-roadmap-adrs.md:466-471):
//
//   - a row whose plugin is not in the module table is inactive when optional
//     and stops boot when required;
//   - a denied optional row is detached as disabled in the launcher's copy
//     and listed; a denied required row stops boot;
//   - a failure marked Stop stops boot whatever the row: isolate on a bound
//     key (a static graph error in 03-c3-kernel.md:359) and the last enabled
//     provider of a bound key being disabled.

// Status is the result of one check on one row.
type Status string

// The three statuses of a gate result.
const (
	StatusPass         Status = "pass"
	StatusFail         Status = "fail"
	StatusNotEvaluated Status = "not-evaluated"
)

// Check numbers of the gate, as in the one gate text
// (08-profiles-roadmap-adrs.md:497-506).
const (
	CheckManifest      = 1
	CheckDefinitions   = 2
	CheckRemoteProto   = 3
	CheckStability     = 4
	CheckTrust         = 5
	CheckLicence       = 6
	CheckKeyHookClass  = 7
	CheckLayerRules    = 8
	CheckRemoteScope   = 9
	CheckAttestation   = 10
	NumChecks          = 10
	checkResolve       = 0 // the module table lookup that runs before the ten checks
	untilS2b           = "S2b"
	reasonNotEvaluated = "not evaluated until "
)

// CheckInfo names one slot. Until is the roadmap step that builds the check, empty
// once it is built. Partial lists the clauses of a built check that the one gate text
// marks with a later step.
type CheckInfo struct {
	N       int
	Name    string
	Until   string
	Partial string
}

// GateChecks returns the ten slots in order. Checks 3 and 9 are S2b everywhere, and
// exemptions S6b (ADR-060 decision 17).
func GateChecks() []CheckInfo {
	const exempt = "exemptions (compatibility.yaml) are S6b"
	return []CheckInfo{
		{CheckManifest, "Manifest schema and author API", "", exempt},
		{CheckDefinitions, "Definition versions", "", exempt},
		{CheckRemoteProto, "Remote protocol", untilS2b, ""},
		{CheckStability, "Stability", "", exempt},
		{CheckTrust, "Trust", "", "the index clause is S6a"},
		{CheckLicence, "Licence", "", ""},
		{CheckKeyHookClass, "Key and hook classes", "", ""},
		{CheckLayerRules, "Layer rules", "", "the tenant overlay is S4b, the scope: tenant clause S4a"},
		{CheckRemoteScope, "Remote scope", untilS2b, ""},
		{CheckAttestation, "Attestation", "", ""},
	}
}

// Entry is an entry point of the gate (08-profiles-roadmap-adrs.md:493; ADR-060
// decision 17). Each evaluates the clauses its inputs decide.
type Entry string

// The entry points. EntryInstall is install of in-process code before the rebuild
// and of every pack, EntryInstallUsed install after dist use. EntryBoot is boot and
// every live generation (profileContext.Admit). EntryRender is aiopsctl profile render,
// which reads what boot reads with no binary that runs the module table (Render).
const (
	EntryBuild       Entry = "dist build"
	EntryCheck       Entry = "plugin check"
	EntryOffline     Entry = "plugin gate --against"
	EntryInstall     Entry = "plugin install"
	EntryInstallUsed Entry = "plugin install after dist use"
	EntryBoot        Entry = "boot"
	EntryRender      Entry = "profile render"
)

// notEvaluated returns the clauses that the entry point of in does not evaluate, each
// with the reason, keyed by the check number, alone for the whole check or with the
// clause's name (ADR-060 decision 17; ADR-020 decision 30, reading 3). Given no signed
// distribution manifest, the gate evaluates what the module table and the bindings
// decide only.
func notEvaluated(in GateInput) map[string]string {
	const (
		noProfile = "no profile is composed"
		noSigner  = "no signer is recorded for what it reads: a first-party row passes, any other is not evaluated (ADR-020 decision 30, reading 3)"
		noDist    = "no signed distribution manifest is given"
		noBinary  = "no binary runs the module table: boot ties each module to the build information of the binary (ADR-020 decision 12)"
	)
	if in.Dist == nil {
		return map[string]string{"1": noDist, "2": noDist, "4": noDist, "5": noDist, "6": noDist, "7 signed": noDist, "7 fake": noDist,
			"8 installed": noDist, "10": noDist}
	}
	out := map[string]string{}
	switch in.Entry {
	case EntryBuild:
		out = map[string]string{"4": noProfile, "8": noProfile, "7 signed": noSigner,
			"5":  "the manifest it would read is the build's output, and dist use records its signer",
			"10": "dist.yaml lists {module, version, sum} and no artifact, so no attestation reaches the builder"}
	case EntryCheck:
		out = map[string]string{"4": "no profile", "5 signer": "no trust.yaml", "5 module": "no site distribution",
			"6": "detected licences belong to a build list (08-profiles-roadmap-adrs.md:1344)", "7 signed": noSigner}
	case EntryOffline:
		out = map[string]string{"4": "no profile", "5": "no trust.yaml", "6": "no module source and no build list", "7 signed": noSigner}
	case EntryInstall:
		out = map[string]string{"6": "in-process code has no build list yet",
			"5 module": "in-process code: when every evaluated clause passes, install answers rebuild-required"}
	}
	// The module tie of checks 5 and 10 reads the binary's build information, which an
	// entry point that gives no Tie, profile render among them, does not hold.
	for _, n := range []string{"5", "10"} {
		if in.Tie == nil && out[n] == "" {
			out[n+" tie"] = noBinary
		}
	}
	return out
}

// RowResult is the result of one check on one row. Check is 0 for the module
// table lookup. Until is set only for StatusNotEvaluated.
type RowResult struct {
	Check  int
	Row    string
	Status Status
	Rule   Rule
	Reason string
	Stop   bool
	Until  string
}

// Slot is one of the ten checks with its failures. An evaluated slot lists only its
// failures: a row of GateResult.Rows with no failure passed, unless Open or
// GateResult.Unevaluated lists it.
// Reason says why the entry point does not evaluate the check at all; Clauses lists
// the clauses of an evaluated check that it does not evaluate, each with the reason,
// and Open the rows that a clause it could not evaluate would have judged.
type Slot struct {
	CheckInfo
	Evaluated bool
	Reason    string
	Clauses   []string
	Fails     []RowResult
	Open      []RowResult
}

// GateResult is the data the gate returns.
//
// Rows lists the ids of the composed rows and, as rows of their own, the plugin ids of
// the installed artifacts whose group id was taken (Composition.Clashes). Pass is true
// when no check failed on any row. Refused is true when boot must stop: a row that is
// required, or a failure marked Stop. Detached lists the optional rows denied by a
// check, which the launcher's copy disables, and those artifacts; Inactive lists the
// optional rows whose plugin is not in the module table. Unevaluated lists the disabled
// rows that no installed artifact's bundle inserted: they mount no code, and only check
// 8, which runs on every row, evaluates them.
type GateResult struct {
	Entry       Entry
	Slots       []Slot
	Rows        []string
	Unevaluated []string
	Unresolved  []RowResult
	Pass        bool
	Refused     bool
	Detached    []string
	Inactive    []string
}

// Result returns the result of one check on one row: pass or fail with a
// reason for an evaluated slot, not evaluated with the reason for the others. Check
// 0 is the module table lookup. An unknown row or check is not evaluated.
func (g GateResult) Result(check int, row string) RowResult {
	r := RowResult{Check: check, Row: row, Status: StatusNotEvaluated}
	var fails []RowResult
	switch {
	case check == checkResolve:
		fails = g.Unresolved
	case check >= 1 && check <= NumChecks:
		s := g.Slots[check-1]
		if !s.Evaluated {
			r.Until, r.Reason = s.Until, s.Reason
			if s.Until != "" {
				r.Reason = reasonNotEvaluated + s.Until
			}
			return r
		}
		fails = slices.Concat(s.Fails, s.Open)
	default:
		return r
	}
	if i := sort.SearchStrings(g.Rows, row); i == len(g.Rows) || g.Rows[i] != row {
		return r
	}
	if check != CheckLayerRules && slices.Contains(g.Unevaluated, row) {
		r.Reason = "a disabled row that no installed artifact inserted is not evaluated: it mounts no code"
		return r
	}
	for _, f := range fails {
		if f.Row == row {
			return f
		}
	}
	r.Status = StatusPass
	return r
}

// Denial returns the first failure of a row, and false when the row has none.
func (g GateResult) Denial(row string) (RowResult, bool) {
	for _, f := range g.Unresolved {
		if f.Row == row {
			return f, true
		}
	}
	for _, s := range g.Slots {
		for _, f := range s.Fails {
			if f.Row == row {
				return f, true
			}
		}
	}
	return RowResult{}, false
}

// Admitted returns the launcher's copy of a composition: every row with a
// failure is disabled, whatever it is. No file is rewritten and the digest is
// that of the composed tree (08-profiles-roadmap-adrs.md:508).
func (g GateResult) Admitted(c Composition) Composition {
	out := c
	out.Rows = slices.Clone(c.Rows)
	for i, r := range out.Rows {
		if _, denied := g.Denial(r.ID); denied {
			out.Rows[i].Enabled = false
		}
	}
	return out
}

// GateWith runs the ten checks on a composition at the entry point in.Entry, on what
// that entry point holds. See the file comment.
func GateWith(c Composition, in GateInput) GateResult {
	dist := in.Bindings
	g := &gate{
		in:       in,
		mods:     ModuleTable{},
		provider: bindingSet(dist.Providers),
		consumer: bindingSet(dist.Consumers),
		sealed:   map[string][]string{},
		required: map[string]bool{},
		rows:     map[string]ComposedRow{},
		mounted:  ActiveRows(c, nil),
		subjects: map[string]*subject{},
		feeds:    map[string][]string{},
		min:      c.MinStability,
	}
	g.pb, g.cb, g.sh = classesOf(in.Mods, dist, in.Dist)
	g.signed, g.faults = signedOf(in.Dist), g.binaryFaults()
	for _, h := range dist.SealedHooks {
		g.sealed[h.Hook] = h.Plugins
	}
	for _, id := range dist.RequiredRows {
		g.required[id] = true
	}
	for _, r := range c.Rows {
		g.rows[r.ID] = r
		if r.Required {
			g.required[r.ID] = true
		}
		if a := installedBundle(r); a != "" {
			g.feeds[a] = append(g.feeds[a], r.Plugin)
		}
	}
	// Before the rebuild the module table holds no entry of an installed in-process
	// plugin, and the rows of its bundle are judged on its own manifest (ADR-060 decision
	// 17); resolve still reads the module table the binary runs.
	maps.Copy(g.mods, in.Mods)
	for id := range g.feeds {
		if _, built := in.Mods[id]; !built && g.subject(id, true).err == nil {
			g.mods[id] = g.subject(id, true).man.Entry()
		}
	}
	open := notEvaluated(in)
	res := GateResult{Entry: in.Entry, Slots: make([]Slot, NumChecks)}
	for i, info := range GateChecks() {
		n := strconv.Itoa(info.N)
		s := Slot{CheckInfo: info, Reason: open[n]}
		s.Evaluated = info.Until == "" && s.Reason == ""
		for _, k := range slices.Sorted(maps.Keys(open)) {
			if clause, ok := strings.CutPrefix(k, n+" "); ok {
				s.Clauses = append(s.Clauses, clause+": "+open[k])
			}
		}
		res.Slots[i] = s
	}
	// A failure of a clause that the entry point does not evaluate is not evaluated
	// for that row, with the reason (ADR-020 decision 30, reading 3).
	add := func(fs []RowResult) {
		for _, f := range fs {
			s := &res.Slots[f.Check-1]
			switch why := open[clauseOf(f)]; {
			case !s.Evaluated:
			case why != "":
				f.Status, f.Reason, f.Stop = StatusNotEvaluated, why, false
				s.Open = append(s.Open, f)
			default:
				s.Fails = append(s.Fails, f)
			}
		}
	}
	// The rows of an installed artifact are evaluated disabled too: install, plugin check
	// and plugin gate --against hold them so (ADR-060 decision 17).
	for _, r := range c.Rows {
		res.Rows = append(res.Rows, r.ID)
		if !r.Enabled && installedBundle(r) == "" {
			res.Unevaluated = append(res.Unevaluated, r.ID)
			continue
		}
		if f, ok := g.resolve(r.Row); !ok && r.Enabled {
			res.Unresolved = append(res.Unresolved, f)
			continue
		}
		add(g.classes(r.Row))
		if r.Plugin != pluginGroup && r.Plugin != pluginInclude {
			add(g.checks(r))
		}
	}
	for _, r := range c.Rows {
		add(g.layers(r))
	}
	add(g.lastProvider(c, dist))
	add(g.installed(c))
	res.Rows = append(slices.Concat(res.Rows, c.Clashes), slices.Collect(maps.Keys(c.Unread))...)
	sort.Strings(res.Rows)
	res.finish()
	return res
}

func (res *GateResult) finish() {
	detached, inactive := map[string]bool{}, map[string]bool{}
	res.Pass = len(res.Unresolved) == 0
	account := func(f RowResult, set map[string]bool) {
		res.Pass = false
		if f.Stop {
			res.Refused = true
		} else {
			set[f.Row] = true
		}
	}
	for _, f := range res.Unresolved {
		account(f, inactive)
	}
	for i := range res.Slots {
		s := &res.Slots[i]
		sort.SliceStable(s.Fails, func(a, b int) bool { return s.Fails[a].Row < s.Fails[b].Row })
		for _, f := range s.Fails {
			account(f, detached)
		}
	}
	res.Detached, res.Inactive = sortedKeys(detached), sortedKeys(inactive)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type gate struct {
	in       GateInput
	mods     ModuleTable         // the module table, and each installed in-process plugin it does not hold yet
	provider map[string][]string // who dist.yaml allows, by key or hook
	consumer map[string][]string
	sealed   map[string][]string
	pb, cb   map[string]bool // provider-bound and consumer-bound keys (classesOf)
	sh       map[string]bool // sealed hooks
	required map[string]bool
	rows     map[string]ComposedRow
	mounted  map[string]bool     // enabled with every group above it, in any role (ActiveRows)
	subjects map[string]*subject // by plugin id, or by "artifact " and the artifact's id
	feeds    map[string][]string // the plugins of the rows of each installed artifact
	signed   map[string]bool     // the keys and hooks that served data puts in the signed class
	faults   []binaryFault       // the denials of every row that the signed manifest decides (binaryFaults)
	min      string              // the profile's minStability
}

// classesOf returns, by name, the provider-bound and the consumer-bound keys and the
// sealed hooks, from their definitions (kernel.KeyClasses, kernel.HookSealed and
// HookUse.Sealed; ADR-059 decision 20): the keys of the root and of the definitions boot
// imports, the hooks of the kernel and the loader, and the keys and hooks that the
// plugins of the module table provide, inject and listen on. Where the module table
// holds no Go value, as at profile render, the classes come from the definition data
// that serves of the signed distribution manifest carries (ADR-060 decision 14), a
// signed key counting as consumer-bound as kernel.KeyClasses reports it. The definition
// test holds the two equal. The bindings list only who is allowed.
//
// ponytail: a key or hook that no definition names is bound or sealed when the
// bindings list who may hold it, as the kernel takes a binding of a bound key and a
// listener list of a sealed hook only; the signed manifest carries every class.
func classesOf(mods ModuleTable, dist DistManifest, d *Distribution) (pb, cb, sh map[string]bool) {
	pb, cb, sh = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, b := range dist.Providers {
		pb[b.Key] = true
	}
	for _, b := range dist.Consumers {
		cb[b.Key] = true
	}
	for _, h := range dist.SealedHooks {
		sh[h.Hook] = true
	}
	defined := map[string]bool{}
	key := func(n string, p, c bool) {
		if !defined[n] { // a definition replaces what dist.yaml implied
			defined[n], pb[n], cb[n] = true, false, false
		}
		pb[n], cb[n] = pb[n] || p, cb[n] || c
	}
	for _, sd := range servedDefinitions(d) {
		for n, kc := range sd.Classes {
			key(n, kc.Provider == "bound", kc.Consumer != "open")
		}
		for h, sv := range sd.Hooks {
			sh[h] = sv.Access == "sealed"
		}
	}
	keys := knownKeys()
	for _, e := range mods {
		if p, ok := e.Plugin.(kernel.Plugin); ok {
			keys = append(keys, p.Provides...)
			for _, d := range p.Inject {
				keys = append(keys, d.Key())
			}
			for _, u := range p.Hooks {
				sh[u.Hook()] = u.Sealed()
			}
		}
	}
	for _, k := range keys {
		if p, c, ok := kernel.KeyClasses(k); ok {
			key(k.Name(), p, c)
		}
	}
	for _, h := range kernelHooks() {
		sh[h.Name()], _ = kernel.HookSealed(h)
	}
	return pb, cb, sh
}

func bindingSet(bs []Binding) map[string][]string {
	m := make(map[string][]string, len(bs))
	for _, b := range bs {
		m[b.Key] = b.Plugins
	}
	return m
}

func (g *gate) fail(check int, r Row, rule Rule, stop bool, format string, a ...any) RowResult {
	return RowResult{Check: check, Row: r.ID, Status: StatusFail, Rule: rule,
		Stop: stop || g.required[r.ID], Reason: fmt.Sprintf(format, a...)}
}

// resolve looks the plugin of an enabled row up in the module table. The two
// core plugins are built into the loader (plugin-system-2026-11.md §6). A
// remote plugin is not read before S2b, so such a row is refused here with that
// reason (03-c3-kernel.md:357).
func (g *gate) resolve(r Row) (RowResult, bool) {
	if r.Plugin == pluginGroup || r.Plugin == pluginInclude {
		return RowResult{}, true
	}
	if _, ok := g.in.Mods[r.Plugin]; ok {
		return RowResult{}, true
	}
	f := g.fail(checkResolve, r, RuleModuleMissing, false,
		"plugin %q is not in the module table (a remote plugin is not read before S2b)", r.Plugin)
	return f, false
}

// classes is gate check 7 for one enabled row whose plugin resolved: the
// provider and consumer bindings, Want, isolate and sealed hooks. A launch key is
// provider-bound to the launcher, which is no row, so dist.yaml lists no provider.
func (g *gate) classes(r Row) []RowResult {
	var out []RowResult
	const c = CheckKeyHookClass
	e := g.mods[r.Plugin]
	for _, k := range e.Provides {
		b := g.provider[k]
		switch {
		case isRootKey(k):
			out = append(out, g.fail(c, r, RuleProviderBinding, false,
				"row %q provides key %q, which the root provides before any row mounts (03-c3-kernel.md:62-63)", r.ID, k))
		case g.pb[k] && !inSet(r.Plugin, b...):
			out = append(out, g.fail(c, r, RuleProviderBinding, false,
				"row %q provides provider-bound key %q and plugin %q is not in its binding [%s]", r.ID, k, r.Plugin, quoted(b)))
		}
	}
	// A Want resolves through the consumer binding as a Need does.
	for _, k := range union(e.Needs, e.Wants, r.Inject) {
		if b := g.consumer[k]; g.cb[k] && !inSet(r.Plugin, b...) {
			out = append(out, g.fail(c, r, RuleConsumerBinding, false,
				"row %q injects consumer-bound key %q and plugin %q is not in its binding [%s]", r.ID, k, r.Plugin, quoted(b)))
		}
	}
	for _, k := range e.Wants {
		if g.pb[k] {
			out = append(out, g.fail(c, r, RuleWantBound, false,
				"row %q wants provider-bound key %q: Want is refused on a provider-bound key", r.ID, k))
		}
	}
	for _, k := range r.Isolate {
		b := g.provider[k]
		switch {
		case !g.pb[k]:
		case !inSet(r.Plugin, b...):
			out = append(out, g.fail(c, r, RuleIsolateBound, true,
				"row %q isolates provider-bound key %q and plugin %q is not its bound provider", r.ID, k, r.Plugin))
		case g.required[r.ID]:
			out = append(out, g.fail(c, r, RuleIsolateBound, true,
				"required row %q isolates provider-bound key %q: isolate is never set on a required row", r.ID, k))
		}
	}
	for _, h := range e.Hooks {
		if l := g.sealed[h.Hook]; g.sh[h.Hook] && !inSet(r.Plugin, l...) {
			out = append(out, g.fail(c, r, RuleSealedHook, false,
				"row %q listens on sealed hook %q and plugin %q is not in its listener list [%s]", r.ID, h.Hook, r.Plugin, quoted(l)))
		}
	}
	return out
}

// patched reports whether a field of a row was last set by a layer item other
// than the insert of the row, and the origin that set it.
func patched(r ComposedRow, f FieldName) (Origin, bool) {
	o, _ := r.Provenance.Of(f)
	ins, _ := r.Provenance.Of(FieldID)
	return o, o != ins
}

// layers is gate check 8 for one row: the per-layer table of
// 08-profiles-roadmap-adrs.md:141-148 as far as S1 data reaches.
func (g *gate) layers(r ComposedRow) []RowResult {
	var out []RowResult
	const c = CheckLayerRules
	e := g.mods[r.Plugin]
	boundProvider := false
	for _, k := range e.Provides {
		if g.pb[k] && inSet(r.Plugin, g.provider[k]...) {
			boundProvider = true
		}
	}
	if g.required[r.ID] || boundProvider {
		rule, what := RuleRequiredRow, "a required row"
		if !g.required[r.ID] {
			rule, what = RuleBoundProvider, "a provider of a bound key"
		}
		// The inject, isolate and group of a group reach the rows it holds, so a
		// patch of a group above the row counts as a patch of the row.
		for _, f := range []FieldName{FieldInject, FieldIsolate, FieldGroup} {
			for a, ok := r, true; ok; a, ok = g.rows[a.Parent] {
				if o, p := patched(a, f); p {
					out = append(out, g.fail(c, r.Row, rule, boundProvider,
						"row %q: %s is never patched on %s or a group that holds it, and layer %d (%s, %s:%d) patched it on row %q", r.ID, f, what, o.Layer, o.Source, o.File, o.Line, a.ID))
					break
				}
			}
		}
	}
	// Compose keeps the origin of each intercept key apart (interceptField): only
	// bundle layers add, change or remove the intercept of a provider-bound key.
	for _, fo := range r.Provenance.Fields {
		k, ok := strings.CutPrefix(string(fo.Field), string(interceptField("")))
		if ok && g.pb[k] && fo.Origin.Layer != 1 {
			out = append(out, g.fail(c, r.Row, RuleInterceptBound, false,
				"row %q: the intercept of provider-bound key %q is set by layer %d (%s), and only bundle layers may", r.ID, k, fo.Origin.Layer, fo.Origin.Source))
		}
	}
	return out
}

// lastProvider finds a provider-bound key whose candidate providers are all
// disabled: the last enabled provider was disabled (08-profiles-roadmap-adrs.md:145).
// A provider in a disabled group is not mounted, so it counts as disabled. A key
// with no candidate row is not composed by this profile and is fine. The keys of
// config-refs are the launcher's to require (ADR-059 decision 18, RequiredFor): with no
// config-refs mounted a row that holds a reference stays PENDING and a live generation
// that holds one is rejected, so this rule never refuses disabling it.
func (g *gate) lastProvider(c Composition, dist DistManifest) []RowResult {
	var out []RowResult
	for _, b := range dist.Providers {
		if !g.pb[b.Key] || b.Key == configRefsReady || b.Key == configRefsDryRun {
			continue
		}
		var cands []ComposedRow
		enabled := false
		for _, r := range c.Rows {
			if inSet(b.Key, g.mods[r.Plugin].Provides...) && inSet(r.Plugin, b.Plugins...) {
				cands = append(cands, r)
				enabled = enabled || g.mounted[r.ID]
			}
		}
		if enabled {
			continue
		}
		for _, r := range cands {
			out = append(out, g.fail(CheckLayerRules, r.Row, RuleLastProvider, true,
				"row %q is a provider of bound key %q and every provider of it is disabled: the last enabled provider cannot be disabled", r.ID, b.Key))
		}
	}
	return out
}

// union returns the sorted union of string sets.
func union(sets ...[]string) []string {
	m := map[string]bool{}
	for _, set := range sets {
		for _, s := range set {
			m[s] = true
		}
	}
	return sortedKeys(m)
}
