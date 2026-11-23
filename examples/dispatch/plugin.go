// SPDX-License-Identifier: Apache-2.0

// Package dispatch is example-dispatch, the dispatcher of the two toy registries.
package dispatch

import "github.com/mutex-solutions/aiops-platform/sdk/kernel"

// ID is the plugin id.
const ID = "aiops/example-dispatch"

// Plugin is the dispatcher.
var Plugin = kernel.Define(kernel.Plugin{ID: ID, Apply: func(*kernel.Context, any) error { return nil }})
