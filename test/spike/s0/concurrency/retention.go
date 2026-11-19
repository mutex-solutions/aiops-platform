// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
)

// Retention contract (both candidates).
//
// Fiber IDs are issued from a counter that only grows, and an ID is never
// reused, so "an ID at or below the last issued one that is not live" is
// decidable without a stored record. After a fiber incarnation is DISPOSED and
// its teardown has been joined, the runtime keeps only:
//
//   - a bounded, insertion-ordered archive of the most recently disposed
//     incarnations (Retention.ArchiveLimit records; the oldest is evicted
//     first). A record carries the DISPOSED snapshot and the retained startup
//     error, so Snapshots, Inspect, Dispose and AwaitFiber on a recently
//     disposed fiber behave exactly as before;
//   - for an ID whose record has been evicted, no per-incarnation state at all.
//     Such an ID is "expired": AwaitFiber returns ErrExpired (the startup error
//     is no longer known, so success must not be claimed), Dispose is the same
//     idempotent no-op as for an archived ID, any other command answers as for
//     any disposed fiber (unchanged), and the fiber is absent from Snapshots
//     and Inspect. An ID that was never issued is still ErrUnknown.
//
// This mirrors the Cordis oracle, where a disposed fiber leaves the registry
// and is garbage once unreferenced: its state and error stay reachable only
// through a reference the caller holds. A caller that waits on a fiber keeps
// the exact record alive here too: a waiter registered before disposal still
// receives the retained startup error even if the archive evicted the record
// before the waiter was served.
//
// The event ledger is bounded as well. The runtime itself keeps the most
// recent Retention.EventWindow events (a diagnostic tail). Complete history is
// opt-in: with Retention.RecordLimit > 0 every event is also appended, in
// sequence order and in the same linearization as the state change, to a
// recorder holding at most RecordLimit events. No event is dropped for a
// recorder: the first event that would exceed the cap makes the recorder
// fail, observably and permanently (Events returns ErrRecorderOverflow, Close
// joins it, RetentionStatus reports it); the runtime itself keeps operating.

var (
	// ErrExpired reports an ID that was issued and disposed, whose archive
	// record has been evicted. It is distinct from ErrUnknown (never issued).
	ErrExpired = errors.New("fiber record expired")
	// ErrEventsTruncated accompanies the retained tail returned by Events when
	// the runtime does not record complete history and older events were dropped.
	ErrEventsTruncated = errors.New("event history truncated; complete history requires Retention.RecordLimit")
	// ErrRecorderOverflow reports that the opt-in recorder reached its cap.
	ErrRecorderOverflow = errors.New("event recorder overflow")
)

const (
	// DefaultArchiveLimit is the number of disposed incarnations kept when
	// Retention.ArchiveLimit is zero.
	DefaultArchiveLimit = 1024
	// DefaultEventWindow is the number of most recent events kept when
	// Retention.EventWindow is zero (512 KiB of 128-byte events).
	DefaultEventWindow = 4096
	// DefaultRecordLimit is a cap for callers that opt into complete history
	// and have no better figure (128 MiB of 128-byte events, never preallocated).
	DefaultRecordLimit = 1 << 20
)

// Retention bounds a runtime's history. The zero value selects the defaults and
// no complete recording. Negative values are treated as zero.
type Retention struct {
	ArchiveLimit int // Disposed incarnations kept; 0 = DefaultArchiveLimit.
	EventWindow  int // Most recent events always kept; 0 = DefaultEventWindow.
	RecordLimit  int // Complete-history recorder cap; 0 = no recorder.
}

// Recording is the retention a test or adapter that needs complete history
// opts into explicitly.
func Recording() Retention { return Retention{RecordLimit: DefaultRecordLimit} }

// NewRecordingReconciler and NewRecordingGraphLock are the constructors for
// callers that compare complete event history (replay, differential receipts,
// property model, observer and composition ledger checks).
func NewRecordingReconciler() Runtime { return NewReconcilerWith(Recording()) }
func NewRecordingGraphLock() Runtime  { return NewGraphLockWith(Recording()) }

func (r Retention) normalized() Retention {
	if r.ArchiveLimit <= 0 {
		r.ArchiveLimit = DefaultArchiveLimit
	}
	if r.EventWindow <= 0 {
		r.EventWindow = DefaultEventWindow
	}
	if r.RecordLimit < 0 {
		r.RecordLimit = 0
	}
	return r
}

// RetentionStatus is a lock-free diagnostic of the bounds and their use.
type RetentionStatus struct {
	ArchiveLimit     int
	Archived         int    // Records currently held.
	ArchiveEvicted   uint64 // Records dropped to honour ArchiveLimit.
	EventWindow      int
	Windowed         int    // Events currently held in the tail.
	EventsTotal      uint64 // Events ever appended (the last sequence).
	RecordLimit      int    // 0 when no recorder is attached.
	Recorded         int
	RecorderOverflow bool   // The recorder reached its cap and stopped.
	RecorderDropped  uint64 // Events not recorded after the overflow.
	// TerminalDropped counts diagnostics of past failed incarnations dropped
	// beyond the reconciler's cap of 64 (always zero for graph-lock, which keeps
	// them only in the bounded archive).
	TerminalDropped uint64
}

// RetentionReporter exposes RetentionStatus without entering the state owner.
type RetentionReporter interface {
	RetentionStatus() RetentionStatus
}

// recordArchive is an insertion-ordered, size-bounded map of retired records.
// It is not synchronized; the owner serializes writers (graph mutex or the
// reconciler owner) and only the counters are read concurrently.
type recordArchive[T any] struct {
	limit   int
	records map[uint64]T
	order   []uint64 // Ring of IDs, oldest at head once full.
	head    int
	n       atomic.Int64
	evicted atomic.Uint64
}

func newRecordArchive[T any](limit int) *recordArchive[T] {
	return &recordArchive[T]{limit: limit, records: make(map[uint64]T)}
}

func (a *recordArchive[T]) put(id uint64, record T) {
	if _, exists := a.records[id]; !exists {
		if len(a.order) < a.limit {
			a.order = append(a.order, id)
		} else {
			delete(a.records, a.order[a.head])
			a.order[a.head] = id
			a.head = (a.head + 1) % a.limit
			a.evicted.Add(1)
		}
	}
	a.records[id] = record
	a.n.Store(int64(len(a.records)))
}

func (a *recordArchive[T]) get(id uint64) (T, bool) {
	record, ok := a.records[id]
	return record, ok
}

// each visits records in no particular order.
func (a *recordArchive[T]) each(visit func(uint64, T)) {
	for id, record := range a.records {
		visit(id, record)
	}
}

// eventLedger holds the bounded tail and the optional recorder. The caller
// serializes append and events; the counters are safe to read concurrently.
type eventLedger struct {
	window  []Event // Ring, oldest at wstart once full.
	wstart  int
	wlimit  int
	rec     []Event
	rlimit  int
	over    atomic.Bool
	total   atomic.Uint64
	wlen    atomic.Int64
	rlen    atomic.Int64
	dropped atomic.Uint64
}

func newEventLedger(r Retention) *eventLedger {
	return &eventLedger{wlimit: r.EventWindow, rlimit: r.RecordLimit}
}

// next is the sequence the next appended event must carry.
func (l *eventLedger) next() uint64 { return l.total.Load() + 1 }

func (l *eventLedger) append(e Event) {
	l.total.Store(e.Sequence)
	if len(l.window) < l.wlimit {
		l.window = append(l.window, e)
	} else {
		l.window[l.wstart] = e
		l.wstart = (l.wstart + 1) % l.wlimit
	}
	l.wlen.Store(int64(len(l.window)))
	if l.rlimit == 0 {
		return
	}
	if len(l.rec) >= l.rlimit {
		l.over.Store(true)
		l.dropped.Add(1)
		return
	}
	l.rec = append(l.rec, e)
	l.rlen.Store(int64(len(l.rec)))
}

// events copies the complete recording, or the tail with ErrEventsTruncated
// when events were dropped, or nothing with ErrRecorderOverflow.
func (l *eventLedger) events() ([]Event, error) {
	if l.rlimit > 0 {
		if l.over.Load() {
			return nil, l.overflowError()
		}
		return slices.Clone(l.rec), nil
	}
	tail := make([]Event, 0, len(l.window))
	tail = append(tail, l.window[l.wstart:]...)
	tail = append(tail, l.window[:l.wstart]...)
	if l.total.Load() > uint64(len(tail)) {
		return tail, fmt.Errorf("%w: %d events appended, last %d kept", ErrEventsTruncated, l.total.Load(), len(tail))
	}
	return tail, nil
}

func (l *eventLedger) overflowError() error {
	return fmt.Errorf("%w: cap %d events, %d dropped", ErrRecorderOverflow, l.rlimit, l.dropped.Load())
}

// failure is the sticky recorder error, safe to call from any goroutine.
func (l *eventLedger) failure() error {
	if l.over.Load() {
		return l.overflowError()
	}
	return nil
}

func (l *eventLedger) status() (windowed int, total uint64, recorded int, over bool, dropped uint64) {
	return int(l.wlen.Load()), l.total.Load(), int(l.rlen.Load()), l.over.Load(), l.dropped.Load()
}

func retentionStatus[T any](r Retention, a *recordArchive[T], l *eventLedger) RetentionStatus {
	windowed, total, recorded, over, dropped := l.status()
	return RetentionStatus{
		ArchiveLimit: r.ArchiveLimit, Archived: int(a.n.Load()), ArchiveEvicted: a.evicted.Load(),
		EventWindow: r.EventWindow, Windowed: windowed, EventsTotal: total,
		RecordLimit: r.RecordLimit, Recorded: recorded, RecorderOverflow: over, RecorderDropped: dropped,
	}
}
