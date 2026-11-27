// SPDX-License-Identifier: Apache-2.0

// Package badconfig is the fault plugin bad-config of the failure matrix
// (08-profiles-roadmap-adrs.md:451 and test/failure-matrix/matrix.yaml). It is in no
// shipped distribution.
package badconfig

import (
	"fmt"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/bad-config"

// New returns a plugin that accepts the config {}, {ok: true} and a field note whose
// value is not the string "refused", and refuses every other: its Decode fails with
// "invalid config", which is "Config fails schema or Validate". A row that starts with
// a bad config is FAILED; an update to a bad config is rejected and the row keeps its
// instance (matrix, "Live recomposition"). A note that holds a reference decodes as it
// is, unresolved: only the value it resolves to can be refused, which a check of the
// raw config does not see. It provides toy.fault.
func New() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Provides: []kernel.AnyKey{toy.FaultKey},
		Decode: func(raw any) (any, error) {
			m, _ := raw.(map[string]any)
			if raw != nil && m == nil {
				return nil, fmt.Errorf("invalid config: config is %T, want a map", raw)
			}
			for k, v := range m {
				if k == "note" && v != "refused" || k == "ok" && v == true {
					continue
				}
				return nil, fmt.Errorf("invalid config: field %q is not accepted; the values are {ok: true} and a note other than \"refused\"", k)
			}
			return nil, nil
		},
		Apply: func(c *kernel.Context, _ any) error { return toy.FaultKey.Provide(c, toy.Fault{Owner: ID}) },
	})
}
