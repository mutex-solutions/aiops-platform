// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"errors"
	"testing"

	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

func TestApplyIsAllOrNothing(t *testing.T) {
	f, provider := fake.New()
	root := kerneltest.New(t)
	kerneltest.Mount(root, provider)
	ctx := context.Background()
	if err := f.Create(ctx, loader.EntrySpec{ID: "a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	res, err := f.Apply(ctx, []loader.Op{
		{Kind: loader.OpCreate, Entry: loader.EntrySpec{ID: "b", Enabled: true}},
		{Kind: loader.OpUpdate, Entry: loader.EntrySpec{ID: "a", Config: 1, Enabled: true}},
		{Kind: loader.OpRemove, Entry: loader.EntrySpec{ID: "a"}},
	})
	if err != nil || len(res) != 3 || res[0].Outcome != loader.Created || res[2].Outcome != loader.Removed {
		t.Fatalf("Apply = %v, %v", res, err)
	}
	if _, ok := f.Locate("a"); ok {
		t.Error("a survived its remove")
	}
	_, err = f.Apply(ctx, []loader.Op{
		{Kind: loader.OpRemove, Entry: loader.EntrySpec{ID: "b"}},
		{Kind: loader.OpCreate, Entry: loader.EntrySpec{ID: "b"}},
		{Kind: loader.OpCreate, Entry: loader.EntrySpec{ID: "b"}},
	})
	if !errors.Is(err, loader.ErrExists) {
		t.Errorf("duplicate create: %v", err)
	}
	if _, ok := f.Locate("b"); !ok {
		t.Error("a refused plan removed b")
	}
}

func TestApplyReportsADeadDispatcher(t *testing.T) {
	f, provider := fake.New()
	root := kerneltest.New(t)
	fiber := root.Plugin(provider, nil)
	if err := fiber.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := f.Create(ctx, loader.EntrySpec{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := fiber.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	_ = fiber.Await(ctx)
	if err := f.Create(ctx, loader.EntrySpec{ID: "b"}); !errors.Is(err, kernel.ErrInactive) {
		t.Errorf("Create after the provider ended: %v", err)
	}
	if _, ok := f.Locate("b"); ok {
		t.Error("a refused Create recorded its entry")
	}
	if err := f.Update(ctx, loader.EntrySpec{ID: "a"}); !errors.Is(err, kernel.ErrInactive) {
		t.Errorf("Update after the provider ended: %v", err)
	}
}
