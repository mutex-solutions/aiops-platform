// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The generated cycle test calls New with no argument and reads Inventory
// (ADR-060 decision 12).
var (
	_ func() (fake.DryRun, kernel.Plugin) = fake.New
	_ interface{ Inventory() []string }   = fake.DryRun{}
)

// New resolves every reference, NewWith those its predicate names; a dry run leaves
// nothing behind, so Inventory is empty.
func TestNewResolvesEveryReference(t *testing.T) {
	ref := map[string]any{"k": launch.Ref{Scheme: "env", Name: "X"}}
	all, _ := fake.New()
	none, _ := fake.NewWith(func(launch.Ref) bool { return false })
	if err := all.Check(t.Context(), ref, nil); err != nil {
		t.Fatalf("New refused a reference: %v", err)
	}
	if err := none.Check(t.Context(), ref, nil); err == nil {
		t.Fatal("NewWith resolved a reference its predicate refuses")
	}
	if got := all.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory = %v", got)
	}
}
