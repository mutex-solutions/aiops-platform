// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
	"reflect"
	"testing"

	"github.com/mutex-solutions/aiops-platform/plugins/reload"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	loaderfake "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The value the reload key serves has the methods of reload.Reload and no other, so a
// consumer reaches nothing beyond the definition by a type assertion, such as the loop
// of the queue (07-c4-plugin-sdk.md:516, "No side door").
func TestServesNoMethodBeyondTheDefinition(t *testing.T) {
	root := kerneltest.New(t)
	_, lp := launchfake.New()
	_, ldp := loaderfake.New()
	kerneltest.Mount(root, lp, ldp)
	active(t, root.Plugin(reload.New(), reload.Config{Queue: 4, History: 4}))
	var served def.Reload
	kerneltest.Mount(root, kernel.Define(kernel.Plugin{ID: "aiops/caller", Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply: func(c *kernel.Context, _ any) error { served = def.Key.Of(c); return nil }}))
	want := reflect.TypeFor[def.Reload]()
	got := reflect.TypeOf(served)
	for i := range got.NumMethod() {
		if name := got.Method(i).Name; !hasMethod(want, name) {
			t.Errorf("the served value has the method %s, which reload.Reload lacks", name)
		}
	}
}

func hasMethod(t reflect.Type, name string) bool {
	_, ok := t.MethodByName(name)
	return ok
}
