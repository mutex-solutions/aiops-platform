// SPDX-License-Identifier: Apache-2.0

package kerneltest

import (
	"sync"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The defaults live here because the kernel holds none (ADR-034 owner decisions 3
// and 4): the launcher supplies every number, and in a test this package is the
// launcher. The deadlines are generous so that a loaded CI machine under -race
// does not trip them; a test that wants a deadline to fire overrides it.
const (
	defaultLoad    = 10 * time.Second
	defaultDrain   = 10 * time.Second
	defaultDispose = 10 * time.Second
	defaultCap     = 256 // Archive, event window, observer queue, owner inbox and terminal limit.
)

// Option overrides a default of New.
type Option func(*kernel.Config)

// WithDeadlines sets the load, drain and dispose deadlines.
func WithDeadlines(load, drain, dispose time.Duration) Option {
	return func(c *kernel.Config) {
		c.LoadDeadline, c.DrainDeadline, c.DisposeDeadline = load, drain, dispose
	}
}

// WithConfig edits the whole kernel.Config, after the defaults and the options
// before it. The binding maps it sets are ignored, as kernel.NewForTest ignores
// them.
func WithConfig(fn func(*kernel.Config)) Option { return fn }

// tbs maps each root New made to the testing.TB it was made with, so that Mount
// can fail the test without a TB argument (the call shape of the worked example,
// 07-c4-plugin-sdk.md:1408). The entry goes when the test ends.
var tbs sync.Map // *kernel.Root -> testing.TB

// New constructs the kernel of a test with both binding maps off
// (07-c4-plugin-sdk.md:564, :247, :1407). It is kernel.NewForTest with test
// defaults: the kernel is closed when the test ends, a leaked fiber fails the test
// and a bad configuration fails it at once.
func New(tb testing.TB, opts ...Option) *kernel.Root {
	tb.Helper()
	cfg := kernel.Config{
		LoadDeadline: defaultLoad, DrainDeadline: defaultDrain, DisposeDeadline: defaultDispose,
		ArchiveLimit: defaultCap, EventWindow: defaultCap, ObserverQueue: defaultCap,
		OwnerInbox: defaultCap, TerminalLimit: defaultCap,
	}
	for _, o := range opts {
		o(&cfg)
	}
	root := kernel.NewForTest(tb, cfg)
	tbs.Store(root, tb)
	tb.Cleanup(func() { tbs.Delete(root) })
	return root
}

// Mount mounts each plugin on root with a nil config, in order, and waits for it
// (07-c4-plugin-sdk.md:1408). A plugin that does not reach ACTIVE, because Apply
// failed or a Need has no provider, fails the test through the TB given to New.
// It panics when root was not made by New or its test has ended.
func Mount(root *kernel.Root, plugins ...kernel.Plugin) {
	v, ok := tbs.Load(root)
	if !ok {
		panic("kerneltest.Mount: root was not made by kerneltest.New, or its test has ended")
	}
	tb := v.(testing.TB)
	tb.Helper()
	ctx := tb.Context()
	for _, p := range plugins {
		f := root.Plugin(p, nil)
		err := f.Await(ctx)
		st, serr := f.State(ctx)
		if err != nil || serr != nil || st != kernel.Active {
			tb.Errorf("kerneltest.Mount: plugin %q is %s, want ACTIVE (await: %v, state: %v)", p.ID, st, err, serr)
		}
	}
}
