// SPDX-License-Identifier: Apache-2.0

// Package echoer is example-echoer, the toy instantiable echoer.
package echoer

import "github.com/mutex-solutions/aiops-platform/sdk/kernel"

// ID is the plugin id.
const ID = "aiops/example-echoer"

// Plugin is the echoer.
var Plugin = kernel.Define(kernel.Plugin{ID: ID, Decode: func(raw any) (any, error) { return raw, nil },
	Apply: func(*kernel.Context, any) error { return nil }})
