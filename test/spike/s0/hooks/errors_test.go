// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParallelErrorPreservesOrderedActualCauses(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	err := aggregateFailures([]error{&ListenerError{PluginID: "a", Cause: first}, nil,
		&ListenerError{PluginID: "b", Cause: second}}, context.Canceled)
	aggregate, ok := err.(*ParallelAggregateError)
	if !ok || !errors.Is(err, first) || !errors.Is(err, second) || !errors.Is(err, context.Canceled) {
		t.Fatalf("lost native aggregate/cause identity: %T %v", err, err)
	}
	causes := aggregate.Unwrap()
	if len(causes) != 3 || !errors.Is(causes[0], first) || !errors.Is(causes[1], second) || causes[2] != context.Canceled {
		t.Fatal("all-settled member order changed")
	}
	causes[0] = nil
	if aggregate.Unwrap()[0] == nil || !strings.Contains(err.Error(), "listener a: first") || !strings.Contains(err.Error(), "listener b: second") {
		t.Fatal("mutable member slice or lost native attribution")
	}
	if _, ok := aggregateFailures([]error{first}, nil).(*ParallelAggregateError); !ok {
		t.Fatal("single failed listener lost aggregate identity")
	}
	if aggregateFailures([]error{nil, nil}, nil) != nil {
		t.Fatal("successful all-settled dispatch produced an error")
	}
}
