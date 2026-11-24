// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the guard half, as the cli row does.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		f, p := fake.New()
		return conformance.Dispatcher(root.Plugin(p, nil), func(r commands.Pending) { f.Pending = append(f.Pending, r) }), root
	})
}

type say struct{}

func (say) Run(_ context.Context, inv commands.Invocation) error {
	v, _ := inv.Env("NAME")
	_, err := inv.Out.Write([]byte(inv.Dir + " " + inv.Home + " " + v + " " + inv.Args[0]))
	return err
}

// The fake records every registration and every dispatch, and gives each invocation
// its directory, home and environment.
func TestRecords(t *testing.T) {
	root := kerneltest.New(t)
	rec, p := fake.New()
	rec.Dir, rec.Home, rec.Env = "/work", "/home", map[string]string{"NAME": "v"}
	spec := commands.Spec{Path: []string{"say"}, Summary: "Say.", Min: 1, Max: 1}
	kerneltest.Mount(root, p, kernel.Define(kernel.Plugin{ID: "test/say", Inject: []kernel.Dep{kernel.Need(commands.Key)},
		Apply: func(c *kernel.Context, _ any) error { return commands.Key.Of(c).Register(c, spec, say{}) }}))
	var out bytes.Buffer
	if code, err := rec.Dispatch(t.Context(), []string{"say", "hi"}, commands.Streams{Out: &out}); code != 0 || err != nil || out.String() != "/work /home v hi" {
		t.Fatalf("Dispatch = %d, %v, stdout %q", code, err, out.String())
	}
	_, _ = rec.Dispatch(t.Context(), []string{"nope"}, commands.Streams{})
	if got := rec.Calls(); !slices.EqualFunc(got, [][]string{{"say", "hi"}, {"nope"}}, slices.Equal) {
		t.Errorf("Calls = %q", got)
	}
	if s, ok := rec.Spec("say"); !ok || s.Summary != "Say." {
		t.Errorf("Spec(say) = %+v, %v", s, ok)
	}
	if !slices.Equal(rec.Inventory(), []string{"say"}) {
		t.Errorf("Inventory = %q", rec.Inventory())
	}
}

// Before its plugin is mounted, the fake is an inactive registry.
func TestBeforeMount(t *testing.T) {
	rec, _ := fake.New()
	if code, err := rec.Dispatch(t.Context(), nil, commands.Streams{}); code != 1 || err != kernel.ErrInactive {
		t.Errorf("Dispatch = %d, %v", code, err)
	}
	if err := rec.Register(nil, commands.Spec{}, nil); err != kernel.ErrInactive || rec.Inventory() != nil {
		t.Errorf("Register = %v, Inventory = %q", err, rec.Inventory())
	}
}
