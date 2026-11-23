// SPDX-License-Identifier: Apache-2.0

// Package examples lists the toy plugins of the S2a test release: the dispatcher of
// the two toy registries, the toy instantiable echoer, a stamper and the toy caller
// (ADR-060 decisions 37 and 38; 08-profiles-roadmap-adrs.md:1220). The test release
// is the release table plus these and the control row teststate of test/dist; no
// shipped bundle holds them, and the link guard of decision 28 keeps every package
// under examples/ out of cmd/aiops and cmd/aiopsctl. The definition lint reads this
// list as the plugins of a test distribution.
package examples

import (
	"github.com/mutex-solutions/aiops-platform/examples/caller"
	"github.com/mutex-solutions/aiops-platform/examples/dispatch"
	"github.com/mutex-solutions/aiops-platform/examples/echoer"
	"github.com/mutex-solutions/aiops-platform/examples/stamper"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Plugins returns the toy plugins, each after the plugins it needs.
func Plugins() []kernel.Plugin {
	return []kernel.Plugin{dispatch.Plugin, echoer.Plugin, stamper.Plugin, caller.Plugin}
}
