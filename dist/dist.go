// SPDX-License-Identifier: Apache-2.0

// Package dist is the distribution of the production binaries cmd/aiops and
// cmd/aiopsctl, one module table for both (ADR-061 decision 24): the module table as Go
// values, the signed distribution manifest the binaries embed (distribution.yaml), the
// launcher's limits (limits.yaml) and the shipped bundles and profiles
// (07-c4-plugin-sdk.md:1339, :1355; 08-profiles-roadmap-adrs.md:1233). dist.yaml is
// the builder's input: the module list, the bindings and the required set of the
// release (ADR-060 decision 20); go run ./tools/plugingen --write . generates from it
// the module table, Modules in modules.go, each plugin with its manifest, which the root
// package embeds, and writes distribution.yaml from it and from the binary's package
// graph; make check fails when either differs. There is no init() registry: Table
// builds the table when called, from the list in Modules.
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

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/loader"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

//go:embed distribution.yaml
var signed []byte

//go:embed limits.yaml
var limitsYAML []byte

// shipped holds the bundles and the profiles of the release: aiops-core, aiops-cli and the
// cli profile (ADR-060 decision 27; ADR-061 decision 29).
//
//go:embed all:bundles all:profiles
var shipped embed.FS

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

// Shipped returns the bundles and profiles of the release, by their paths under the
// home: bundles/<name>/bundle.yaml and profiles/<name>/profile.yaml. A file of the home
// wins over a shipped one.
func Shipped() fs.FS { return shipped }

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
