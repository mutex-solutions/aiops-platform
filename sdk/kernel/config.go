// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
)

// ErrCommitVetoed is what Fiber.Update returns when a listener of the waterfall
// internal/commit does not call next. A veto of the commit stops the update as an
// error does: nothing is retained and the fiber is not restarted. It is an error
// and not a silent success because, unlike a veto of internal/update, the update
// did not happen at all (ADR-034 owner decision 15).
var ErrCommitVetoed = errors.New("config commit vetoed")

// The config pipeline of one fiber. The loader hands the kernel a raw config; the
// kernel holds it, runs the kernel waterfall internal/config over it to get the
// effective config, calls Plugin.Decode on that and gives the result to Apply
// (07-c4-plugin-sdk.md:192, :463; plugin record row 16; ADR-034 owner decision 9).
// The kernel knows no configuration format: the payload of every hook here is any.

// cfgCell holds the raw config of one fiber: the value of the latest mount or
// Update, never decoded in place.
type cfgCell struct {
	root   *Root
	decode func(raw any) (any, error)
	commit func(applied, committed any) // The hand-off of WithCommit (G13); nil for none.
	mu     sync.Mutex
	raw    any
	seq    uint64 // Counts the retains of raw.
	// cur is the running activation of a fiber with a hand-off: what its Apply
	// received, its Std context and the retain it resolved.
	cur struct {
		v   any
		ctx context.Context
		seq uint64
	}
	// cmu serialises the changes of raw after the mount. Invariant: a value is
	// retained only by change, which takes cmu, or by the owner in the turn that
	// admits a batch (batchOp.cell), whose caller holds cmu from before the first
	// internal/update of the batch until the core has admitted or refused it. So a
	// refused batch retains nothing, no activation reads a config its batch then
	// loses, and a newer Update or Commit waits and retains after the admission.
	// cmu is held across nothing but the internal/update hooks, the core's
	// admission, a hand-off and plain assignments, and a batch takes it in
	// ascending fiber id.
	cmu sync.Mutex
}

// retain makes raw the config of every later activation and returns its count.
func (c *cfgCell) retain(raw any) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw = raw
	c.seq++
	return c.seq
}

// change retains raw under the config lock.
func (c *cfgCell) change(raw any) uint64 {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return c.retain(raw)
}

// handOff gives the running activation the config of retain s (G13): committed,
// resolved by vet or stageUpdates, or cfg resolved now, under no lock, when the
// fiber had no target then (loaded is false). It hands nothing when the activation
// ended or holds s already, or once a newer retain passed s, and returns the error
// of that resolve or a panic of the hand-off as a *PanicError.
func (c *cfgCell) handOff(ctx context.Context, s uint64, cfg, committed any, loaded bool) (err error) {
	if c.commit == nil {
		return nil
	}
	if _, ok := c.running(s); !ok {
		return nil
	}
	if !loaded {
		if committed, err = c.resolve(ctx, cfg); err != nil {
			return err
		}
	}
	c.cmu.Lock()
	defer c.cmu.Unlock()
	applied, ok := c.running(s)
	if !ok {
		return nil
	}
	defer func() {
		if p := recover(); p != nil {
			err = &PanicError{Value: p}
		}
	}()
	c.commit(applied, committed)
	return nil
}

// running returns the value of the running activation when s is the latest retain
// and the activation, whose unload has not begun, resolved an older one.
func (c *cfgCell) running(s uint64) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur.v, c.seq == s && c.cur.ctx != nil && c.cur.ctx.Err() == nil && c.cur.seq < s
}

// resolve turns raw into what Apply receives: internal/config, then Decode. A
// panic of Decode is contained as a *PanicError, as a listener's is.
func (c *cfgCell) resolve(ctx context.Context, raw any) (v any, err error) {
	v, err = runInner(ctx, InternalConfig, c.root, raw, func(_ context.Context, a any) (any, error) { return a, nil })
	if err != nil || c.decode == nil {
		return v, err
	}
	defer func() {
		if p := recover(); p != nil {
			v, err = nil, &PanicError{Value: p}
		}
	}()
	return c.decode(v)
}

// prepare is the config of one activation. An error fails the fiber like an error
// of Apply. With a hand-off it records the activation as the running one, and
// resolves again when a retain overtook its resolve, so that no retain falls
// between its resolve and the hand-offs it receives.
func (c *cfgCell) prepare(ctx context.Context) (any, error) {
	for {
		c.mu.Lock()
		raw, s := c.raw, c.seq
		c.mu.Unlock()
		v, err := c.resolve(ctx, raw)
		if err != nil || c.commit == nil {
			return v, err
		}
		c.mu.Lock()
		ok := c.seq == s
		if ok {
			c.cur.v, c.cur.ctx, c.cur.seq = v, ctx, s
		}
		c.mu.Unlock()
		if ok {
			return v, nil
		}
	}
}

// runInner runs w with the kernel's own behaviour innermost (07-c4-plugin-sdk.md:209,
// :386). It reaches the listeners of r's kernel only: the kernel's hooks are
// shared by every kernel of the process.
func runInner[A, R any](ctx context.Context, w *Waterfall[A, R], r *Root, a A, inner func(context.Context, A) (R, error)) (R, error) {
	own := &listener[WaterfallListener[A, R]]{id: r.ctx.frame.id, stage: len(w.def.stages), ctx: r.ctx,
		fn: func(ctx context.Context, a A, _ Next[R]) (R, error) { return inner(ctx, a) }}
	return w.run(ctx, r.aud, a, own)
}

// stageUpdates is the half of every UpdateOpWith and CommitOp of a batch that runs
// plugin code, done before the batch is admitted (ADR-059 decision 2; see
// UpdateOpWith). It marks native[i] as batchKeep where internal/update vetoed the
// restart, and gives native[i] the cell and the config that the owner retains in the
// turn that admits the batch: nothing is retained before, so an activation cannot
// apply the config of a batch the core then refuses. It marks for a hand-off
// (G13) the last operation of each fiber that carries a config when it is a
// CommitOp. It returns done, which the caller must call with the ids of the core's
// batch once the core has admitted it (nil once refused): done releases the config
// locks of cell.cmu, then hands off each CommitOp at whose position the core put
// its fiber's id. A listener of internal/update runs with those locks held, so it
// must not change the config of a fiber of its own batch.
func (r *Root) stageUpdates(ctx context.Context, ops []BatchOp, native []batchOp) (done func(ids []uint64), err error) {
	type staged struct {
		i             int
		id            uint64
		cell          *cfgCell
		cfg, resolved any
		loaded, keep  bool
	}
	var list []staged
	for i, op := range ops {
		if !op.hasCfg || op.fiber.cell == nil {
			continue
		}
		snap, err := op.fiber.ref.snapshot(ctx)
		if err != nil {
			return nil, err
		}
		if !snap.Alive {
			return nil, ErrInactive
		}
		s := staged{i: i, id: op.fiber.ref.id, cell: op.fiber.cell, cfg: op.cfg, loaded: snap.Target != nil, keep: op.kind == batchKeep}
		if s.loaded {
			if s.resolved, err = s.cell.resolve(ctx, op.cfg); err != nil {
				return nil, err
			}
		}
		committed, err := runInner(ctx, InternalCommit, r, op.cfg, func(context.Context, any) (bool, error) { return true, nil })
		if err != nil {
			return nil, err
		}
		if !committed {
			return nil, ErrCommitVetoed
		}
		list = append(list, s)
	}
	var held []*cfgCell
	for _, s := range slices.SortedStableFunc(slices.Values(list), func(a, b staged) int { return cmp.Compare(a.id, b.id) }) {
		if len(held) == 0 || held[len(held)-1] != s.cell {
			s.cell.cmu.Lock()
			held = append(held, s.cell)
		}
	}
	unlock := func() {
		for _, c := range held {
			c.cmu.Unlock()
		}
	}
	for _, s := range list {
		if !s.loaded || s.keep {
			continue
		}
		restart := false
		if _, err := runInner(ctx, InternalUpdate, r, s.resolved, func(context.Context, any) (any, error) { restart = true; return nil, nil }); err != nil {
			unlock()
			return nil, err
		}
		if !restart {
			native[s.i].Kind = batchKeep
		}
	}
	last := make(map[*cfgCell]int, len(list))
	for _, s := range list {
		last[s.cell] = s.i
	}
	for _, s := range list {
		native[s.i].cell, native[s.i].cfg = s.cell, s.cfg
		native[s.i].handOff = ops[s.i].kind == batchKeep && s.cell.commit != nil && last[s.cell] == s.i
	}
	return func(ids []uint64) {
		var offs []func()
		for _, s := range list {
			if s.i < len(ids) && ids[s.i] != 0 {
				s.cell.mu.Lock()
				seq := s.cell.seq // The batch's retain: the batch still holds the lock.
				s.cell.mu.Unlock()
				offs = append(offs, func() { _ = s.cell.handOff(ctx, seq, s.cfg, s.resolved, s.loaded) })
			}
		}
		unlock()
		for _, off := range offs {
			off()
		}
	}, nil
}

// vet is the half of Update and Commit that comes before anything is retained:
// when the fiber has a desired target it resolves cfg through internal/config and
// Decode, then it runs the waterfall internal/commit, which fails closed. It
// returns the resolved config and whether the fiber has a target.
func (f *Fiber) vet(ctx context.Context, cfg any) (resolved any, loaded bool, err error) {
	snap, err := f.ref.snapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	if !snap.Alive {
		return nil, false, ErrInactive
	}
	if loaded = snap.Target != nil; loaded {
		if resolved, err = f.cell.resolve(ctx, cfg); err != nil {
			return nil, false, err
		}
	}
	committed, err := runInner(ctx, InternalCommit, f.cell.root, cfg, func(context.Context, any) (bool, error) { return true, nil })
	if err == nil && !committed {
		err = ErrCommitVetoed
	}
	return resolved, loaded, err
}

// Commit retains cfg as the fiber's config without restarting it (ADR-059 decision
// 2; 07-c4-plugin-sdk.md:205, :1374). It vets cfg exactly as Update does (see
// Update): when the fiber has a desired target it resolves cfg through
// internal/config and Decode, then it runs the waterfall internal/commit, which
// fails closed. An error, or a veto (ErrCommitVetoed), retains nothing. Only a
// commit that completes retains cfg. Then no internal/update runs, the fiber is not
// restarted and its Apply is not called again: a plugin learns the new value from
// whoever commits it (the loader dispatches loader/volatile-update) and, with a
// hand-off (WithCommit), from its Volatile fields; without one the running
// activation keeps the value it was applied with. The next activation of the
// fiber, whatever starts it, resolves the retained cfg. With a hand-off, Commit
// hands cfg off before it returns (ADR-034 G13): to the running activation, the
// latest whose config was resolved for its Apply, unless its unload has begun or
// it resolved this cfg itself. When the fiber had no target at the vet, cfg is
// resolved now for such an activation; an error of it, or a panic of the hand-off
// as a *PanicError, hands nothing more, restarts nothing and is returned. A fiber
// mounted with When takes no config and Commit ignores cfg. Changes of one fiber's
// config, and hand-offs, are serialised with those of Update and Batch (see
// cfgCell.cmu). Decode must be pure.
func (f *Fiber) Commit(ctx context.Context, cfg any) error {
	if f.cell == registryCell {
		return errRegistryConfig
	}
	if f.cell == nil {
		return nil
	}
	resolved, loaded, err := f.vet(ctx, cfg)
	if err != nil {
		return err
	}
	return f.cell.handOff(ctx, f.cell.change(cfg), cfg, resolved, loaded)
}

// Update replaces the fiber's config with cfg and restarts it, and returns once
// the request is admitted; use Await for the restart (07-c4-plugin-sdk.md:205,
// :239, :358; ADR-034 owner decision 9). The contract gives no signature: this
// shape is an open point.
//
// Whenever the fiber is alive and has a desired target (it is ACTIVE, LOADING or
// UNLOADING with a restart pending, as the prototype's "loaded"), Update first
// resolves cfg through internal/config and Decode, so internal/config sees the
// candidate at the time of the update, not at the next activation. If that fails,
// Update returns the error in every state, retains nothing and leaves a running
// activation untouched (ADR-034 owner decision 13). Otherwise it runs
// the waterfall internal/commit over cfg, whose result reports that the kernel's
// own innermost step was reached. A commit listener that errors or panics fails
// closed (decision 12), and one that does not call next vetoes the commit
// (ErrCommitVetoed); either way Update returns that error, retains nothing, runs
// no internal/update, does not restart the fiber and leaves a running activation
// untouched (decision 15). Only a commit that completes retains cfg, and Update
// then restarts the fiber through the
// waterfall internal/update, whose payload is the resolved config: a listener that
// does not call next vetoes the restart, and the retained cfg then applies at the
// next restart. A fiber with no target (gated on a need, FAILED, a disposal
// requested) skips resolution and the waterfall: cfg is retained and the
// activation that follows resolves it and fails the fiber on an error. A fiber
// mounted with When takes no config and Update ignores cfg.
//
// Decode must be pure: it runs here for the candidate and again at the activation
// that applies the retained cfg.
func (f *Fiber) Update(ctx context.Context, cfg any) error {
	if f.cell == registryCell && cfg != nil {
		return errRegistryConfig
	}
	if f.cell == nil || f.cell == registryCell {
		return f.ref.update(ctx)
	}
	resolved, loaded, err := f.vet(ctx, cfg)
	if err != nil {
		return err
	}
	f.cell.change(cfg)
	if !loaded {
		return f.ref.update(ctx)
	}
	_, err = runInner(ctx, InternalUpdate, f.cell.root, resolved, func(ctx context.Context, _ any) (any, error) {
		return nil, f.ref.update(ctx)
	})
	return err
}
