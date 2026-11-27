// SPDX-License-Identifier: Apache-2.0

// Package fake provides toy.store, toy.policy and toy.fault, for the generated cycle
// test of a plugin that injects one of them (ADR-060 decision 12).
package fake

import (
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// Toy is the fake provider of the keys of the toy definition.
type Toy struct{}

// New returns the fake and the plugin that provides the three keys. Each call returns
// a new plugin value.
func New() (*Toy, kernel.Plugin) {
	const id = "aiops-test/toy-fake"
	return &Toy{}, kernel.Define(kernel.Plugin{
		ID:       id,
		Provides: []kernel.AnyKey{toy.StoreKey, toy.PolicyKey, toy.FaultKey},
		Apply: func(c *kernel.Context, _ any) error {
			if err := toy.StoreKey.Provide(c, toy.Store{Owner: id}); err != nil {
				return err
			}
			if err := toy.PolicyKey.Provide(c, toy.Policy{Owner: id}); err != nil {
				return err
			}
			return toy.FaultKey.Provide(c, toy.Fault{Owner: id})
		},
	})
}

// Inventory is empty: a caller of these keys only reads, so it leaves nothing behind.
func (*Toy) Inventory() []string { return nil }
