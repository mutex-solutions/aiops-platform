// SPDX-License-Identifier: Apache-2.0

// Package observers keeps the prototype observer API and real-runtime regression
// tests. The stdlib-only implementation lives in internal/eventqueue so runtime
// candidates can publish events without importing their own test harness.
// Neither this bridge nor the dispatcher selects a final runtime API.
package observers

import "github.com/mutex-solutions/aiops-platform/test/spike/s0/internal/eventqueue"

// Error identities are shared with eventqueue, not recreated at this boundary.
var (
	ErrClosed      = eventqueue.ErrClosed
	ErrSequenceGap = eventqueue.ErrSequenceGap
	ErrNotAccepted = eventqueue.ErrNotAccepted
	ErrBinding     = eventqueue.ErrBinding
	ErrObserver    = eventqueue.ErrObserver
	ErrInactive    = eventqueue.ErrInactive
	ErrReentrant   = eventqueue.ErrReentrant
)

// Binding attaches observers to real activation leases and native cleanup.
type Binding = eventqueue.Binding

// Observer runs off-owner with an independent, dispatcher-marked context.
type Observer[T any] = eventqueue.Observer[T]

// Logger receives one contained observer failure on the dispatcher worker.
type Logger = eventqueue.Logger

// Disposer idempotently removes a registration from future enqueue snapshots.
type Disposer = eventqueue.Disposer

// Status reports accepted and delivered work, including contained failures.
type Status = eventqueue.Status

// CloseError reports incomplete real worker drain without deleting queued work.
type CloseError = eventqueue.CloseError

// PanicError preserves a contained callback panic and its error identity.
type PanicError = eventqueue.PanicError

// ObserverError has cached off-owner-formatted text and the original cause.
type ObserverError = eventqueue.ObserverError

// Dispatcher is the same generic type used by native event publishers.
type Dispatcher[T any] = eventqueue.Dispatcher[T]

// New starts exactly one off-owner dispatcher worker. See eventqueue.New and the
// shared Dispatcher method documentation for sequencing, leases, and Close.
func New[T any](logger Logger) *Dispatcher[T] { return eventqueue.New[T](logger) }
