// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"context"
	"errors"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

var errNotBuilt = errors.New("not built yet")

func (s served) Gate(context.Context, operations.GateRequest) (launch.Verdict, error) {
	return do(s, "Gate", func() (launch.Verdict, error) { return launch.Verdict{}, errNotBuilt })
}

func (s served) Install(context.Context, operations.InstallRequest) (operations.Change, error) {
	return do(s, "Install", func() (operations.Change, error) { return operations.Change{}, errNotBuilt })
}

func (s served) Enable(context.Context, operations.RowRequest) (operations.Change, error) {
	return do(s, "Enable", func() (operations.Change, error) { return operations.Change{}, errNotBuilt })
}

func (s served) Disable(context.Context, operations.RowRequest) (operations.Change, error) {
	return do(s, "Disable", func() (operations.Change, error) { return operations.Change{}, errNotBuilt })
}

func (s served) Remove(context.Context, operations.RemoveRequest) (operations.Change, error) {
	return do(s, "Remove", func() (operations.Change, error) { return operations.Change{}, errNotBuilt })
}
