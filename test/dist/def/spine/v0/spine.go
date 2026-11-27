// SPDX-License-Identifier: Apache-2.0

// Package spine is the test definition of toy.spine, the key of the heavy spine
// provider of the test distribution (test/dist/plugins/toy-spine). It is a fixture of
// the launcher's tests: no shipped distribution links it (ADR-059 decision 9), and its
// definition.yaml declares suite revision 0, no suite (ADR-060 decisions 28 and 55).
package spine

import (
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Spine is what toy.spine serves: the number of workers its provider holds, and Call,
// a call into the provider under a lease of its fiber, as a dispatcher makes one.
type Spine struct {
	Workers int
	call    func(hold time.Duration) error
}

// New returns the value of a spine whose Call is call.
func New(workers int, call func(hold time.Duration) error) Spine {
	return Spine{Workers: workers, call: call}
}

// Call calls the provider and holds its lease for hold. It returns the error of the
// provider: kernel.ErrInactive once the provider has begun to unload.
func (s Spine) Call(hold time.Duration) error {
	if s.call == nil {
		return nil
	}
	return s.call(hold)
}

// Key is toy.spine, class "-" (open).
var Key = kernel.NewKey[Spine]("toy.spine", 0)
