// SPDX-License-Identifier: Apache-2.0

package plugingen

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// Definition is a definition package as its definition.yaml declares it (ADR-060
// decision 7).
type Definition struct {
	Name           string // the <name> of sdk/def/<name>/v<N>
	Path           string // the import path of the package
	Major          int
	Version        string // "<major>.<minor>"
	Stability      string
	Remote         string
	CompatibleFrom string
	Deprecated     map[string]string
	Suite          int
	Classes        map[string]Class // by key name
	Hooks          map[string]Hook  // by hook name
}

// Class is the provider class and the consumer class of a key.
type Class struct {
	Provider string `yaml:"provider"`
	Consumer string `yaml:"consumer"`
}

// Hook is a hook as served data names it: its stages in chain order, its access
// class and its remote mark.
type Hook struct {
	Stages []string `yaml:"stages,flow"`
	Access string   `yaml:"access"`
	Remote string   `yaml:"remote"`
}

var (
	defPath    = regexp.MustCompile(`/([a-z][a-z0-9]*)/v(0|[1-9][0-9]*)$`)
	majorMinor = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

// ParseDefinition reads the definition.yaml of the package at importPath. The
// definition test of each package holds the file to its Go declarations; this reads
// what requires and serves need, and refuses an unknown field.
func ParseDefinition(importPath string, src []byte) (Definition, error) {
	m := defPath.FindStringSubmatch(importPath)
	if m == nil {
		return Definition{}, fmt.Errorf("%s: a definition lives at <name>/v<N>", importPath)
	}
	var f struct {
		Key            string            `yaml:"key"`
		Version        string            `yaml:"version"`
		Stability      string            `yaml:"stability"`
		Class          yaml.Node         `yaml:"class"`
		Remote         string            `yaml:"remote"`
		CompatibleFrom string            `yaml:"compatibleFrom"`
		Deprecated     map[string]string `yaml:"deprecated"`
		Suite          int               `yaml:"suite"`
		Hooks          map[string]Hook   `yaml:"hooks"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Definition{}, fmt.Errorf("%s/definition.yaml: %w", importPath, err)
	}
	mm := majorMinor.FindStringSubmatch(f.Version)
	if mm == nil || mm[1] != m[2] {
		return Definition{}, fmt.Errorf("%s/definition.yaml: version %q is not <major>.<minor> with the major of v%s", importPath, f.Version, m[2])
	}
	d := Definition{Name: m[1], Path: importPath, Version: f.Version, Stability: f.Stability, Remote: f.Remote,
		CompatibleFrom: f.CompatibleFrom, Deprecated: f.Deprecated, Suite: f.Suite, Hooks: f.Hooks, Classes: map[string]Class{}}
	d.Major, _ = strconv.Atoi(m[2])
	var one Class
	if err := f.Class.Decode(&one); err == nil && one.Provider != "" {
		d.Classes[f.Key] = one
	} else if err := f.Class.Decode(&d.Classes); err != nil || len(d.Classes) == 0 {
		return Definition{}, fmt.Errorf("%s/definition.yaml: class is neither one class nor one per key", importPath)
	}
	return d, nil
}

// builtinKeys and builtinHooks belong to pluginApi, not to a definition (ADR-060
// decision 9; ADR-048 decision 12).
var (
	builtinKeys  = []kernel.AnyKey{kernel.ReflectKey, kernel.RegistryKey, kernel.HooksKey, kernel.LoggerKey}
	builtinHooks = []kernel.AnyHook{kernel.InternalPlugin, kernel.InternalStatus, kernel.InternalService,
		kernel.InternalConfig, kernel.InternalUpdate, kernel.InternalCommit}
)

// KernelPart is the kernel part of serves: the four built-in keys with their classes
// and the six hooks of the kernel with their stage and seal (ADR-048 decision 12).
type KernelPart struct {
	Keys  map[string]Class `yaml:"keys"`
	Hooks map[string]Hook  `yaml:"hooks"`
}

// Kernel returns the kernel part of serves, read from sdk/kernel.
func Kernel() KernelPart {
	k := KernelPart{Keys: map[string]Class{}, Hooks: map[string]Hook{}}
	for _, key := range builtinKeys {
		pb, cb, _ := kernel.KeyClasses(key)
		k.Keys[key.Name()] = Class{Provider: bound(pb), Consumer: bound(cb)}
	}
	for _, h := range builtinHooks {
		access := "open"
		if sealed, _ := kernel.HookSealed(h); sealed {
			access = "sealed"
		}
		k.Hooks[h.Name()] = Hook{Stages: []string{kernel.DefaultStage}, Access: access, Remote: "no"}
	}
	return k
}

func bound(b bool) string {
	if b {
		return "bound"
	}
	return "open"
}

// Served is one served major of a definition (ADR-060 decision 9).
type Served struct {
	Version        string            `yaml:"version"`
	CompatibleFrom string            `yaml:"compatibleFrom,omitempty"`
	Deprecated     map[string]string `yaml:"deprecated,omitempty"`
	Suite          int               `yaml:"suite"`
	Classes        map[string]Class  `yaml:"classes"`
	Hooks          map[string]Hook   `yaml:"hooks,omitempty"`
}

// ServesData is serves of the signed distribution manifest: the pluginApi the binary
// serves with the kernel part, and per definition each major it links. The builder
// writes it from the definitions of the binary's package graph.
type ServesData struct {
	PluginAPI   string              `yaml:"pluginApi"`
	Kernel      KernelPart          `yaml:"kernel"`
	Definitions map[string][]Served `yaml:"definitions"`
}

// Serves returns serves for the definitions a binary links, each major once, sorted
// by major.
func Serves(defs []Definition) ServesData {
	s := ServesData{PluginAPI: plugin.APIVersion, Kernel: Kernel(), Definitions: map[string][]Served{}}
	for _, d := range defs {
		s.Definitions[d.Name] = append(s.Definitions[d.Name], Served{Version: d.Version, CompatibleFrom: d.CompatibleFrom,
			Deprecated: d.Deprecated, Suite: d.Suite, Classes: d.Classes, Hooks: d.Hooks})
	}
	for _, list := range s.Definitions {
		slices.SortFunc(list, func(a, b Served) int { return cmp.Compare(majorOf(a.Version), majorOf(b.Version)) })
	}
	return s
}

func majorOf(v string) int {
	n, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return n
}

// Requires fills g.Requires.Definitions from defs, the definitions in the package
// graph of the plugin (ADR-060 decision 9): each key and hook of g maps to the
// definition whose definition.yaml declares it at the key's major. The kernel's keys
// and hooks belong to pluginApi and are left out, and so is a key the plugin
// provides that no definition declares, which is the plugin's own. A used or
// registered key and a hook that no definition declares fail, and so does a key
// that its definitions declare at other majors only.
func Requires(g *plugin.Generated, defs []Definition) error {
	byKey, byHook := map[string][]Definition{}, map[string]Definition{}
	for _, d := range defs {
		for k := range d.Classes {
			byKey[k] = append(byKey[k], d)
		}
		for h := range d.Hooks {
			byHook[h] = d
		}
	}
	kernelHooks := Kernel().Hooks
	need := map[string]map[string]bool{}
	var errs []error
	add := func(field, key string, major int, own bool) {
		if slices.ContainsFunc(builtinKeys, func(k kernel.AnyKey) bool { return k.Name() == key }) {
			return
		}
		ds := byKey[key]
		i := slices.IndexFunc(ds, func(d Definition) bool { return d.Major == major })
		switch {
		case len(ds) == 0 && own:
			return
		case len(ds) == 0:
			errs = append(errs, fmt.Errorf("%s: key %s is declared by no definition in the plugin's package graph", field, key))
			return
		case i < 0:
			errs = append(errs, fmt.Errorf("%s: key %s at major %d, its definition %s is at %s", field, key, major, ds[0].Name, ds[0].Version))
			return
		}
		d := ds[i]
		if need[d.Name] == nil {
			need[d.Name] = map[string]bool{}
		}
		need[d.Name][d.Version] = true
	}
	for _, u := range g.Uses {
		add("uses", u.Key, u.Major, false)
	}
	for _, p := range g.Provides {
		add("provides", p.Key, p.Major, true)
	}
	for _, r := range g.Registers {
		add("registers", r.Key, r.Major, false)
	}
	for _, h := range g.Hooks {
		if _, ok := kernelHooks[h.Hook]; ok {
			continue
		}
		d, ok := byHook[h.Hook]
		if !ok {
			errs = append(errs, fmt.Errorf("hooks: hook %s is declared by no definition in the plugin's package graph", h.Hook))
			continue
		}
		if need[d.Name] == nil {
			need[d.Name] = map[string]bool{}
		}
		need[d.Name][d.Version] = true
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	g.Requires.Definitions = map[string][]string{}
	for name, vs := range need {
		list := slices.Collect(maps.Keys(vs))
		slices.SortFunc(list, func(a, b string) int { return cmp.Compare(majorOf(a), majorOf(b)) })
		g.Requires.Definitions[name] = list
	}
	return nil
}
