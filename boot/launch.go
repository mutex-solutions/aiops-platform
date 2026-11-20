package boot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The six launch services of the prepare step (03-c3-kernel.md:63, :360;
// 07-c4-plugin-sdk.md:1359). Each key is provider-bound to the launcher, which
// provides it from the root context before any row mounts.

// ConfigRefsID is the plugin id of config-refs, the row that resolves references.
const ConfigRefsID = "aiops/config-refs"

// configRefsKey is the one key of the launcher that is not a launch service: it is
// available once config-refs is ACTIVE, and every row whose config holds a reference
// has it as a row-level inject (refsInject), so that no such row is applied before
// its references can resolve. It is provider-bound, so only the launcher provides it.
var configRefsKey = kernel.NewKey[struct{}]("configRefsReady", 0, kernel.ProviderBound())

// rootKeyNames are the keys that the root provides: the six launch keys, the four
// built-ins of the kernel and configRefsKey. No row provides them and none waits for
// them: the static graph leaves them out of the needs of a row (builder.needs) and
// the run-time audit counts them provided.
func rootKeyNames() []string {
	return []string{
		launch.ProfileContextKey.Name(), launch.CatalogKey.Name(), launch.CmdlineKey.Name(),
		launch.ReadyKey.Name(), launch.KillSwitchesKey.Name(), launch.AppExitKey.Name(),
		kernel.ReflectKey.Name(), kernel.RegistryKey.Name(), kernel.HooksKey.Name(), kernel.LoggerKey.Name(),
		configRefsKey.Name(),
	}
}

func isRootKey(name string) bool { return slices.Contains(rootKeyNames(), name) }

// refsInject returns the inject of a row for the loader: its own, and configRefsReady
// when its config holds a reference. config-refs itself never waits for itself.
func refsInject(r ComposedRow) []string {
	if len(r.Refs) == 0 || r.Plugin == ConfigRefsID || slices.Contains(r.Inject, configRefsKey.Name()) {
		return slices.Clone(r.Inject)
	}
	return slices.Sorted(slices.Values(append(slices.Clone(r.Inject), configRefsKey.Name())))
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
	if _, ok := p.known[c.Digest]; ok {
		return
	}
	p.known[c.Digest] = c
	p.order = append(p.order, c.Digest)
	if len(p.order) > maxKnown {
		delete(p.known, p.order[0])
		p.order = p.order[1:]
	}
}

// lookup finds a composition by digest. A composition that is looked up again is the
// newest, so the one that reload plans from is not forgotten while it is in use.
func (p *profileContext) lookup(d launch.Digest) (Composition, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.committed.Digest == Digest(d) {
		return p.committed, nil
	}
	if c, ok := p.known[Digest(d)]; ok {
		i := slices.Index(p.order, c.Digest) // a known digest is always in order
		p.order = append(slices.Delete(p.order, i, i+1), c.Digest)
		return c, nil
	}
	return Composition{}, fmt.Errorf("composition %x was not composed by this launcher (or was forgotten)", d[:6])
}

// Committed is the composition the process booted. At S1 nothing commits a later one:
// reload applies and judges a generation and keeps its own record of what it applied
// (ADR-059 decision 5; the committer is S4a), so this does not move.
func (p *profileContext) Committed() launch.Composition {
	p.mu.Lock()
	defer p.mu.Unlock()
	return launchComposition(p.committed)
}

// Required is the required set of the committed composition for the role. The loader
// is not a row of the composition it serves, so its id is left out: boot's own mount
// of it is checked at boot, and readiness would otherwise wait for a row it cannot
// see.
func (p *profileContext) Required() launch.RequiredSet {
	p.mu.Lock()
	c := WithLoader(p.committed)
	p.mu.Unlock()
	set := RequiredFor(c, p.mods, p.dist, p.roles)
	return launch.RequiredSet{Rows: slices.DeleteFunc(set.Rows, func(id string) bool { return id == LoaderRowID }), Keys: set.Keys}
}

func (p *profileContext) Compose(ctx context.Context) (launch.Composition, error) {
	if err := ctx.Err(); err != nil {
		return launch.Composition{}, err
	}
	c, _, err := Compose(p.src, p.opt)
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
	c, err := p.lookup(lc.Digest)
	if err != nil {
		return launch.Admission{}, err
	}
	a := Admit(WithLoader(c), p.mods, p.dist, p.roles)
	if a.Refused() {
		return launch.Admission{}, errors.New(strings.Join(errorLines(a), "; "))
	}
	var out launch.Admission
	for _, id := range a.Gate.Rows {
		if d, ok := a.Gate.Denial(id); ok {
			out.Denied = append(out.Denied, launch.Denial{Row: id, Check: checkName(d.Check), Reason: d.Reason})
		}
	}
	return out, nil
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
	f, err := p.lookup(from.Digest)
	if err != nil {
		return launch.Plan{}, err
	}
	t, err := p.lookup(to.Digest)
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
	for _, k := range []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey, launch.ReadyKey,
		launch.KillSwitchesKey, launch.AppExitKey, configRefsKey, loaderdef.Key, composition.Key, kernel.ReflectKey,
		kernel.RegistryKey, kernel.HooksKey, kernel.LoggerKey} {
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
// goes to changed, which the launcher publishes to the outside.
type latch struct {
	on      atomic.Bool
	changed func(bool)
}

var _ launch.Ready = (*latch)(nil)

func (l *latch) set(v bool) {
	if l.on.Swap(v) != v {
		l.changed(v)
	}
}

func (l *latch) Set()          { l.set(true) }
func (l *latch) Drop()         { l.set(false) }
func (l *latch) IsReady() bool { return l.on.Load() }

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
