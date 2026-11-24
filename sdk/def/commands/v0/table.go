// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Table is the registry of one activation of its provider, with the rules of decisions
// 8 to 10. Not built yet: every method refuses.
type Table struct{}

var _ Registry = (*Table)(nil)

var errStub = errors.New("commands: not built yet")

// NewTable returns an empty registry of the activation act.
func NewTable(*kernel.Context, Environment) *Table { return &Table{} }

// Register refuses.
func (*Table) Register(*kernel.Context, Spec, Command) error { return errStub }

// Dispatch refuses.
func (*Table) Dispatch(context.Context, []string, Streams) (int, error) { return 1, errStub }

// Inventory lists nothing.
func (*Table) Inventory() []string { return nil }
