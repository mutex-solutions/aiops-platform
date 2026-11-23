// SPDX-License-Identifier: Apache-2.0

// Package twokeys is a plugin that plugingen refuses to generate from: it injects
// one key of the launch definition and provides another, so the fake of launch,
// which provides every key of it, would provide that key a second time in the cycle
// test.
package twokeys

import (
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Plugin is the plugin.
var Plugin = kernel.Define(kernel.Plugin{
	ID:       "test/twokeys",
	Inject:   []kernel.Dep{kernel.Need(launch.CatalogKey)},
	Provides: []kernel.AnyKey{launch.AppExitKey},
	Apply:    func(*kernel.Context, any) error { return nil },
})
