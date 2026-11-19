// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"sync"
)

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

// Update replaces the fiber's config with cfg and restarts it, and returns once
// the request is admitted; use Await for the restart (07-c4-plugin-sdk.md:205,
// :239, :358; ADR-034 owner decision 9). The contract gives no signature: this
// shape is an open point.
//
// On an ACTIVE fiber, Update first resolves cfg through internal/config and Decode.
// If that fails, Update returns the error, retains nothing and leaves the running
// activation untouched. Otherwise it emits internal/commit with cfg, retains cfg,
// and restarts the fiber through the waterfall internal/update, whose payload is
// the resolved config: a listener that does not call next vetoes the restart, and
// the retained cfg then applies at the next restart. On a fiber in any other state
// the resolution is skipped: cfg is retained and the activation that follows
// resolves it and fails the fiber on an error. A fiber mounted with When takes no
// config and Update ignores cfg.
func (f *Fiber) Update(ctx context.Context, cfg any) error {
	if f.cell == nil {
		return f.ref.update(ctx)
	}
	st, err := f.ref.state(ctx)
	if err != nil {
		return err
	}
	root := f.cell.root
	var resolved any
	if st == Active {
		if resolved, err = f.cell.resolve(ctx, cfg); err != nil {
			return err
		}
	}
	if err = InternalCommit.emit(ctx, root.aud, cfg); err != nil {
		return err
	}
	f.cell.retain(cfg)
	if st != Active {
		return f.ref.update(ctx)
	}
	_, err = runInner(ctx, InternalUpdate, root, resolved, func(ctx context.Context, _ any) (any, error) {
		return nil, f.ref.update(ctx)
	})
	return err
}
