// SPDX-License-Identifier: Apache-2.0

// Package dist is the distribution of the production server binary cmd/aiops: the
// module table as Go values, the part of the distribution manifest the launcher
// reads (dist.yaml) and the launcher's limits (limits.yaml)
// (07-c4-plugin-sdk.md:1339, :1355; 08-profiles-roadmap-adrs.md:1233).
//
// At S1 the module table is written by hand; the distribution builder that
// generates it is S2a (08-profiles-roadmap-adrs.md:402). There is no init()
// registry: Registry builds the table when called, from the list in modules below.
//
// This package, like cmd/aiops, must never import a test plugin, a fake or
// sdk/testkit (07-c4-plugin-sdk.md:47, :564). test/dist is the separate
// distribution that links the toy and fault plugins.
package dist

import (
	_ "embed" // dist.yaml and limits.yaml are embedded
	"fmt"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/loader"
	"github.com/mutex-solutions/aiops-platform/plugins/clock"
	configrefs "github.com/mutex-solutions/aiops-platform/plugins/config-refs"
	"github.com/mutex-solutions/aiops-platform/plugins/readiness"
	"github.com/mutex-solutions/aiops-platform/plugins/reload"
	restartpolicy "github.com/mutex-solutions/aiops-platform/plugins/restart-policy"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

//go:embed dist.yaml
var distYAML []byte

//go:embed limits.yaml
var limitsYAML []byte

// Module is the declared data of one plugin of the module table that its kernel.Plugin
// does not carry. Provides, Needs, Wants and Hooks of the entry are read from the
// plugin; Run checks the entry against it.
type Module struct {
	Plugin   kernel.Plugin
	Volatile []string // config paths a change commits in place (ADR-059 decision 10)
}

// Entry returns the boot.ModuleEntry of the module, with the module path and version
// of the repository's own root module.
func (m Module) Entry() boot.ModuleEntry {
	e := boot.ModuleEntry{ID: m.Plugin.ID, Module: ModulePath, Version: Version, Plugin: m.Plugin, Volatile: m.Volatile}
	for _, k := range m.Plugin.Provides {
		e.Provides = append(e.Provides, k.Name())
	}
	for _, u := range m.Plugin.Hooks {
		// The ordinal of the stage is the definition's; the plugin value carries only its
		// name, so the dump orders by plugin id until S2a generates the table.
		e.Hooks = append(e.Hooks, boot.HookDecl{Hook: u.Hook(), StageName: u.Stage()})
	}
	for _, d := range m.Plugin.Inject {
		if d.Optional() {
			e.Wants = append(e.Wants, d.Key().Name())
		} else {
			e.Needs = append(e.Needs, d.Key().Name())
		}
	}
	return e
}

// ModulePath and Version stand in for the module data that the builder will read from
// go.mod and the lock file.
const (
	ModulePath = "github.com/mutex-solutions/aiops-platform"
	Version    = "v0.0.0-s1"
)

// Table builds the module table and the registry around it from a list of modules.
// The test distribution calls it with this list plus its own.
func Table(mods ...Module) boot.Registry {
	t := boot.ModuleTable{}
	for _, m := range mods {
		t[m.Plugin.ID] = m.Entry()
	}
	return boot.Registry{
		Modules: t,
		Hooks: map[string]kernel.AnyHook{
			loaderdef.PatchContext.Name(): loaderdef.PatchContext, loaderdef.EntryInit.Name(): loaderdef.EntryInit,
			loaderdef.VolatileUpdate.Name(): loaderdef.VolatileUpdate,
		},
		LoaderConfig: func(rows []loaderdef.EntrySpec, batch boot.BatchFunc) any {
			return loader.Config{Rows: rows, Batch: batch}
		},
	}
}

// Modules is the compiled-in list of plugins of the release distribution. ADD A
// PLUGIN HERE: one Module per plugin, and its bindings in dist.yaml. At S1 the list
// holds the loader and the five core rows of S1; the others arrive with their own
// steps. The bindings of each are in dist.yaml.
func Modules() []Module {
	return []Module{
		{Plugin: loader.New()},
		{Plugin: clock.Plugin},
		{Plugin: configrefs.Plugin},
		{Plugin: readiness.Plugin},
		{Plugin: reload.Plugin},
		{Plugin: restartpolicy.Plugin},
	}
}

// Load returns the registry, the distribution manifest and the limits of the
// release distribution.
func Load() (boot.Registry, boot.DistManifest, boot.Limits, error) {
	man, err := boot.ParseDist("dist.yaml", distYAML)
	if err != nil {
		return boot.Registry{}, boot.DistManifest{}, boot.Limits{}, fmt.Errorf("dist: %w", err)
	}
	lim, err := boot.ParseLimits("limits.yaml", limitsYAML)
	if err != nil {
		return boot.Registry{}, boot.DistManifest{}, boot.Limits{}, fmt.Errorf("dist: %w", err)
	}
	return Table(Modules()...), man, lim, nil
}
