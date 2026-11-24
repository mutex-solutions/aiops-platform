// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"context"
	"errors"

	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

func (s served) New(context.Context, operations.NewRequest) (operations.Written, error) {
	return operations.Written{}, errors.New("not built yet")
}

func (s served) Check(context.Context, operations.CheckRequest) (operations.CheckReport, error) {
	return operations.CheckReport{}, errors.New("not built yet")
}
