// SPDX-License-Identifier: Apache-2.0

package harness

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
)

// TestLimitsAreTheShippedOnes: the limits that every process of a suite gets are the
// ones the binary embeds (test/dist/limits.yaml), so that tuning that file tunes the
// failure matrix and the recomposition suite as well as loader-smoke.
func TestLimitsAreTheShippedOnes(t *testing.T) {
	d, err := testdist.Distribution()
	if err != nil {
		t.Fatal(err)
	}
	got, err := boot.ParseLimits("harness.Limits", []byte(Limits))
	if err != nil {
		t.Fatal(err)
	}
	if got != d.Limits {
		t.Fatalf("harness.Limits is %+v, the binary embeds %+v", got, d.Limits)
	}
}
