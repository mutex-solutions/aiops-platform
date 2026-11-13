// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

// Bind derives lifecycle and metadata from the same composed activation, rather
// than accepting a caller's claim that unrelated metadata belongs to a runtime.
// Uses are still trusted declaration input in this prototype; final Spec.Hooks
// admission and hook access classes remain separate mandatory integration work.
func Bind(owner *composition.Context, uses ...Use) (Binding, error) {
	if owner == nil || owner.Metadata() == nil {
		return Binding{}, ErrBinding
	}
	return Binding{Metadata: owner.Metadata(), Runtime: owner, Uses: append([]Use(nil), uses...),
		Owns: owner.Owns,
		Defer: func(label string, dispose func(context.Context) error) error {
			return owner.Defer(label, concurrency.Disposer(dispose))
		}}, nil
}
