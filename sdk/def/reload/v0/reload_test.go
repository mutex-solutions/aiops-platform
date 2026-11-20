// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
	"errors"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func TestStatusDone(t *testing.T) {
	for s, want := range map[reload.Status]bool{
		reload.Queued: false, reload.Running: false, reload.Passed: true,
		reload.RolledBack: true, reload.RollbackFailed: true, reload.Rejected: true, reload.Abandoned: true,
	} {
		if s.Done() != want {
			t.Errorf("%s.Done() = %v, want %v", s, !want, want)
		}
	}
}

func TestKeyClassAndEvent(t *testing.T) {
	if reload.Key.Name() != "reload" || reload.Key.Major() != 0 {
		t.Fatalf("key = %s@%d", reload.Key.Name(), reload.Key.Major())
	}
	if reload.Applied.Name() != "composition/applied" || reload.Applied.Owner().Name() != "reload" {
		t.Fatalf("event = %s owned by %v", reload.Applied.Name(), reload.Applied.Owner())
	}
	_, prov := fake.New()
	want := kernel.Define(kernel.Plugin{ID: "aiops/listed",
		Inject: []kernel.Dep{kernel.Need(reload.Key)},
		Apply:  func(c *kernel.Context, _ any) error { _ = reload.Key.Of(c); return nil }})
	other := kernel.Define(kernel.Plugin{ID: "aiops/other",
		Inject: []kernel.Dep{kernel.Need(reload.Key)},
		Apply:  func(c *kernel.Context, _ any) error { _ = reload.Key.Of(c); return nil }})
	root := deftest.Bound(t, deftest.Config(nil, kernel.Bindings{reload.Key: {want}}))
	deftest.Active(t, root.Plugin(prov, nil))
	deftest.Active(t, root.Plugin(want, nil))
	err := deftest.Failure(t, root.Plugin(other, nil))
	if !errors.Is(err, kernel.ErrBinding) {
		t.Errorf("unlisted consumer: err = %v, want ErrBinding", err)
	}
}
