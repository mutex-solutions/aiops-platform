package boot

import (
	"cmp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// modules.yaml, the S1 stand-in of the module table, is a fixture format of boot's
// tests: the launcher reads the module table from the signed distribution manifest
// (Distribution.Table), and a test writes a table in a few lines. One entry per plugin:
//
//	<plugin id>:
//	  module: <Go module path>     version: <v>     sum: <h1:...>
//	  provides: [<key>]  needs: [<key>]  wants: [<key>]
//	  volatile: [<config path>]    a path under config, "config." is implied
//	  hooks: [{hook: <name>, stage: <ordinal>, name: <stage name>}]   one per hook and stage

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
		default:
			err = p.err(f.k, RuleUnknownField, pr.key, "unknown field %q in a module entry", f.key)
		}
		if err != nil {
			return ModuleEntry{}, err
		}
	}
	return e, nil
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
