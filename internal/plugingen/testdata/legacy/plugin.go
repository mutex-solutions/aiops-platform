// SPDX-License-Identifier: Apache-2.0

// Package legacy is a plugin that plugingen's tests generate from: written with
// kernel.Define and made by New, as the loader is.
package legacy

import (
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// New returns the plugin.
func New() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:     "test/legacy",
		Inject: []kernel.Dep{kernel.Need(launch.CatalogKey)},
		Apply:  func(*kernel.Context, any) error { return nil },
	})
}
