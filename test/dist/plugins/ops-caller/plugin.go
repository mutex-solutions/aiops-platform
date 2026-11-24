// SPDX-License-Identifier: Apache-2.0

// Package opscaller is a control row of the test distribution that consumes the
// operations key, which is consumer-bound (ADR-061 decision 15): the binding of the
// test distribution lists it, so a profile that composes it beside the operations row
// boots both. It stands for the command rows of aiops-cli, the consumers that the
// release lists (ADR-061 decision 30), until they land. It is in no shipped
// distribution.
package opscaller

import (
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// ID is the plugin id.
const ID = "aiops-test/ops-caller"

// New returns a plugin that needs the operations key and does nothing with it.
func New() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:     ID,
		Inject: []kernel.Dep{kernel.Need(def.Key)},
		Apply:  func(c *kernel.Context, _ any) error { _ = def.Key.Of(c); return nil },
	})
}
