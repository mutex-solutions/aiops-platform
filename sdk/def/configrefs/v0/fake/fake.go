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
type DryRun struct{ resolves func(launch.Ref) bool }

var _ configrefs.DryRun = DryRun{}

// New returns the dry run, in which a reference resolves when resolves says so, and
// the plugin that provides it under configrefs.Key. Each call returns a new plugin
// value, a new entry of its own.
func New(resolves func(launch.Ref) bool) (DryRun, kernel.Plugin) {
	f := DryRun{resolves: resolves}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/config-refs-fake",
		Provides: []kernel.AnyKey{configrefs.Key},
		Apply:    func(c *kernel.Context, _ any) error { return configrefs.Key.Provide(c, f) },
	})
}

// Check walks maps and lists and fails at the first reference that does not resolve.
func (f DryRun) Check(ctx context.Context, config any) error {
	var items []any
	switch v := config.(type) {
	case launch.Ref:
		if !f.resolves(v) {
			return fmt.Errorf("invalid config: reference ${%s:%s}: %w", v.Scheme, v.Name, configrefs.ErrUnresolved)
		}
	case map[string]any:
		for _, e := range v {
			items = append(items, e)
		}
	case []any:
		items = v
	}
	for _, e := range items {
		if err := f.Check(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
