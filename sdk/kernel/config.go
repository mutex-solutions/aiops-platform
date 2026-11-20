// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
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
	mu     sync.Mutex
	raw    any
}

func (c *cfgCell) load() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.raw
}

func (c *cfgCell) retain(raw any) {
	c.mu.Lock()
	c.raw = raw
	c.mu.Unlock()
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
// of Apply.
func (c *cfgCell) prepare(ctx context.Context) (any, error) {
	return c.resolve(ctx, c.load())
}

// runInner runs w with the kernel's own behaviour innermost (07-c4-plugin-sdk.md:209,
// :386). It reaches the listeners of r's kernel only: the kernel's hooks are
// shared by every kernel of the process.
func runInner[A, R any](ctx context.Context, w *Waterfall[A, R], r *Root, a A, inner func(context.Context, A) (R, error)) (R, error) {
	own := &listener[WaterfallListener[A, R]]{id: r.ctx.frame.id, stage: len(w.def.stages), ctx: r.ctx,
		fn: func(ctx context.Context, a A, _ Next[R]) (R, error) { return inner(ctx, a) }}
	return w.run(ctx, r.aud, a, own)
}

// stageUpdates is the half of every UpdateOpWith of a batch that runs plugin code,
// done before the batch is admitted (ADR-059 decision 2; see UpdateOpWith). It
// marks native[i] as batchKeep where internal/update vetoed the restart. It returns
// the function that puts the previous configs back, for a batch the core refuses.
func (r *Root) stageUpdates(ctx context.Context, ops []BatchOp, native []batchOp) (undo func(), err error) {
	type staged struct {
		i                  int
		cell               *cfgCell
		cfg, resolved, old any
		loaded             bool
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
		s := staged{i: i, cell: op.fiber.cell, cfg: op.cfg, loaded: snap.Target != nil}
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
	for _, s := range list {
		if !s.loaded {
			continue
		}
		restart := false
		if _, err := runInner(ctx, InternalUpdate, r, s.resolved, func(context.Context, any) (any, error) { restart = true; return nil, nil }); err != nil {
			return nil, err
		}
		if !restart {
			native[s.i].Kind = batchKeep
		}
	}
	for k := range list {
		list[k].old = list[k].cell.load()
		list[k].cell.retain(list[k].cfg)
	}
	return func() {
		for k := len(list) - 1; k >= 0; k-- {
			list[k].cell.retain(list[k].old)
		}
	}, nil
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
	if f.cell == nil {
		return f.ref.update(ctx)
	}
	snap, err := f.ref.snapshot(ctx)
	if err != nil {
		return err
	}
	if !snap.Alive {
		return ErrInactive
	}
	root := f.cell.root
	loaded := snap.Target != nil
	var resolved any
	if loaded {
		if resolved, err = f.cell.resolve(ctx, cfg); err != nil {
			return err
		}
	}
	committed, err := runInner(ctx, InternalCommit, root, cfg, func(context.Context, any) (bool, error) { return true, nil })
	if err != nil {
		return err
	}
	if !committed {
		return ErrCommitVetoed
	}
	f.cell.retain(cfg)
	if !loaded {
		return f.ref.update(ctx)
	}
	_, err = runInner(ctx, InternalUpdate, root, resolved, func(ctx context.Context, _ any) (any, error) {
		return nil, f.ref.update(ctx)
	})
	return err
}
