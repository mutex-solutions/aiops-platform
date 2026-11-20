// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory reload of the reload definition: it queues nothing,
// records each submission and answers it with the result a test scripts. Its plugin
// provides reload.Key and binds composition/applied, so a listener of the event can
// be driven with Publish (07-c4-plugin-sdk.md:1410). It imports the standard library,
// sdk/kernel and its definition only.
package fake

import (
	"context"
	"slices"
	"sync"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Reload is an in-memory reload.Reload. Every submission is Passed at once unless
// Next says otherwise.
type Reload struct {
	// Next, when set before a Submit, is the result the submission gets; its ID and
	// Op are overwritten. SubmitErr, when set, is what Submit returns.
	Next      *reload.Result
	SubmitErr error

	mu      sync.Mutex
	results map[reload.ID]reload.Result
	subs    []reload.Op
	applied kernel.EventDispatcher[composition.Applied]
}

var _ reload.Reload = (*Reload)(nil)

// New returns an empty reload and the plugin that provides it under reload.Key.
// Each call returns a new plugin value, a new entry of its own.
func New() (*Reload, kernel.Plugin) {
	f := &Reload{results: map[reload.ID]reload.Result{}}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/reload-fake",
		Provides: []kernel.AnyKey{reload.Key},
		Apply: func(c *kernel.Context, _ any) error {
			f.applied = reload.Applied.Bind(c)
			return reload.Key.Provide(c, f)
		},
	})
}

// Submit records op and gives it the next result.
func (f *Reload) Submit(_ context.Context, op reload.Op) (reload.ID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SubmitErr != nil {
		return 0, f.SubmitErr
	}
	id := reload.ID(len(f.subs) + 1)
	f.subs = append(f.subs, op)
	r := reload.Result{Status: reload.Passed}
	if f.Next != nil {
		r = *f.Next
	}
	r.ID, r.Op = id, op
	f.results[id] = r
	return id, nil
}

// Submissions lists the operations submitted so far, oldest first.
func (f *Reload) Submissions() []reload.Op {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.subs)
}

// Result returns the result of id.
func (f *Reload) Result(id reload.ID) (reload.Result, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.results[id]
	return r, ok
}

// Await returns the result of id at once.
func (f *Reload) Await(ctx context.Context, id reload.ID) (reload.Result, error) {
	if err := ctx.Err(); err != nil {
		return reload.Result{}, err
	}
	r, ok := f.Result(id)
	if !ok {
		return reload.Result{}, reload.ErrUnknown
	}
	return r, nil
}

// Publish emits composition/applied to every listener.
func (f *Reload) Publish(ctx context.Context, a composition.Applied) error {
	return f.applied.Emit(ctx, kernel.Unscoped, a)
}
