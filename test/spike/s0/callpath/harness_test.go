// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"os"
	"testing"
)

func TestFrozenProtocol(t *testing.T) {
	if Repetitions*SamplesPerRun < 1_000_000 || ScheduledUpdates != UpdatesPerSecond*int(StressDuration.Seconds()) || Leaves != 100 || OutstandingLimit != 8 {
		t.Fatal("frozen callpath protocol changed")
	}
}

func TestFullCallpath(t *testing.T) {
	if os.Getenv("S0_CALLPATH_RUN") != "1" {
		t.Skip("opt-in only; parent executes full performance/stress later")
	}
	t.Fatal("authoring scaffold only: full runner not yet implemented; no acceptance result")
}
