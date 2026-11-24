// SPDX-License-Identifier: Apache-2.0

package plugingen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// BuildFlags are the flags of every build of a binary of the repository: the tag
// grpcnotrace drops golang.org/x/net/trace, which gRPC links through the signing
// library and whose init registers /debug/requests and /debug/events on
// http.DefaultServeMux (ADR-020 decision 27; ADR-044 decision 21). Distribution lists
// a graph with them, make check and CI build with them in GOFLAGS, and so does the
// harness of the test binary.
var BuildFlags = []string{"-tags=grpcnotrace"}

const tracePackage = "golang.org/x/net/trace"

// traceFree refuses the package graph pkgs of main when it holds the trace package,
// so a signed manifest never records a build without the tag.
func traceFree(main string, pkgs []string) error {
	if slices.Contains(pkgs, tracePackage) {
		return fmt.Errorf("%s links %s, which serves its handlers on http.DefaultServeMux: build it with the tag grpcnotrace (ADR-020 decision 27)", main, tracePackage)
	}
	return nil
}

// Distribution writes the signed distribution manifest (ADR-060 decision 20) of the
// binary whose main package is mainPkg in the module at root, as boot reads it
// (boot.ReadDistribution), with the bindings and the required set of the builder's
// input dist.yaml. It stands in for the builder of ADR-044 for the builds inside this
// repository, cmd/aiops and the test distribution's binary, until the builder writes
// the module table and the manifest into dist/ (ADR-060 decision 20):
//
//   - the module table is each package of the binary's package graph that holds a
//     plugin.yaml, with that manifest byte for byte;
//   - the modules and pins are those that go list records for the build, a module of
//     this repository by its path and its replacement with no version and no sum
//     (ADR-020 decision 28), and the licence of each module is read from the upstream
//     register, Apache-2.0 for this repository's own, where the builder detects it;
//   - serves is what Serves writes for the definitions in the graph;
//   - each plugin's statement names every definition of its requires and the suite
//     revision of each it provides or contributes to, with both halves where the
//     revision is 1 or more (ADR-060 decisions 55 and 56): make check and CI run the
//     suites of every plugin on the same tree as the parity check of this file, which
//     the release pipeline's attestation replaces.
func Distribution(root, mainPkg string, input, register []byte) ([]byte, error) {
	bindings, err := boot.ParseDist("dist.yaml", input)
	if err != nil {
		return nil, err
	}
	licences, err := registerLicences(register)
	if err != nil {
		return nil, err
	}
	out, err := goCmd(root, slices.Concat([]string{"list", "-deps", "-json"}, BuildFlags, []string{mainPkg})...)
	if err != nil {
		return nil, err
	}
	type module struct {
		Path, Version, Sum, GoModSum string
		Main                         bool
		Replace                      *struct{ Path, Version, Sum string }
	}
	doc := distDoc{DistVersion: 1, Plugins: map[string]distPlugin{}, Bindings: bindingsDoc(bindings), Packages: []string{}}
	mods := map[string]module{}
	var defs []Definition
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct {
			ImportPath, Dir string
			Standard        bool
			Module          *module
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		if p.Standard || p.Module == nil {
			continue
		}
		doc.Packages = append(doc.Packages, p.ImportPath)
		mods[p.Module.Path] = *p.Module
		if src, err := os.ReadFile(filepath.Join(p.Dir, "definition.yaml")); err == nil {
			d, err := ParseDefinition(p.ImportPath, src)
			if err != nil {
				return nil, err
			}
			defs = append(defs, d)
		}
		if src, err := os.ReadFile(filepath.Join(p.Dir, "plugin.yaml")); err == nil {
			m, err := boot.ReadManifest(p.ImportPath+"/plugin.yaml", src, boot.ManifestStrict)
			if err != nil {
				return nil, err
			}
			doc.Plugins[m.ID] = distPlugin{Package: p.ImportPath, Manifest: string(src)}
		}
	}
	slices.Sort(doc.Packages)
	if err := traceFree(mainPkg, doc.Packages); err != nil {
		return nil, err
	}
	for _, path := range slices.Sorted(maps.Keys(mods)) {
		m := mods[path]
		e := distModule{Path: path, Licence: licences[path]}
		switch {
		case strings.HasPrefix(path+"/", modPath+"/"):
			e.Licence = "Apache-2.0"
			if m.Replace != nil {
				e.Replace = &goModule{Path: m.Replace.Path, Version: m.Replace.Version, Sum: m.Replace.Sum}
			}
		case m.Replace != nil:
			return nil, fmt.Errorf("module %s is replaced outside this repository", path)
		default:
			e.Version, e.Sum = m.Version, m.Sum
			doc.Pins = append(doc.Pins, distPin{Path: path, Version: m.Version, Sum: m.Sum, GoMod: m.GoModSum})
		}
		doc.Modules = append(doc.Modules, e)
	}
	doc.Serves = Serves(defs)
	for id, p := range doc.Plugins {
		if p.Attestation, err = statement(id, []byte(p.Manifest), doc.Serves); err != nil {
			return nil, err
		}
		doc.Plugins[id] = p
	}
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// statement writes the conformance attestation of one plugin of the module table: its
// subject is the digest of its manifest as the table holds it (ADR-020 decision 9).
func statement(id string, manifest []byte, serves ServesData) (string, error) {
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
				if _, ok := sd.Classes[k.Key]; ok && k.Major == majorOf(sd.Version) {
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

// registerLicences returns the licence of each module of the upstream register: the
// entry's for a module of its modules, and its own for a module of its contains.
func registerLicences(register []byte) (map[string]string, error) {
	var r struct {
		Entries []struct {
			Modules  []string `yaml:"modules"`
			Licence  string   `yaml:"licence"`
			Contains []struct {
				Module, Licence string
			} `yaml:"contains"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(register, &r); err != nil {
		return nil, fmt.Errorf("the upstream register: %w", err)
	}
	out := map[string]string{}
	for _, e := range r.Entries {
		for _, m := range e.Modules {
			out[m] = e.Licence
		}
		for _, c := range e.Contains {
			if c.Module != "" {
				out[c.Module] = c.Licence
			}
		}
	}
	return out, nil
}

// The document of Distribution, in the field order of boot.Distribution.
type (
	distDoc struct {
		DistVersion    int                   `yaml:"distVersion"`
		Plugins        map[string]distPlugin `yaml:"plugins"`
		Modules        []distModule          `yaml:"modules"`
		Packages       []string              `yaml:"packages"`
		Pins           []distPin             `yaml:"pins"`
		Bindings       distBindings          `yaml:"bindings"`
		AcceptLicenses []string              `yaml:"acceptLicenses,flow"`
		Serves         ServesData            `yaml:"serves"`
	}
	distPlugin struct {
		Package     string `yaml:"package"`
		Manifest    string `yaml:"manifest"`
		Attestation string `yaml:"attestation"`
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
		Path    string `yaml:"path"`
		Version string `yaml:"version"`
		Sum     string `yaml:"sum,omitempty"`
		GoMod   string `yaml:"gomod,omitempty"`
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
