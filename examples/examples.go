// SPDX-License-Identifier: Apache-2.0

// Package examples lists the toy plugins of the S2a test release.
package examples

import (
	"github.com/mutex-solutions/aiops-platform/examples/caller"
	"github.com/mutex-solutions/aiops-platform/examples/dispatch"
	"github.com/mutex-solutions/aiops-platform/examples/echoer"
	"github.com/mutex-solutions/aiops-platform/examples/stamper"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Plugins returns the toy plugins.
func Plugins() []kernel.Plugin {
	return []kernel.Plugin{dispatch.Plugin, echoer.Plugin, stamper.Plugin, caller.Plugin}
}
