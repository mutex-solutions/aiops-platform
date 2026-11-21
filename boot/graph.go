package boot

import (
	"fmt"
	"sort"
	"strings"
)

// The static graph: step 3 of boot checks it before any row mounts
// (03-c3-kernel.md:359; 08-profiles-roadmap-adrs.md:469). It is built from
// what the module table declares (Provides, Needs) and what the rows declare
// (Inject, Isolate, Parent), with no plugin code run
// (plugin-system-2026-11.md §2 and §15: "Provides is mandatory so the composer
// analyses the graph without running code").
//
// Rows. Only an active row is a node: enabled, its roles meet the selected
// roles and its parent group is active (see ActiveRows). A disabled row, a
// row of another role and the children of either are not in the graph.
//
// Edges. A row depends on the provider of each key it needs: the Needs of its
// module entry and its row-level Inject. A child row also depends on its
// group, because a group is mounted before its children. This second edge is
// this package's reading: the documents say a group row can carry inject and
// stay PENDING (07-c4-plugin-sdk.md:1244), not that a child waits for it.
// Wants are no edge (a When body is a dependant of its own,
// plugin-system-2026-11.md §4).
//
// Realms. Isolate on a row creates a realm for that row and its descendants
// for each key it names (plugin-system-2026-11.md §15, rows 5 and 8: "one provider
// per name per realm", children inherit within a realm). The realm of a key
// at a row is the nearest row, the row itself first and then its ancestors,
// that isolates the key, or the root realm. A provider lives in the realm of
// its own row; a consumer sees only the providers of its realm, so inside a
// private realm it never reaches a provider outside it (the stricter reading
// the design note records, 524). Intercept changes no edge.
//
// Findings. A cycle, two providers of one key in one realm and a binding that
// names a plugin outside the module table are errors, and they stop boot for
// optional and required rows alike (08-profiles-roadmap-adrs.md:469). A need
// with no provider is an error when the consumer or the key is required, and
// otherwise one grouped warning (08:471, 03-c3-kernel.md:361). Isolate on a
// bound key is a gate failure (Stop); see Gate.

// Edge is a dependency: From needs To. Key is the key that To provides to
// From, or "" for the edge from a child to its group.
type Edge struct {
	From, To, Key string
}

// Provision is the set of rows that provide one key in one realm. Realm is
// the id of the row that isolates the key, "" for the root realm.
type Provision struct {
	Key, Realm string
	Rows       []string
}

// Graph is the static graph of one composition for one set of roles.
type Graph struct {
	Roles     []Role
	Active    []string
	Edges     []Edge
	Providers []Provision

	mods     ModuleTable
	needs    map[string][]string
	provides map[string][]string
	rdeps    map[string][]string
}

// RoleFilter returns the predicate that says whether a row runs in a process
// started with the given roles. No roles means every row. A row that states
// no roles (a bundle that is not a server bundle) runs everywhere.
func RoleFilter(roles []Role) func(Row) bool {
	return func(r Row) bool {
		if len(roles) == 0 || len(r.Roles) == 0 {
			return true
		}
		for _, x := range r.Roles {
			for _, y := range roles {
				if x == y {
					return true
				}
			}
		}
		return false
	}
}

// ActiveRows returns the ids of the rows that are mounted: enabled, accepted
// by the role filter and with an active parent. A composition lists parents
// before children, so one pass is enough.
func ActiveRows(c Composition, roles []Role) map[string]bool {
	in := RoleFilter(roles)
	active := make(map[string]bool, len(c.Rows))
	for _, r := range c.Rows {
		active[r.ID] = r.Enabled && in(r.Row) && (r.Parent == "" || active[r.Parent])
	}
	return active
}

type builder struct {
	rows     map[string]ComposedRow
	active   []string
	mods     ModuleTable
	required map[string]bool
	keyReq   map[string]bool
}

func (b *builder) provides(id string) []string { return b.mods[b.rows[id].Plugin].Provides }

// needs returns the keys a row waits for. The keys of the root are left out: the
// launcher provides the six launch keys before any row mounts (03-c3-kernel.md:360)
// and the kernel its four built-ins, so no row provides them and none waits for
// them. A manifest lists them in needs all the same (the loader needs profileContext
// and catalog, readiness needs logger). A row whose config holds a reference needs
// configRefsReady, which config-refs provides (refsInject).
func (b *builder) needs(id string) []string {
	return withoutRoot(union(b.mods[b.rows[id].Plugin].Needs, refsInject(b.rows[id])))
}

// realm returns the realm of a key at a row.
func (b *builder) realm(id, key string) string {
	for id != "" {
		r := b.rows[id]
		if inSet(key, r.Isolate...) {
			return id
		}
		id = r.Parent
	}
	return ""
}

func realmName(r string) string {
	if r == "" {
		return "the root realm"
	}
	return fmt.Sprintf("the realm of row %q", r)
}

// BuildGraph builds the static graph of the active rows of c and reports its
// findings, in a fixed order: bindings, two providers, needs with no
// provider, then at most one cycle, the shortest. The caller passes the
// composition after the gate has detached the denied rows (GateResult.Admitted).
func BuildGraph(c Composition, mods ModuleTable, dist DistManifest, roles []Role) (Graph, []StaticError, []StaticWarning) {
	b := &builder{rows: map[string]ComposedRow{}, mods: mods, required: map[string]bool{}, keyReq: map[string]bool{}}
	for _, r := range c.Rows {
		b.rows[r.ID] = r
	}
	req := RequiredFor(c, mods, dist, roles)
	for _, id := range req.Rows {
		b.required[id] = true
	}
	for _, k := range req.Keys {
		b.keyReq[k] = true
	}
	act := ActiveRows(c, roles)
	for _, r := range c.Rows {
		if act[r.ID] {
			b.active = append(b.active, r.ID)
		}
	}
	sort.Strings(b.active)
	g := Graph{Roles: append([]Role(nil), roles...), Active: b.active, mods: mods,
		needs: map[string][]string{}, provides: map[string][]string{}, rdeps: map[string][]string{}}
	var errs []StaticError
	errs = append(errs, b.bindings(dist)...)

	// Providers per (key, realm).
	prov := map[[2]string][]string{}
	for _, id := range b.active {
		g.provides[id], g.needs[id] = b.provides(id), b.needs(id)
		for _, k := range g.provides[id] {
			key := [2]string{k, b.realm(id, k)}
			prov[key] = append(prov[key], id)
		}
	}
	keys := make([][2]string, 0, len(prov))
	for k := range prov {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	for _, k := range keys {
		g.Providers = append(g.Providers, Provision{Key: k[0], Realm: k[1], Rows: prov[k]})
		if rows := prov[k]; len(rows) > 1 {
			errs = append(errs, StaticError{Stage: "static graph", Rule: RuleTwoProviders, Rows: rows, Keys: []string{k[0]},
				Msg: fmt.Sprintf("rows %s all provide key %q in %s; one provider per key per realm", quoted(rows), k[0], realmName(k[1]))})
		}
	}

	// Edges, missing providers.
	var missing []StaticError
	waits := map[string][]string{}
	for _, id := range b.active {
		for _, k := range g.needs[id] {
			rows := prov[[2]string{k, b.realm(id, k)}]
			for _, p := range rows {
				g.Edges = append(g.Edges, Edge{From: id, To: p, Key: k})
			}
			if len(rows) > 0 {
				continue
			}
			realm := b.realm(id, k)
			if b.required[id] || b.keyReq[k] {
				missing = append(missing, StaticError{Stage: "static graph", Rule: RuleNoProvider, Rows: []string{id}, Keys: []string{k},
					Msg: fmt.Sprintf("row %q needs key %q, which has no provider in %s, and the row or the key is required", id, k, realmName(realm))})
			} else {
				waits[id] = append(waits[id], k)
			}
		}
		if p := b.rows[id].Parent; p != "" {
			g.Edges = append(g.Edges, Edge{From: id, To: p})
		}
	}
	errs = append(errs, missing...)
	sort.Slice(g.Edges, func(i, j int) bool {
		a, c := g.Edges[i], g.Edges[j]
		if a.From != c.From {
			return a.From < c.From
		}
		if a.To != c.To {
			return a.To < c.To
		}
		return a.Key < c.Key
	})
	for _, e := range g.Edges {
		g.rdeps[e.To] = append(g.rdeps[e.To], e.From)
	}
	if cyc := shortestCycle(g.Edges); cyc != nil {
		errs = append(errs, cycleError(cyc))
	}
	return g, errs, groupWaits(waits)
}

// bindings reports a binding of the distribution manifest that names a plugin
// the module table does not hold.
func (b *builder) bindings(dist DistManifest) []StaticError {
	var out []StaticError
	for _, side := range []struct {
		name string
		bs   []Binding
	}{{"provider", dist.Providers}, {"consumer", dist.Consumers}} {
		for _, bd := range side.bs {
			for _, p := range bd.Plugins {
				if _, ok := b.mods[p]; !ok {
					out = append(out, StaticError{Stage: "static graph", Rule: RuleBindingBroken, Keys: []string{bd.Key},
						Msg: fmt.Sprintf("the %s binding of key %q lists plugin %q, which is not in the module table", side.name, bd.Key, p)})
				}
			}
		}
	}
	return out
}

func groupWaits(waits map[string][]string) []StaticWarning {
	if len(waits) == 0 {
		return nil
	}
	rows := make([]string, 0, len(waits))
	keys := map[string]bool{}
	for id, ks := range waits {
		rows = append(rows, id)
		for _, k := range ks {
			keys[k] = true
		}
	}
	sort.Strings(rows)
	parts := make([]string, len(rows))
	for i, id := range rows {
		sort.Strings(waits[id])
		parts[i] = fmt.Sprintf("%q waits for %s", id, strings.Join(waits[id], ", "))
	}
	return []StaticWarning{{Stage: "static graph", Rule: RuleNoProvider, Rows: rows, Keys: sortedKeys(keys),
		Msg: fmt.Sprintf("%d optional rows would be PENDING: %s", len(rows), strings.Join(parts, "; "))}}
}

// shortestCycle returns the edges of one shortest cycle, or nil. Among
// cycles of equal length it returns the one whose node list, rotated to start
// at its smallest id, sorts first among those found, so the answer depends on
// the edges only. Edges must be sorted.
func shortestCycle(edges []Edge) []Edge {
	out := map[string][]Edge{}
	outdeg := map[string]int{}
	in := map[string][]string{}
	for _, e := range edges {
		out[e.From] = append(out[e.From], e)
		outdeg[e.From]++
		in[e.To] = append(in[e.To], e.From)
		if _, ok := outdeg[e.To]; !ok {
			outdeg[e.To] = 0
		}
	}
	// Strip nodes that cannot reach a cycle: those with no outgoing edge, over
	// and over. What stays is on a cycle or leads to one.
	var stack []string
	for n, d := range outdeg {
		if d == 0 {
			stack = append(stack, n)
		}
	}
	gone := map[string]bool{}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		gone[n] = true
		for _, m := range in[n] {
			if outdeg[m]--; outdeg[m] == 0 {
				stack = append(stack, m)
			}
		}
	}
	var nodes []string
	for n := range outdeg {
		if !gone[n] {
			nodes = append(nodes, n)
		}
	}
	sort.Strings(nodes)
	var best []Edge
	for _, s := range nodes {
		c := cycleThrough(s, out, gone)
		if c != nil && (best == nil || len(c) < len(best) || len(c) == len(best) && lessCycle(c, best)) {
			best = c
		}
	}
	return best
}

// cycleThrough runs a breadth-first search from s over the edges and returns
// the shortest path that returns to s, or nil when s leads to a cycle without
// being on one.
func cycleThrough(s string, out map[string][]Edge, gone map[string]bool) []Edge {
	prev := map[string]Edge{}
	queue := []string{s}
	seen := map[string]bool{s: true}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, e := range out[u] {
			if gone[e.To] {
				continue
			}
			if e.To == s {
				path := []Edge{e}
				for n := u; n != s; n = prev[n].From {
					path = append(path, prev[n])
				}
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return path
			}
			if !seen[e.To] {
				seen[e.To] = true
				prev[e.To] = e
				queue = append(queue, e.To)
			}
		}
	}
	return nil
}

// rotate returns the cycle starting at its smallest node.
func rotate(c []Edge) []Edge {
	m := 0
	for i, e := range c {
		if e.From < c[m].From {
			m = i
		}
	}
	return append(append([]Edge(nil), c[m:]...), c[:m]...)
}

func lessCycle(a, b []Edge) bool {
	a, b = rotate(a), rotate(b)
	for i := range a {
		if a[i].From != b[i].From {
			return a[i].From < b[i].From
		}
	}
	return false
}

func cycleError(c []Edge) StaticError {
	c = rotate(c)
	var rows, keys []string
	var b strings.Builder
	for _, e := range c {
		rows = append(rows, e.From)
		label := "group"
		if e.Key != "" {
			label = e.Key
			keys = append(keys, e.Key)
		}
		fmt.Fprintf(&b, "%q -(%s)-> ", e.From, label)
	}
	fmt.Fprintf(&b, "%q", c[0].From)
	return StaticError{Stage: "static graph", Rule: RuleCycle, Rows: rows, Keys: keys,
		Msg: fmt.Sprintf("dependency cycle of %d rows: %s", len(c), b.String())}
}

// Dependants returns the rows that must restart if the row restarts: every
// row that needs a key it provides, and every child of a group, followed
// transitively, sorted, without the row itself. A row that provides nothing
// has dependants only when it is a group. An id that is not in the graph has
// none.
func (g Graph) Dependants(id string) []string {
	return g.reach([]string{id})
}

// ConsumersOfKeys returns the rows that need any of the keys, in any realm.
func (g Graph) ConsumersOfKeys(keys []string) []string {
	var out []string
	for _, id := range g.Active {
		for _, k := range g.needs[id] {
			if inSet(k, keys...) {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

// reach returns the rows reachable over the reverse edges from the seeds. A
// seed is in the result only when another seed or a cycle reaches it.
func (g Graph) reach(seeds []string) []string {
	seen := map[string]bool{}
	queue := append([]string(nil), seeds...)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, d := range g.rdeps[n] {
			if !seen[d] {
				seen[d] = true
				queue = append(queue, d)
			}
		}
	}
	return sortedKeys(seen)
}

// ProvidedBy returns the keys a row of the given plugin provides, from the
// module table the graph was built with.
func (g Graph) ProvidedBy(plugin string) []string { return g.mods[plugin].Provides }

// NeedsOf returns the keys an active row needs, sorted.
func (g Graph) NeedsOf(id string) []string { return g.needs[id] }
