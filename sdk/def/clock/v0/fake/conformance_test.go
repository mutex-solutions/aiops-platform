// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the suite that the clock row passes.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (*kernel.Root, *kernel.Fiber) {
		_, p := fake.New(t0)
		root := kerneltest.New(t)
		f := root.Plugin(p, nil)
		deftest.Active(t, f)
		return root, f
	})
}
