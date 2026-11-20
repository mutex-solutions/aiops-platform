// SPDX-License-Identifier: Apache-2.0

package clock

import (
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the clock key, class `-` (open), at major 0 (03-c3-kernel.md:69).
var Key = kernel.NewKey[Clock]("clock", 0)

// Clock is now and one-shot timers. "Now, timers and tickers as effects"
// (03-c3-kernel.md:69): a timer is an effect of the caller's fiber, so it stops
// when the fiber unloads (07-c4-plugin-sdk.md:516 "Caller context first").
type Clock interface {
	// Now is the current time.
	Now() time.Time
	// After starts a timer that fires once, d from now, and records its stop as an
	// effect of c's fiber. A d of zero or less fires at once. It returns an error
	// that matches kernel.ErrInactiveEffect on a fiber that is unloading.
	After(c *kernel.Context, d time.Duration) (Timer, error)
}

// Timer is one pending firing. C receives the time of the firing, once, and is
// never closed.
type Timer interface {
	C() <-chan time.Time
	// Stop cancels the timer and reports whether it had not yet fired.
	Stop() bool
}
