package boot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The six launch services of the prepare step (03-c3-kernel.md:63, :360;
// 07-c4-plugin-sdk.md:1359). Each key is provider-bound to the launcher, which
// provides it from the root context before any row mounts.

// ConfigRefsID is the plugin id of config-refs, the row that resolves references.
const ConfigRefsID = "aiops/config-refs"

// configRefsReady names the key that config-refs provides once its listener is on
// internal/config (ADR-059, "configRefsReady"). Every row whose config holds a reference has
// it as a row-level inject (refsInject), so no such row is applied before its
// references can resolve, and each unloads in the owner turn in which config-refs
// quiesces. The static graph sees the same need: the restart closure of config-refs
// holds the rows with references, and where a required row of the role holds one the
// launcher adds config-refs and the key to the required set (RequiredFor; ADR-059
// decision 18), and so it does where an active provider of a required key in the root
// realm holds one (decision 24). With no config-refs in the module table the catalog
// still has the key, which nothing provides, so such a row stays PENDING.
const configRefsReady = "configRefsReady"

// configRefsDryRun names the key of the dry run of references that config-refs
// provides (sdk/def/configrefs/v0; ADR-059 decision 16). boot imports no definition but
// launch, loader and composition (07-c4-plugin-sdk.md:37), so it names the key.
const configRefsDryRun = "configRefsDryRun"

// rootKeyNames are the keys that the root provides: the six launch keys and the four
// built-ins of the kernel. No row provides them and none waits for them: the static
// graph leaves them out of the needs of a row (builder.needs) and the run-time audit
// counts them provided.
func rootKeyNames() []string {
	return append(launchKeyNames(), kernel.ReflectKey.Name(), kernel.RegistryKey.Name(), kernel.HooksKey.Name(), kernel.LoggerKey.Name())
}

// launchKeyNames are the six launch keys, which the definition makes provider-bound
// (to the launcher, which is no row), whatever dist.yaml lists.
func launchKeyNames() []string {
	return []string{launch.ProfileContextKey.Name(), launch.CatalogKey.Name(), launch.CmdlineKey.Name(),
		launch.ReadyKey.Name(), launch.KillSwitchesKey.Name(), launch.AppExitKey.Name()}
}

func isRootKey(name string) bool { return slices.Contains(rootKeyNames(), name) }

// refsInject returns the inject of a row: its own, and configRefsReady when its config
// holds a reference. config-refs itself never waits for itself.
func refsInject(r ComposedRow) []string {
	if len(r.Refs) == 0 || r.Plugin == ConfigRefsID || slices.Contains(r.Inject, configRefsReady) {
		return slices.Clone(r.Inject)
	}
	return slices.Sorted(slices.Values(append(slices.Clone(r.Inject), configRefsReady)))
}

// profileContext is the profileContext service. Compose reads the layer files
// again, so it sees a file that changed since boot; Admit and Plan work on
// compositions that this launcher composed, which it remembers by digest: the
// definition's Composition holds plain values and has no provenance, which gate
// check 8 reads (08-profiles-roadmap-adrs.md:507). A composition it never made is
// refused.
type profileContext struct {
	profile, role string
	src           Sources
	opt           Options
	mods          ModuleTable
	dist          DistManifest
	roles         []Role
	root          *kernel.Root // read for config-refs's dry run; set before the services are provided

	mu        sync.Mutex
	committed Composition
	known     map[Digest]Composition
	order     []Digest
}

// maxKnown bounds the compositions remembered besides the committed one.
const maxKnown = 16

var _ launch.ProfileContext = (*profileContext)(nil)

func (p *profileContext) Profile() string { return p.profile }
func (p *profileContext) Role() string    { return p.role }

func (p *profileContext) Layers() []launch.Layer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return layersOf(p.committed)
}

func (p *profileContext) remember(c Composition) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rememberLocked(c)
}

// rememberLocked keeps c as the composition of its digest, replacing an older one: the
// digest leaves provenance out, and the newest compose is the one to judge.
func (p *profileContext) rememberLocked(c Composition) {
	if i := slices.Index(p.order, c.Digest); i >= 0 {
		p.order = slices.Delete(p.order, i, i+1)
	}
	p.known[c.Digest] = c
	p.order = append(p.order, c.Digest)
	if len(p.order) > maxKnown {
		delete(p.known, p.order[0])
		p.order = p.order[1:]
	}
}

// lookup finds the composition lc stands for. Two can share a digest: the committed one,
// which is the launcher's admitted copy, and the newest that Compose returned, which may
// enable a row the copy has detached or carry other provenance. Admit judges the newest
// (newest); Plan and Commit take the committed one when lc is it, row for row. A
// composition that is looked up again is the newest, so the one that reload plans from
// is not forgotten while it is in use.
func (p *profileContext) lookup(lc launch.Composition, newest bool) (Composition, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := Digest(lc.Digest)
	k, known := p.known[d]
	if known {
		p.rememberLocked(k)
	}
	switch {
	case p.committed.Digest == d && (!known || !newest && sameEnabled(lc, p.committed)):
		return p.committed, nil
	case known:
		return k, nil
	}
	return Composition{}, fmt.Errorf("composition %x was not composed by this launcher (or was forgotten)", d[:6])
}

// sameEnabled reports whether lc has the rows of c, each enabled as in c: the launcher's
// copy differs from what it was composed from in that only.
func sameEnabled(lc launch.Composition, c Composition) bool {
	return slices.EqualFunc(lc.Rows, c.Rows, func(a launch.Row, b ComposedRow) bool { return a.ID == b.ID && a.Enabled == b.Enabled })
}

// admitted is the launcher's copy of c, what the tree runs of it: every row the gate
// denies is disabled (GateResult.Admitted). The loader's row is not in it.
func (p *profileContext) admitted(c Composition) Composition {
	w := WithLoader(c)
	a := Gate(w, p.mods, p.dist).Admitted(w)
	a.Rows = a.Rows[1:]
	return a
}

// Committed is the composition the process runs: the one it booted until reload commits
// another (ADR-059 decision 15; the committer of S4a replaces the caller). It is the
// launcher's admitted copy, so a row the gate detached at boot is disabled in it and a
// rollback to it does not mount the row.
func (p *profileContext) Committed() launch.Composition {
	p.mu.Lock()
	defer p.mu.Unlock()
	return launchComposition(p.committed)
}

// Commit makes a composition that Compose returned the committed one, so that Committed,
// Layers and Required follow what the tree runs. The composition it replaces is
// remembered, so that a rollback can commit it again.
func (p *profileContext) Commit(lc launch.Composition) error {
	c, err := p.lookup(lc, false)
	if err != nil {
		return err
	}
	c = p.admitted(c)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.committed.Digest != c.Digest {
		p.rememberLocked(p.committed)
	}
	p.committed = c
	return nil
}

// Required is the required set of the committed composition for the role. The loader
// is not a row of the composition it serves, so its id is left out: boot's own mount
// of it is checked at boot, and readiness would otherwise wait for a row it cannot
// see.
func (p *profileContext) Required() launch.RequiredSet {
	p.mu.Lock()
	c := p.committed
	p.mu.Unlock()
	return p.requiredOf(c)
}

// RequiredOf is the required set of a composition that Commit would take.
func (p *profileContext) RequiredOf(lc launch.Composition) (launch.RequiredSet, error) {
	c, err := p.lookup(lc, false)
	if err != nil {
		return launch.RequiredSet{}, err
	}
	return p.requiredOf(c), nil
}

func (p *profileContext) requiredOf(c Composition) launch.RequiredSet {
	set := RequiredFor(WithLoader(c), p.mods, p.dist, p.roles)
	return launch.RequiredSet{Rows: slices.DeleteFunc(set.Rows, func(id string) bool { return id == LoaderRowID }), Keys: set.Keys}
}

func (p *profileContext) Compose(ctx context.Context) (launch.Composition, error) {
	if err := ctx.Err(); err != nil {
		return launch.Composition{}, err
	}
	c, _, err := Compose(p.src, p.opt)
	if err == nil {
		err = CheckReserved(c)
	}
	if err != nil {
		return launch.Composition{}, err
	}
	p.remember(c)
	return launchComposition(c), nil
}

func (p *profileContext) Admit(ctx context.Context, lc launch.Composition) (launch.Admission, error) {
	if err := ctx.Err(); err != nil {
		return launch.Admission{}, err
	}
	c, err := p.lookup(lc, true)
	if err != nil {
		return launch.Admission{}, err
	}
	a := Admit(WithLoader(c), p.mods, p.dist, p.roles)
	// Every denied row is listed, a required one too: its denial is also an error that
	// rejects the generation, and reload reports either as ErrDenied (ADR-059 decision 15).
	var out launch.Admission
	for _, id := range a.Gate.Rows {
		if d, ok := a.Gate.Denial(id); ok {
			out.Denied = append(out.Denied, launch.Denial{Row: id, Check: checkName(d.Check), Reason: d.Reason})
		}
	}
	errs := p.configErrors(ctx, c)
	if lines := errorLines(a); len(lines) > 0 {
		errs = append([]error{errors.New(strings.Join(lines, "; "))}, errs...)
	}
	return out, errors.Join(errs...)
}

// dryRun is the dry run that config-refs provides under configRefsDryRun
// (sdk/def/configrefs/v0 DryRun), by its method set.
type dryRun interface {
	Check(ctx context.Context, config any, decode func(any) error) error
}

// configErrors checks the config of every row that the generation c creates, enables
// or changes in any field, and of the rows held by a group it replaces (changedRows;
// 03-c3-kernel.md:377, "every changed config"; ADR-059 decision 16). A config with no
// reference is decoded with the plugin's own Decode. One that holds a reference is
// checked by the dry run of the config-refs row ACTIVE in the root realm, which resolves
// it as it would for the mount, decodes the resolved tree with the plugin's Decode and
// returns no value. With no such row, or when c disables, removes or mounts config-refs
// again, so that the tree would resolve the reference with another instance or none, it
// is refused with launch.ErrRefsUnchecked. The error of a dry run is wrapped, so it still
// matches the definition's ErrUnresolved.
func (p *profileContext) configErrors(ctx context.Context, c Composition) []error {
	p.mu.Lock()
	old := p.committed
	p.mu.Unlock()
	changed, remounted := changedRows(old, c, p.roles)
	moves := refsMove(old, c, p.roles, remounted)
	var dr dryRun
	looked := false
	var out []error
	for _, r := range c.Rows {
		if !changed[r.ID] {
			continue
		}
		var decode func(any) error
		if pl, _ := p.mods[r.Plugin].Plugin.(kernel.Plugin); pl.Decode != nil {
			decode = func(v any) error {
				if err := decodes(pl.Decode, v); err != nil {
					return fmt.Errorf("config: %w", err)
				}
				return nil
			}
		}
		var err error
		switch cfg := plain(r.Config); {
		case len(r.Refs) == 0 && decode != nil:
			err = decode(cfg)
		case len(r.Refs) == 0:
		case moves:
			err = fmt.Errorf("config-refs changes in this generation: %w", launch.ErrRefsUnchecked)
		default:
			if !looked {
				dr, looked = p.configRefs(ctx), true
			}
			err = fmt.Errorf("config-refs is not ACTIVE: %w", launch.ErrRefsUnchecked)
			if dr != nil {
				err = dr.Check(ctx, cfg, decode)
			}
		}
		if err != nil {
			out = append(out, fmt.Errorf("row %q: %w", r.ID, err))
		}
	}
	return out
}

// refsMove reports whether a generation from old to nw disables, removes or mounts again
// a config-refs row (remounted), or makes one: the running one cannot answer for the
// references of such a generation.
func refsMove(old, nw Composition, roles []Role, remounted map[string]bool) bool {
	was, act := ActiveRows(old, roles), ActiveRows(nw, roles)
	return slices.ContainsFunc(slices.Concat(old.Rows, nw.Rows), func(r ComposedRow) bool {
		return r.Plugin == ConfigRefsID && (was[r.ID] != act[r.ID] || remounted[r.ID])
	})
}

// configRefs returns the dry run of the config-refs row while it is ACTIVE in the root
// realm, or nil. The launcher holds the root, which resolves no key, so it reads the
// provision: Root.Inspect copies out the value of a plain Provide.
func (p *profileContext) configRefs(ctx context.Context) dryRun {
	ins, _ := p.root.Inspect(ctx) // an error lists nothing: no dry run, so a reference is refused
	for _, pv := range ins.Provisions {
		name, realm, _ := strings.Cut(pv.Key, "@")
		if d, ok := pv.Value.(dryRun); ok && pv.Active && pv.Plugin == ConfigRefsID && name == configRefsDryRun && !strings.Contains(realm, "/") {
			return d
		}
	}
	return nil
}

// changedRows returns the active rows of nw that a generation from old creates, enables
// or changes in any field (changed), and of those the ones it mounts with a row other
// than the one that runs (remounted): created, enabled, changed in a field other than
// required and roles, or held by a group that is, which the loader replaces with its
// rows. A row held by a remounted group is changed too.
func changedRows(old, nw Composition, roles []Role) (changed, remounted map[string]bool) {
	was, act, before := ActiveRows(old, roles), ActiveRows(nw, roles), indexRows(old)
	changed, remounted = map[string]bool{}, map[string]bool{}
	for _, r := range nw.Rows {
		fields := changedFields(before[r.ID].Row, r.Row)
		mounts := slices.ContainsFunc(fields, func(f FieldName) bool { return f != FieldRequired && f != FieldRoles })
		remounted[r.ID] = act[r.ID] && (!was[r.ID] || mounts || remounted[r.Parent])
		changed[r.ID] = remounted[r.ID] || act[r.ID] && len(fields) > 0
	}
	return changed, remounted
}

// decodes runs a Decode as the kernel does: a panic is an error.
func decodes(decode func(any) (any, error), raw any) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("Decode panicked: %v", v)
		}
	}()
	_, err = decode(raw)
	return err
}

func checkName(n int) string {
	if n == checkResolve {
		return "module table"
	}
	return fmt.Sprintf("%d %s", n, GateChecks()[n-1].Name)
}

// errorLines are the lines of an admission that stop boot.
func errorLines(a Admission) []string {
	var out []string
	for _, l := range a.Diagnostics() {
		if rest, ok := strings.CutPrefix(l, "error: "); ok {
			out = append(out, rest)
		}
	}
	return out
}

func (p *profileContext) Plan(ctx context.Context, from, to launch.Composition) (launch.Plan, error) {
	if err := ctx.Err(); err != nil {
		return launch.Plan{}, err
	}
	f, err := p.lookup(from, false)
	if err != nil {
		return launch.Plan{}, err
	}
	t, err := p.lookup(to, false)
	if err != nil {
		return launch.Plan{}, err
	}
	f, t = WithLoader(f), WithLoader(t)
	fa, ta := Gate(f, p.mods, p.dist).Admitted(f), Gate(t, p.mods, p.dist).Admitted(t)
	g, _, _ := BuildGraph(ta, p.mods, p.dist, p.roles)
	return launchPlan(Diff(fa, ta, g)), nil
}

// catalog is the catalog service: the module table, its Go values and the key
// table. All of it is read-only after construction.
type catalog struct {
	entries []launch.CatalogEntry
	byID    map[string]launch.CatalogEntry
	plugins map[string]kernel.Plugin
	keys    map[string]kernel.AnyKey
}

var _ launch.Catalog = (*catalog)(nil)

func (c *catalog) Entries() []launch.CatalogEntry { return slices.Clone(c.entries) }

func (c *catalog) Lookup(id string) (launch.CatalogEntry, bool) {
	e, ok := c.byID[id]
	return e, ok
}

func (c *catalog) Plugin(id string) (kernel.Plugin, bool) {
	p, ok := c.plugins[id]
	return p, ok
}

func (c *catalog) Key(name string) (kernel.AnyKey, bool) {
	k, ok := c.keys[name]
	return k, ok
}

// newCatalog builds the catalog from a registry. It checks that every entry of the
// module table carries a defined kernel.Plugin whose own declarations agree with
// the entry's (the static graph reads the entry and the kernel reads the plugin, so
// a disagreement would be silent), and builds the key table from the keys the
// plugins declare, the launch keys, the keys of the loader and composition
// definitions, the built-in keys of the kernel and Registry.Keys.
func newCatalog(reg Registry) (*catalog, error) {
	c := &catalog{byID: map[string]launch.CatalogEntry{}, plugins: map[string]kernel.Plugin{}, keys: map[string]kernel.AnyKey{}}
	var errs []error
	add := func(k kernel.AnyKey) {
		// A Dep holds the key's data and a Key[T] holds a wrapper of it: the two are told
		// apart by name and major, which is how the loader compares keys too.
		if old, ok := c.keys[k.Name()]; ok && old.Major() != k.Major() {
			errs = append(errs, fmt.Errorf("two keys are named %q, at majors %d and %d", k.Name(), old.Major(), k.Major()))
			return
		}
		c.keys[k.Name()] = k
	}
	for _, k := range knownKeys() {
		add(k)
	}
	for _, k := range reg.Keys {
		add(k)
	}
	for _, id := range sortedKeys2(reg.Modules) {
		e := reg.Modules[id]
		p, ok := e.Plugin.(kernel.Plugin)
		if !ok {
			errs = append(errs, fmt.Errorf("module %q: Plugin is %T, want a kernel.Plugin from kernel.Define", id, e.Plugin))
			continue
		}
		if p.ID != id {
			errs = append(errs, fmt.Errorf("module %q: its plugin declares the id %q", id, p.ID))
		}
		var provides, needs, wants []string
		for _, k := range p.Provides {
			add(k)
			provides = append(provides, k.Name())
		}
		for _, d := range p.Inject {
			add(d.Key())
			if d.Optional() {
				wants = append(wants, d.Key().Name())
			} else if !isRootKey(d.Key().Name()) {
				needs = append(needs, d.Key().Name())
			}
		}
		for _, f := range []struct {
			what       string
			plugin, ds []string
		}{{"provides", provides, e.Provides}, {"needs", needs, withoutRoot(e.Needs)}, {"wants", wants, e.Wants}} {
			if !sameSet(f.plugin, f.ds) {
				errs = append(errs, fmt.Errorf("module %q: the plugin %s %v and the module entry %s %v differ", id, f.what, sorted(f.plugin), f.what, sorted(f.ds)))
			}
		}
		for _, name := range slices.Concat(e.Provides, e.Needs, e.Wants) {
			if _, ok := c.keys[name]; !ok {
				errs = append(errs, fmt.Errorf("module %q: key %q is not in the key table (add it to Registry.Keys)", id, name))
			}
		}
		c.plugins[id] = p
		ce := launch.CatalogEntry{ID: id, Module: e.Module, Version: e.Version, Provides: slices.Clone(e.Provides),
			Needs: slices.Clone(e.Needs), Volatile: volatilePaths(e.Volatile)}
		c.entries = append(c.entries, ce)
		c.byID[id] = ce
	}
	if _, ok := c.keys[configRefsReady]; !ok { // no config-refs: a key nothing may provide
		c.keys[configRefsReady] = kernel.NewKey[struct{}](configRefsReady, 0, kernel.ProviderBound())
	}
	return c, errors.Join(errs...)
}

func sortedKeys2[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	sort.Strings(s)
	return s
}

func sameSet(a, b []string) bool { return slices.Equal(sorted(a), sorted(b)) }

func withoutRoot(in []string) []string {
	var out []string
	for _, s := range in {
		if !isRootKey(s) {
			out = append(out, s)
		}
	}
	return out
}

// cmdline is the cmdline service. Flag reads the flags the operator gave, plus the
// home as "home" whether it came from --home or from AIOPS_HOME, so a row can read
// where the process keeps its files (the documents name no flag but the references
// ${flag:name}, 03-c3-kernel.md:68).
type cmdline struct {
	args  []string
	flags map[string]string
	env   func(string) string
}

var _ launch.Cmdline = cmdline{}

func (c cmdline) Args() []string { return slices.Clone(c.args) }

func (c cmdline) Flag(name string) (string, bool) {
	v, ok := c.flags[name]
	return v, ok
}

// Env reads the environment of the process: a variable that is empty is not set. It
// filters nothing; the AIOPS_REF_ prefix rule is config-refs's
// (08-profiles-roadmap-adrs.md:162).
func (c cmdline) Env(name string) (string, bool) {
	v := c.env(name)
	return v, v != ""
}

// latch is the ready service: a flag that readiness sets when the required set is
// ACTIVE and drops while a provider restarts (03-c3-kernel.md:67). Every change
// goes to changed, which the launcher publishes to the outside, under the latch's
// lock, so that the outside reads what the latch holds. end drops it for good: after
// a leak, and once the process stops, readiness has ended (ADR-059 decision 13) and
// the readiness row's next Set changes nothing. changed must not call IsReady.
type latch struct {
	mu        sync.Mutex
	on, ended bool
	changed   func(bool)
}

var _ launch.Ready = (*latch)(nil)

func (l *latch) set(v, end bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ended = l.ended || end
	if l.on != v && !(v && l.ended) {
		l.on = v
		l.changed(v)
	}
}

func (l *latch) Set()  { l.set(true, false) }
func (l *latch) Drop() { l.set(false, false) }
func (l *latch) end()  { l.set(false, true) }

func (l *latch) IsReady() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.on
}

// switches is the killSwitches service, layer 6, read once at boot from the
// environment (08-profiles-roadmap-adrs.md:111, :126; ReadKillSwitches).
type switches KillSwitches

var _ launch.KillSwitches = switches{}

func (s switches) On(name string) bool {
	switch name {
	case launch.AgentsPaused:
		return s.AgentsPaused
	case launch.TelemetryDisabled:
		return s.TelemetryDisabled
	}
	return false
}

// appExit is the appExit service. A request is not an exit: it is queued for the
// run loop, which owns the shutdown and the exit code (03-c3-kernel.md:63). The
// first request wins; later ones are dropped.
type appExit struct{ reqs chan launch.ExitRequest }

var _ launch.AppExit = appExit{}

func (a appExit) RequestExit(r launch.ExitRequest) {
	select {
	case a.reqs <- r:
	default:
	}
}
