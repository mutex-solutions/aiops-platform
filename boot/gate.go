package boot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The gate: admission of a composition (03-c3-kernel.md:357, "Admit"). It has
// ten named slots in the order of the table at
// 08-profiles-roadmap-adrs.md:495-506 (ADR-059 decision 4). It reads the
// composition, the module table and the distribution manifest, plus the keys
// the root provides and the classes of keys and hooks that their definitions
// declare (classesOf), which no distribution changes, and runs no plugin code. Before the ten checks it resolves each
// enabled row's plugin (03-c3-kernel.md:357).
//
// A result is data. Per check and per row it is pass, fail with a reason, or
// not evaluated until the step that builds the check. What a failure means
// follows the failure matrix (08-profiles-roadmap-adrs.md:466-471):
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

// Check numbers of the gate, as in the table at
// 08-profiles-roadmap-adrs.md:497-506.
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
	untilS1            = ""
	untilS2a           = "S2a"
	untilS2b           = "S2b"
	reasonNotEvaluated = "not evaluated until "
)

// CheckInfo names one slot. Until is the roadmap step that builds the check,
// empty when the slot reads S1 data. Partial lists what an implemented slot
// does not read yet.
type CheckInfo struct {
	N       int
	Name    string
	Until   string
	Partial string
}

// GateChecks returns the ten slots in order.
func GateChecks() []CheckInfo {
	return []CheckInfo{
		{CheckManifest, "Manifest schema and author API", untilS2a, ""},
		{CheckDefinitions, "Definition versions", untilS2a, ""},
		{CheckRemoteProto, "Remote protocol", untilS2b, ""},
		{CheckStability, "Stability", untilS2a, ""},
		{CheckTrust, "Trust", untilS2a, ""},
		{CheckLicence, "Licence", untilS2a, ""},
		{CheckKeyHookClass, "Key and hook classes", untilS1,
			"signed keys and hooks and fakes in a server graph need trust data (S2a)"},
		{CheckLayerRules, "Layer rules", untilS1,
			"the insert-only bundle of an installed artifact is S2a; the tenant overlay is S4b"},
		{CheckRemoteScope, "Remote scope", untilS2b, ""},
		{CheckAttestation, "Attestation", untilS2a, ""},
	}
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

// Slot is one of the ten checks with its failures. An evaluated slot lists
// only its failures: a row of GateResult.Rows with no failure passed.
type Slot struct {
	CheckInfo
	Evaluated bool
	Fails     []RowResult
}

// GateResult is the data the gate returns.
//
// Pass is true when no check failed on any row. Refused is true when boot
// must stop: a row that is required, or a failure marked Stop. Detached lists
// the optional rows denied by a check, which the launcher's copy disables;
// Inactive lists the optional rows whose plugin is not in the module table.
type GateResult struct {
	Slots      []Slot
	Rows       []string
	Unresolved []RowResult
	Pass       bool
	Refused    bool
	Detached   []string
	Inactive   []string
}

// Result returns the result of one check on one row: pass or fail with a
// reason for an evaluated slot, not evaluated for the others. Check 0 is the
// module table lookup. An unknown row or check is not evaluated.
func (g GateResult) Result(check int, row string) RowResult {
	r := RowResult{Check: check, Row: row, Status: StatusNotEvaluated}
	var fails []RowResult
	switch {
	case check == checkResolve:
		fails = g.Unresolved
	case check >= 1 && check <= NumChecks:
		s := g.Slots[check-1]
		if !s.Evaluated {
			r.Until = s.Until
			r.Reason = reasonNotEvaluated + s.Until
			return r
		}
		fails = s.Fails
	default:
		return r
	}
	if i := sort.SearchStrings(g.Rows, row); i == len(g.Rows) || g.Rows[i] != row {
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
	out := Composition{Rows: append([]ComposedRow(nil), c.Rows...), Digest: c.Digest}
	for i, r := range out.Rows {
		if _, denied := g.Denial(r.ID); denied {
			out.Rows[i].Enabled = false
		}
	}
	return out
}

// Gate runs the ten checks on a composition. See the file comment.
func Gate(c Composition, mods ModuleTable, dist DistManifest) GateResult {
	g := &gate{
		mods:     mods,
		provider: bindingSet(dist.Providers),
		consumer: bindingSet(dist.Consumers),
		sealed:   map[string][]string{},
		required: map[string]bool{},
		rows:     map[string]ComposedRow{},
		mounted:  ActiveRows(c, nil),
	}
	g.pb, g.cb, g.sh = classesOf(mods, dist)
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
	}
	res := GateResult{Slots: make([]Slot, NumChecks)}
	for i, info := range GateChecks() {
		res.Slots[i] = Slot{CheckInfo: info, Evaluated: info.Until == untilS1}
	}
	for _, r := range c.Rows {
		res.Rows = append(res.Rows, r.ID)
		if !r.Enabled {
			continue
		}
		if f, ok := g.resolve(r.Row); !ok {
			res.Unresolved = append(res.Unresolved, f)
			continue
		}
		res.Slots[CheckKeyHookClass-1].Fails = append(res.Slots[CheckKeyHookClass-1].Fails, g.classes(r.Row)...)
	}
	for _, r := range c.Rows {
		res.Slots[CheckLayerRules-1].Fails = append(res.Slots[CheckLayerRules-1].Fails, g.layers(r)...)
	}
	res.Slots[CheckLayerRules-1].Fails = append(res.Slots[CheckLayerRules-1].Fails, g.lastProvider(c, dist)...)
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
	mods     ModuleTable
	provider map[string][]string // who dist.yaml allows, by key or hook
	consumer map[string][]string
	sealed   map[string][]string
	pb, cb   map[string]bool // provider-bound and consumer-bound keys (classesOf)
	sh       map[string]bool // sealed hooks
	required map[string]bool
	rows     map[string]ComposedRow
	mounted  map[string]bool // enabled with every group above it, in any role (ActiveRows)
}

// classesOf returns, by name, the provider-bound and the consumer-bound keys and the
// sealed hooks, from their definitions (kernel.KeyClasses, kernel.HookSealed; ADR-059
// decision 20): the keys of the root and of the definitions boot imports, the hooks of
// the kernel and the loader, and the keys that the plugins of the module table provide
// and inject. dist.yaml lists only who is allowed. A key or hook whose definition is
// not at hand (a module table read from modules.yaml has no Go values, and a plugin's
// HookUse names its hook only) is bound or sealed when dist.yaml lists who may hold it,
// as the kernel takes a binding of a bound key and a listener list of a sealed hook only.
func classesOf(mods ModuleTable, dist DistManifest) (pb, cb, sh map[string]bool) {
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
	keys := knownKeys()
	for _, e := range mods {
		if p, ok := e.Plugin.(kernel.Plugin); ok {
			keys = append(keys, p.Provides...)
			for _, d := range p.Inject {
				keys = append(keys, d.Key())
			}
		}
	}
	defined := map[string]bool{}
	for _, k := range keys {
		p, c, ok := kernel.KeyClasses(k)
		if !ok {
			continue
		}
		n := k.Name()
		if !defined[n] { // a definition replaces what dist.yaml implied
			defined[n], pb[n], cb[n] = true, false, false
		}
		pb[n], cb[n] = pb[n] || p, cb[n] || c
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
// core plugins are built into the loader (plugin-system-2026-11.md:260).
// Remote plugins and plugins.lock are not read before S2a, so such a row is
// refused here with that reason (03-c3-kernel.md:357).
func (g *gate) resolve(r Row) (RowResult, bool) {
	if r.Plugin == pluginGroup || r.Plugin == pluginInclude {
		return RowResult{}, true
	}
	if _, ok := g.mods[r.Plugin]; ok {
		return RowResult{}, true
	}
	f := g.fail(checkResolve, r, RuleModuleMissing, false,
		"plugin %q is not in the module table (a remote plugin and plugins.lock are not read before S2a)", r.Plugin)
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
// config-refs are the launcher's to require (ADR-059 decision 18): with no config-refs
// mounted a row that holds a reference stays PENDING and a live generation that holds
// one is rejected, so disabling it is an optional choice.
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
