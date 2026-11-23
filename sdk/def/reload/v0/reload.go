// SPDX-License-Identifier: Apache-2.0

package reload

import (
	"context"
	"errors"
	"fmt"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the reload key: consumer-bound, class C, at major 0, with an open provider
// side (03-c3-kernel.md:65; 07-c4-plugin-sdk.md:557, where `reload` is among the
// twelve consumer-bound keys).
var Key = kernel.NewKey[Reload]("reload", 0, kernel.ConsumerBound())

// Applied is the composition/applied event, owned by Key, as the composition
// definition prescribes (sdk/def/composition/v0 NewApplied; 07-c4-plugin-sdk.md:381).
// reload publishes it after a generation was applied and judged
// (03-c3-kernel.md:382).
var Applied = composition.NewApplied(Key)

// Operation is the reload/operation event, owned by Key, access open. reload emits it
// with the Result as it stands when an operation is about to apply a generation
// (Status Running, Stage apply, with its Plan), and once more when that operation is
// final, whatever its status: one event of each per operation. A passed generation
// that changes the reload row itself is final before that change is applied, as the
// change may end the activation that would emit the event; a refusal of that change
// is reported on the Result only, and the operation stays passed. Between the two the
// rows of the tree are not judged: a failure there is rolled back when the verdict
// fails (08-profiles-roadmap-adrs.md:470, :473), so restart-policy holds its give-ups,
// and the retries of the rows the plan changes or restarts, until the final event.
// Both dispatches carry the context of the reload goroutine, which ends when the
// reload fiber unloads: an operation whose final event never came is over once that
// context is done. A final event whose Err holds ErrStateUnknown says that the state
// of the tree is unknown; when the activation that ran the operation was unloading,
// the next activation of the row emits it. The hooks table lists it as an S1 addition
// (07-c4-plugin-sdk.md:462).
var Operation = kernel.NewEvent[Result]("reload/operation", kernel.OwnedBy(Key))

// The refusals of Submit and Await.
var (
	// ErrQueueFull refuses a Submit while the queue holds its bound of waiting
	// operations: backpressure, never a wait.
	ErrQueueFull = errors.New("reload: queue is full")
	// ErrClosed refuses a Submit once the reload fiber is unloading or disposed. It
	// matches kernel.ErrInactive: a call after release (07-c4-plugin-sdk.md §4.3).
	ErrClosed = fmt.Errorf("reload: closed: %w", kernel.ErrInactive)
	// ErrNested refuses a nested submission: a Submit made under the context of the
	// reload goroutine's own work, for example from a listener of composition/applied
	// or of a loader hook (03-c3-kernel.md:83).
	ErrNested = errors.New("reload: nested submission")
	// ErrUnknown reports an operation id that was never issued or whose result was
	// evicted from the bounded history.
	ErrUnknown = errors.New("reload: unknown operation")
	// ErrAbandoned is the cause of a result whose operation was queued or running when
	// the reload row unloaded, unless the unload is the restart that applies the row's
	// own change, whose next activation runs the queue. A queued operation that is
	// abandoned never touched the tree; a submitter that still wants the change submits
	// again to the row's next activation (an Op carries no content).
	ErrAbandoned = errors.New("reload: abandoned at disposal")
	// ErrVerdict is the cause of a result whose generation was applied and judged
	// failed.
	ErrVerdict = errors.New("reload: failed verdict")
	// ErrDenied is the cause of a generation that the gate denied rows of: it is
	// rejected whole (ADR-059 decision 15).
	ErrDenied = errors.New("reload: the gate denied rows of the generation")
	// ErrRollback is part of the cause of a result whose failed generation could not
	// be rolled back.
	ErrRollback = errors.New("reload: rollback failed")
	// ErrStateUnknown is part of the cause of a result after which reload does not know
	// what the tree runs: RollbackFailed, or Abandoned after its generation was applied.
	// It refuses every later Submit, and every operation still queued, until the
	// process restarts: reload never plans from a composition it is not sure the loader
	// holds (ADR-059).
	ErrStateUnknown = errors.New("reload: the state of the tree is unknown after a failed rollback or an abandoned generation; restart the process")
)

// ID names one submitted operation. The first is 1.
type ID uint64

// Op is one submission: a request to compose the layers again and apply the result.
// Source names the submitter ("aiopsctl", "reload-watch-files") on the Result and the
// reload/operation event. It is a label, not an identity: it is not authenticated, and
// composition/applied does not carry it (ADR-059 decision 45).
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
	// StageRollback is the stage after a failed verdict: the previous composition is
	// applied again and judged.
	StageRollback Stage = "rollback"
)

// Status is where an operation stands.
type Status string

// The statuses. Queued and Running are not final.
const (
	Queued  Status = "queued"
	Running Status = "running"
	// Passed: the generation was applied and the verdict is pass.
	Passed Status = "passed"
	// RolledBack: the generation was applied and its verdict is fail, and reload applied Result.Previous again with one
	// inverse plan, and judged that its rows are ACTIVE again (ADR-059 decision 12).
	// Result.Report is the failed verdict and Result.Rollback the outcome.
	RolledBack Status = "rolled back"
	// RollbackFailed: the verdict failed and the rollback failed too: the inverse plan
	// was refused, or the restored rows are not ACTIVE. Result.Report and
	// Result.Rollback hold both verdicts. The rollback is not tried again.
	RollbackFailed Status = "rollback failed"
	// Rejected: a stage before the apply, or the apply itself, refused the
	// generation, and the tree was not touched. A generation that holds a row the
	// gate denied, or of a plugin that is not in the module table, is rejected at the
	// admit stage with ErrDenied (ADR-059 decision 15).
	Rejected Status = "rejected"
	// Abandoned: the reload row unloaded first, and the unload is not the restart that
	// applies the row's own change, whose next activation runs the queue. An operation
	// that was running may have touched the tree, and Stage says how far it got; one
	// whose generation was applied has ErrStateUnknown in its Err. One that was queued
	// never touched the tree, and its submitter submits again if it still wants the
	// change (ADR-059 decision 48).
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
	// Err is the cause of Rejected, of Abandoned (ErrAbandoned), of RolledBack
	// (ErrVerdict) and of RollbackFailed (ErrVerdict and ErrRollback, and why the
	// rollback failed). It holds ErrStateUnknown when the tree's state is unknown
	// after the operation.
	Err error

	Generation uint64             // the number of the applied generation; 0 until one is applied
	Digest     launch.Digest      // the digest of the candidate
	Previous   launch.Composition // the composition the tree ran before this operation: what a rollback restores
	Admission  launch.Admission
	Plan       launch.Plan
	Applied    []loader.Result    // what the loader did, in the order of its operations
	Report     composition.Report // the verdict on the generation; when it failed, the failed verdict
	// Rollback is what restoring Previous did after a failed verdict, nil when the
	// verdict passed or the generation was not applied.
	Rollback *Rollback
}

// Rollback is the outcome of restoring the previous composition after a failed
// verdict (ADR-059 decision 12): one inverse plan, applied through the loader and
// judged. The generation numbers are those that composition/applied carries: the
// failed generation is N and the restored one is N+1, published after it.
type Rollback struct {
	Plan       launch.Plan        // the diff from the failed generation back to Previous
	Applied    []loader.Result    // what the loader did, in the order of its operations
	Report     composition.Report // the verdict on the restored rows: Pass says they are ACTIVE again
	Generation uint64             // N+1; 0 when the rollback failed
	Err        error              // nil when the previous composition is running again
}

// Reload is what the reload key serves (03-c3-kernel.md:65, :83).
type Reload interface {
	// Submit queues op and returns its id at once. It never waits, so it is safe in a
	// leased call (plugin-system-2026-11.md §9). It returns ErrQueueFull, ErrClosed or
	// ErrStateUnknown. ctx marks the reload goroutine's own work, as the
	// AsyncLocalStorage of dsh runExclusive does: a Submit made under it is a nested
	// submission and is refused with ErrNested, and any other Submit only queues
	// (ADR-059 decision 45).
	Submit(ctx context.Context, op Op) (ID, error)
	// Result returns the result of id as it stands, false for an unknown id.
	Result(id ID) (Result, bool)
	// Await waits until id is final and returns its result. It returns ctx's error if
	// ctx ends first and ErrUnknown for an unknown id. Do not call it inside a leased
	// call of a fiber that the operation may restart: the apply waits for the lease.
	Await(ctx context.Context, id ID) (Result, error)
}
