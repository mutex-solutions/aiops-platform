// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"slices"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The generated cycle test calls New with no argument and reads Inventory
// (ADR-060 decision 12).
var (
	_ func() (*fake.Launcher, kernel.Plugin) = fake.New
	_ interface{ Inventory() []string }      = (*fake.Launcher)(nil)
)

// Inventory reports the ready latch while a caller leaves it set.
func TestInventoryReportsTheReadyLatch(t *testing.T) {
	f, _ := fake.New()
	if got := f.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory = %v", got)
	}
	f.Set()
	if got := f.Inventory(); !slices.Equal(got, []string{"ready"}) {
		t.Fatalf("Inventory with the latch set = %v", got)
	}
	f.Drop()
	if got := f.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory after Drop = %v", got)
	}
}
