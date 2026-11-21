// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"errors"
	"testing"

	configrefs "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func TestCheckFindsAReferenceThatDoesNotResolve(t *testing.T) {
	env := func(r launch.Ref) bool { return r.Scheme == "env" }
	f, p := fake.New(env)
	// The key is provider-bound: the kernel admits the fake's plugin when the binding
	// lists it, and the dry run is what a consumer resolves.
	root := deftest.Bound(t, deftest.Config(kernel.Bindings{configrefs.Key: {p}}, nil))
	got := make(chan configrefs.DryRun, 1)
	user := kernel.Define(kernel.Plugin{ID: "t/user", Inject: []kernel.Dep{kernel.Need(configrefs.Key)},
		Apply: func(c *kernel.Context, _ any) error { got <- configrefs.Key.Of(c); return nil }})
	deftest.Active(t, root.Plugin(p, nil))
	deftest.Active(t, root.Plugin(user, nil))
	dr := <-got
	ok := map[string]any{"a": launch.Ref{Scheme: "env", Name: "AIOPS_REF_A"}, "l": []any{int64(1), "x"}}
	if err := dr.Check(t.Context(), ok); err != nil || f.Check(t.Context(), nil) != nil {
		t.Fatalf("Check = %v", err)
	}
	for _, bad := range []any{
		launch.Ref{Scheme: "secret", Name: "x"},
		map[string]any{"m": map[string]any{"s": launch.Ref{Scheme: "secret", Name: "x"}}},
		[]any{"y", launch.Ref{Scheme: "file", Name: "f"}},
	} {
		if err := dr.Check(t.Context(), bad); !errors.Is(err, configrefs.ErrUnresolved) {
			t.Errorf("Check(%v) = %v, want ErrUnresolved", bad, err)
		}
	}
}
