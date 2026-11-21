// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// plan is what the operations of one Apply come to. Everything but the kernel
// calls is decided under one hold of the tree's lock, so a group that mounts its
// children meanwhile sees the whole plan or none of it.
type plan struct {
	snap     snapshot
	dispose  []held // the topmost fiber of each branch the plan retires
	retired  []held // every fiber the plan retires
	vets     []kernel.BatchOp
	updates  []held
	mounts   []string // ids of the entries to mount, in order
	announce []def.VolatileUpdateArgs
	renewed  map[string]bool // entries whose fiber the plan replaces
	results  []def.Result
}

// held is a fiber of an entry, or an operation on it.
type held struct {
	id    string
	fiber *kernel.Fiber
	op    kernel.BatchOp
}

// snapshot is the tree before a plan, for a plan that the kernel refuses.
type snapshot struct {
	entries map[string]entry
	order   []string
	prev    map[string][]*kernel.Fiber
}

func (t *tree) snapshot() snapshot {
	s := snapshot{entries: make(map[string]entry, len(t.entries)), order: slices.Clone(t.order), prev: maps.Clone(t.prev)}
	for id, e := range t.entries {
		s.entries[id] = *e
	}
	return s
}

// restore puts the tree back as s holds it. Only a group activation that ended
// meanwhile can have changed it, since one that mounts waits for excl, and that
// activation's children went with it: they are forgotten again.
func (t *tree) restore(s snapshot) {
	t.entries, t.order, t.prev = make(map[string]*entry, len(s.entries)), s.order, s.prev
	for id, e := range s.entries {
		t.entries[id] = &e
	}
	for id, e := range t.entries {
		if e.kids != nil && e.kids.Std().Err() != nil {
			t.forget(id, e.kids)
		}
	}
}

// Apply applies a plan of reload: remove, update and create, in one kernel batch
// (03-c3-kernel.md:84, :379). The operations are checked, in order, against the tree
// as the earlier ones leave it, and a refused one refuses the plan before anything
// is committed (the tree is put back). The kernel batch holds every dispose, every
// update with its config and every mount that may be created at once; it is
// refused whole or committed whole.
//
// The loader never creates an entry for an id whose previous fiber is not DISPOSED
// (07-c4-plugin-sdk.md:341; plugin-system-2026-11.md:368: "dsh recomposeProfile
// waits for disposal"), and a batch commits its desired state before any fiber
// unloads, so a mount of an id that was removed or replaced, in this plan or in an
// earlier one, goes in a second batch after the wait for that disposal. The docs do
// not say what happens when the wait is cut short: the first batch stays committed,
// the entry stays in the tree with no fiber, and the error is returned (open point).
//
// A group activation that mounts its children waits while a plan is staged and its
// first batch runs, and the plan waits for it, so a refused plan is put back exactly
// and no activation mounts a child of a plan that is then refused.
func (t *tree) Apply(ctx context.Context, ops []def.Op) ([]def.Result, error) {
	if t.batch == nil {
		return nil, def.ErrUnsupported
	}
	select {
	case t.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-t.sem }()
	t.sweep(ctx)
	p, later, err := t.first(ctx, ops)
	if err != nil {
		return nil, err
	}
	for _, a := range p.announce {
		_ = t.volatile.Emit(ctx, kernel.Unscoped, a) // the commit is done: see adopt
	}
	if len(later) == 0 {
		return p.results, nil
	}
	if err := t.settle(ctx, later); err != nil {
		return p.results, err
	}
	items := t.mountItems(ctx, later)
	var kops []kernel.BatchOp
	for _, m := range items {
		kops = append(kops, m.op)
	}
	return p.results, t.commit(ctx, kops, items)
}

// first stages ops and commits the plan's first batch, holding excl, and puts the
// tree back when the kernel refuses the batch. It returns the mounts that wait for
// a previous fiber.
func (t *tree) first(ctx context.Context, ops []def.Op) (*plan, []string, error) {
	select {
	case t.excl <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	defer func() { <-t.excl }()
	p, err := t.stage(ctx, ops)
	if err != nil {
		return nil, nil, err
	}
	now, later := t.split(p.mounts)
	items := t.mountItems(ctx, now)
	kops := p.vets
	for _, d := range p.dispose {
		kops = append(kops, kernel.DisposeOp(d.fiber))
	}
	for _, u := range p.updates {
		kops = append(kops, u.op)
	}
	for _, m := range items {
		kops = append(kops, m.op)
	}
	if err := t.commit(ctx, kops, items); err != nil {
		t.mu.Lock()
		t.restore(p.snap)
		t.mu.Unlock()
		return nil, nil, err
	}
	return p, later, nil
}

// commit runs the kernel batch and adopts the fibers of its mounts, which are its
// last operations.
func (t *tree) commit(ctx context.Context, kops []kernel.BatchOp, items []mountItem) error {
	if len(kops) == 0 {
		return nil
	}
	fibers, err := t.batch(ctx, kops...)
	if err != nil {
		return err
	}
	for i, m := range items {
		t.adopt(ctx, m, fibers[len(kops)-len(items)+i])
	}
	return nil
}

// split sorts ids into the mounts that may go in the first batch and those that
// wait for a previous fiber of the same id.
func (t *tree) split(ids []string) (now, later []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range ids {
		if len(t.prev[id]) == 0 {
			now = append(now, id)
		} else {
			later = append(later, id)
		}
	}
	return now, later
}

// mountItems builds the mount of each entry of ids that still exists and whose
// group is active: the others are mounted by their group when it activates.
func (t *tree) mountItems(ctx context.Context, ids []string) []mountItem {
	var items []mountItem
	t.mu.Lock()
	seen := map[string]bool{}
	for _, id := range ids {
		if e := t.entries[id]; e != nil && !seen[id] {
			seen[id] = true
			// A group that activated meanwhile has mounted the entry itself, or is
			// mounting it (the plan's second batch does not hold excl).
			if pc := t.parentCtx(e); pc != nil && pc != e.claim && (e.fiber == nil || gone(ctx, e.fiber)) {
				items = append(items, mountItem{spec: e.spec, pc: pc})
			}
		}
	}
	t.mu.Unlock()
	for i := range items {
		p, cfg, mc := t.build(ctx, items[i].pc, items[i].spec)
		items[i].mc, items[i].op = mc, mc.MountOp(p, cfg)
	}
	return items
}

// stage checks the operations and applies them to the tree, which is the desired
// state that a group reads when it mounts its children.
func (t *tree) stage(ctx context.Context, ops []def.Op) (*plan, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lc == nil {
		return nil, kernel.ErrInactive
	}
	p := &plan{snap: t.snapshot(), renewed: map[string]bool{}}
	for i, op := range ops {
		r, err := t.stageOne(ctx, p, op)
		if err != nil {
			t.restore(p.snap)
			return nil, fmt.Errorf("operation %d (%s %q): %w", i, op.Kind, op.Entry.ID, err)
		}
		p.results = append(p.results, r)
	}
	// A plan never disposes a running fiber to mount its entry again with a config that
	// does not resolve (08-profiles-roadmap-adrs.md §1.6: "Rejected; the row keeps its
	// instance"). The batch first commits the entry's new config to the fiber it then
	// disposes, its own or its group's, which has the entry's plugin, so the kernel
	// resolves it through internal/config and Decode while the fiber has its target, as
	// for an update in place, and refuses the whole batch on an error. A row that gains
	// its first reference is one: its inject gains configRefsReady, which replaces it.
	// So is each child of a group the plan replaces, whatever the order of the
	// operations. A fiber whose plugin changes cannot vet the config: the launcher's
	// admission does (boot profileContext.Admit).
	for _, d := range p.retired {
		e, was := t.entries[d.id], p.snap.entries[d.id]
		if e != nil && t.mount(e) == def.Mounted && !isGroup(e.spec.Plugin) && was.fiber == d.fiber && was.spec.Plugin == e.spec.Plugin {
			p.vets = append(p.vets, kernel.CommitOp(d.fiber, e.spec.Config))
		}
	}
	return p, nil
}

// retireIn gives up for p the fibers of id and of the entries beneath it, which stay
// in the tree: each fiber that is not DISPOSED becomes the previous fiber of its id, so
// that no entry of that id is created until it is. p disposes the topmost of each
// branch, the others go with their group, and vets each (stage). It takes back what p
// queued for those entries: the kernel refuses an update or a commit of a fiber after
// the batch disposes it, and an entry with no activation left is told nothing.
func (t *tree) retireIn(ctx context.Context, p *plan, id string) {
	ids := t.subtree(id)
	covered := map[string]bool{}
	for _, x := range ids {
		e := t.entries[x]
		live := e.fiber != nil && !gone(ctx, e.fiber)
		if live {
			if !covered[e.spec.Parent] {
				p.dispose = append(p.dispose, held{id: x, fiber: e.fiber})
			}
			p.retired = append(p.retired, held{id: x, fiber: e.fiber})
			t.addPrev(x, e.fiber)
		}
		covered[x] = live || covered[e.spec.Parent]
		e.fiber, e.kids = nil, nil
	}
	p.updates = slices.DeleteFunc(p.updates, func(u held) bool { return slices.Contains(ids, u.id) })
	p.announce = slices.DeleteFunc(p.announce, func(a def.VolatileUpdateArgs) bool { return slices.Contains(ids, a.ID) })
}

func (t *tree) stageOne(ctx context.Context, p *plan, op def.Op) (def.Result, error) {
	e := cloneSpec(op.Entry)
	cur, r := t.entries[e.ID], def.Result{ID: e.ID}
	if e.ID == "" { // "" names the root: an entry of that id would be its own parent
		return r, def.ErrEmptyID
	}
	switch op.Kind {
	case def.OpCreate:
		if cur != nil {
			return r, def.ErrExists
		}
		if g := t.entries[e.Parent]; e.Parent != "" && g == nil {
			return r, def.ErrParent
		} else if g != nil && !isGroup(g.spec.Plugin) {
			return r, def.ErrNotGroup
		}
		cur = &entry{spec: e}
		t.entries[e.ID], t.order = cur, append(t.order, e.ID)
		r.Outcome = def.Recorded
		if t.mount(cur) == def.Mounted {
			p.mounts, r.Outcome = append(p.mounts, e.ID), def.Created
		}
	case def.OpRemove:
		if cur == nil {
			return r, def.ErrNotFound
		}
		t.retireIn(ctx, p, e.ID)
		for _, id := range t.subtree(e.ID) {
			delete(t.entries, id)
		}
		t.order = slices.DeleteFunc(t.order, func(id string) bool { return t.entries[id] == nil })
		r.Outcome = def.Removed
	case def.OpUpdate:
		switch {
		case cur == nil:
			return r, def.ErrNotFound
		case e.Parent != cur.spec.Parent:
			return r, def.ErrParent
		}
		r.Outcome = t.stageUpdate(ctx, p, cur, e)
	default:
		return r, fmt.Errorf("unknown operation kind %q", op.Kind)
	}
	return r, nil
}

// stageUpdate decides what a new spec of an existing entry does (07-c4-plugin-sdk.md:1374,
// :1375). A change of the plugin, inject, isolate or intercept replaces the fiber,
// because a fiber's needs and context are fixed when it is mounted (stage vets the new
// config on the fiber it disposes); an entry under a group that the plan replaces is
// mounted by the group's next activation, so a change of its config restarts it; a change of
// config restarts it with the new config, unless only volatile paths changed
// (def.VolatileOnly) and the fiber is ACTIVE, as in the oracle: a FAILED or PENDING
// fiber has no activation to take values in place. A volatile commit is the tree's
// new config, the same config retained by the kernel with kernel.CommitOp in the
// plan's batch, and a loader/volatile-update event once the batch is committed. The
// two records change
// together or not at all: a plan whose batch the kernel refuses, for a config that
// internal/config, Decode or internal/commit refuses as for any other, puts the tree
// back and the kernel's retained config is put back with the batch, and the error is
// returned. The running activation is not restarted and its Apply is not called
// again: the consumer of the event makes the new values visible, and a restart that
// the kernel starts later (a provider's epoch change, a retry) applies the committed
// config.
func (t *tree) stageUpdate(ctx context.Context, p *plan, cur *entry, e def.EntrySpec) def.Outcome {
	old, was := cur.spec, t.mount(cur)
	cur.spec = e
	now := t.mount(cur)
	if cur.fiber != nil && gone(ctx, cur.fiber) {
		cur.fiber = nil
	}
	mount := func(o def.Outcome) def.Outcome { p.mounts = append(p.mounts, e.ID); return o }
	switch {
	case was != def.Mounted && now != def.Mounted:
		return def.Unchanged
	case was != def.Mounted:
		return mount(def.Created)
	case now != def.Mounted:
		t.retireIn(ctx, p, e.ID)
		return def.Unmounted
	case old.Plugin != e.Plugin || !sameSet(old.Inject, e.Inject) || !sameSet(old.Isolate, e.Isolate) || !sameIntercept(old.Intercept, e.Intercept):
		// The entries beneath are mounted again too: an earlier commit in place of one
		// is a restart, whose config stage vets.
		ids := t.subtree(e.ID)
		for i, r := range p.results {
			if r.Outcome == def.Committed && slices.Contains(ids, r.ID) {
				p.results[i].Outcome = def.Restarted
			}
		}
		t.retireIn(ctx, p, e.ID)
		p.renewed[e.ID] = true
		return mount(def.Restarted)
	case cur.fiber == nil:
		switch {
		case t.parentCtx(cur) != nil:
			return mount(def.Created)
		case t.renewedAbove(p, cur) && !isGroup(e.Plugin) && !reflect.DeepEqual(old.Config, e.Config):
			// The group's next activation mounts the entry with its new config.
			return def.Restarted
		}
		return def.Unchanged
	case isGroup(e.Plugin): // a group has no config of its own
		return def.Unchanged
	}
	var volatile []string
	if ce, ok := t.cat.Lookup(e.Plugin); ok {
		volatile = ce.Volatile
	}
	paths, only := def.VolatileOnly(old.Config, e.Config, volatile)
	switch {
	case len(paths) == 0:
		return def.Unchanged
	case only && active(ctx, cur.fiber): // a fiber that is not ACTIVE runs no activation to tell
		p.updates = append(p.updates, held{id: e.ID, op: kernel.CommitOp(cur.fiber, e.Config)})
		p.announce = append(p.announce, def.VolatileUpdateArgs{ID: e.ID, Config: e.Config, Paths: paths})
		return def.Committed
	}
	p.updates = append(p.updates, held{id: e.ID, op: kernel.UpdateOpWith(cur.fiber, e.Config)})
	return def.Restarted
}

// renewedAbove reports whether p replaces the fiber of a group above e.
func (t *tree) renewedAbove(p *plan, e *entry) bool {
	for g := t.entries[e.spec.Parent]; g != nil; g = t.entries[g.spec.Parent] {
		if p.renewed[g.spec.ID] {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

func sameIntercept(a, b map[string]any) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}

// subtree lists id and the ids beneath it, parents first.
func (t *tree) subtree(id string) []string {
	out := []string{id}
	for _, k := range t.order {
		if k != id && slices.Contains(out, t.entries[k].spec.Parent) {
			out = append(out, k)
		}
	}
	return out
}

// gone reports whether f is DISPOSED, or was and its record was evicted. A failed
// question is not an answer: the fiber is taken to be there.
func gone(ctx context.Context, f *kernel.Fiber) bool {
	st, err := f.State(ctx)
	return st == kernel.Disposed || errors.Is(err, kernel.ErrExpired)
}

func active(ctx context.Context, f *kernel.Fiber) bool {
	st, _ := f.State(ctx)
	return st == kernel.Active
}

// settle waits until every previous fiber of the ids is DISPOSED, the rule of
// 07-c4-plugin-sdk.md:341. A fiber that settles in another state is refused with
// def.ErrNotDisposed.
func (t *tree) settle(ctx context.Context, ids []string) error {
	for _, id := range ids {
		t.mu.Lock()
		fibers := t.prev[id]
		t.mu.Unlock()
		for _, f := range fibers {
			_ = f.Await(ctx) // the startup error of a fiber that failed is not the question
			if err := ctx.Err(); err != nil {
				return err
			}
			if !gone(ctx, f) {
				return fmt.Errorf("entry %q: %w", id, def.ErrNotDisposed)
			}
		}
		t.mu.Lock()
		delete(t.prev, id)
		t.mu.Unlock()
	}
	return nil
}

// sweep forgets the previous fibers that are DISPOSED.
func (t *tree) sweep(ctx context.Context) {
	t.mu.Lock()
	prev := maps.Clone(t.prev)
	t.mu.Unlock()
	var dead []*kernel.Fiber
	for _, fibers := range prev {
		for _, f := range fibers {
			if gone(ctx, f) {
				dead = append(dead, f)
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for id := range t.prev {
		t.prev[id] = slices.DeleteFunc(slices.Clone(t.prev[id]), func(f *kernel.Fiber) bool { return slices.Contains(dead, f) })
		if len(t.prev[id]) == 0 {
			delete(t.prev, id)
		}
	}
}
