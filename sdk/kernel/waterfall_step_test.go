// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
)

// A next that is still running on another goroutine when its listener returns is
// waited for. The wait is arranged in a synctest bubble: Wait returns once the
// expiring goroutine is parked on the condition, so no timing decides it.
func TestStepExpireWaitsForARunningNext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &step[int, int]{state: stepRunning}
		s.cond.L = &s.mu
		var expired atomic.Bool
		go func() { s.expire(); expired.Store(true) }()
		synctest.Wait()
		if expired.Load() {
			t.Fatal("expire returned while next was running")
		}
		s.mu.Lock()
		s.state = stepDone
		s.mu.Unlock()
		s.cond.Broadcast()
		synctest.Wait()
		if !expired.Load() {
			t.Fatal("expire did not return after next finished")
		}
	})
}
