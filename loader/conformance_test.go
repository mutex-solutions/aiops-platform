// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The loader passes the loader definition's suite, as its fake does (T2,
// 07-c4-plugin-sdk.md).
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (def.Loader, *kernel.Root) {
		var mods []launch.CatalogEntry
		for id := range s.Plugins {
			mods = append(mods, launch.CatalogEntry{ID: id, Volatile: s.Volatile[id]})
		}
		x := newFx(t, fxOpt{role: s.Role, plugins: s.Plugins, modules: mods})
		return x.ld, x.root
	})
}
