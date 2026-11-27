// SPDX-License-Identifier: Apache-2.0

// Package rogueprovider is the fault plugin rogue-provider of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml). It is in no
// shipped distribution.
package rogueprovider

import (
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/rogue-provider"

// New returns a plugin that provides the provider-bound key toy.policy, which the
// distribution binds to toy-provider and not to it: the gate denies it (check 7,
// "plugin provides a provider-bound key and is not in its binding"), and a row of it
// with isolate on toy.policy is a static graph error. Were the gate bypassed, the
// kernel refuses the Provide with ErrBinding and the row is FAILED.
func New() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Provides: []kernel.AnyKey{toy.PolicyKey},
		Apply: func(c *kernel.Context, _ any) error {
			return toy.PolicyKey.Provide(c, toy.Policy{Owner: ID})
		},
	})
}
