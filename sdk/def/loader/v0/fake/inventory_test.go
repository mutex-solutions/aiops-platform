// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"slices"
	"testing"

	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The generated cycle test calls New with no argument and reads Inventory
// (ADR-060 decision 12).
var (
	_ func() (*fake.Loader, kernel.Plugin) = fake.New
	_ interface{ Inventory() []string }    = (*fake.Loader)(nil)
)

// Inventory lists the entries its callers left in the tree, sorted.
func TestInventoryListsTheEntries(t *testing.T) {
	f, provider := fake.New()
	kerneltest.Mount(kerneltest.New(t), provider)
	ctx := context.Background()
	for _, id := range []string{"b", "a"} {
		if err := f.Create(ctx, loader.EntrySpec{ID: id, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Inventory(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Inventory = %v", got)
	}
	if err := f.Remove(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if got := f.Inventory(); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("Inventory after Remove = %v", got)
	}
}
