// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the suite that the loader passes. It holds what the test commits:
// the rows of the setup, ACTIVE, with the hooks their plugins declare.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (*kernel.Root, *kernel.Fiber) {
		f, p := fake.New()
		g := composition.Generation{Scope: composition.ScopeProcess}
		for _, r := range s.Rows {
			r.State = kernel.Active
			for _, u := range s.Plugins[r.Plugin].Hooks {
				r.Hooks = append(r.Hooks, u.Hook())
			}
			g.Rows = append(g.Rows, r)
		}
		f.Commit(g)
		root := kerneltest.New(t)
		fb := root.Plugin(p, nil)
		deftest.Active(t, fb)
		return root, fb
	})
}
