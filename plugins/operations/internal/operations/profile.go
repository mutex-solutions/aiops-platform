// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

var errNotBuilt = errors.New("not built")

// Render is profile render.
func (l Library) Render(context.Context, def.RenderRequest) (launch.Rendered, error) {
	return launch.Rendered{}, errNotBuilt
}

// VerifyProfile is profile verify.
func (l Library) VerifyProfile(context.Context, def.RenderRequest) (launch.Rendered, error) {
	return launch.Rendered{}, errNotBuilt
}

// InitProfile is profile init.
func (l Library) InitProfile(context.Context, def.ProfileRequest) (def.Change, error) {
	return def.Change{}, errNotBuilt
}

// SanitizeProfile is profile sanitize.
func (l Library) SanitizeProfile(context.Context, def.ProfileRequest) (def.Change, error) {
	return def.Change{}, errNotBuilt
}
