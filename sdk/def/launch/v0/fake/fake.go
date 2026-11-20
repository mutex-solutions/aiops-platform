// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory launcher of the launch definition: one value that
// serves all six launch keys, scripted by the test (07-c4-plugin-sdk.md:1410;
// 03-c3-kernel.md:81 "Launch services are definitions with fakes, so reload,
// readiness and config-refs mount alone in a test"). It imports the standard
// library, sdk/kernel and its definition only.
package fake

import (
	"context"
	"slices"
	"sync"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Launcher serves every launch key. Set the exported fields before the plugin is
// mounted; they are read, never written, by the services. The func fields script
// Compose, Admit and Plan, and a nil one returns the zero value and no error.
type Launcher struct {
	ProfileName string
	RoleName    string
	LayerList   []launch.Layer
	Modules     []launch.CatalogEntry
	Plugins     map[string]kernel.Plugin // by plugin id, for Catalog.Plugin
	Argv        []string
	Flags       map[string]string
	Switches    map[string]bool

	ComposeFn func(context.Context) (launch.Composition, error)
	AdmitFn   func(context.Context, launch.Composition) (launch.Admission, error)
	PlanFn    func(context.Context, launch.Composition, launch.Composition) (launch.Plan, error)

	mu    sync.Mutex
	ready bool
	exits []launch.ExitRequest
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
// map to stand for the launcher.
func New() (*Launcher, kernel.Plugin) {
	f := &Launcher{}
	return f, kernel.Define(kernel.Plugin{
		ID: "aiops/launch-fake",
		Provides: []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey,
			launch.ReadyKey, launch.KillSwitchesKey, launch.AppExitKey},
		Apply: func(c *kernel.Context, _ any) error {
			for _, err := range []error{
				launch.ProfileContextKey.Provide(c, f), launch.CatalogKey.Provide(c, f),
				launch.CmdlineKey.Provide(c, f), launch.ReadyKey.Provide(c, f),
				launch.KillSwitchesKey.Provide(c, f), launch.AppExitKey.Provide(c, f),
			} {
				if err != nil {
					return err
				}
			}
			return nil
		},
	})
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

func (f *Launcher) Compose(ctx context.Context) (launch.Composition, error) {
	if f.ComposeFn == nil {
		return launch.Composition{}, nil
	}
	return f.ComposeFn(ctx)
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

func (f *Launcher) Plugin(id string) (kernel.Plugin, bool) {
	p, ok := f.Plugins[id]
	return p, ok
}

// Cmdline.

func (f *Launcher) Args() []string { return slices.Clone(f.Argv) }

func (f *Launcher) Flag(name string) (string, bool) {
	v, ok := f.Flags[name]
	return v, ok
}

// Ready.

func (f *Launcher) Set()  { f.setReady(true) }
func (f *Launcher) Drop() { f.setReady(false) }

func (f *Launcher) setReady(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = v
}

func (f *Launcher) IsReady() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ready
}

// KillSwitches.

func (f *Launcher) On(name string) bool { return f.Switches[name] }

// AppExit.

func (f *Launcher) RequestExit(r launch.ExitRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exits = append(f.exits, r)
}
