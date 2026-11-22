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
	ok := map[string]any{"a": launch.Ref{Scheme: "env", Name: "AIOPS_REF_A"}, "l": []any{int64(1), launch.Ref{Scheme: "env", Name: "AIOPS_REF_L"}}}
	if err := dr.Check(t.Context(), ok, nil); err != nil || f.Check(t.Context(), nil, nil) != nil {
		t.Fatalf("Check = %v", err)
	}
	// decode gets the resolved tree, each reference resolved to its name, and its error
	// is the result.
	var seen any
	errDecode := errors.New("refused")
	if err := dr.Check(t.Context(), ok, func(v any) error { seen = v; return errDecode }); err != errDecode {
		t.Errorf("Check with a decode that refuses = %v", err)
	}
	if m, _ := seen.(map[string]any); m["a"] != "AIOPS_REF_A" || m["l"].([]any)[1] != "AIOPS_REF_L" {
		t.Errorf("decode saw %v", seen)
	}
	for _, bad := range []any{
		launch.Ref{Scheme: "secret", Name: "x"},
		map[string]any{"m": map[string]any{"s": launch.Ref{Scheme: "secret", Name: "x"}}},
		[]any{"y", launch.Ref{Scheme: "file", Name: "f"}},
	} {
		called := false
		if err := dr.Check(t.Context(), bad, func(any) error { called = true; return nil }); !errors.Is(err, configrefs.ErrUnresolved) || called {
			t.Errorf("Check(%v) = %v, decode called %v; want ErrUnresolved and no decode", bad, err, called)
		}
	}
}
