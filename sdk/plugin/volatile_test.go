// SPDX-License-Identifier: Apache-2.0

package plugin_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// Tests of a Volatile field committed in place (ADR-034 G13): a commit of the row's
// config hands each committed Volatile value to the running activation.

type volatileCfg struct {
	URL   plugin.Volatile[string]
	Level plugin.Volatile[int]
	Mode  string
	List  []int
	Map   map[string]int
}

// running mounts a plugin of C on a test kernel with tree and returns its fiber and
// the *C its activation received.
func running[C any](t *testing.T, r *kernel.Root, tree map[string]any, apply func(*kernel.Context, *C) error) (*kernel.Fiber, *C) {
	t.Helper()
	got := make(chan *C, 1)
	f := r.Plugin(plugin.Define(plugin.Spec[C]{ID: "test/volatile", Apply: func(c *kernel.Context, cfg *C) error {
		got <- cfg
		if apply != nil {
			return apply(c, cfg)
		}
		return nil
	}}), tree)
	if s, err := await(t, f); s != kernel.Active {
		t.Fatalf("state %s, err %v", s, err)
	}
	return f, <-got
}

// A commit reaches the running activation's Volatile fields, through a copy of its
// config as well, and leaves the other fields as Apply received them. A Volatile
// field that the applied config left out takes a committed value too.
func TestGetReturnsTheValueCommittedInPlace(t *testing.T) {
	r := kerneltest.New(t)
	f, cfg := running[volatileCfg](t, r, map[string]any{"url": "https://a", "mode": "m1"}, nil)
	copied := *cfg
	if err := f.Commit(t.Context(), map[string]any{"url": "https://b", "level": int64(3), "mode": "m2"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*volatileCfg{cfg, &copied} {
		if c.URL.Get() != "https://b" || c.Level.Get() != 3 || c.Mode != "m1" {
			t.Fatalf("after the commit the activation reads url %q, level %d, mode %q", c.URL.Get(), c.Level.Get(), c.Mode)
		}
	}
}

// A map of Volatile values: new values of its keys are committed in place and reach
// the running activation, while a key added or removed is not volatile, by the
// loader's rule over the paths Describe reports, so the row restarts and its next
// activation has the keys (ADR-034 G13).
func TestAMapOfVolatileValuesCommitsItsValuesOnly(t *testing.T) {
	type mapCfg struct {
		Labels map[string]plugin.Volatile[string]
	}
	info, err := plugin.Describe[mapCfg]()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, v := range info.Volatile {
		paths = append(paths, strings.Join(v, "."))
	}
	before := map[string]any{"labels": map[string]any{"a": "1"}}
	after := map[string]any{"labels": map[string]any{"a": "2"}}
	for _, labels := range []map[string]any{{"a": "2", "b": "x"}, {}} {
		if _, only := loader.VolatileOnly(before, map[string]any{"labels": labels}, paths); only {
			t.Errorf("labels %v are volatile over the paths %q", labels, paths)
		}
	}
	if _, only := loader.VolatileOnly(before, after, paths); !only {
		t.Fatalf("a change of a label's value is not volatile over the paths %q", paths)
	}
	f, cfg := running[mapCfg](t, kerneltest.New(t), before, nil)
	if err := f.Commit(t.Context(), after); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Labels["a"].Get(); got != "2" || len(cfg.Labels) != 1 {
		t.Fatalf("after the commit the activation reads %q of %d labels", got, len(cfg.Labels))
	}
}

// Get takes no lock and allocates nothing.
func TestGetAllocatesNothing(t *testing.T) {
	_, cfg := running[volatileCfg](t, kerneltest.New(t), map[string]any{"url": "u"}, nil)
	var n int
	if a := testing.AllocsPerRun(1000, func() { n += len(cfg.URL.Get()) + cfg.Level.Get() }); a != 0 {
		t.Fatalf("Get allocates %v times per call", a)
	}
}

func BenchmarkVolatileGet(b *testing.B) {
	r := kerneltest.New(b)
	got := make(chan *volatileCfg, 1)
	f := r.Plugin(plugin.Define(plugin.Spec[volatileCfg]{ID: "test/bench", Apply: func(_ *kernel.Context, cfg *volatileCfg) error {
		got <- cfg
		return nil
	}}), map[string]any{"url": "u"})
	if err := f.Await(b.Context()); err != nil {
		b.Fatal(err)
	}
	cfg := <-got
	var n int
	for b.Loop() {
		n += cfg.Level.Get()
	}
	_ = n
}

// Reads run concurrently with commits; each returns a value of one commit and none
// goes back to an older one.
func TestGetRunsConcurrentlyWithCommits(t *testing.T) {
	const commits = 200
	f, cfg := running[volatileCfg](t, kerneltest.New(t), nil, nil)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for last := 0; ; {
			select {
			case <-stop:
				return
			default:
			}
			n := cfg.Level.Get()
			if n < last {
				t.Errorf("a read returned %d after %d", n, last)
				return
			}
			last = n
		}
	})
	for i := 1; i <= commits; i++ {
		if err := f.Commit(t.Context(), map[string]any{"level": int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if n := cfg.Level.Get(); n != commits {
		t.Fatalf("the activation reads %d after %d commits", n, commits)
	}
}

// An Apply that changes the slices and maps of its config while commits run: the
// hand-off never reads them.
func TestAnApplyMayChangeItsConfigWhileCommitsRun(t *testing.T) {
	const commits = 100
	done := make(chan struct{})
	f, cfg := running(t, kerneltest.New(t), map[string]any{"list": []any{}, "map": map[string]any{}}, func(c *kernel.Context, cfg *volatileCfg) error {
		c.Go("mutate", func(context.Context) error {
			defer close(done)
			for i := range 1000 {
				cfg.List = append(cfg.List, i)
				cfg.Map["k"] = i
			}
			return nil
		})
		return nil
	})
	for i := 1; i <= commits; i++ {
		if err := f.Commit(t.Context(), map[string]any{"level": int64(i), "list": []any{}, "map": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	<-done
	if cfg.Level.Get() != commits || len(cfg.List) != 1000 {
		t.Fatalf("level %d, %d items", cfg.Level.Get(), len(cfg.List))
	}
}

// A Volatile field whose reference internal/config resolves to a new value takes it
// when a sibling literal changes (ADR-034 G13, question Q5 (a)).
func TestAVolatileReferenceTakesItsNewValueWithASiblingLiteral(t *testing.T) {
	r := kerneltest.New(t)
	var mu sync.Mutex
	value := "https://a"
	kernel.InternalConfig.On(r.Context(), kernel.DefaultStage, func(_ context.Context, _ any, next kernel.Next[any]) (any, error) {
		eff, err := next()
		tree, _ := eff.(map[string]any)
		out := map[string]any{}
		for k, v := range tree {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "${") {
				mu.Lock()
				v = value
				mu.Unlock()
			}
			out[k] = v
		}
		return out, err
	})
	f, cfg := running[volatileCfg](t, r, map[string]any{"url": "${ref}", "level": int64(1)}, nil)
	mu.Lock()
	value = "https://b"
	mu.Unlock()
	if err := f.Commit(t.Context(), map[string]any{"url": "${ref}", "level": int64(2)}); err != nil {
		t.Fatal(err)
	}
	if cfg.URL.Get() != "https://b" || cfg.Level.Get() != 2 {
		t.Fatalf("after the commit the activation reads url %q, level %d", cfg.URL.Get(), cfg.Level.Get())
	}
}
