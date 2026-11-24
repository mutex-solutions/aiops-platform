// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

var errNotBuilt = errors.New("not built yet")

// Pack is plugin pack.
func (l Library) Pack(context.Context, def.PackRequest) (def.Written, error) {
	return def.Written{}, errNotBuilt
}

// Sign is plugin sign.
func (l Library) Sign(context.Context, def.SignRequest) (def.Written, error) {
	return def.Written{}, errNotBuilt
}

// Push is plugin push.
func (l Library) Push(context.Context, def.PushRequest) (def.Pushed, error) {
	return def.Pushed{}, errNotBuilt
}

// Inspect is plugin inspect.
func (l Library) Inspect(context.Context, def.SpecRequest) (def.Inspected, error) {
	return def.Inspected{}, errNotBuilt
}

// Verify is plugin verify.
func (l Library) Verify(context.Context, def.SpecRequest) (def.Verified, error) {
	return def.Verified{}, errNotBuilt
}
