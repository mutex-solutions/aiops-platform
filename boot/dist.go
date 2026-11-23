package boot

import (
	"cmp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The S1 readers of the two distribution files. The documents do not say
// where the CLI gets the module table and the distribution manifest before
// the builder of S2a generates them (08-profiles-roadmap-adrs.md:402), so S1
// reads two small YAML files through the same strict reader as the layers
// (ADR-059 decision 6). They are an S1 stand-in, not the signed manifest.
//
// dist.yaml, the part of the distribution manifest the launcher reads
// (08-profiles-roadmap-adrs.md:451; 07-c4-plugin-sdk.md:1340):
//
//	providers:     {<key>: [<plugin id>, ...]}   provider-bound keys
//	consumers:     {<key>: [<plugin id>, ...]}   consumer-bound keys
//	sealedHooks:   {<hook>: [<plugin id>, ...]}  who may listen on a sealed hook
//	requiredKeys:  [<key>, ...]
//	requiredRows:  [<row id>, ...]
//
// modules.yaml, one entry per plugin of the module table:
//
//	<plugin id>:
//	  module: <Go module path>     version: <v>     sum: <h1:...>
//	  provides: [<key>]  needs: [<key>]  wants: [<key>]
//	  volatile: [<config path>]    a path under config, "config." is implied
//	  hooks: [{hook: <name>, stage: <ordinal>, name: <stage name>}]   one per hook and stage
//	  classes: {providerBound: [<key>], consumerBound: [<key>], sealedHooks: [<hook>]}

// ParseDist reads dist.yaml.
func ParseDist(file string, data []byte) (DistManifest, error) {
	root, err := parseDocument(file, data)
	if err != nil {
		return DistManifest{}, err
	}
	p := &parser{file: file}
	prs, err := p.pairs(root, "", "dist.yaml")
	if err != nil {
		return DistManifest{}, err
	}
	var d DistManifest
	for _, pr := range prs {
		switch pr.key {
		case "providers":
			d.Providers, err = p.bindings(pr.v, "providers", okIdent)
		case "consumers":
			d.Consumers, err = p.bindings(pr.v, "consumers", okIdent)
		case "sealedHooks":
			var bs []Binding
			bs, err = p.bindings(pr.v, "sealedHooks", okHook)
			for _, b := range bs {
				d.SealedHooks = append(d.SealedHooks, HookListeners{Hook: b.Key, Plugins: b.Plugins})
			}
		case "requiredKeys":
			d.RequiredKeys, err = p.set(pr.v, "", "requiredKeys", okIdent)
		case "requiredRows":
			d.RequiredRows, err = p.set(pr.v, "", "requiredRows", func(s string) bool { return okID(s, true) })
		default:
			err = p.err(pr.k, RuleUnknownField, "", "unknown field %q in dist.yaml", pr.key)
		}
		if err != nil {
			return DistManifest{}, err
		}
	}
	return d, nil
}

// ParseModules reads modules.yaml.
func ParseModules(file string, data []byte) (ModuleTable, error) {
	root, err := parseDocument(file, data)
	if err != nil {
		return nil, err
	}
	p := &parser{file: file}
	prs, err := p.pairs(root, "", "modules.yaml")
	if err != nil {
		return nil, err
	}
	t := make(ModuleTable, len(prs))
	for _, pr := range prs {
		if !okPlugin(pr.key) || strings.HasPrefix(pr.key, "core:") {
			return nil, p.err(pr.k, RuleSyntaxID, pr.key, "plugin id %q is not <owner>/<name>", pr.key)
		}
		e, err := p.module(pr)
		if err != nil {
			return nil, err
		}
		t[pr.key] = e
	}
	return t, nil
}

func (p *parser) module(pr pair) (ModuleEntry, error) {
	fields, err := p.pairs(pr.v, pr.key, "a module entry")
	if err != nil {
		return ModuleEntry{}, err
	}
	e := ModuleEntry{ID: pr.key}
	for _, f := range fields {
		switch f.key {
		case "module":
			e.Module, err = p.text(f.v, pr.key, "module")
		case "version":
			e.Version, err = p.text(f.v, pr.key, "version")
		case "sum":
			e.Sum, err = p.text(f.v, pr.key, "sum")
		case "provides":
			e.Provides, err = p.set(f.v, pr.key, "provides", okIdent)
		case "needs":
			e.Needs, err = p.set(f.v, pr.key, "needs", okIdent)
		case "wants":
			e.Wants, err = p.set(f.v, pr.key, "wants", okIdent)
		case "volatile":
			e.Volatile, err = p.set(f.v, pr.key, "volatile", okPath)
			for i, v := range e.Volatile {
				if v != "config" && !strings.HasPrefix(v, "config.") && !strings.HasPrefix(v, "config[") {
					e.Volatile[i] = "config." + v
				}
			}
			sort.Strings(e.Volatile)
		case "hooks":
			e.Hooks, err = p.hooks(f.v, pr.key)
		case "classes":
			e.Classes, err = p.classes(f.v, pr.key)
		default:
			err = p.err(f.k, RuleUnknownField, pr.key, "unknown field %q in a module entry", f.key)
		}
		if err != nil {
			return ModuleEntry{}, err
		}
	}
	return e, nil
}

// classes reads the classes of a module entry: the keys and hooks of it whose
// definitions bind or seal them.
func (p *parser) classes(n *yaml.Node, row string) (*ModuleClasses, error) {
	fields, err := p.pairs(n, row, "classes")
	if err != nil {
		return nil, err
	}
	c := &ModuleClasses{}
	for _, f := range fields {
		switch f.key {
		case "providerBound":
			c.ProviderBound, err = p.set(f.v, row, "providerBound", okIdent)
		case "consumerBound":
			c.ConsumerBound, err = p.set(f.v, row, "consumerBound", okIdent)
		case "sealedHooks":
			c.SealedHooks, err = p.set(f.v, row, "sealedHooks", okHook)
		default:
			err = p.err(f.k, RuleUnknownField, row, "unknown field %q in classes", f.key)
		}
		if err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (p *parser) hooks(n *yaml.Node, row string) ([]HookDecl, error) {
	if err := p.guard(n, row); err != nil {
		return nil, err
	}
	if n.Kind != yaml.SequenceNode {
		return nil, p.err(n, RuleType, row, "hooks must be a list")
	}
	var out []HookDecl
	seen := map[HookDecl]bool{} // keyed by hook and stage, as the kernel keys a HookUse
	for _, c := range n.Content {
		prs, err := p.pairs(c, row, "a hook")
		if err != nil {
			return nil, err
		}
		var h HookDecl
		named := false
		for _, f := range prs {
			switch f.key {
			case "hook":
				h.Hook, err = p.text(f.v, row, "hook")
				if err == nil && !okHook(h.Hook) {
					err = p.err(f.v, RuleSyntaxID, row, "hook %q is not a hook name", h.Hook)
				}
				named = err == nil
			case "stage":
				var s string
				if s, err = p.plain(f.v, row, "stage"); err == nil {
					var i int64
					if i, err = strconv.ParseInt(s, 10, 32); err != nil || i < 0 {
						err = p.err(f.v, RuleType, row, "stage %q is not a non-negative integer", s)
					}
					h.Stage = int(i)
				}
			case "name":
				h.StageName, err = p.text(f.v, row, "name")
			default:
				err = p.err(f.k, RuleUnknownField, row, "unknown field %q in a hook", f.key)
			}
			if err != nil {
				return nil, err
			}
		}
		if !named {
			return nil, p.err(c, RuleType, row, "a hook states hook")
		}
		if seen[h] {
			return nil, p.err(c, RuleDuplicateKey, row, "hook %q is listed twice at one stage", h.Hook)
		}
		seen[h] = true
		out = append(out, h)
	}
	slices.SortFunc(out, func(a, b HookDecl) int {
		return cmp.Or(strings.Compare(a.Hook, b.Hook), cmp.Compare(a.Stage, b.Stage), strings.Compare(a.StageName, b.StageName))
	})
	return out, nil
}

// set reads a list of strings that satisfy valid, as a sorted set.
func (p *parser) set(n *yaml.Node, row, what string, valid func(string) bool) ([]string, error) {
	if err := p.guard(n, row); err != nil {
		return nil, err
	}
	if n.Kind != yaml.SequenceNode {
		return nil, p.err(n, RuleType, row, "%s must be a list", what)
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		s, err := p.text(c, row, what+" item")
		if err != nil {
			return nil, err
		}
		if !valid(s) {
			return nil, p.err(c, RuleSyntaxID, row, "%s item %q is not well formed", what, s)
		}
		out = append(out, s)
	}
	sort.Strings(out)
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			return nil, p.err(n, RuleSyntaxID, row, "%s lists %q twice", what, out[i])
		}
	}
	return out, nil
}

// bindings reads {<key>: [<plugin id>]} as bindings sorted by key.
func (p *parser) bindings(n *yaml.Node, what string, validKey func(string) bool) ([]Binding, error) {
	prs, err := p.pairs(n, "", what)
	if err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(prs))
	for _, pr := range prs {
		if !validKey(pr.key) {
			return nil, p.err(pr.k, RuleSyntaxID, "", "%s key %q is not well formed", what, pr.key)
		}
		ids, err := p.set(pr.v, "", what+" "+pr.key, func(s string) bool { return okPlugin(s) && !strings.HasPrefix(s, "core:") })
		if err != nil {
			return nil, err
		}
		out = append(out, Binding{Key: pr.key, Plugins: ids})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// okHook reports whether s is a hook name such as "composition/applied": 1 to
// 128 bytes of [A-Za-z0-9._/-], starting with a letter or digit.
func okHook(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, c := range s {
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && (i == 0 || c != '.' && c != '_' && c != '-' && c != '/') {
			return false
		}
	}
	return true
}

// okPath reports whether s is a config path: no space or control character,
// at most 256 bytes.
func okPath(s string) bool {
	return s != "" && len(s) <= 256 && !strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f })
}
