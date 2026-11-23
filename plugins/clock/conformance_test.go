// SPDX-License-Identifier: Apache-2.0

package clock_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/plugins/clock"
	"github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The clock row passes the clock definition's suite, as its fake does (T2,
// 07-c4-plugin-sdk.md §8.2).
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (*kernel.Root, *kernel.Fiber) {
		root := kerneltest.New(t)
		f := root.Plugin(clock.Plugin, nil)
		if err := f.Await(t.Context()); err != nil {
			t.Fatal(err)
		}
		return root, f
	})
}
