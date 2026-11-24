// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"time"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

var errStub = errors.New("not implemented")

// Library is the operations library behind the operations key.
type Library struct {
	LockWait time.Duration // how long a second writer waits for write.lock
}

func (l Library) Home(ctx context.Context, r def.HomeRequest) (def.Home, error) {
	return def.Home{}, errStub
}

func (l Library) AddTrust(ctx context.Context, r def.TrustRequest) (def.Change, error) {
	return def.Change{}, errStub
}

func (l Library) GenerateKey(ctx context.Context, r def.KeyRequest) (def.Written, error) {
	return def.Written{}, errStub
}

func (l Library) SignDist(ctx context.Context, r def.SignDistRequest) (def.Written, error) {
	return def.Written{}, errStub
}

func (l Library) UseDist(ctx context.Context, r def.UseRequest) (def.Change, error) {
	return def.Change{}, errStub
}
