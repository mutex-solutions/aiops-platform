// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"slices"
	"testing"

	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the guard half, as the real dispatcher does.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		_, p := fake.New()
		return conformance.Dispatcher(root.Plugin(p, nil)), root
	})
}

type upper struct{}

func (upper) Stamp(_ context.Context, body string) (string, error) { return "up:" + body, nil }

// The fake records every call and every registered stamper.
func TestRecords(t *testing.T) {
	root := kerneltest.New(t)
	rec, p := fake.New()
	kerneltest.Mount(root, p, kernel.Define(kernel.Plugin{ID: "test/upper", Inject: []kernel.Dep{kernel.Need(stampers.Key)},
		Apply: func(c *kernel.Context, _ any) error { return stampers.Key.Of(c).Register(c, "up", upper{}) }}))
	if r, err := rec.Stamp(t.Context(), "up", "hi"); r != "up:hi" || err != nil {
		t.Fatalf("Stamp = %q, %v", r, err)
	}
	_, _ = rec.Stamp(t.Context(), "nope", "x")
	if got := rec.Calls(); !slices.Equal(got, []fake.Call{{Name: "up", Body: "hi"}, {Name: "nope", Body: "x"}}) {
		t.Errorf("Calls = %v", got)
	}
	if x, ok := rec.Contribution("up"); !ok || x != (upper{}) {
		t.Errorf("Contribution(up) = %v, %v", x, ok)
	}
	if _, ok := rec.Contribution("nope"); ok {
		t.Error("Contribution(nope) found a stamper")
	}
}
