// SPDX-License-Identifier: Apache-2.0

package composition

import (
	"context"
	"errors"
	"fmt"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// MountRequest is trusted construction data, using the same registered-entry
// admission as Start. It does not grant a plugin the RootAuthority capability.
type MountRequest struct {
	Parent *Context
	Entry  cm.Plugin
	Spec   Spec
}

// BatchStart compiles every request before committing one actual native Batch.
// No temporary runtime, provisional fiber IDs, hidden mounts or caller callbacks
// participate in staging. Returned fibers use only IDs accepted by the backend.
// This prototype does not batch configuration callbacks or expose a final SDK API.
func (r *RootAuthority) BatchStart(ctx context.Context, requests []MountRequest) ([]*Fiber, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.backend == nil {
		return nil, cm.ErrContext
	}
	batcher, ok := r.backend.(cc.Batcher)
	if !ok {
		return nil, errors.New("native Batch unavailable")
	}
	operations := make([]cc.BatchOperation, len(requests))
	templates := make([]*Context, len(requests))
	for i, request := range requests {
		if request.Parent == nil || request.Parent.authority != r {
			return nil, cm.ErrContext
		}
		metadata, err := r.metadata.WithEntry(request.Parent.metadata, request.Entry, request.Spec.Metadata)
		if err != nil {
			return nil, err
		}
		template, native, err := r.compile(request.Parent, metadata, request.Spec)
		if err != nil {
			return nil, err
		}
		templates[i] = template
		operations[i] = cc.BatchOperation{Kind: cc.BatchMount, Spec: native}
	}
	ids, err := batcher.Batch(ctx, operations)
	if err != nil {
		return nil, err
	}
	if len(ids) != len(operations) {
		return nil, fmt.Errorf("native Batch returned %d IDs for %d accepted mounts", len(ids), len(operations))
	}
	fibers := make([]*Fiber, len(ids))
	for i, id := range ids {
		if id == 0 {
			return nil, fmt.Errorf("native Batch omitted accepted mount ID at position %d", i)
		}
		fibers[i] = &Fiber{ID: id, Context: templates[i], backend: r.backend}
	}
	return fibers, nil
}
