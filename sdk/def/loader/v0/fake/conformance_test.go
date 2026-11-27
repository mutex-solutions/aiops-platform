// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the suite that the real loader passes.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (*kernel.Root, *kernel.Fiber) {
		f, p := fake.New()
		f.Role, f.Volatile = s.Role, s.Volatile
		root := kerneltest.New(t)
		fiber := root.Plugin(p, nil)
		deftest.Active(t, fiber)
		return root, fiber
	})
}
