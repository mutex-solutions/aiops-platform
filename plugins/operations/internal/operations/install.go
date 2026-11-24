// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

var errNotBuilt = errors.New("not built yet")

func (l Library) Gate(context.Context, def.GateRequest) (launch.Verdict, error) {
	return launch.Verdict{}, errNotBuilt
}

func (l Library) Install(context.Context, def.InstallRequest) (def.Change, error) {
	return def.Change{}, errNotBuilt
}

func (l Library) Enable(context.Context, def.RowRequest) (def.Change, error) {
	return def.Change{}, errNotBuilt
}

func (l Library) Disable(context.Context, def.RowRequest) (def.Change, error) {
	return def.Change{}, errNotBuilt
}

func (l Library) Remove(context.Context, def.RemoveRequest) (def.Change, error) {
	return def.Change{}, errNotBuilt
}
