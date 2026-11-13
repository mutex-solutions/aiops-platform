// SPDX-License-Identifier: Apache-2.0

package composition

import (
	"context"
	"errors"
	"maps"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
)

// builtinState is an immutable ownership receipt set, not a service store. Its
// resources retain the actual host values and are removed by native root effects.
// Readers never acquire the construction mutex or invoke an ownership callback.
type builtinState struct{ resources map[string]*builtinResource }

type builtinResource struct {
	key       cm.AnyKey
	value     any
	root      *cc.Context
	hostEntry cm.Plugin
	dispose   cc.Disposer
}

// InstallBuiltin transfers one actual kernel resource to the native root's effect
// ledger. The four version-zero builtins are properties, not ordinary service
// provisions: installation creates neither a user fiber nor a provision row.
//
// RootAuthority is trusted construction authority and must never reach plugins.
// The returned context has the root's private host entry and a normal Provides
// declaration for this key only; it grants no bound-key bypass or unchecked read.
// On failure, ownership of value remains with the caller and dispose is not run.
func InstallBuiltin[T any](ctx context.Context, r *RootAuthority, key cm.Key[T], value T, dispose func(context.Context, T) error) (*Context, error) {
	if r == nil || r.metadata == nil || r.root == nil {
		return nil, cm.ErrContext
	}
	definition := key.Any()
	if definition.Major() != 0 || !builtinName(definition.Name()) {
		return nil, cm.ErrKey
	}
	if definition.ProviderClass() != cm.Open {
		return nil, cm.ErrBinding
	}
	host, err := r.HostContext(ctx, cm.Spec{Provides: []cm.AnyKey{definition}})
	if err != nil {
		return nil, err
	}
	address, err := host.NativeAddress(definition)
	if err != nil {
		return nil, err
	}
	if !host.Enter() {
		return nil, cc.ErrInactive
	}
	defer host.Leave()

	// A shared reservation excludes both another builtin installation and an
	// ordinary provision registration, without holding r.mu across native calls.
	release, err := r.reserveProvision(address)
	if err != nil {
		return nil, err
	}
	defer release()
	inspector, ok := r.backend.(cc.Inspector)
	if !ok {
		return nil, errors.New("native provision inspection unavailable")
	}
	inspection, err := inspector.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	for _, provision := range inspection.Provisions {
		if provision.Key == address {
			return nil, cm.ErrDuplicate
		}
	}

	resource := &builtinResource{key: definition, value: value, root: host.raw, hostEntry: host.Entry()}
	if dispose != nil {
		resource.dispose = func(cleanup context.Context) error { return dispose(cleanup, value) }
	}
	if err := host.raw.Defer("builtin:"+definition.Name(), func(cleanup context.Context) error {
		// Cleanup revokes the receipt before invoking user code. The native root
		// drains leases before effects, so this cannot overtake publication.
		if !r.removeBuiltin(address, resource) {
			return nil
		}
		if resource.dispose != nil {
			return resource.dispose(cleanup)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Native effect registration is the transfer point. The held root lease
	// prevents cleanup until the actual resource receipt has been published.
	r.mu.Lock()
	next := &builtinState{resources: make(map[string]*builtinResource)}
	if previous := r.builtinState.Load(); previous != nil {
		maps.Copy(next.resources, previous.resources)
	}
	next.resources[address] = resource
	r.builtinState.Store(next)
	r.mu.Unlock()
	return host, nil
}

func builtinName(name string) bool {
	switch name {
	case "reflect", "registry", "hooks", "logger":
		return true
	default:
		return false
	}
}

// reserveProvision covers the synchronous control-path registration prefix, not
// callbacks or a resource's lifetime. Provide, ProvideFor and ProvideRoot use the
// same reservation as InstallBuiltin; failed registration leaves no ownership.
func (r *RootAuthority) reserveProvision(address string) (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hasBuiltin(address) {
		return nil, cm.ErrDuplicate
	}
	if _, pending := r.pendingBuiltins[address]; pending {
		return nil, cm.ErrDuplicate
	}
	if r.pendingBuiltins == nil {
		r.pendingBuiltins = make(map[string]struct{})
	}
	r.pendingBuiltins[address] = struct{}{}
	return func() {
		r.mu.Lock()
		delete(r.pendingBuiltins, address)
		r.mu.Unlock()
	}, nil
}

func (r *RootAuthority) hasBuiltin(address string) bool {
	if r == nil {
		return false
	}
	state := r.builtinState.Load()
	return state != nil && state.resources[address] != nil
}

func (r *RootAuthority) removeBuiltin(address string, resource *builtinResource) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous := r.builtinState.Load()
	if previous == nil || previous.resources[address] != resource {
		return false
	}
	next := &builtinState{resources: maps.Clone(previous.resources)}
	delete(next.resources, address)
	r.builtinState.Store(next)
	return true
}

// ownsBuiltin recognizes only a published actual resource, the exact native root
// activation, its private host entry, and its original key/realm address. Metadata
// derivation may preserve ownership; a native child, isolation or entry spoof may
// not. As with native Owns, privileged operations must also hold Enter/Leave.
func (c *Context) ownsBuiltin(key cm.AnyKey) bool {
	if c == nil || c.authority == nil || c.metadata == nil || c.raw == nil || c.metadata.AuthorizeProvide(key) != nil {
		return false
	}
	state := c.authority.builtinState.Load()
	if state == nil {
		return false
	}
	resource := state.resources[c.addresses[key]]
	return resource != nil && resource.key == key && resource.root == c.raw && resource.hostEntry == c.Entry()
}
