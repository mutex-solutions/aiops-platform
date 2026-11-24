// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"context"
	"errors"

	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

var errNotBuilt = errors.New("the fake does not build it yet")

func (served) Pack(context.Context, operations.PackRequest) (operations.Written, error) {
	return operations.Written{}, errNotBuilt
}

func (served) Sign(context.Context, operations.SignRequest) (operations.Written, error) {
	return operations.Written{}, errNotBuilt
}

func (served) Push(context.Context, operations.PushRequest) (operations.Pushed, error) {
	return operations.Pushed{}, errNotBuilt
}

func (served) Inspect(context.Context, operations.SpecRequest) (operations.Inspected, error) {
	return operations.Inspected{}, errNotBuilt
}

func (served) Verify(context.Context, operations.SpecRequest) (operations.Verified, error) {
	return operations.Verified{}, errNotBuilt
}
