// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// New is plugin new.
func (l Library) New(context.Context, def.NewRequest) (def.Written, error) {
	return def.Written{}, errors.New("not built yet")
}

// Check is plugin check.
func (l Library) Check(context.Context, def.CheckRequest) (def.CheckReport, error) {
	return def.CheckReport{}, errors.New("not built yet")
}
