// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"

	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the suite that the real loader passes.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (loader.Loader, *kernel.Root) {
		f, p := fake.New()
		f.Role, f.Volatile = s.Role, s.Volatile
		root := kerneltest.New(t)
		kerneltest.Mount(root, p)
		return f, root
	})
}
