package boot

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The required-set audit (00-decisions.md section 6.1; 03-c3-kernel.md:359,
// 361; 08-profiles-roadmap-adrs.md:451-459). "Required" is one concept with
// two sources: the signed distribution manifest, which no layer can remove,
// and required: true on a row, which a layer may add and never remove. The
// set is the keys that must have an ACTIVE provider and the rows named by id.
// A required key or row that is absent, disabled or not ACTIVE stops boot;
// every other row that is not ACTIVE is one grouped warning.
//
// The audit has a static half, run by `aiopsctl profile render` before any
// row mounts (AuditStatic), and a run half over a view of the fiber states
// that the launcher feeds from the kernel later (AuditRequired).

// State is the state of a fiber, in capitals as the failure matrix names it
// (test/failure-matrix/matrix.yaml). boot defines its own so that it does not
// import the kernel.
type State string

// The six fiber states.
const (
	StatePending   State = "PENDING"
	StateLoading   State = "LOADING"
	StateActive    State = "ACTIVE"
	StateFailed    State = "FAILED"
	StateUnloading State = "UNLOADING"
	StateDisposed  State = "DISPOSED"
)

// StateView is a read-only view of the tree: the state of each row and
// whether each key has an ACTIVE provider. A row that is not in Rows has no
// fiber. The run path builds it from the kernel; a test builds it by hand.
type StateView struct {
	Rows      map[string]State
	Providers map[string]bool
}

// RequiredSet is the required set of one process: the ids of the required
// rows and the keys that need an ACTIVE provider, both sorted.
type RequiredSet struct {
	Rows []string
	Keys []string
}

// RequiredFor computes the required set. A row is required when its own
// required field is set or the distribution manifest names it. With roles, the
// set applies to the rows that role runs (08-profiles-roadmap-adrs.md:459): a
// required row of another role drops out, and so does a required key whose
// providers in the composition all run in another role. A key with no provider
// row at all stays, and the audit reports it. When a row inside the set holds a
// reference, a required row of the role or an active provider of a required key in the
// root realm, the launcher adds the config-refs rows that serve the process (refsRows)
// and configRefsReady to the set (ADR-059 decision 18): the row is applied only while
// config-refs is. That key stays even when every config-refs row runs in another role.
func RequiredFor(c Composition, mods ModuleTable, dist DistManifest, roles []Role) RequiredSet {
	in, active := RoleFilter(roles), ActiveRows(c, roles)
	b := builder{rows: make(map[string]ComposedRow, len(c.Rows))}
	for _, r := range c.Rows {
		b.rows[r.ID] = r
	}
	rows := map[string]bool{}
	for _, id := range dist.RequiredRows {
		rows[id] = true
	}
	for _, r := range c.Rows {
		if r.Required {
			rows[r.ID] = true
		}
	}
	for _, r := range c.Rows {
		if !in(r.Row) {
			delete(rows, r.ID)
		}
	}
	want := dist.RequiredKeys
	inside := func(r ComposedRow) bool {
		return rows[r.ID] || active[r.ID] && slices.ContainsFunc(mods[r.Plugin].Provides, func(k string) bool {
			return inSet(k, dist.RequiredKeys...) && b.realm(r.ID, k) == ""
		})
	}
	refs := slices.ContainsFunc(c.Rows, func(r ComposedRow) bool { return len(r.Refs) > 0 && r.Plugin != ConfigRefsID && inside(r) })
	if refs {
		want = union(want, []string{configRefsReady})
		for _, id := range refsRows(c, in, active, b) {
			rows[id] = true
		}
	}
	keys := []string{}
	for _, k := range want {
		providers, local := 0, 0
		for _, r := range c.Rows {
			if inSet(k, mods[r.Plugin].Provides...) {
				providers++
				if in(r.Row) {
					local++
				}
			}
		}
		// config-refs serves its own process only, so one of another role never meets
		// the need decision 18 adds.
		if providers == 0 || local > 0 || refs && k == configRefsReady {
			keys = append(keys, k)
		}
	}
	return RequiredSet{Rows: sortedKeys(rows), Keys: keys}
}

// refsRows returns the config-refs rows that serve the process: active in the roles
// and providing configRefsReady in the root realm. A disabled one, one inside a
// disabled group and one of a private realm do not. With none, it returns every
// config-refs row of the roles, so that the audit names the row that does not serve.
func refsRows(c Composition, in func(Row) bool, active map[string]bool, b builder) []string {
	var serving, all []string
	for _, r := range c.Rows {
		if r.Plugin != ConfigRefsID || !in(r.Row) {
			continue
		}
		all = append(all, r.ID)
		if active[r.ID] && b.realm(r.ID, configRefsReady) == "" {
			serving = append(serving, r.ID)
		}
	}
	if len(serving) == 0 {
		return all
	}
	return serving
}

// observability returns the keys that are never in a closure
// (08-profiles-roadmap-adrs.md:459): they are never a Need.
func observability() []string { return []string{"health", "logger", "metrics", "tracer"} }

// RequiredClosure returns the active rows of the required set and the rows
// they need, followed transitively and sorted: the static closure of its
// needs that the dump prints. Observability keys never enter it.
func RequiredClosure(g Graph, set RequiredSet) []string {
	in := map[string]bool{}
	var queue []string
	add := func(id string) {
		if !in[id] {
			in[id] = true
			queue = append(queue, id)
		}
	}
	for _, id := range set.Rows {
		if inSet(id, g.Active...) {
			add(id)
		}
	}
	for _, p := range g.Providers {
		if inSet(p.Key, set.Keys...) && p.Realm == "" {
			for _, id := range p.Rows {
				add(id)
			}
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range g.Edges {
			if e.From == n && !inSet(e.Key, observability()...) {
				add(e.To)
			}
		}
	}
	return sortedKeys(in)
}

// AuditStatic is the static half of the audit: it needs no running fiber.
// Every required row must be present, enabled and active in the selected
// roles; every required key must have a provider among the active rows; and
// the static need closure of the required set must hold no provider outside
// it, which is the last clause of gate check 7 (08-profiles-roadmap-adrs.md:503).
// A provider is inside the required set when its row is required or it provides
// a required key; the edge from a child to its group is not a need, and neither is
// the need of configRefsReady, which RequiredFor adds with config-refs where such a
// provider holds a reference (ADR-059 decision 18).
func AuditStatic(c Composition, mods ModuleTable, dist DistManifest, g Graph, roles []Role) []StaticError {
	set := RequiredFor(c, mods, dist, roles)
	var errs []StaticError
	for _, id := range set.Rows {
		if inSet(id, g.Active...) {
			continue
		}
		why := "is absent"
		if _, ok := findRow(c, id); ok {
			why = "is disabled, detached by the gate, or inside a group that is"
		}
		errs = append(errs, StaticError{Stage: "required set", Rule: RuleRequiredMissing, Rows: []string{id},
			Msg: fmt.Sprintf("required row %q %s", id, why)})
	}
	for _, k := range set.Keys {
		found := false
		for _, p := range g.Providers {
			found = found || p.Key == k && p.Realm == ""
		}
		if !found {
			errs = append(errs, StaticError{Stage: "required set", Rule: RuleRequiredKey, Keys: []string{k},
				Msg: fmt.Sprintf("required key %q has no provider among the active rows", k)})
		}
	}
	inside := map[string]bool{}
	for _, id := range set.Rows {
		inside[id] = true
	}
	for _, p := range g.Providers {
		if inSet(p.Key, set.Keys...) && p.Realm == "" {
			for _, id := range p.Rows {
				inside[id] = true
			}
		}
	}
	closure := RequiredClosure(g, set)
	for _, e := range g.Edges {
		if e.Key == "" || e.Key == configRefsReady || inside[e.To] || !inSet(e.From, closure...) || inSet(e.Key, observability()...) {
			continue
		}
		errs = append(errs, StaticError{Stage: "required set", Rule: RuleRequiredClosure, Rows: []string{e.From, e.To}, Keys: []string{e.Key},
			Msg: fmt.Sprintf("row %q is in the static closure of the required set and needs key %q from row %q, which is outside the required set", e.From, e.Key, e.To)})
	}
	return errs
}

func findRow(c Composition, id string) (ComposedRow, bool) {
	for _, r := range c.Rows {
		if r.ID == id {
			return r, true
		}
	}
	return ComposedRow{}, false
}

// AuditRequired is the run half, over the states observed once the tree has
// settled (03-c3-kernel.md:361). Every active required row must be ACTIVE and
// every required key must have an ACTIVE provider: otherwise boot stops. Every
// other active row that is not ACTIVE is named in one grouped warning, with its
// state and the keys it waits for.
func AuditRequired(g Graph, set RequiredSet, v StateView) ([]StaticError, []StaticWarning) {
	var errs []StaticError
	required := map[string]bool{}
	for _, id := range set.Rows {
		required[id] = true
		if inSet(id, g.Active...) && v.Rows[id] != StateActive {
			errs = append(errs, StaticError{Stage: "required set", Rule: RuleRequiredMissing, Rows: []string{id},
				Msg: fmt.Sprintf("required row %q is %s, not ACTIVE%s", id, stateName(v.Rows[id]), waitsFor(g, v, id))})
		}
	}
	for _, k := range set.Keys {
		if !v.Providers[k] {
			errs = append(errs, StaticError{Stage: "required set", Rule: RuleRequiredKey, Keys: []string{k},
				Msg: fmt.Sprintf("required key %q has no ACTIVE provider", k)})
		}
	}
	var rows, parts []string
	keys := map[string]bool{}
	for _, id := range g.Active {
		if required[id] || v.Rows[id] == StateActive {
			continue
		}
		rows = append(rows, id)
		parts = append(parts, fmt.Sprintf("%q is %s%s", id, stateName(v.Rows[id]), waitsFor(g, v, id)))
		for _, k := range g.needs[id] {
			if !v.Providers[k] {
				keys[k] = true
			}
		}
	}
	if len(rows) == 0 {
		return errs, nil
	}
	return errs, []StaticWarning{{Stage: "required set", Rule: RuleNoProvider, Rows: rows, Keys: sortedKeys(keys),
		Msg: fmt.Sprintf("%d rows are not ACTIVE: %s", len(rows), strings.Join(parts, "; "))}}
}

func stateName(s State) string {
	if s == "" {
		return "without a fiber"
	}
	return string(s)
}

// waitsFor returns " and waits for k1, k2" for the keys of a row that have no
// ACTIVE provider, or "".
func waitsFor(g Graph, v StateView, id string) string {
	var ks []string
	for _, k := range g.needs[id] {
		if !v.Providers[k] {
			ks = append(ks, k)
		}
	}
	if len(ks) == 0 {
		return ""
	}
	sort.Strings(ks)
	return " and waits for " + strings.Join(ks, ", ")
}
