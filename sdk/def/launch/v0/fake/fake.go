// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory launcher of the launch definition: one value that
// serves all six launch keys, scripted by the test (07-c4-plugin-sdk.md:1410;
// 03-c3-kernel.md:81 "Launch services are definitions with fakes, so reload,
// readiness and config-refs mount alone in a test"). It imports the standard
// library, sdk/kernel and its definition only.
package fake

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Launcher serves every launch key. Set the exported fields before the plugin is
// mounted; the services read them and write only Current. The func fields script
// Compose, Admit, Plan and Commit, and a nil one returns the zero value and no error.
//
// ProfileContext keeps the rules of boot's: Commit takes only a composition that
// Compose returned, the committed one or one committed before, and Required is
// Needed, the distribution's set, plus the rows of Current marked required, both
// filtered by the role as boot.RequiredFor does (ADR-059 decision 15). RequiredOf
// applies the same rule to a composition that Commit would take.
type Launcher struct {
	ProfileName string
	RoleName    string
	LayerList   []launch.Layer
	Modules     []launch.CatalogEntry
	Keys        map[string]kernel.AnyKey // by key name, for Catalog.Key
	Argv        []string
	Flags       map[string]string
	Switches    map[string]bool
	Environment map[string]string  // the environment of the process: Cmdline.Env serves its names with launch.EnvPrefix, Environ lists every one
	In          io.Reader          // for Cmdline.Stdin
	Out, ErrOut io.Writer          // for Cmdline.Stdout and Stderr
	WorkDir     string             // for Cmdline.Dir
	Current     launch.Composition // for ProfileContext.Committed; Commit sets it
	Box         launch.Toolbox     // what ProfileContext.Toolbox returns; nil answers ErrNotCarried from every part
	Needed      launch.RequiredSet // the distribution's required rows and keys, for ProfileContext.Required

	ComposeFn func(context.Context) (launch.Composition, error)
	AdmitFn   func(context.Context, launch.Composition) (launch.Admission, error)
	PlanFn    func(context.Context, launch.Composition, launch.Composition) (launch.Plan, error)
	CommitFn  func(launch.Composition) error // a non-nil error refuses the commit, before the digest is checked

	mu     sync.Mutex
	ready  bool
	ended  bool // End was called: Set does nothing
	boot   sync.Once
	booted chan struct{}
	exit   sync.Once
	exited chan struct{}
	exits  []launch.ExitRequest
	known  map[launch.Digest]bool // composed or replaced by a commit
}

var (
	_ launch.ProfileContext = (*Launcher)(nil)
	_ launch.Catalog        = (*Launcher)(nil)
	_ launch.Cmdline        = (*Launcher)(nil)
	_ launch.Ready          = (*Launcher)(nil)
	_ launch.KillSwitches   = (*Launcher)(nil)
	_ launch.AppExit        = (*Launcher)(nil)
)

// New returns an empty launcher and the plugin that provides the six launch keys.
// Each call returns a new plugin value, which a test lists in the provider binding
// map to stand for the launcher. Each key serves a value with the methods of its own
// interface only: Exits is the test's (no side door, 07-c4-plugin-sdk.md §4.3). The
// profileContext of an activation refuses its calls once that activation is
// released, as served.
func New() (*Launcher, kernel.Plugin) {
	f := &Launcher{known: map[launch.Digest]bool{}, booted: make(chan struct{}), exited: make(chan struct{})}
	return f, kernel.Define(kernel.Plugin{
		ID: "aiops/launch-fake",
		Provides: []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey,
			launch.ReadyKey, launch.KillSwitchesKey, launch.AppExitKey},
		Apply: func(c *kernel.Context, _ any) error {
			for _, err := range []error{
				launch.ProfileContextKey.Provide(c, served{f, c.Std()}),
				launch.CatalogKey.Provide(c, struct{ launch.Catalog }{f}),
				launch.CmdlineKey.Provide(c, struct{ launch.Cmdline }{f}),
				launch.ReadyKey.Provide(c, struct{ launch.Ready }{f}),
				launch.KillSwitchesKey.Provide(c, struct{ launch.KillSwitches }{f}),
				launch.AppExitKey.Provide(c, struct{ launch.AppExit }{f}),
			} {
				if err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// served is what an activation provides under profileContext: the profileContext,
// whose methods with an error result return kernel.ErrInactive once that activation
// is released, as the launcher's do (07-c4-plugin-sdk.md:518).
type served struct {
	launch.ProfileContext
	std context.Context
}

func (s served) Compose(ctx context.Context) (launch.Composition, error) {
	if s.std.Err() != nil {
		return launch.Composition{}, kernel.ErrInactive
	}
	return s.ProfileContext.Compose(ctx)
}

func (s served) Admit(ctx context.Context, c launch.Composition) (launch.Admission, error) {
	if s.std.Err() != nil {
		return launch.Admission{}, kernel.ErrInactive
	}
	return s.ProfileContext.Admit(ctx, c)
}

func (s served) Plan(ctx context.Context, from, to launch.Composition) (launch.Plan, error) {
	if s.std.Err() != nil {
		return launch.Plan{}, kernel.ErrInactive
	}
	return s.ProfileContext.Plan(ctx, from, to)
}

func (s served) Commit(c launch.Composition) error {
	if s.std.Err() != nil {
		return kernel.ErrInactive
	}
	return s.ProfileContext.Commit(c)
}

func (s served) RequiredOf(c launch.Composition) (launch.RequiredSet, error) {
	if s.std.Err() != nil {
		return launch.RequiredSet{}, kernel.ErrInactive
	}
	return s.ProfileContext.RequiredOf(c)
}

// Exits returns the exit requests received so far, oldest first.
func (f *Launcher) Exits() []launch.ExitRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.exits)
}

// ProfileContext.

func (f *Launcher) Profile() string        { return f.ProfileName }
func (f *Launcher) Role() string           { return f.RoleName }
func (f *Launcher) Layers() []launch.Layer { return slices.Clone(f.LayerList) }

// Compose returns what ComposeFn does and remembers it, so that Commit takes it.
func (f *Launcher) Compose(ctx context.Context) (launch.Composition, error) {
	var c launch.Composition
	var err error
	if f.ComposeFn != nil {
		c, err = f.ComposeFn(ctx)
	}
	if err == nil {
		f.remember(c.Digest)
	}
	return c, err
}

func (f *Launcher) remember(d launch.Digest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.known[d] = true
}

func (f *Launcher) Admit(ctx context.Context, c launch.Composition) (launch.Admission, error) {
	if f.AdmitFn == nil {
		return launch.Admission{}, nil
	}
	return f.AdmitFn(ctx, c)
}

func (f *Launcher) Plan(ctx context.Context, from, to launch.Composition) (launch.Plan, error) {
	if f.PlanFn == nil {
		return launch.Plan{}, nil
	}
	return f.PlanFn(ctx, from, to)
}

func (f *Launcher) Committed() launch.Composition {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Current
}

// Commit makes c the committed composition, unless CommitFn refuses it or its digest
// is neither Current's nor one that Compose returned or Commit replaced.
func (f *Launcher) Commit(c launch.Composition) error {
	if f.CommitFn != nil {
		if err := f.CommitFn(c); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.knownLocked(c); err != nil {
		return err
	}
	f.known[f.Current.Digest], f.Current = true, c
	return nil
}

// knownLocked is the refusal of a composition that Commit does not take. f.mu is held.
func (f *Launcher) knownLocked(c launch.Composition) error {
	if c.Digest != f.Current.Digest && !f.known[c.Digest] {
		return fmt.Errorf("composition %x was not composed by this launcher (or was forgotten)", c.Digest[:6])
	}
	return nil
}

// Toolbox returns Box, or with none a toolbox whose every part answers ErrNotCarried.
func (f *Launcher) Toolbox() launch.Toolbox {
	if f.Box != nil {
		return f.Box
	}
	return notCarried{}
}

// notCarried is the toolbox of a fake that was given none.
type notCarried struct{ launch.NoTools }

func (notCarried) Render(context.Context, launch.RenderRequest) (launch.Rendered, error) {
	return launch.Rendered{}, launch.ErrNotCarried
}

func (notCarried) Gate(context.Context, launch.GateRequest) (launch.Verdict, error) {
	return launch.Verdict{}, launch.ErrNotCarried
}

func (notCarried) ReadManifest(context.Context, launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	return launch.ManifestSummary{}, nil, launch.ErrNotCarried
}

func (notCarried) Shipped(context.Context) (map[string][]byte, error) {
	return nil, launch.ErrNotCarried
}

func (notCarried) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{}, launch.ErrNotCarried
}

// Required is RequiredOf(Current).
func (f *Launcher) Required() launch.RequiredSet { return f.requiredOf(f.Committed()) }

// RequiredOf is the required set of c, a composition that Commit would take.
func (f *Launcher) RequiredOf(c launch.Composition) (launch.RequiredSet, error) {
	f.mu.Lock()
	err := f.knownLocked(c)
	f.mu.Unlock()
	if err != nil {
		return launch.RequiredSet{}, err
	}
	return f.requiredOf(c), nil
}

// requiredOf is Needed plus the rows of cur marked required, sorted. For a role other
// than RoleAll ("" is RoleAll) a row of cur whose roles do not name it drops out, and
// so does a key whose provider rows in cur all run elsewhere.
func (f *Launcher) requiredOf(cur launch.Composition) launch.RequiredSet {
	runs := func(r launch.Row) bool {
		return f.RoleName == "" || f.RoleName == launch.RoleAll || len(r.Roles) == 0 || slices.Contains(r.Roles, f.RoleName)
	}
	rows := slices.Clone(f.Needed.Rows)
	for _, r := range cur.Rows {
		if r.Required {
			rows = append(rows, r.ID)
		}
	}
	rows = slices.DeleteFunc(rows, func(id string) bool {
		i := slices.IndexFunc(cur.Rows, func(r launch.Row) bool { return r.ID == id })
		return i >= 0 && !runs(cur.Rows[i])
	})
	keys := slices.DeleteFunc(slices.Clone(f.Needed.Keys), func(k string) bool {
		providers, local := 0, 0
		for _, r := range cur.Rows {
			if e, _ := f.Lookup(r.Plugin); slices.Contains(e.Provides, k) {
				providers++
				if runs(r) {
					local++
				}
			}
		}
		return providers > 0 && local == 0
	})
	slices.Sort(rows)
	slices.Sort(keys)
	return launch.RequiredSet{Rows: slices.Compact(rows), Keys: keys}
}

// Catalog.

func (f *Launcher) Entries() []launch.CatalogEntry { return slices.Clone(f.Modules) }

func (f *Launcher) Lookup(id string) (launch.CatalogEntry, bool) {
	for _, e := range f.Modules {
		if e.ID == id {
			return e, true
		}
	}
	return launch.CatalogEntry{}, false
}

func (f *Launcher) Key(name string) (kernel.AnyKey, bool) {
	k, ok := f.Keys[name]
	return k, ok
}

// Cmdline.

func (f *Launcher) Args() []string { return slices.Clone(f.Argv) }

func (f *Launcher) Flag(name string) (string, bool) {
	v, ok := f.Flags[name]
	return v, ok
}

// Env serves Environ for a name with launch.EnvPrefix; any other name is not set.
func (f *Launcher) Env(name string) (string, bool) {
	if !strings.HasPrefix(name, launch.EnvPrefix) {
		return "", false
	}
	v, ok := f.Environment[name]
	return v, ok
}

func (f *Launcher) Stdin() io.Reader  { return f.In }
func (f *Launcher) Stdout() io.Writer { return f.Out }
func (f *Launcher) Stderr() io.Writer { return f.ErrOut }
func (f *Launcher) Dir() string       { return f.WorkDir }

// Environ lists Environment as "NAME=value", sorted.
func (f *Launcher) Environ() []string {
	var out []string
	for n, v := range f.Environment {
		out = append(out, n+"="+v)
	}
	slices.Sort(out)
	return out
}

// Ready.

func (f *Launcher) Set()  { f.setReady(true, false) }
func (f *Launcher) Drop() { f.setReady(false, false) }

// End ends the latch, as the launcher does when the process stops: it drops it, and
// Set does nothing from then on.
func (f *Launcher) End() { f.setReady(false, true) }

func (f *Launcher) setReady(v, end bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = f.ended || end
	f.ready = v && !f.ended
}

func (f *Launcher) IsReady() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ready
}

// Boot closes the channel of Booted, as the launcher does once the tree has settled;
// a second call changes nothing.
func (f *Launcher) Boot() { f.boot.Do(func() { close(f.booted) }) }

func (f *Launcher) Booted() <-chan struct{} { return f.booted }

// Inventory lists what the callers left behind: "ready" while the latch is set
// (ADR-060 decision 12).
func (f *Launcher) Inventory() []string {
	if f.IsReady() {
		return []string{"ready"}
	}
	return nil
}

// KillSwitches.

func (f *Launcher) On(name string) bool { return f.Switches[name] }

// AppExit.

func (f *Launcher) RequestExit(r launch.ExitRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exits = append(f.exits, r)
	f.exit.Do(func() { close(f.exited) })
}

// ExitRequested is closed at the first exit request, so a test waits for one without
// polling Exits.
func (f *Launcher) ExitRequested() <-chan struct{} { return f.exited }
