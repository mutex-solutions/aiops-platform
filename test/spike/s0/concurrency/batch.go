// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// BatchKind is a desired-state operation, not a plugin callback.
type BatchKind uint8

const (
	BatchMount BatchKind = iota + 1
	BatchUpdate
	BatchDispose
)

// BatchOperation is neutral experimental data. Mount uses Spec; other operations
// use an existing nonzero Fiber. Mount IDs are assigned only at actual commit.
type BatchOperation struct {
	Kind  BatchKind
	Fiber uint64
	Spec  Spec
}

// Batcher commits one validated desired-state prefix before admitting resulting
// lifecycle/check work. Results have the same length/order as the input; only
// Mount positions contain newly assigned IDs. Two Updates increment revision
// twice but cannot start an intermediate activation. Repeated Dispose is allowed;
// an Update after Dispose is rejected before the entire prefix commits.
//
// No callbacks, configuration resolve/commit/veto hooks or native-root operations
// run in this data-only API. UpdateConfig batching is NOT established by this
// prototype. Batch either returns the ids of a committed prefix or has no effect:
// cancellation before the core takes the command makes no changes and returns the
// context error, and once the core has taken it the caller receives the result
// (the reconciler claims each command atomically, see reconcilerRuntime.exchange;
// the graph-lock commits under its only check of the context).
type Batcher interface {
	Batch(context.Context, []BatchOperation) ([]uint64, error)
}

func batchUnique(keys []string) bool {
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			return false
		}
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

// prepareBatch copies declarations and checks every operation before any writer
// mutates state. live is native state data copied/read by the single state owner;
// it must not invoke plugin code. False means absent or already disposing.
func prepareBatch(operations []BatchOperation, live func(uint64) bool) ([]BatchOperation, error) {
	prepared := slices.Clone(operations)
	disposed := make(map[uint64]bool)
	for i := range prepared {
		op := &prepared[i]
		switch op.Kind {
		case BatchMount:
			if op.Fiber != 0 || op.Spec.ID == "" || !batchUnique(op.Spec.Needs) || !batchUnique(op.Spec.Provides) ||
				(op.Spec.Apply != nil && op.Spec.ApplyEffect != nil) {
				return nil, fmt.Errorf("invalid batch Mount at operation %d", i)
			}
			op.Spec.Needs, op.Spec.Provides = slices.Clone(op.Spec.Needs), slices.Clone(op.Spec.Provides)
		case BatchUpdate, BatchDispose:
			if op.Fiber == 0 || !live(op.Fiber) {
				return nil, fmt.Errorf("batch operation %d: %w", i, ErrUnknown)
			}
			if op.Spec.ID != "" || op.Spec.Apply != nil || op.Spec.ApplyEffect != nil || op.Spec.Check != nil ||
				op.Spec.Config != nil || len(op.Spec.Needs) != 0 || len(op.Spec.Provides) != 0 {
				return nil, fmt.Errorf("batch operation %d carries an unused Mount specification", i)
			}
			if disposed[op.Fiber] && op.Kind == BatchUpdate {
				return nil, fmt.Errorf("batch operation %d updates a disposed target", i)
			}
			if op.Kind == BatchDispose {
				disposed[op.Fiber] = true
			}
		default:
			return nil, errors.New("unknown batch operation kind")
		}
	}
	return prepared, nil
}
