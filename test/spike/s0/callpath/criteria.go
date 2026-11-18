// SPDX-License-Identifier: Apache-2.0

// Package callpath is a bounded, test-only §8.3 harness for both real S0 candidates.
// It does not select a kernel, amend criteria, or run performance work by default.
package callpath

import "time"

const (
	Repetitions      = 10
	SamplesPerRun    = 100_000 // Per candidate/concurrency/operation: 1,000,000 over ten runs.
	Leaves           = 100
	UpdatesPerSecond = 100
	OutstandingLimit = 8
	OwnerUpdateEvery = 1_000
	StressDuration   = 120 * time.Second
	ScheduledUpdates = 12_000
)

// CallerConcurrencies returns fresh immutable-protocol data.
func CallerConcurrencies() []int { return []int{1, 8, 32} }

// Operation separates complete costs; Lease includes the actual owner's scheduler
// wake callback, not an isolated atomic. Dispatch includes Resolve + view + Call.
type Operation string

const (
	LookupView Operation = "lookup_view"
	Lease      Operation = "owner_enter_leave"
	Dispatch   Operation = "lookup_view_lease_call"
	Timer      Operation = "timer_overhead"
)
