// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"fmt"
	"strings"
)

// ListenerError preserves native attribution without erasing the actual cause.
// A fixture codec can identify this known transparent boundary explicitly;
// arbitrary Go wrappers must not be treated as equivalent provenance.
type ListenerError struct {
	PluginID string
	Cause    error
}

func (e *ListenerError) Error() string { return fmt.Sprintf("listener %s: %v", e.PluginID, e.Cause) }
func (e *ListenerError) Unwrap() error { return e.Cause }

// ParallelAggregateError is the actual all-settled result in listener order,
// including a single failure. Its diagnostic text retains every member and
// attribution. It is distinct from arbitrary errors.Join; codecs must not infer
// aggregate identity merely from a generic multi-error Unwrap method.
type ParallelAggregateError struct{ causes []error }

func (e *ParallelAggregateError) Error() string {
	messages := make([]string, len(e.causes))
	for i, cause := range e.causes {
		messages[i] = cause.Error()
	}
	return strings.Join(messages, "\n")
}

// Unwrap returns an owned slice: callers cannot remove or replace native causes.
func (e *ParallelAggregateError) Unwrap() []error { return append([]error(nil), e.causes...) }

func aggregateFailures(errs []error, extra error) error {
	causes := make([]error, 0, len(errs)+1)
	for _, err := range errs {
		if err != nil {
			causes = append(causes, err)
		}
	}
	if extra != nil {
		causes = append(causes, extra)
	}
	if len(causes) == 0 {
		return nil
	}
	return &ParallelAggregateError{causes: causes}
}
