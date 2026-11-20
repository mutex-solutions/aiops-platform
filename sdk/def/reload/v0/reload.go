// SPDX-License-Identifier: Apache-2.0

package reload

import (
	"context"
	"errors"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the reload key: consumer-bound, class C, at major 0, with an open provider
// side (03-c3-kernel.md:65; 07-c4-plugin-sdk.md:556, where `reload` is among the
// eleven consumer-bound keys).
var Key = kernel.NewKey[Reload]("reload", 0, kernel.ConsumerBound())

// Applied is the composition/applied event, owned by Key, as the composition
// definition prescribes (sdk/def/composition/v0 NewApplied; 07-c4-plugin-sdk.md:381).
// reload publishes it after a generation was applied and judged
// (03-c3-kernel.md:382).
var Applied = composition.NewApplied(Key)

// The refusals of Submit and Await.
var (
	// ErrQueueFull refuses a Submit while the queue holds its bound of waiting
	// operations: backpressure, never a wait.
	ErrQueueFull = errors.New("reload: queue is full")
	// ErrClosed refuses a Submit once the reload fiber is unloading or disposed.
	ErrClosed = errors.New("reload: closed")
	// ErrNested refuses a Submit made with the context of the reload goroutine's own
	// work, for example from a listener of composition/applied or of a loader hook
	// ("a nested submission is rejected", 03-c3-kernel.md:87).
	ErrNested = errors.New("reload: nested submission")
	// ErrUnknown reports an operation id that was never issued or whose result was
	// evicted from the bounded history.
	ErrUnknown = errors.New("reload: unknown operation")
	// ErrAbandoned is the cause of a result whose operation was queued or running when
	// the reload fiber was disposed.
	ErrAbandoned = errors.New("reload: abandoned at disposal")
	// ErrVerdict is the cause of a result whose generation was applied and judged
	// failed.
	ErrVerdict = errors.New("reload: failed verdict")
)

// ID names one submitted operation. The first is 1.
type ID uint64

// Op is one submission: a request to compose the layers again and apply the result.
// Source says who submitted, for the audit trail ("aiopsctl", "reload-watch-files");
// the docs give an operation no field (open point).
type Op struct{ Source string }

// Stage is a step of the pipeline of 03-c3-kernel.md:376-382.
type Stage string

// The stages, in order.
const (
	StageCompose Stage = "compose"
	StageAdmit   Stage = "admit"
	StagePlan    Stage = "plan"
	StageApply   Stage = "apply"
	StageSettle  Stage = "settle"
	StagePublish Stage = "publish"
)

// Status is where an operation stands.
type Status string

// The statuses. Queued and Running are not final.
const (
	Queued  Status = "queued"
	Running Status = "running"
	// Passed: the generation was applied and the verdict is pass.
	Passed Status = "passed"
	// Failed: the generation was applied and the verdict is fail. At S1 there is no
	// committer and no restore (ADR-059 decision 5, S4a), so the tree stays as
	// applied; Result.Previous is what a later step restores.
	Failed Status = "failed"
	// Rejected: a stage before the apply, or the apply itself, refused the
	// generation, and the tree was not touched.
	Rejected Status = "rejected"
	// Abandoned: the reload fiber was disposed first. An operation that was running
	// may have touched the tree, and Stage says how far it got.
	Abandoned Status = "abandoned"
)

// Done reports whether s is final.
func (s Status) Done() bool { return s != Queued && s != Running }

// Result is the readable outcome of one operation. A field a stage did not reach is
// its zero value.
type Result struct {
	ID     ID
	Op     Op
	Status Status
	// Stage is the stage that rejected or was running when the operation was
	// abandoned, and the last stage reached otherwise.
	Stage Stage
	// Err is the cause of Rejected, Abandoned (ErrAbandoned) and Failed (ErrVerdict).
	Err error

	Generation uint64             // the number of the applied generation; 0 until one is applied
	Digest     launch.Digest      // the digest of the candidate
	Previous   launch.Composition // the composition the tree ran before this operation: what a later step restores
	Admission  launch.Admission
	Plan       launch.Plan
	Applied    []loader.Result // what the loader did, in the order of its operations
	Report     composition.Report
}

// Reload is what the reload key serves (03-c3-kernel.md:65, :87).
type Reload interface {
	// Submit queues op and returns its id at once. It never waits, so it is safe in a
	// leased call (plugin-system-2026-11.md:363). It returns ErrQueueFull, ErrClosed,
	// or ErrNested for a ctx that carries the reload goroutine's work. The docs write
	// Submit(op); ctx is added so that a nested submission can be told from an
	// independent one (open point).
	Submit(ctx context.Context, op Op) (ID, error)
	// Result returns the result of id as it stands, false for an unknown id.
	Result(id ID) (Result, bool)
	// Await waits until id is final and returns its result. It returns ctx's error if
	// ctx ends first and ErrUnknown for an unknown id. Do not call it inside a leased
	// call of a fiber that the operation may restart: the apply waits for the lease.
	Await(ctx context.Context, id ID) (Result, error)
}
