// SPDX-License-Identifier: Apache-2.0

// Package caller is example-caller, the toy caller of the two toy registries.
package caller

import "github.com/mutex-solutions/aiops-platform/sdk/kernel"

// ID is the plugin id; Control and Result are the names of the control file and of
// the result file.
const (
	ID      = "aiops/example-caller"
	Control = "call.json"
	Result  = "call.result.json"
)

// Outcome is the content of the result file.
type Outcome struct {
	Request  string           `json:"request"`
	Echoers  map[string]Reply `json:"echoers"`
	Stampers map[string]Reply `json:"stampers"`
}

// Reply is what one contribution answered.
type Reply struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// Plugin is the caller.
var Plugin = kernel.Define(kernel.Plugin{ID: ID, Decode: func(raw any) (any, error) { return raw, nil },
	Apply: func(*kernel.Context, any) error { return nil }})
