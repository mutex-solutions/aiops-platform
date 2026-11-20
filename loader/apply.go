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
	dispose  []*kernel.Fiber
	updates  []kernel.BatchOp
	mounts   []string // ids of the entries to mount, in order
	announce []def.VolatileUpdateArgs
	results  []def.Result
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

func (t *tree) restore(s snapshot) {
	t.entries, t.order, t.prev = make(map[string]*entry, len(s.entries)), s.order, s.prev
	for id, e := range s.entries {
		t.entries[id] = &e
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
	p, err := t.stage(ctx, ops)
	if err != nil {
		return nil, err
	}
	now, later := t.split(p.mounts)
	items := t.mountItems(ctx, now)
	var kops []kernel.BatchOp
	for _, f := range p.dispose {
		kops = append(kops, kernel.DisposeOp(f))
	}
	kops = append(kops, p.updates...)
	for _, m := range items {
		kops = append(kops, m.op)
	}
	if err := t.commit(ctx, kops, items); err != nil {
		t.mu.Lock()
		t.restore(p.snap)
		t.mu.Unlock()
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
	items = t.mountItems(ctx, later)
	kops = nil
	for _, m := range items {
		kops = append(kops, m.op)
	}
	return p.results, t.commit(ctx, kops, items)
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
			// A group that activated meanwhile has mounted the entry itself.
			if pc := t.parentCtx(e); pc != nil && (e.fiber == nil || gone(ctx, e.fiber)) {
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
	p := &plan{snap: t.snapshot()}
	for i, op := range ops {
		r, err := t.stageOne(ctx, p, op)
		if err != nil {
			t.restore(p.snap)
			return nil, fmt.Errorf("operation %d (%s %q): %w", i, op.Kind, op.Entry.ID, err)
		}
		p.results = append(p.results, r)
	}
	return p, nil
}

func (t *tree) stageOne(ctx context.Context, p *plan, op def.Op) (def.Result, error) {
	e := cloneSpec(op.Entry)
	cur, r := t.entries[e.ID], def.Result{ID: e.ID}
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
		p.dispose = append(p.dispose, t.retire(ctx, e.ID)...)
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
// because a fiber's needs and context are fixed when it is mounted; a change of
// config restarts it with the new config, unless only volatile paths changed
// (volatile.go) and the fiber is ACTIVE, as in the oracle: a FAILED or PENDING
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
		p.dispose = append(p.dispose, t.retire(ctx, e.ID)...)
		return def.Unmounted
	case old.Plugin != e.Plugin || !sameSet(old.Inject, e.Inject) || !sameSet(old.Isolate, e.Isolate) || !sameIntercept(old.Intercept, e.Intercept):
		p.dispose = append(p.dispose, t.retire(ctx, e.ID)...)
		return mount(def.Restarted)
	case cur.fiber == nil:
		if t.parentCtx(cur) != nil {
			return mount(def.Created)
		}
		return def.Unchanged
	case isGroup(e.Plugin): // a group has no config of its own
		return def.Unchanged
	}
	var volatile []string
	if ce, ok := t.cat.Lookup(e.Plugin); ok {
		volatile = ce.Volatile
	}
	paths, only := volatileOnly(old.Config, e.Config, volatile)
	switch {
	case len(paths) == 0:
		return def.Unchanged
	case only && active(ctx, cur.fiber): // a fiber that is not ACTIVE runs no activation to tell
		p.updates = append(p.updates, kernel.CommitOp(cur.fiber, e.Config))
		p.announce = append(p.announce, def.VolatileUpdateArgs{ID: e.ID, Config: e.Config, Paths: paths})
		return def.Committed
	}
	p.updates = append(p.updates, kernel.UpdateOpWith(cur.fiber, e.Config))
	return def.Restarted
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

// retire gives up the fibers of id and of the entries beneath it, which stay in the
// tree: each fiber that is not DISPOSED becomes the previous fiber of its id, so that
// no entry of that id is created until it is. It returns the fibers to dispose, the
// topmost of each branch: the others go with their group.
func (t *tree) retire(ctx context.Context, id string) (dispose []*kernel.Fiber) {
	covered := map[string]bool{}
	for _, x := range t.subtree(id) {
		e := t.entries[x]
		live := e.fiber != nil && !gone(ctx, e.fiber)
		if live {
			if !covered[e.spec.Parent] {
				dispose = append(dispose, e.fiber)
			}
			t.addPrev(x, e.fiber)
		}
		covered[x] = live || covered[e.spec.Parent]
		e.fiber, e.kids = nil, nil
	}
	return dispose
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
