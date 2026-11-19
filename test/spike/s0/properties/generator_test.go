// SPDX-License-Identifier: Apache-2.0

package properties

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSplitMix64Vectors(t *testing.T) {
	rng := splitMix64{}
	for i, want := range []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f} {
		if got := rng.next(); got != want {
			t.Fatalf("SplitMix64 zero-seed vector %d=%016x want %016x", i, got, want)
		}
	}
}

func TestGeneratorIsDeterministicAndCommandsAreLegal(t *testing.T) {
	for index := 0; index < 32; index++ {
		seed := firstSeed + uint64(index)
		commands := generate(seed, 1000)
		if !reflect.DeepEqual(commands, generate(seed, 1000)) || len(commands) != 1000 {
			t.Fatalf("seed %x is not deterministic/exact length", seed)
		}
		var expected model
		for _, command := range commands {
			if err := expected.apply(command); err != nil {
				t.Fatalf("seed %x operation %+v: %v", seed, command, err)
			}
		}
	}
	if reflect.DeepEqual(generate(firstSeed, 128), generate(firstSeed+1, 128)) {
		t.Fatal("neighboring seeds generated the same command stream")
	}
	// A saved stream is ordinary JSON, not a dependency on PRNG state or a
	// candidate's numeric ID allocation. Replaying bytes must retain every op.
	commands := generate(firstSeed, 128)
	encoded, err := json.Marshal(commands)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []Operation
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(commands, decoded) {
		t.Fatalf("command stream JSON round trip: %v", err)
	}
}

func TestIndependentModelStickyFailureAndResourceCounts(t *testing.T) {
	var expected model
	apply := func(op Operation) {
		t.Helper()
		if err := expected.apply(op); err != nil {
			t.Fatal(err)
		}
	}
	apply(Operation{Kind: "mount", Slot: 1, Incarnation: 1, Generation: 1, Needs: []int{0}, Fail: true})
	f := expected.Current[1]
	if f.State != "PENDING" || f.Applications != 0 {
		t.Fatal("missing dependency must not execute even a failing body")
	}
	apply(Operation{Kind: "mount", Slot: 0, Incarnation: 1, Generation: 1})
	if f.State != "FAILED" || f.Applications != 1 || f.Disposals != 1 || f.FailureGeneration != 1 {
		t.Fatalf("initial intentional failure did not unwind: %+v", f)
	}
	apply(Operation{Kind: "value", Slot: 0, Incarnation: 1, Generation: 2, Value: 7})
	apply(Operation{Kind: "dispose", Slot: 0, Incarnation: 1, Generation: 2, Value: 7})
	apply(Operation{Kind: "mount", Slot: 0, Incarnation: 2, Generation: 1, Value: 9})
	if f.State != "FAILED" || f.Applications != 1 || f.Disposals != 1 || f.FailureGeneration != 1 {
		t.Fatalf("dependency arrival/removal retried sticky failure: %+v", f)
	}
	apply(Operation{Kind: "fail", Slot: 1, Incarnation: 1, Generation: 2, Fail: false})
	if f.State != "ACTIVE" || f.Applications != 2 || f.Disposals != 1 || f.FailureGeneration != 0 {
		t.Fatalf("explicit update did not recover: %+v", f)
	}
	apply(Operation{Kind: "dispose", Slot: 0, Incarnation: 2, Generation: 1, Value: 9})
	if f.State != "PENDING" || f.Applications != 2 || f.Disposals != 2 {
		t.Fatalf("dependency withdrawal did not clean successful consumer: %+v", f)
	}
	apply(Operation{Kind: "mount", Slot: 0, Incarnation: 3, Generation: 1})
	apply(Operation{Kind: "dispose", Slot: 1, Incarnation: 1, Generation: 2})
	if f.State != "DISPOSED" || f.Applications != 3 || f.Disposals != 3 || f.Alive {
		t.Fatalf("final resource ledger did not close: %+v", f)
	}
}

func TestIndependentModelRejectsCyclesAndDuplicates(t *testing.T) {
	for _, needs := range [][]int{{0}, {1}, {missingNeed + 1}, {missingNeed, missingNeed}} {
		var expected model
		if err := expected.apply(Operation{Kind: "mount", Slot: 0, Incarnation: 1, Generation: 1, Needs: needs}); err == nil {
			t.Fatalf("model accepted invalid DAG needs %v", needs)
		}
	}
}

func TestIntentionalFailureFilteringRejectsUnknownAggregateLeaves(t *testing.T) {
	known := &intentionalFailure{Name: "fixture", Generation: 3}
	allowed := map[error]bool{known: true}
	if !onlyKnownErrors(nil, allowed) || !onlyKnownErrors(errors.Join(known, known), allowed) {
		t.Fatal("exact known aggregate rejected")
	}
	for _, unexpected := range []error{context.DeadlineExceeded, context.Canceled, errors.New(known.Error()), errors.New("callback panic"), errors.New("cleanup failed")} {
		if onlyKnownErrors(unexpected, allowed) || onlyKnownErrors(errors.Join(known, unexpected), allowed) {
			t.Fatalf("unexpected error leaf was ignored: %v", unexpected)
		}
	}
}

func TestGoroutineOwnershipMatchesStacksNotCountOffsets(t *testing.T) {
	if !ownedGoroutine("github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency.(*rcOwner).run(0x1)") || !ownedGoroutine("github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency.(*GraphLock).runUnload(0x1)") {
		t.Fatal("native runtime owner/transition stacks must be counted")
	}
	if !ownedGoroutine("github.com/mutex-solutions/aiops-platform/test/spike/s0/internal/eventqueue.(*Dispatcher[...]).run(0x1)") {
		t.Fatal("native observer worker must be counted, not exempted as another package")
	}
	if !ownedGoroutine("github.com/mutex-solutions/aiops-platform/sdk/kernel.(*owner).run(0x1)") {
		t.Fatal("kernel owner stacks must be counted")
	}
	for _, stack := range []string{"runtime.forcegchelper()", "testing.(*T).Run(0x1)", "runtime.bgsweep()"} {
		if ownedGoroutine(stack) {
			t.Fatalf("unrelated Go runtime/test stack treated as fixture-owned: %s", stack)
		}
	}
	for _, stack := range goroutines() {
		if !strings.HasPrefix(stack, "goroutine ") {
			t.Fatalf("malformed stack identity: %q", stack)
		}
	}
}
