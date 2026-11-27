// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The generated cycle test calls New with no argument and reads Inventory
// (ADR-060 decision 12).
var (
	_ func() (*fake.Registry, kernel.Plugin) = fake.New
	_ interface{ Inventory() []string }      = (*fake.Registry)(nil)
)
