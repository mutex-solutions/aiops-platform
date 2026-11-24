// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory engine of the operations definition.
package fake

import (
	"context"
	"errors"
	"time"

	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Config is what the fake takes.
type Config struct {
	LockWait    time.Duration
	Held        string
	Interrupted bool
	Running     bool
}

// Fake is the in-memory engine.
type Fake struct{}

// New returns the fake and the plugin that provides it.
func New(cfg Config) (*Fake, kernel.Plugin) {
	f := &Fake{}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/operations-fake",
		Provides: []kernel.AnyKey{operations.Key},
		Apply:    func(c *kernel.Context, _ any) error { return operations.Key.Provide(c, f) },
	})
}

var errStub = errors.New("not implemented")

// Inventory lists nothing: the definition takes no registration.
func (f *Fake) Inventory() []string { return nil }

func (f *Fake) Home(context.Context, operations.HomeRequest) (operations.Home, error) {
	return operations.Home{}, errStub
}

func (f *Fake) AddTrust(context.Context, operations.TrustRequest) (operations.Change, error) {
	return operations.Change{}, errStub
}

func (f *Fake) GenerateKey(context.Context, operations.KeyRequest) (operations.Written, error) {
	return operations.Written{}, errStub
}

func (f *Fake) SignDist(context.Context, operations.SignDistRequest) (operations.Written, error) {
	return operations.Written{}, errStub
}

func (f *Fake) UseDist(context.Context, operations.UseRequest) (operations.Change, error) {
	return operations.Change{}, errStub
}
