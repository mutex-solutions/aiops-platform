// SPDX-License-Identifier: Apache-2.0

// Package concurrency holds experimental scheduler candidates, not the kernel.
package concurrency

import (
	"context"
	"errors"
	"sync/atomic"
)

// State is the observable lifecycle state shared by both candidates.
type State string

const (
	Pending   State = "PENDING"
	Loading   State = "LOADING"
	Active    State = "ACTIVE"
	Failed    State = "FAILED"
	Unloading State = "UNLOADING"
	Disposed  State = "DISPOSED"
)

var (
	ErrInactive = errors.New("inactive context")
	ErrClosed   = errors.New("runtime closed")
	ErrUnknown  = errors.New("unknown fiber")
)

// Disposer reverts a successful setup operation.
type Disposer func(context.Context) error

// Spec is deliberately neutral fixture data. No product service lives here.
type Spec struct {
	ID       string
	Needs    []string
	Provides []string
	Apply    func(*Context) error
	// Check is a consumer-bound availability predicate evaluated off the graph
	// state owner. False is PENDING, not a failed activation; Notify retries it.
	Check func(*Context) bool
}

// Notifier is an experimental availability recheck operation shared by candidates.
// It is separate from Runtime so existing narrow test adapters need not invoke it.
type Notifier interface {
	Notify(context.Context, ...string) error
}

// Waiter joins one fiber's lifecycle without waiting for unrelated transitions.
type Waiter interface {
	AwaitFiber(context.Context, uint64) error
}

// Recorder is test-only instrumentation for actual fixture operations. Candidates
// linearize these immutable envelopes in the same ledger as real state events;
// it is not a production service or a source of expected runtime responses.
type Recorder interface {
	Record(context.Context, string, map[string]any) error
}

// Snapshot is immutable once returned by a scheduler.
type Snapshot struct {
	ID       uint64
	Name     string
	State    State
	Revision uint64
	Epoch    []uint64
	Effects  []string
	Error    string
}

// Event captures the total state/service order, before any observer executes.
type Event struct {
	Sequence uint64
	Fiber    uint64
	Name     string // Spec.ID, available even before Mount returns.
	Kind     string
	From     State
	State    State
	Key      string
	UID      uint64
	Value    any            // Observed service value, never an expected response.
	Data     map[string]any // Immutable envelope when Kind is "fixture".
}

// Runtime is the narrow test contract both concurrency cores must implement.
type Runtime interface {
	Mount(context.Context, Spec) (uint64, error)
	Update(context.Context, uint64) error
	Dispose(context.Context, uint64) error
	Settle(context.Context) error
	Snapshots(context.Context) ([]Snapshot, error)
	Events(context.Context) ([]Event, error)
	Close(context.Context) error
}

// Context describes one activation. It cannot revive an earlier activation.
type Context struct {
	std          context.Context
	fiber        uint64
	live         atomic.Bool // New effects and leases are accepted only while live.
	released     atomic.Bool // Committed reads remain valid during orderly teardown.
	lease        atomic.Int64
	bindings     map[string]provision
	provide      func(string, any) error
	effect       func(string, Disposer) error
	dispose      func() error
	leaseChanged func() // Optional nonblocking wake; installed by the scheduler.
}

type provision struct {
	uid   uint64
	owner uint64
	value any
}

func (c *Context) Std() context.Context { return c.std }

// Read requires a dependency captured for the current activation.
func (c *Context) Read(key string) (any, error) {
	if c.released.Load() {
		return nil, ErrInactive
	}
	p, ok := c.bindings[key]
	if !ok {
		return nil, ErrUnknown
	}
	return p.value, nil
}

func (c *Context) Provide(key string, value any) error {
	if !c.live.Load() {
		return ErrInactive
	}
	return c.provide(key, value)
}

func (c *Context) Defer(label string, dispose Disposer) error {
	if !c.live.Load() {
		return ErrInactive
	}
	return c.effect(label, dispose)
}

// Dispose requests disposal without awaiting the activation that called it.
func (c *Context) Dispose() error {
	if c.dispose == nil {
		return ErrInactive
	}
	return c.dispose()
}

// Enter acquires a call-path lease with no graph lock and no allocation.
// The caller must pair a successful Enter with exactly one Leave.
func (c *Context) Enter() bool {
	if !c.live.Load() {
		return false
	}
	c.lease.Add(1)
	if c.live.Load() {
		return true
	}
	c.lease.Add(-1)
	if c.leaseChanged != nil {
		c.leaseChanged()
	}
	return false
}

func (c *Context) Leave() {
	c.lease.Add(-1)
	if c.leaseChanged != nil {
		c.leaseChanged()
	}
}
