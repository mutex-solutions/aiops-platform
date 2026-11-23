// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The generated cycle test calls New with no argument and reads Inventory
// (ADR-060 decision 12).
var (
	_ func() (*fake.Reader, kernel.Plugin) = fake.New
	_ interface{ Inventory() []string }    = (*fake.Reader)(nil)
)

// A caller of composition only reads, so it leaves nothing behind.
func TestInventoryIsEmpty(t *testing.T) {
	f, _ := fake.New()
	if got := f.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory = %v", got)
	}
}
