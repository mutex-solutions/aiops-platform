// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory composition service: a test commits generations
// with Commit and a consumer reads them through composition.Key
// (07-c4-plugin-sdk.md:1410). It imports the standard library, sdk/kernel and its
// definition only.
package fake

import (
	"slices"
	"sync"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Reader is an in-memory composition.Reader.
type Reader struct {
	mu   sync.Mutex
	gens map[composition.Scope]composition.Generation
}

var _ composition.Reader = (*Reader)(nil)

// New returns an empty composition and the plugin that provides it under
// composition.Key. Each call returns a new plugin value. The plugin provides the
// methods of composition.Reader only: Commit is the test's (no side door,
// 07-c4-plugin-sdk.md §4.3).
func New() (*Reader, kernel.Plugin) {
	f := &Reader{gens: map[composition.Scope]composition.Generation{}}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/composition-fake",
		Provides: []kernel.AnyKey{composition.Key},
		Apply: func(c *kernel.Context, _ any) error {
			return composition.Key.Provide(c, struct{ composition.Reader }{f})
		},
	})
}

// Commit makes a copy of g the committed generation of its scope.
func (f *Reader) Commit(g composition.Generation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g.Rows = clone(g.Rows)
	f.gens[g.Scope] = g
}

// Committed returns a copy of the committed generation of scope.
func (f *Reader) Committed(scope composition.Scope) (composition.Generation, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.gens[scope]
	g.Rows = clone(g.Rows)
	return g, ok
}

// Declaring returns the rows of the committed generation of scope that declare
// hook.
func (f *Reader) Declaring(scope composition.Scope, hook string) []composition.Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []composition.Row
	for _, r := range f.gens[scope].Rows {
		if slices.Contains(r.Hooks, hook) {
			out = append(out, r)
		}
	}
	return clone(out)
}

// clone copies rows and the lists each row holds.
func clone(rows []composition.Row) []composition.Row {
	out := slices.Clone(rows)
	for i := range out {
		out[i].Hooks, out[i].Registrations = slices.Clone(out[i].Hooks), slices.Clone(out[i].Registrations)
	}
	return out
}
