// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/conformance"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The loader, the provider of the composition key, passes the composition
// definition's suite, as its fake does (T2, 07-c4-plugin-sdk.md §8.2).
func TestCompositionConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (*kernel.Root, *kernel.Fiber) {
		rows := make([]def.EntrySpec, len(s.Rows))
		for i, r := range s.Rows {
			rows[i] = def.EntrySpec{ID: r.ID, Plugin: r.Plugin, Parent: r.Parent, Enabled: true, Roles: []string{"api"}}
		}
		x := newFx(t, fxOpt{rows: rows, plugins: s.Plugins})
		return x.root, x.fiber
	})
}
