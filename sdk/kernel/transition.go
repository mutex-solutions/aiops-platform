// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// handshake is the handshake between one off-owner callback and the owner's verdict
// on that callback's deadline. A callback is judged by when IT returned, never
// by when the owner got to its message: the worker stamps the instant the
// callback returned (finish) under mu, and the owner reads that stamp under the
// same mu when the deadline falls due (inTime).
//
// Why no interleaving of timer and completion can mis-judge. The owner reads the
// clock (now >= deadline) before it takes mu, and both sides read the stamp or
// write it only under mu, so exactly one of two orders holds. (1) finish ran
// first: the stamp is the true return time, so the verdict is exact whether the
// message is still in flight, queued behind other work, or already handled:
// stamp < deadline means finished in time and the owner defers to the message,
// stamp >= deadline means it overran. (2) The owner took mu first: the callback
// has not returned at an instant >= deadline, and finish can only stamp later
// than that, so it truly overran. The stamp is taken before the worker sends its
// message (which may block on a full inbox), so queueing delay never counts. mu
// guards two stores and one clock read on the worker and one load on the owner;
// no callback ever runs under it.
type handshake struct {
	mu sync.Mutex
	at time.Time // Zero until the callback returned.
}

func (r *handshake) finish() time.Time {
	r.mu.Lock()
	r.at = time.Now()
	at := r.at
	r.mu.Unlock()
	return at
}

func (r *handshake) zero() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.at.IsZero()
}

// once stamps only the first call.
func (r *handshake) once() {
	r.mu.Lock()
	if r.at.IsZero() {
		r.at = time.Now()
	}
	r.mu.Unlock()
}

// inTime reports whether the callback returned strictly before deadline.
func (r *handshake) inTime(deadline time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.at.IsZero() && r.at.Before(deadline)
}

// The owner keeps one heap entry per pending deadline, never stale entries.
// Idle fibers add neither timer work nor deadline storage.
type deadlineHeap []*fiber

func (d deadlineHeap) Len() int           { return len(d) }
func (d deadlineHeap) Less(i, j int) bool { return d[i].deadline.Before(d[j].deadline) }
func (d deadlineHeap) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
	d[i].deadlineIndex, d[j].deadlineIndex = i, j
}
func (d *deadlineHeap) Push(value any) {
	f := value.(*fiber)
	f.deadlineIndex = len(*d)
	*d = append(*d, f)
}
func (d *deadlineHeap) Pop() any {
	n := len(*d) - 1
	f := (*d)[n]
	(*d)[n] = nil
	*d = (*d)[:n]
	f.deadlineIndex = -1
	return f
}

// load starts the activation of f for epoch: the load deadline runs from here
// (coreConfig.loadDeadline), and apply runs on its own goroutine.
func (o *owner) load(f *fiber, epoch []uint64, bindings map[svcKey]*liveProvision) {
	a := o.activation(f, epoch, bindings)
	c := a.ctx
	f.act, f.committed, f.invalid, f.phase = a, bindings, false, phApplying
	o.touch(f)
	run := new(handshake)
	f.run = run
	o.busy++
	o.deadline(f, time.Now().Add(o.r.cfg.loadDeadline))
	o.state(f, Loading)
	apply, applyEffect := f.spec.Apply, f.spec.ApplyEffect
	start := func() {
		go func() {
			var returned disposer
			err := invokeCallback(func() error {
				if applyEffect != nil {
					var err error
					returned, err = applyEffect(c)
					return err
				}
				if apply == nil {
					return nil
				}
				return apply(c)
			})
			at := run.finish()
			o.r.completed(message{kind: cmdLoadDone, id: c.fiber, act: a, dispose: returned, err: err, errText: errorText(err), at: at})
		}()
	}
	if o.serviceHeld(f.spec.Needs) {
		o.holdLoad(f.spec.Needs, start)
		return
	}
	start()
}

// Error formatting can invoke plugin methods just like Check or Apply. Cache it
// on the worker that obtained the error; keep the original object for errors.Is.
func errorText(err error) (text string) {
	if err == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			text = "error formatting panic"
		}
	}()
	return err.Error()
}

func joinText(texts ...string) string {
	return strings.Join(slices.DeleteFunc(texts, func(text string) bool { return text == "" }), "\n")
}

type diagnostic struct {
	text  string
	cause error
}

func (e *diagnostic) Error() string { return e.text }
func (e *diagnostic) Unwrap() error { return e.cause }

// invokeCallback is the panic boundary of every plugin callback the core runs
// off the owner. A panic that carries an error keeps its identity, so a refused
// registration (refusedCall) still matches ErrInactiveEffect.
func invokeCallback(fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			if cause, ok := p.(error); ok {
				err = fmt.Errorf("callback panic: %w", cause)
			} else {
				err = fmt.Errorf("callback panic: %v", p)
			}
		}
	}()
	return fn()
}

// loaded handles the completion of an apply callback.
func (o *owner) loaded(m message) {
	// An activation has exactly one Apply, and f stays in phApplying, with this
	// activation, until this message: only loaded leaves that phase, and nothing
	// replaces or removes f before it.
	f := o.fibers[m.id]
	f.run = nil
	if m.err == nil && m.dispose != nil {
		// A returned plugin disposer is collected after all body effects, even
		// when the load was superseded or requested its own disposal meanwhile.
		f.effects = append(f.effects, effectRec{label: "apply", dispose: m.dispose})
		o.event(f, "effect", "apply", 0)
	}
	if m.act.selfDispose.Load() {
		o.requestDispose(f)
	}
	current := !f.invalid && !f.remove && f.target != nil && slices.Equal(f.target, m.act.epoch)
	o.deadline(f, time.Time{})
	o.block(f, nil)
	if current && m.err == nil {
		f.phase = phIdle
		o.touch(f)
		o.busy--
		f.publishActive = true
		if !o.scopeWork(f) {
			f.publishActive = false
			o.state(f, Active)
		}
		return
	}
	if current && m.err != nil {
		f.awaitErr, f.awaitText = m.err, m.errText
		o.fail(f, m.err, m.errText)
	}
	o.quiesce(f)
	f.phase = phDraining
	o.touch(f)
	o.state(f, Unloading)
	o.armDrain(f)
	o.enqueue(f)
}

// quiesce ends the activation of f: it stops accepting leases and effects and
// cancels Std. Drain, then dispose, follow on the owner.
func (o *owner) quiesce(f *fiber) {
	if f.act == nil || f.invalid {
		return
	}
	f.invalid = true
	f.act.ctx.live.Store(false)
	f.act.cancel()
	for _, effect := range f.effects {
		if effect.scope != nil {
			effect.scope.cancelPending(ErrInactive)
		}
	}
	// Structural ownership is independent of service dependencies and is NOT
	// invalidated here. As in the oracle, each child is one effect of this
	// activation: disposeNext requests its disposal at its LIFO slot, after any
	// later-registered effect and before any earlier one, and joins it only
	// after the walk. A child holding one of our service bindings still leaves
	// first through the service withdrawal, which is the separate dependency axis.
	if f.phase == phIdle {
		o.busy++
		f.phase = phDraining
		o.touch(f)
		o.armDrain(f)
	}
	if f.phase != phApplying {
		o.state(f, Unloading)
	}
}

// ownBusy reports whether the quiesced activation of f still has work of its own:
// leases, tracked workers or an availability check. Only this work is covered by
// the drain budget.
func (o *owner) ownBusy(f *fiber) bool {
	return len(f.act.tracked) != 0 || f.act.ctx.lease.Load() != 0 || f.checking != nil
}

// armDrain starts the drain budget of f, which measures f's own draining and
// nothing else. A fiber with no work of its own to wait for has no drain clock
// at all: whatever it then waits for (dependants, structural children) is bounded
// by those fibers' own deadlines and is reported through them (see awaited).
func (o *owner) armDrain(f *fiber) {
	if o.ownBusy(f) {
		o.deadline(f, time.Now().Add(o.r.cfg.drainDeadline))
	} else {
		o.deadline(f, time.Time{})
	}
}

func (o *owner) drain(f *fiber) {
	if o.ownBusy(f) {
		return
	}
	// The fiber's own draining is over: stop its clock and drop its own
	// diagnostic. What follows is a wait on other fibers, not drain time.
	o.deadline(f, time.Time{})
	o.block(f, nil)
	if f.id == 0 && o.closing && len(o.fibers) != 0 {
		return
	}
	for _, p := range f.provisions {
		if len(p.dependents) != 0 {
			return
		}
	}
	f.phase = phDisposing
	o.touch(f)
	o.deadline(f, time.Now().Add(o.r.cfg.disposeDeadline))
	f.cleanupCtx, f.cleanupCancel = context.WithDeadline(context.Background(), f.deadline)
	o.disposeNext(f)
}

func (o *owner) disposeNext(f *fiber) {
	for len(f.effects) != 0 {
		e := f.effects[len(f.effects)-1]
		if e.child != 0 {
			f.effects[len(f.effects)-1] = effectRec{}
			f.effects = f.effects[:len(f.effects)-1]
			if child := o.fibers[e.child]; child != nil && child.parent == f.act {
				o.requestDispose(child)
			}
			continue
		}
		o.event(f, "dispose-effect", e.label, 0)
		if e.scope != nil && e.scope.cleanupFinished() {
			// Quiescence can cancel an admitted scope before setup starts. Its
			// handle reports inactivity, but no cleanup callback failed here.
			f.effects[len(f.effects)-1] = effectRec{}
			f.effects = f.effects[:len(f.effects)-1]
			continue
		}
		if e.provision != nil {
			p := e.provision
			if o.providers[p.key] == p {
				delete(o.providers, p.key)
			}
			f.act.ctx.unpublishOwned(p.key)
			f.provisions = slices.DeleteFunc(f.provisions, func(other *liveProvision) bool { return other == p })
			o.keyEvent(f, "remove", p.key, p.binding.uid)
			o.notifyKey(p.key)
			f.effects[len(f.effects)-1] = effectRec{}
			f.effects = f.effects[:len(f.effects)-1]
			continue
		}
		f.effectRunning = true
		run := new(handshake)
		f.run = run
		act, cleanupCtx, id := f.act, f.cleanupCtx, f.id
		go func() {
			err := invokeCallback(func() error {
				if e.scope != nil {
					e.scope.cancelPending(ErrInactive)
					return e.scope.runCleanup(cleanupCtx)
				}
				return e.dispose(cleanupCtx)
			})
			at := run.finish()
			o.r.completed(message{kind: cmdEffectDone, id: id, act: act, err: err, errText: errorText(err), at: at})
		}()
		return
	}
	if len(f.act.children) != 0 {
		// Join after the walk. The last child's remove() enqueues this owner and
		// work() re-enters here. The join is a wait on the children, which have
		// their own drain and dispose budgets: it has no clock of its own.
		if !f.joining {
			f.joining = true
			o.block(f, nil)
			o.deadline(f, time.Time{})
		}
		return
	}
	f.joining = false
	o.unloaded(f)
}

func (o *owner) effectDone(m message) {
	// At most one disposer is in flight, and f stays in phDisposing, with this
	// activation, while effectRunning is set: disposeNext sets it as it starts the
	// disposer and only this message clears it.
	f := o.lookup(m.id)
	f.effectRunning, f.run = false, nil
	// Disposal has one total deadline for this activation, not a fresh budget
	// per effect. Once it expires, retain the blocked diagnostic until all
	// remaining LIFO cleanup actually returns. Whether this disposer overran is
	// judged by the instant it returned (m.at, stamped on its worker), not by the
	// clock of the owner turn that handles the message.
	expired := false
	if f.cleanupCtx != nil {
		at, _ := f.cleanupCtx.Deadline()
		expired = !m.at.Before(at)
	}
	if !expired {
		o.block(f, nil)
	}
	// This callback finished inside the budget, but the budget lapsed while its
	// message waited: the effects still to run are under the same total deadline,
	// so re-arm it and let the next turn judge them (see expire).
	if due := f.due; !due.IsZero() {
		o.deadline(f, due)
	}
	if m.err != nil {
		o.fail(f, errors.Join(f.failure, m.err), joinText(f.failureText, m.errText))
	}
	f.effects[len(f.effects)-1] = effectRec{}
	f.effects = f.effects[:len(f.effects)-1]
	o.disposeNext(f)
}

func (o *owner) unloaded(f *fiber) {
	f.cleanupCancel()
	f.act.ctx.released.Store(true)
	// A provider may already be queued by a sibling's release. Keep these
	// actual dependent links until this consumer records its next lifecycle
	// state; enqueue order alone cannot stop that provider overtaking it.
	f.releasing, f.releaseBindings = f.act.ctx, f.committed
	f.committed, f.act, f.effects, f.provisions = nil, nil, nil, nil
	f.phase = phIdle
	o.touch(f)
	o.busy--
	o.deadline(f, time.Time{})
	o.block(f, nil)
	f.cleanupCtx, f.cleanupCancel = nil, nil
	if f.id == 0 {
		// Keep the host's final diagnostics without publishing an ordinary fiber
		// snapshot or restarting its lifetime. All user fibers are already gone.
		if f.failure != nil {
			d := &diagnostic{text: f.failureText, cause: f.failure}
			o.addTerminal(d)
			o.r.rootErr = d
		}
		o.fail(f, nil)
		o.state(f, Disposed)
		o.releaseDependencies(f)
		return
	}
	if f.checking != nil {
		// A new desired provider can be checked while old cleanup is gated.
		// Once cleanup ends its worker still owns a bounded check deadline.
		o.deadline(f, f.checking.deadline)
	}
	// The next work turn publishes PENDING/FAILED/DISPOSED or starts a new
	// LOADING activation before it drops the old binding's dependent records.
	o.enqueue(f)
}

func (o *owner) releaseDependencies(f *fiber) {
	if f.releasing == nil {
		return
	}
	for _, p := range f.releaseBindings {
		delete(p.dependents, f.releasing)
		if owner := o.lookup(p.binding.owner); owner != nil {
			o.enqueue(owner)
		}
	}
	f.releasing, f.releaseBindings = nil, nil
}

// coreConfig.terminalLimit bounds the diagnostics of past failed incarnations that
// every later settle and close still reports. Beyond it only a count is kept, and
// the reported error says so: a failing plugin restarted forever must not grow the
// owner without bound.

func (o *owner) addTerminal(err error) {
	if len(o.terminal) < o.r.cfg.terminalLimit {
		o.terminal = append(o.terminal, err)
		return
	}
	o.r.terminalDropped.Add(1)
}

// terminalErrors is the retained diagnostics plus, past the cap, one error that
// states how many were dropped.
func (o *owner) terminalErrors() []error {
	dropped := o.r.terminalDropped.Load()
	if dropped == 0 {
		return o.terminal
	}
	return append(slices.Clone(o.terminal), fmt.Errorf("%d further terminal diagnostics dropped (cap %d)", dropped, o.r.cfg.terminalLimit))
}

func (o *owner) remove(f *fiber) {
	o.touch(f)
	rendered := o.snapshot(f).Error
	if f.failure != nil {
		o.addTerminal(&diagnostic{text: f.failureText, cause: f.failure})
	}
	o.fail(f, nil)
	o.block(f, nil)
	o.deadline(f, time.Time{})
	for _, key := range f.spec.Needs {
		delete(o.byKey[key], f.id)
		if len(o.byKey[key]) == 0 {
			delete(o.byKey, key)
		}
	}
	o.state(f, Disposed)
	archive := o.snapshot(f)
	archive.Error = rendered
	archive.Epoch, archive.Effects, archive.EffectTree = nil, nil, nil
	o.r.archive.put(f.id, retired{snapshot: archive, awaitErr: f.awaitErr})
	delete(o.fibers, f.id)
	if parent := f.parent; parent != nil {
		delete(parent.children, f.id)
		if owner := o.lookup(parent.ctx.fiber); owner != nil && owner.act == parent {
			// Not the running effect: only unpopped child slots are removed.
			owner.effects = slices.DeleteFunc(owner.effects, func(e effectRec) bool { return e.child == f.id })
			o.enqueue(owner)
		}
		f.parent = nil
	}
	if o.closing && o.root != nil && o.root.act != nil {
		o.enqueue(o.root)
	}
}
func (o *owner) deadline(f *fiber, at time.Time) {
	f.deadline, f.due = at, time.Time{}
	if at.IsZero() {
		if f.deadlineIndex >= 0 {
			heap.Remove(&o.deadlines, f.deadlineIndex)
		}
	} else if f.deadlineIndex >= 0 {
		heap.Fix(&o.deadlines, f.deadlineIndex)
	} else {
		heap.Push(&o.deadlines, f)
	}
}

func (o *owner) nextDeadline() time.Time {
	if len(o.deadlines) == 0 {
		return time.Time{}
	}
	return o.deadlines[0].deadline
}

// expire fires the deadlines that are due at now. A deadline is a verdict on a
// callback that has not returned, so before it blocks a fiber it asks the
// callback's handshake (handshake) whether the callback returned before the deadline.
// If it did, the work finished in time and only its message is late: the entry is
// retired without a verdict and the message completes the transition. Only work
// that had not returned by the deadline is blocked, so an owner busy with other
// commands cannot fail a callback that finished in budget, and a callback that
// truly overran is still failed even if its completion is already in flight.
func (o *owner) expire(now time.Time) {
	for len(o.deadlines) != 0 && !now.Before(o.deadlines[0].deadline) {
		f := o.deadlines[0]
		at := f.deadline
		stage, run := o.stageOf(f)
		inTime := run != nil && run.inTime(at)
		if stage == "drain" {
			inTime = o.drainInTime(f, at)
		}
		o.deadline(f, time.Time{})
		if inTime {
			f.due = at
			continue
		}
		if stage == "check" {
			f.checking.timedOut = true
			f.checking.cancel()
		}
		if stage == "drain" || stage == "dispose" && f.effectRunning {
			// A drain whose tracked goroutines or leases are still open, or a disposer
			// still running, past its deadline: a goroutine cannot be killed, so the
			// contract marks the fiber leaked and has the launcher replace the process
			// after the drain window (07-c4-plugin-sdk.md:359 and 03-c3-kernel.md:372).
			f.leaked = true
			err := fmt.Errorf("fiber %d %s deadline: %w: %w", f.id, stage, context.DeadlineExceeded, ErrLeaked)
			o.block(f, err)
			go o.r.logError("fiber marked leaked", f.id, f.spec.ID, o.leakLabel(f, stage, at), err.Error())
		} else {
			o.block(f, fmt.Errorf("fiber %d %s deadline: %w", f.id, stage, context.DeadlineExceeded))
		}
		if f.phase == phApplying {
			if !f.invalid && !f.remove {
				// The text is already the failure text: the await copy would
				// repeat it in the snapshot once the blocked text clears.
				f.awaitErr, f.awaitText = f.blocked, ""
				o.fail(f, f.blocked, f.blockedText)
			}
			o.quiesce(f)
			o.state(f, Unloading)
		}
		// Native cleanup contexts already own this exact deadline. Calling their
		// plain cancel function here races that timer and changes the observable
		// cause from DeadlineExceeded to Canceled. Keep ownership/diagnostics;
		// the context's own deadline cancels blocked disposer work truthfully.
		o.event(f, "deadline", stage, 0)
	}
}

// leakLabel names what still runs at the deadline at of a fiber marked leaked
// (08-profiles-roadmap-adrs.md §1.6, "Logged with its label"): the label of the
// disposer in flight, or those of the tracked goroutines that had not returned,
// sorted and comma-separated. A drain held by a lease alone is named by its stage.
func (o *owner) leakLabel(f *fiber, stage string, at time.Time) string {
	if stage == "dispose" {
		return f.effects[len(f.effects)-1].label
	}
	var open []string
	for _, w := range f.act.tracked {
		if !w.run.inTime(at) {
			open = append(open, w.label)
		}
	}
	if len(open) == 0 {
		return stage
	}
	slices.Sort(open)
	return strings.Join(open, ",")
}

// stageOf names the stage a due deadline of f belongs to and returns the handshake
// of the off-owner callback it waits for, if any.
func (o *owner) stageOf(f *fiber) (string, *handshake) {
	switch {
	case f.phase == phScoping:
		return "scope", f.run
	case f.phase == phDraining:
		return "drain", nil
	case f.phase == phDisposing:
		return "dispose", f.run // Nil unless a disposer is in flight: effectDone clears it.
	case f.checking != nil && f.phase == phIdle:
		return "check", f.checking.run
	}
	return "load", f.run
}

// drainInTime reports whether all of a draining fiber's own work is already done,
// or finished before at, so that only owner messages are pending.
// The last lease is judged by the instant it was released (act.freed, stamped by
// the releasing goroutine on the quiesced path only). A tracked worker keeps its
// lease until the owner acknowledges its completion, so a worker whose callback
// returned in time and waits for that acknowledgement is discounted from the
// lease count instead of being blamed for the owner's delay.
func (o *owner) drainInTime(f *fiber, at time.Time) bool {
	a := f.act
	finished := int64(0)
	for _, w := range a.tracked {
		if !w.run.inTime(at) {
			return false
		}
		finished++
	}
	if a.ctx.lease.Load() > finished {
		return false
	}
	if !a.freed.inTime(at) && !a.freed.zero() {
		return false
	}
	return f.checking == nil || f.checking.run.inTime(at)
}

// ErrLeaked marks a fiber whose drain or disposer is still running past its
// deadline. It is wrapped in the blocked diagnostic that Settle and Close report,
// and the fiber's snapshot carries Leaked until the incarnation is archived.
var ErrLeaked = errors.New("fiber leaked")
