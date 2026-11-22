// SPDX-License-Identifier: Apache-2.0

// Package clock is the definition of the clock key: now, and timers as effects
// (03-c3-kernel.md:69; plugin-system-2026-11.md §5). It is built at S1 so that
// restart-policy, which "retries a FAILED fiber in place with backoff and gives up
// after a number of failures in a window" (03-c3-kernel.md:66), is testable in
// time (ADR-059 decision 8). The docs also name tickers; none is declared here
// because restart-policy needs only Now and one-shot timers, and a ticker is
// additive in a later minor. The key is class `-` (open) (03-c3-kernel.md:69).
//
// The package imports the standard library and sdk/kernel only.
package clock
