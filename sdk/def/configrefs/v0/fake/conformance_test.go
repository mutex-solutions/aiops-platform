// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the suite that the config-refs row passes: an ${env:} reference
// resolves when the setup's environment holds its name.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (*kernel.Root, *kernel.Fiber) {
		_, p := fake.NewWith(func(r launch.Ref) bool {
			_, ok := s.Env[r.Name]
			return r.Scheme == "env" && ok
		})
		root := kerneltest.New(t)
		f := root.Plugin(p, nil)
		deftest.Active(t, f)
		return root, f
	})
}
