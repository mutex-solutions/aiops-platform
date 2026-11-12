// SPDX-License-Identifier: Apache-2.0

package s0probe

import (
	"context"
	"testing"
	"time"
)

// TestSQLViewBoundary deliberately fails when the actual caller's raw SQL view
// can change role or transaction identity. A successful attack is evidence of a
// failed design gate, not an expected-success assertion that makes CI green.
func TestSQLViewBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := openFixture(t, ctx)
	report, err := f.probeSQLBoundary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writeReport(t, report)
	for _, check := range report.Checks {
		if check.Escaped {
			t.Errorf("SQL contract escaped: %s: %s (SQLSTATE %s)", check.Name, check.Effect, check.SQLState)
		}
	}
	if report.Classification != "pass" {
		t.Errorf("boundary classification=%s; no performance acceptance is possible before an approved design response", report.Classification)
	}
}
