// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import "context"

// The relay carries the ledger to the three kernel events (ADR-059 decision 2). It
// is one internal observer of the dispatcher that Root.Observe uses, so it runs off
// the owner, in the order of Sequence, under the bound and the overflow rule of
// Config.ObserverQueue, and the owner does nothing for it. The mapping from ledger
// kinds to hooks is documented at InternalStatus.

// startRelay subscribes the relay once, when the first listener of a kernel hook is
// registered: a kernel with no listener has no subscription and pays nothing.
func (r *Root) startRelay() { r.relayOnce.Do(func() { r.relay(false) }) }

// relayLease is the leaseHolder of the relay. Each listener the relay calls runs
// under its own fiber's lease (callEvent), so the relay takes none of the root's: a
// listener stuck in a callback blames its own fiber alone, and the root drains. It
// delivers while the root is live, as the root's lease would, and it registers no
// cleanup effect: nothing needs releasing, and a subscription that replaces one
// detached for overflow would otherwise leave one more effect on the root each time.
type relayLease struct{ *handle }

func (l relayLease) enter() (func(), bool)         { return func() {}, l.live.Load() }
func (relayLease) tryDefer(string, disposer) error { return nil }

// relay subscribes the relay to the ledger. gap is set when it replaces a
// subscription detached for overflow: its first event is then preceded by the
// notice that events were lost. The subscription fails only when the kernel is
// closing, and then there is nothing left to relay.
func (r *Root) relay(gap bool) {
	_, _ = r.core.observers.observe(r.ctx.h, relayLease{r.ctx.h}, "kernel/events", func(ctx context.Context, e ledgerEvent) error {
		ev := exportEvent(e)
		if gap {
			gap = false
			r.dispatchKernel(ctx, LedgerEvent{Kind: "overflow", Sequence: ev.Sequence - 1})
		}
		r.dispatchKernel(ctx, ev)
		return nil
	}, func(context.Context, *overflowError) error {
		r.relay(true)
		return nil
	})
}

// dispatchKernel hands e to the hooks its Kind maps to. A listener's failure is
// skipped and logged by the dispatch itself, and a dispatch that ctx ends has
// nothing left to do.
func (r *Root) dispatchKernel(ctx context.Context, e LedgerEvent) {
	switch e.Kind {
	case "mount":
		_ = InternalPlugin.emit(ctx, r.aud, e)
	case "state":
		_ = InternalStatus.emit(ctx, r.aud, e)
		if e.State == Disposed {
			_ = InternalPlugin.emit(ctx, r.aud, e)
		}
	case "provide", "service", "remove":
		_ = InternalService.emit(ctx, r.aud, e)
	case "overflow":
		_ = InternalPlugin.emit(ctx, r.aud, e)
		_ = InternalStatus.emit(ctx, r.aud, e)
		_ = InternalService.emit(ctx, r.aud, e)
	}
}
