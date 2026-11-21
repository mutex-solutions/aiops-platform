// SPDX-License-Identifier: Apache-2.0

package configrefs

import (
	"context"
	"errors"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the dry run, class P: provider-bound to config-refs, the row that resolves
// references (ADR-059 decision 16). A consumer is open.
var Key = kernel.NewKey[DryRun]("configRefsDryRun", 0, kernel.ProviderBound())

// ErrUnresolved matches the error of a reference that does not resolve, the failure
// "a reference does not resolve" of the failure matrix (08-profiles-roadmap-adrs.md
// §1.6), whose diagnostic is "invalid config".
var ErrUnresolved = errors.New("reference does not resolve")

// DryRun checks the references of a config tree.
type DryRun interface {
	// Check reports whether every reference of config resolves now, as the provider
	// would resolve it for an activation. config is a parsed tree as launch.Row.Config
	// holds it, each reference a launch.Ref. When one does not resolve, Check returns an
	// error that matches ErrUnresolved and says which reference failed and why. When
	// each one resolves and decode is not nil, Check calls decode with the resolved
	// tree, as the row's plugin would receive it, and returns its error: the row's own
	// Decode, which the mount of the row runs on the same tree, so the error is the one
	// that mount would report ("Config fails schema or Validate",
	// 08-profiles-roadmap-adrs.md §1.6). The resolved tree never leaves the call. Check
	// itself never returns or names a value it read.
	Check(ctx context.Context, config any, decode func(resolved any) error) error
}
