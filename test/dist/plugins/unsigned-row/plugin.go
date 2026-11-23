// SPDX-License-Identifier: Apache-2.0

// Package unsignedrow is the fault plugin unsigned-row of the failure matrix
// (test/failure-matrix/matrix.yaml; ADR-060 decision 31). The code is harmless: the
// harness installs the plugin as an in-process artifact with no verified signer
// recorded beside its digest and no allowUnsigned entry, so gate check 5 denies its
// row. It is in no shipped distribution.
package unsignedrow

import (
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// ID is the plugin id.
const ID = "aiops-test/unsigned-row"

// New returns a plugin that needs toy.store and does nothing.
func New() kernel.Plugin {
	return kernel.Define(kernel.Plugin{
		ID:     ID,
		Inject: []kernel.Dep{kernel.Need(toy.StoreKey)},
		Apply:  func(*kernel.Context, any) error { return nil },
	})
}
