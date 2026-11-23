// SPDX-License-Identifier: Apache-2.0

// Package stamper is example-stamper, the toy stamper.
package stamper

import "github.com/mutex-solutions/aiops-platform/sdk/kernel"

// ID is the plugin id.
const ID = "aiops/example-stamper"

// Plugin is the stamper.
var Plugin = kernel.Define(kernel.Plugin{ID: ID, Decode: func(raw any) (any, error) { return raw, nil },
	Apply: func(*kernel.Context, any) error { return nil }})
