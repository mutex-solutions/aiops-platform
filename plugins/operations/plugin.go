// SPDX-License-Identifier: Apache-2.0

// Package operations is the operations row, aiops/operations: it provides the
// operations key with the operations library of internal/operations (ADR-061
// decisions 14 to 19; ADR-060 decision 41). The library writes a home under its one
// write lock, signs and verifies with the signing library of ADR-020, writes the
// records under verified/ that boot reads, and reads the environment only through the
// caller of each request. It reaches the launcher's toolbox through profileContext, the
// one consumer of it among the command paths, as reload reaches compose, admit and plan
// (ADR-061 decisions 20 and 33), and no other row.
package operations

import (
	"context"
	"errors"
	"time"

	ops "github.com/mutex-solutions/aiops-platform/plugins/operations/internal/operations"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// Config is the config of the row.
type Config struct {
	// LockWait is how long a second writer waits for the write lock of a home before
	// it fails naming the holder: two minutes, the wait of the reference design
	// (ADR-061 decision 19).
	LockWait time.Duration `default:"2m"`
}

// Plugin is the operations row. Each binary has one value of it: the module table
// holds it and the release manifest lists its consumers.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:       "aiops/operations",
	Inject:   []kernel.Dep{kernel.Need(launch.ProfileContextKey)},
	Provides: []kernel.AnyKey{def.Key},
	Validate: func(c *Config) error {
		if c.LockWait < 0 {
			return errors.New("lockWait is a duration of zero or more")
		}
		return nil
	},
	Apply: func(c *kernel.Context, cfg *Config) error {
		lib := ops.Library{LockWait: cfg.LockWait, Box: launch.ProfileContextKey.Of(c).Toolbox()}
		return def.Key.Provide(c, served{lib: lib, act: c})
	},
})

// served is what an activation provides: the library, each call refused with
// kernel.ErrInactive once the activation is released (07-c4-plugin-sdk.md:520).
type served struct {
	lib ops.Library
	act *kernel.Context
}

func call[R, Q any](s served, ctx context.Context, r Q, m func(ops.Library, context.Context, Q) (R, error)) (R, error) {
	leave, ok := s.act.Enter()
	if !ok {
		var zero R
		return zero, kernel.ErrInactive
	}
	defer leave()
	return m(s.lib, ctx, r)
}

func (s served) Home(ctx context.Context, r def.HomeRequest) (def.Home, error) {
	return call(s, ctx, r, ops.Library.Home)
}

func (s served) Render(ctx context.Context, r def.RenderRequest) (launch.Rendered, error) {
	return call(s, ctx, r, ops.Library.Render)
}

func (s served) VerifyProfile(ctx context.Context, r def.RenderRequest) (launch.Rendered, error) {
	return call(s, ctx, r, ops.Library.VerifyProfile)
}

func (s served) InitProfile(ctx context.Context, r def.ProfileRequest) (def.Change, error) {
	return call(s, ctx, r, ops.Library.InitProfile)
}

func (s served) SanitizeProfile(ctx context.Context, r def.ProfileRequest) (def.Change, error) {
	return call(s, ctx, r, ops.Library.SanitizeProfile)
}

func (s served) AddTrust(ctx context.Context, r def.TrustRequest) (def.Change, error) {
	return call(s, ctx, r, ops.Library.AddTrust)
}

func (s served) GenerateKey(ctx context.Context, r def.KeyRequest) (def.Written, error) {
	return call(s, ctx, r, ops.Library.GenerateKey)
}

func (s served) SignDist(ctx context.Context, r def.SignDistRequest) (def.Written, error) {
	return call(s, ctx, r, ops.Library.SignDist)
}

func (s served) UseDist(ctx context.Context, r def.UseRequest) (def.Change, error) {
	return call(s, ctx, r, ops.Library.UseDist)
}

func (s served) Pack(ctx context.Context, r def.PackRequest) (def.Written, error) {
	return call(s, ctx, r, ops.Library.Pack)
}

func (s served) Sign(ctx context.Context, r def.SignRequest) (def.Written, error) {
	return call(s, ctx, r, ops.Library.Sign)
}

func (s served) Push(ctx context.Context, r def.PushRequest) (def.Pushed, error) {
	return call(s, ctx, r, ops.Library.Push)
}

func (s served) Inspect(ctx context.Context, r def.SpecRequest) (def.Inspected, error) {
	return call(s, ctx, r, ops.Library.Inspect)
}

func (s served) Verify(ctx context.Context, r def.SpecRequest) (def.Verified, error) {
	return call(s, ctx, r, ops.Library.Verify)
}
