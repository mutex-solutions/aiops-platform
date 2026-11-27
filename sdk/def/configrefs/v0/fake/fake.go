// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory dry run of the configrefs definition: a reference
// resolves when the test's predicate says so (07-c4-plugin-sdk.md:1410). It imports
// the standard library, sdk/kernel and definitions only.
package fake

import (
	"context"
	"fmt"

	configrefs "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// DryRun is a configrefs.DryRun over a predicate.
type DryRun struct {
	resolves func(launch.Ref) bool
	std      context.Context // of the activation that provides it; nil in the value New returns
}

var _ configrefs.DryRun = DryRun{}

// New returns the dry run in which every reference resolves, and its plugin. It
// takes no argument, so the generated cycle test mounts it (ADR-060 decision 12).
func New() (DryRun, kernel.Plugin) { return NewWith(func(launch.Ref) bool { return true }) }

// NewWith returns the dry run, in which a reference resolves when resolves says so,
// and the plugin that provides it under configrefs.Key. Each call returns a new
// plugin value, a new entry of its own. Each activation provides a dry run of its
// own, which is refused once that activation unloads.
func NewWith(resolves func(launch.Ref) bool) (DryRun, kernel.Plugin) {
	f := DryRun{resolves: resolves}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/config-refs-fake",
		Provides: []kernel.AnyKey{configrefs.Key},
		Apply: func(c *kernel.Context, _ any) error {
			// The served value has the methods of configrefs.DryRun only: Inventory is the test's.
			return configrefs.Key.Provide(c, struct{ configrefs.DryRun }{DryRun{resolves: resolves, std: c.Std()}})
		},
	})
}

// Inventory is empty: a dry run leaves nothing behind (ADR-060 decision 12).
func (DryRun) Inventory() []string { return nil }

// Check walks maps and lists and fails at the first reference that does not resolve.
// When each one resolves, it calls decode with the tree in which every reference is
// replaced by its name, the value the fake resolves it to. A dry run that the plugin
// provided returns an error that matches kernel.ErrInactive once the activation that
// provided it unloads.
func (f DryRun) Check(_ context.Context, config any, decode func(any) error) error {
	if f.std != nil && f.std.Err() != nil {
		return fmt.Errorf("config-refs fake: dry run after release: %w", kernel.ErrInactive)
	}
	resolved, err := f.resolve(config)
	if err != nil || decode == nil {
		return err
	}
	return decode(resolved)
}

func (f DryRun) resolve(v any) (any, error) {
	switch x := v.(type) {
	case launch.Ref:
		if !f.resolves(x) {
			return nil, fmt.Errorf("invalid config: reference ${%s:%s}: %w", x.Scheme, x.Name, configrefs.ErrUnresolved)
		}
		return x.Name, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			r, err := f.resolve(e)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			r, err := f.resolve(e)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}
	return v, nil
}
