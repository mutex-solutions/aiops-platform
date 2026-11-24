// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
)

// rootModule is the path of the root module, whose modules a release builds.
const rootModule = "github.com/mutex-solutions/aiops-platform"

// Manifest is what a signed distribution manifest records of one build (ADR-060
// decision 20): the package graph of the server's main package, the detected licence
// of each of its modules, the pins of the build, the release's bindings, what a site
// added to them, and the licences the site accepts.
type Manifest struct {
	Graph    []Package
	Licences map[string]string
	Pins     []boot.DistPin
	Release  boot.DistManifest
	Site     boot.DistManifest
	Accept   []string
}

// Write returns the manifest as boot reads it (boot.ReadDistribution): the module table
// is each package of the graph that holds a plugin.yaml, with that manifest byte for
// byte and the statement of a first-party plugin; it refuses an id that two packages
// declare, and an id under aiops/ or contrib/, the two owners reserved to the release
// pipeline, of a package outside the modules it builds (08:402); the modules are those
// of the graph with their detected licences, a module of the repository replaced by a
// directory with no version and no sum (ADR-020 decision 28); serves is plugingen's for
// the definitions in the graph; the pins are sorted, with the toolchain module's.
func (m Manifest) Write() ([]byte, error) {
	doc := distDoc{DistVersion: 1, Plugins: map[string]distPlugin{}, Bindings: bindingsDoc(m.Release), Packages: []string{},
		AcceptLicenses: slices.Sorted(slices.Values(m.Accept))}
	if s := bindingsDoc(m.Site); len(s.Providers)+len(s.Consumers)+len(s.SealedHooks)+len(s.Required.Keys)+len(s.Required.Rows) > 0 {
		doc.Site = &s
	}
	var defs []plugingen.Definition
	for _, p := range m.Graph {
		if p.Standard {
			continue
		}
		doc.Packages = append(doc.Packages, p.ImportPath)
		if src, err := os.ReadFile(filepath.Join(p.Dir, "definition.yaml")); err == nil {
			d, err := plugingen.ParseDefinition(p.ImportPath, src)
			if err != nil {
				return nil, err
			}
			defs = append(defs, d)
		}
		if src, err := os.ReadFile(filepath.Join(p.Dir, "plugin.yaml")); err == nil {
			man, err := boot.ReadManifest(p.ImportPath+"/plugin.yaml", src, boot.ManifestStrict)
			if err != nil {
				return nil, err
			}
			owner, _, _ := strings.Cut(man.ID, "/")
			switch have, dup := doc.Plugins[man.ID]; {
			case dup:
				return nil, fmt.Errorf("the id %s is declared by %s and by %s", man.ID, have.Package, p.ImportPath)
			case (owner == "aiops" || owner == "contrib") && !slices.Contains(sdkModules, p.Module.Path):
				return nil, fmt.Errorf("%s declares the id %s: the owner %s is reserved to the artifacts the release pipeline builds, and its module %s is outside the release (08:402)", p.ImportPath, man.ID, owner, p.Module.Path)
			}
			doc.Plugins[man.ID] = distPlugin{Package: p.ImportPath, Manifest: string(src), firstParty: p.Module.Path == rootModule}
		}
	}
	slices.Sort(doc.Packages)
	mods := modulesOf(m.Graph)
	for _, path := range sortedKeys(mods) {
		mod := mods[path]
		e := distModule{Path: path, Licence: m.Licences[path]}
		switch {
		case mod.Replace != nil && (path == rootModule || strings.HasPrefix(path, rootModule+"/")) && mod.Replace.Version == "":
			e.Replace = &goModule{Path: mod.Replace.Path}
		case mod.Replace != nil:
			return nil, fmt.Errorf("module %s is replaced outside this repository", path)
		default:
			e.Version, e.Sum = mod.Version, mod.Sum
		}
		if e.Licence == "" {
			return nil, fmt.Errorf("module %s has no detected licence", path)
		}
		doc.Modules = append(doc.Modules, e)
	}
	for _, p := range m.Pins {
		doc.Pins = append(doc.Pins, distPin{Path: p.Path, Version: p.Version, Sum: p.Sum, GoMod: p.GoMod, Release: p.Release, Unchecked: p.Unchecked})
	}
	slices.SortFunc(doc.Pins, func(a, b distPin) int { return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Version, b.Version)) })
	doc.Serves = plugingen.Serves(defs)
	for id, p := range doc.Plugins {
		if p.firstParty {
			st, err := statement(id, []byte(p.Manifest), doc.Serves)
			if err != nil {
				return nil, err
			}
			p.Attestation = st
			doc.Plugins[id] = p
		}
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Distribution returns the signed distribution manifest of the binary whose main
// package is main, built by g in the repository, with the bindings and the required set
// of the release input dist.yaml (ADR-060 decisions 20 and 28): the manifest that
// cmd/aiops and the test binary embed, which the parity check of make check holds to
// this function. Its licences are those the scan detects, and its pins the go.sum lines
// of the modules of the graph and the toolchain module's.
func Distribution(ctx context.Context, g Go, main string, input []byte) ([]byte, error) {
	release, err := boot.ParseDist("dist.yaml", input)
	if err != nil {
		return nil, err
	}
	pkgs, err := Graph(ctx, g, main)
	if err != nil {
		return nil, err
	}
	var linked []Package
	for _, p := range pkgs {
		if !p.Standard {
			linked = append(linked, p)
		}
	}
	l, err := ScanLicences(linked)
	if err != nil {
		return nil, err
	}
	m := Manifest{Graph: pkgs, Licences: l.Modules, Release: release,
		Pins: []boot.DistPin{{Path: Toolchain, Version: ToolchainVersion, Sum: ToolchainSum}}}
	for _, path := range sortedKeys(modulesOf(pkgs)) {
		if mod := modulesOf(pkgs)[path]; mod.Version != "" && mod.Replace == nil {
			m.Pins = append(m.Pins, boot.DistPin{Path: path, Version: mod.Version, Sum: mod.Sum, GoMod: mod.GoModSum})
		}
	}
	return m.Write()
}

// statement writes the conformance attestation of a first-party plugin of the module
// table: its subject is the digest of its manifest as the table holds it (ADR-020
// decision 9); it names every definition of its requires and the suite revision of each
// it provides or contributes to, with both halves where the revision is 1 or more
// (ADR-060 decisions 55 and 56), since CI runs the suites of every first-party plugin on
// the tree it builds, which the release pipeline's attestation replaces.
func statement(id string, manifest []byte, serves plugingen.ServesData) (string, error) {
	m, err := boot.ReadManifest(id, manifest, boot.ManifestStrict)
	if err != nil {
		return "", err
	}
	type def struct {
		Version string   `json:"version"`
		Suite   *int     `json:"suite,omitempty"`
		Halves  []string `json:"halves,omitempty"`
	}
	defs := map[string]def{}
	for name, vs := range m.Requires.Definitions {
		defs[name] = def{Version: vs[len(vs)-1]}
	}
	for name, served := range serves.Definitions {
		for _, sd := range served {
			for _, k := range append(m.Provides, m.Registers...) {
				if _, ok := sd.Classes[k.Key]; ok && fmt.Sprint(k.Major) == strings.SplitN(sd.Version, ".", 2)[0] {
					d := defs[name]
					d.Suite = &sd.Suite
					if sd.Suite > 0 {
						d.Halves = []string{"obligations", "guard"}
					}
					defs[name] = d
				}
			}
		}
	}
	sum := sha256.Sum256(manifest)
	st := map[string]any{"_type": "https://in-toto.io/Statement/v1", "predicateType": boot.AttestationType,
		"subject":   []any{map[string]any{"name": m.ID + "@" + m.Version, "digest": map[string]string{"sha256": hex.EncodeToString(sum[:])}}},
		"predicate": map[string]any{"definitions": defs}}
	b, err := json.Marshal(st)
	return string(b), err
}

// The document of Manifest.Write, in the field order of boot.Distribution.
type (
	distDoc struct {
		DistVersion    int                   `yaml:"distVersion"`
		Plugins        map[string]distPlugin `yaml:"plugins"`
		Modules        []distModule          `yaml:"modules"`
		Packages       []string              `yaml:"packages"`
		Pins           []distPin             `yaml:"pins"`
		Bindings       distBindings          `yaml:"bindings"`
		Site           *distBindings         `yaml:"site,omitempty"`
		AcceptLicenses []string              `yaml:"acceptLicenses,flow"`
		Serves         plugingen.ServesData  `yaml:"serves"`
	}
	distPlugin struct {
		Package     string `yaml:"package"`
		Manifest    string `yaml:"manifest"`
		Attestation string `yaml:"attestation,omitempty"`
		firstParty  bool
	}
	goModule struct {
		Path    string `yaml:"path"`
		Version string `yaml:"version,omitempty"`
		Sum     string `yaml:"sum,omitempty"`
	}
	distModule struct {
		Path    string    `yaml:"path"`
		Version string    `yaml:"version,omitempty"`
		Sum     string    `yaml:"sum,omitempty"`
		Replace *goModule `yaml:"replace,omitempty"`
		Licence string    `yaml:"licence"`
	}
	distPin struct {
		Path      string `yaml:"path"`
		Version   string `yaml:"version"`
		Sum       string `yaml:"sum,omitempty"`
		GoMod     string `yaml:"gomod,omitempty"`
		Release   string `yaml:"release,omitempty"`
		Unchecked bool   `yaml:"unchecked,omitempty"`
	}
	distBindings struct {
		Providers   map[string][]string `yaml:"providers,omitempty"`
		Consumers   map[string][]string `yaml:"consumers,omitempty"`
		SealedHooks map[string][]string `yaml:"sealedHooks,omitempty"`
		Required    struct {
			Keys []string `yaml:"keys,flow"`
			Rows []string `yaml:"rows,flow"`
		} `yaml:"required"`
	}
)

func bindingsDoc(d boot.DistManifest) distBindings {
	m := func(bs []boot.Binding) map[string][]string {
		out := map[string][]string{}
		for _, b := range bs {
			out[b.Key] = b.Plugins
		}
		return out
	}
	var hooks []boot.Binding
	for _, h := range d.SealedHooks {
		hooks = append(hooks, boot.Binding{Key: h.Hook, Plugins: h.Plugins})
	}
	b := distBindings{Providers: m(d.Providers), Consumers: m(d.Consumers), SealedHooks: m(hooks)}
	b.Required.Keys, b.Required.Rows = d.RequiredKeys, d.RequiredRows
	return b
}

// pinOf returns the pin of path at version, the zero pin for none.
func pinOf(pins []boot.DistPin, path, version string) boot.DistPin {
	for _, p := range pins {
		if p.Path == path && p.Version == version {
			return p
		}
	}
	return boot.DistPin{}
}
