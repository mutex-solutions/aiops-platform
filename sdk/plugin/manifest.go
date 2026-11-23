// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Manifest is manifest schema 1.0, plugin.yaml, both halves (ADR-048). The package
// holds no YAML code (ADR-060 decision 4): the yaml tags name each field as the
// schema does, for the generator that writes it. An optional string that is empty
// reads as absent. The owners aiops and contrib are reserved to the release
// pipeline (ADR-048 decision 14). boot reads the file into types of its own, in a
// strict and a lenient mode, and holds every rule of the schema; a test of plugingen
// holds its reading equal to this type.
type Manifest struct {
	ManifestVersion string        `yaml:"manifestVersion"` // "1.0"
	ID              string        `yaml:"id"`              // <owner>/<name>, equal to Spec.ID
	Version         string        `yaml:"version"`         // Semantic Versioning 2.0.0, no leading v
	Title           string        `yaml:"title"`
	Description     string        `yaml:"description,omitempty"`
	License         string        `yaml:"license"` // an SPDX expression
	Publisher       Publisher     `yaml:"publisher"`
	Source          string        `yaml:"source,omitempty"`
	Support         *Support      `yaml:"support,omitempty"`
	Stability       string        `yaml:"stability"` // experimental, preview or ga
	Scope           []string      `yaml:"scope"`     // a set of process and tenant (ADR-048 decision 22)
	Runtimes        []string      `yaml:"runtimes"`  // a set of inproc, remote and wasm; [] is a pack
	Capabilities    *Capabilities `yaml:"capabilities,omitempty"`
	Integrates      []Integration `yaml:"integrates,omitempty"`
	Instantiable    bool          `yaml:"instantiable,omitempty"`
	Generated       `yaml:",inline"`
}

// Publisher is the publisher of a plugin; ID equals the owner segment of the id.
type Publisher struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name,omitempty"`
	URL  string `yaml:"url,omitempty"`
}

// Support says where a user gets help: at least one of the two.
type Support struct {
	URL   string `yaml:"url,omitempty"`
	Email string `yaml:"email,omitempty"`
}

// Capabilities lists the hosts a plugin dials and the secrets it reads. A network
// entry is a host, optionally with :<port>, or config: and a JSON Pointer into the
// config, such as config:/url.
type Capabilities struct {
	Network []string `yaml:"network,omitempty"`
	Secrets []string `yaml:"secrets,omitempty"`
}

// Integration is one external system a plugin integrates (ADR-048 decision 5).
type Integration struct {
	System     string   `yaml:"system"`
	Versions   string   `yaml:"versions"`
	Tested     []string `yaml:"tested"`
	Credential string   `yaml:"credential"` // read-only or broader
	License    string   `yaml:"license,omitempty"`
}

// Generated is the generated half: the seven fields that plugingen writes and the
// parity check compares as read (ADR-048 decision 1). Every list is a set, sorted.
type Generated struct {
	Uses         []Use          `yaml:"uses"`
	Provides     []KeyRef       `yaml:"provides"`
	Registers    []KeyRef       `yaml:"registers"`
	Hooks        []HookRef      `yaml:"hooks"`
	ConfigSchema map[string]any `yaml:"configSchema"` // JSON Schema 2020-12, inline
	Durable      []any          `yaml:"durable"`      // [] until S3 fixes its entry shape
	Requires     Requires       `yaml:"requires"`
}

// Use is one dependency of Spec.Inject: As is "need" for Need and NeedWith, "want"
// for Want.
type Use struct {
	Key   string `yaml:"key"`
	Major int    `yaml:"major"`
	As    string `yaml:"as"`
}

// KeyRef names a key by its name and major: an entry of provides or registers.
type KeyRef struct {
	Key   string `yaml:"key"`
	Major int    `yaml:"major"`
}

// HookRef is one hook a plugin listens on, at one stage by name. The ordinal and the
// access class are served data, not manifest data.
type HookRef struct {
	Hook  string `yaml:"hook"`
	Stage string `yaml:"stage"`
}

// Requires is what a plugin needs from a distribution (ADR-048 decision 12).
// Definitions maps a definition name, the <name> of sdk/def/<name>/v<N>, to the
// "<major>.<minor>" it needs, one per major, sorted.
type Requires struct {
	PluginAPI      string              `yaml:"pluginApi,omitempty"`
	RemoteProtocol []int               `yaml:"remoteProtocol,omitempty"`
	Definitions    map[string][]string `yaml:"definitions"`
}

// declared is what Define keeps of a Spec beyond kernel.Plugin. Generate reads it
// through the plugin's Decode: only this package makes a *declared, so no parsed
// config tree is one.
type declared struct {
	registers []kernel.AnyKey
	schema    []byte
}

// declaredOf returns what Define kept for p, or nil for a value Define did not make.
// It never runs Apply; a Decode of kernel.Define that fails or panics on the probe
// answers nil.
func declaredOf(p kernel.Plugin) (d *declared) {
	if p.Decode == nil {
		return nil
	}
	d = &declared{}
	defer func() {
		if recover() != nil {
			d = nil
		}
	}()
	if _, err := p.Decode(d); err != nil || d.schema == nil {
		return nil
	}
	return d
}

// Generate returns the generated half of p's manifest that p decides (ADR-048
// decisions 6 to 12): uses, provides, registers, hooks, configSchema, durable, and
// requires.pluginApi, the APIVersion the code is built against. It runs no Apply and
// decodes no config. requires.definitions is left empty: which definition declares a
// key or a hook is in its definition.yaml, which plugingen reads.
//
// For a value made by kernel.Define, which has no Spec, registers is empty and
// configSchema is {}, the schema every value satisfies (ADR-048 decision 9; ADR-060
// decision 13). Generate fails where the kernel would refuse the value: a zero key,
// dependency or hook, an unknown stage, an entry twice in one set.
func Generate(p kernel.Plugin) (Generated, error) {
	g := Generated{Uses: []Use{}, Provides: []KeyRef{}, Registers: []KeyRef{}, Hooks: []HookRef{},
		ConfigSchema: map[string]any{}, Durable: []any{}, Requires: Requires{PluginAPI: APIVersion}}
	var errs []error
	for _, d := range p.Inject {
		k := d.Key()
		if k == nil || k.Name() == "" {
			errs = append(errs, errors.New("inject: a zero dependency"))
			continue
		}
		as := "need"
		if d.Optional() {
			as = "want"
		}
		g.Uses = append(g.Uses, Use{Key: k.Name(), Major: k.Major(), As: as})
	}
	d := declaredOf(p)
	var registers []kernel.AnyKey
	if d != nil {
		registers = d.registers
		if err := json.Unmarshal(d.schema, &g.ConfigSchema); err != nil {
			errs = append(errs, err) // unreachable: the derivation writes JSON
		}
	}
	g.Provides, errs = keyRefs("provides", p.Provides, errs)
	g.Registers, errs = keyRefs("registers", registers, errs)
	for _, u := range p.Hooks {
		if u.Hook() == "" || u.Stage() == "" {
			errs = append(errs, fmt.Errorf("hooks: a zero hook or an unknown stage of %q", u.Hook()))
			continue
		}
		g.Hooks = append(g.Hooks, HookRef{Hook: u.Hook(), Stage: u.Stage()})
	}
	slices.SortFunc(g.Uses, func(a, b Use) int { return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.Major, b.Major)) })
	slices.SortFunc(g.Hooks, func(a, b HookRef) int { return cmp.Or(cmp.Compare(a.Hook, b.Hook), cmp.Compare(a.Stage, b.Stage)) })
	for i := 1; i < len(g.Uses); i++ {
		if a, b := g.Uses[i-1], g.Uses[i]; a.Key == b.Key && a.Major == b.Major {
			errs = append(errs, fmt.Errorf("uses: %s major %d twice", a.Key, a.Major))
		}
	}
	for i := 1; i < len(g.Hooks); i++ {
		if g.Hooks[i-1] == g.Hooks[i] {
			errs = append(errs, fmt.Errorf("hooks: %s at stage %s twice", g.Hooks[i].Hook, g.Hooks[i].Stage))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Generated{}, fmt.Errorf("plugin %s: %w", p.ID, err)
	}
	return g, nil
}

// keyRefs returns the sorted set of keys, adding an error for a zero key or a key
// twice.
func keyRefs(field string, keys []kernel.AnyKey, errs []error) ([]KeyRef, []error) {
	out := []KeyRef{}
	for _, k := range keys {
		if k == nil || k.Name() == "" {
			errs = append(errs, fmt.Errorf("%s: a zero key", field))
			continue
		}
		out = append(out, KeyRef{Key: k.Name(), Major: k.Major()})
	}
	slices.SortFunc(out, func(a, b KeyRef) int { return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.Major, b.Major)) })
	for i := 1; i < len(out); i++ {
		if out[i-1] == out[i] {
			errs = append(errs, fmt.Errorf("%s: %s major %d twice", field, out[i].Key, out[i].Major))
		}
	}
	return out, errs
}
