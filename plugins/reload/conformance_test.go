// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/plugins/reload"
	"github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The reload row passes the reload definition's suite, as its fake does (T2,
// 07-c4-plugin-sdk.md §8.2). The launcher fake composes the committed composition
// again, so every operation applies an empty plan and passes.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) (*kernel.Root, *kernel.Fiber) {
		w := base(t)
		f := w.root.Plugin(reload.New(), reload.Config{Queue: 8, History: 8})
		active(t, f)
		return w.root, f
	})
}
