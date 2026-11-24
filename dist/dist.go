// SPDX-License-Identifier: Apache-2.0

// Package dist is the distribution of the production server binary cmd/aiops: the
// module table as Go values, the signed distribution manifest the binary embeds
// (distribution.yaml), and the launcher's limits (limits.yaml)
// (07-c4-plugin-sdk.md:1339, :1355; 08-profiles-roadmap-adrs.md:1233). dist.yaml is
// the builder's input: the bindings and the required set of the release (ADR-060
// decision 20); go run ./tools/plugingen --write . writes distribution.yaml from it
// and from the binary's package graph, and make check fails when they differ.
//
// The module table is written by hand, each plugin with its manifest, which the root
// package embeds; the distribution builder of S2a generates it once it lands
// (08-profiles-roadmap-adrs.md:402; ADR-060 decision 20). There is no init()
// registry: Table builds the table when called, from the list in Modules below.
//
// This package, like cmd/aiops, must never import a test plugin, a fake or
// sdk/testkit (07-c4-plugin-sdk.md:47, :564). test/dist is the separate
// distribution that links the toy and fault plugins.
package dist

import (
	"embed" // distribution.yaml, limits.yaml, bundles/ and profiles/ are embedded
	"errors"
	"fmt"
	"io/fs"

	aiops "github.com/mutex-solutions/aiops-platform"
	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/loader"
	"github.com/mutex-solutions/aiops-platform/plugins/cli"
	"github.com/mutex-solutions/aiops-platform/plugins/clock"
	commanddist "github.com/mutex-solutions/aiops-platform/plugins/command-dist"
	commandkey "github.com/mutex-solutions/aiops-platform/plugins/command-key"
	commandplugin "github.com/mutex-solutions/aiops-platform/plugins/command-plugin"
	commandprofile "github.com/mutex-solutions/aiops-platform/plugins/command-profile"
	commandtrust "github.com/mutex-solutions/aiops-platform/plugins/command-trust"
	configrefs "github.com/mutex-solutions/aiops-platform/plugins/config-refs"
	"github.com/mutex-solutions/aiops-platform/plugins/readiness"
	"github.com/mutex-solutions/aiops-platform/plugins/reload"
	restartpolicy "github.com/mutex-solutions/aiops-platform/plugins/restart-policy"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

//go:embed distribution.yaml
var signed []byte

//go:embed limits.yaml
var limitsYAML []byte

// Module is one plugin of the module table: its value and its manifest, plugin.yaml
// (07-c4-plugin-sdk.md:1344).
type Module struct {
	Plugin   kernel.Plugin
	Manifest []byte
}

// Entry returns the boot.ModuleEntry of the module, read from its manifest with the
// gate's lenient reader (ADR-048 decisions 15 and 18): what it provides, uses and
// listens on, and the volatile paths of its configSchema, so no list of them is
// written by hand (ADR-060 decision 4). Boot takes the module of each entry from the
// signed distribution manifest. Run checks the entry against the plugin.
func (m Module) Entry() (boot.ModuleEntry, error) {
	man, err := boot.ReadManifest("plugin.yaml of "+m.Plugin.ID, m.Manifest, boot.ManifestLenient)
	if err != nil {
		return boot.ModuleEntry{}, err
	}
	e := man.Entry()
	e.Plugin = m.Plugin
	return e, nil
}

// Manifest returns dir/plugin.yaml of fsys, or nil, which Entry refuses.
func Manifest(fsys fs.FS, dir string) []byte {
	b, _ := fs.ReadFile(fsys, dir+"/plugin.yaml")
	return b
}

// ModulePath is the path of the repository's root module.
const ModulePath = "github.com/mutex-solutions/aiops-platform"

// Table builds the module table and the registry around it from a list of modules,
// each entry under the id of its manifest. The test distribution calls it with this
// list plus its own.
func Table(mods ...Module) (boot.Registry, error) {
	t := boot.ModuleTable{}
	var errs []error
	for _, m := range mods {
		e, err := m.Entry()
		errs = append(errs, err)
		t[e.ID] = e
	}
	if err := errors.Join(errs...); err != nil {
		return boot.Registry{}, err
	}
	return boot.Registry{
		Modules: t,
		Hooks: map[string]kernel.AnyHook{
			loaderdef.PatchContext.Name(): loaderdef.PatchContext, loaderdef.EntryInit.Name(): loaderdef.EntryInit,
			loaderdef.VolatileUpdate.Name(): loaderdef.VolatileUpdate,
		},
		LoaderConfig: func(rows []loaderdef.EntrySpec, batch boot.BatchFunc, plugins map[string]kernel.Plugin) any {
			return loader.Config{Rows: rows, Batch: batch, Plugins: plugins}
		},
	}, nil
}

// Modules is the compiled-in list of plugins of the release distribution. ADD A
// PLUGIN HERE: one Module per plugin, with the manifest beside its package, which the
// root package embeds, and its bindings in dist.yaml; then go run ./tools/plugingen
// --write . writes distribution.yaml again. The list holds the loader, the five
// core rows of S1, and the cli row and the command rows of aiops-cli (ADR-061
// decisions 24 and 30); the others arrive with their own steps.
func Modules() []Module {
	m := func(p kernel.Plugin, dir string) Module {
		return Module{Plugin: p, Manifest: Manifest(aiops.Manifests, dir)}
	}
	return []Module{
		m(loader.New(), "loader"),
		m(clock.Plugin, "plugins/clock"),
		m(configrefs.Plugin, "plugins/config-refs"),
		m(readiness.New(), "plugins/readiness"),
		m(reload.New(), "plugins/reload"),
		m(restartpolicy.New(), "plugins/restart-policy"),
		m(cli.Plugin, "plugins/cli"),
		m(commandprofile.Plugin, "plugins/command-profile"),
		m(commandplugin.Plugin, "plugins/command-plugin"),
		m(commandtrust.Plugin, "plugins/command-trust"),
		m(commanddist.Plugin, "plugins/command-dist"),
		m(commandkey.Plugin, "plugins/command-key"),
	}
}

// Shipped returns the bundles and profiles of the release. Not built yet: it holds none.
func Shipped() fs.FS { return embed.FS{} }

// Load returns the registry, the signed distribution manifest and the limits of the
// release distribution.
func Load() (boot.Registry, []byte, boot.Limits, error) {
	lim, err := boot.ParseLimits("limits.yaml", limitsYAML)
	if err != nil {
		return boot.Registry{}, nil, boot.Limits{}, fmt.Errorf("dist: %w", err)
	}
	reg, err := Table(Modules()...)
	if err != nil {
		return boot.Registry{}, nil, boot.Limits{}, fmt.Errorf("dist: %w", err)
	}
	return reg, signed, lim, nil
}
