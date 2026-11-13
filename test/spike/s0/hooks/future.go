// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"sync"
)

// Future represents already-started work, never a lazy callback. The frontend
// must own/cancel that work. Await may return early on cancellation, but OnSettled
// MUST run its callback exactly once only when the actual work has finished; that
// is what permits the hook engine to retain an activation lease after Emit/Bail
// return without starting an untracked waiter goroutine. Callbacks run outside
// future locks. Await returns a final Decision without another Future.
type Future interface {
	Await(context.Context) (Decision, error)
	OnSettled(func())
}

// Completion is a small frontend/replay bridge, not a scheduler. It starts no
// goroutine: the caller starts and lifecycle-owns the operation, then calls the
// completion function after the operation really exits. Results must be treated
// as immutable, just like event payloads. A Completion must not be copied.
type Completion struct {
	mu        sync.Mutex
	done      chan struct{}
	settled   bool
	value     Decision
	err       error
	callbacks []func()
}

func NewCompletion() (*Completion, func(Decision, error)) {
	p := &Completion{done: make(chan struct{})}
	return p, p.complete
}

func (p *Completion) complete(value Decision, err error) {
	p.mu.Lock()
	if p.settled {
		p.mu.Unlock()
		return
	}
	p.settled, p.value, p.err = true, value, err
	callbacks := p.callbacks
	p.callbacks = nil
	close(p.done)
	p.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

func (p *Completion) Await(ctx context.Context) (Decision, error) {
	select {
	case <-p.done:
		// The closed channel publishes value and err; no callback runs here.
		return p.value, p.err
	case <-ctx.Done():
		return Decision{}, ctx.Err()
	}
}

func (p *Completion) OnSettled(callback func()) {
	p.mu.Lock()
	if !p.settled {
		p.callbacks = append(p.callbacks, callback)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	callback()
}
