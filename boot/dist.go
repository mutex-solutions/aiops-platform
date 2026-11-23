package boot

import (
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// dist.yaml is the builder's input: the bindings and the required set of a
// distribution, from which the builder writes them into the signed distribution
// manifest (ADR-060 decision 20; Distribution). It is read through the same strict
// reader as the layers (ADR-059 decision 6):
//
//	providers:     {<key>: [<plugin id>, ...]}   provider-bound keys
//	consumers:     {<key>: [<plugin id>, ...]}   consumer-bound keys
//	sealedHooks:   {<hook>: [<plugin id>, ...]}  who may listen on a sealed hook
//	requiredKeys:  [<key>, ...]
//	requiredRows:  [<row id>, ...]

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
