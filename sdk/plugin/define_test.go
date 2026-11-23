// SPDX-License-Identifier: Apache-2.0

package plugin_test

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

type config struct {
	Timeout time.Duration           `default:"5s"`
	URL     plugin.Volatile[string] `aiops:"tenant"`
	Limit   int
}

var (
	key  = kernel.NewKey[string]("test.define", 0)
	hook = kernel.NewEvent[int]("test/define", kernel.OwnedBy(key), kernel.Stages("a", "b"))
)

func await(t *testing.T, f *kernel.Fiber) (kernel.State, error) {
	t.Helper()
	err := f.Await(t.Context())
	s, serr := f.State(t.Context())
	if serr != nil {
		t.Fatal(serr)
	}
	return s, err
}

// Define keeps the declarations of the Spec and hands Apply the decoded *C.
func TestDefineAppliesTheDecodedConfig(t *testing.T) {
	got := make(chan *config, 1)
	spec := plugin.Spec[config]{
		ID:       "test/define",
		Inject:   []kernel.Dep{kernel.Want(kernel.LoggerKey)},
		Provides: []kernel.AnyKey{key},
		Hooks:    []kernel.HookUse{hook.Use("b")},
		Apply: func(c *kernel.Context, cfg *config) error {
			got <- cfg
			return key.Provide(c, cfg.URL.Get())
		},
	}
	p := plugin.Define(spec)
	if p.ID != spec.ID || len(p.Inject) != 1 || len(p.Provides) != 1 || len(p.Hooks) != 1 || p.Hooks[0].Stage() != "b" {
		t.Fatalf("Define lost a declaration: %+v", p)
	}
	r := kerneltest.New(t)
	f := r.Plugin(p, map[string]any{"url": "https://h", "limit": int64(4)})
	if s, err := await(t, f); s != kernel.Active {
		t.Fatalf("state %s, err %v", s, err)
	}
	cfg := <-got
	if cfg.URL.Get() != "https://h" || cfg.Limit != 4 || cfg.Timeout != 5*time.Second {
		t.Fatalf("Apply got %+v", cfg)
	}
}

// An invalid tree and a Validate error each fail the row with "invalid config",
// and Apply does not run.
func TestDefineFailsTheRowOnAnInvalidConfig(t *testing.T) {
	var applied atomic.Bool
	p := plugin.Define(plugin.Spec[config]{
		ID: "test/invalid",
		Validate: func(cfg *config) error {
			if cfg.Limit < 0 {
				return errors.New("limit must not be negative")
			}
			return nil
		},
		Apply: func(*kernel.Context, *config) error { applied.Store(true); return nil },
	})
	r := kerneltest.New(t)
	for want, tree := range map[string]any{
		"invalid config: config.limit: want an integer, got a string": map[string]any{"limit": "x"},
		"invalid config: limit must not be negative":                  map[string]any{"limit": int64(-1)},
	} {
		s, err := await(t, r.Plugin(p, tree))
		if s != kernel.Failed || err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("state %s, err %v, want FAILED with %q", s, err, want)
		}
	}
	if applied.Load() {
		t.Fatal("Apply ran on an invalid config")
	}
}

// Validate sees the decoded value with its defaults, and Decode is pure: each
// call returns a new value.
func TestDefineValidatesTheDecodedValue(t *testing.T) {
	var seen []*config
	p := plugin.Define(plugin.Spec[config]{ID: "test/pure", Validate: func(c *config) error { seen = append(seen, c); return nil }})
	a, err := p.Decode(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.Decode(nil)
	if a == b || len(seen) != 2 || seen[0] != plugin.ConfigOf(a) || seen[0].Timeout != 5*time.Second {
		t.Fatalf("Decode returned %p and %p, Validate saw %v", a, b, seen)
	}
	if p.Apply != nil {
		t.Fatal("a Spec with no Apply gave the plugin an Apply")
	}
	if _, err := plugin.Define(plugin.Spec[struct{}]{ID: "test/none"}).Decode(map[string]any{"a": true}); err == nil {
		t.Fatal("a config type with no field took a field")
	}
}

// secret is defined over Volatile, which keeps none of its methods.
type secret plugin.Volatile[string]

// Define refuses a config type that breaks a rule, at once.
func TestDefineRefusesABadConfigType(t *testing.T) {
	type inner struct{ X int }
	type rec struct{ Next []rec }
	type embedded struct{ inner }
	for _, tc := range []struct {
		name string
		def  func()
		want string
	}{
		{"not a struct", func() { plugin.Define(plugin.Spec[string]{ID: "t/x"}) }, "must be a struct"},
		{"a volatile root", func() { plugin.Define(plugin.Spec[plugin.Volatile[inner]]{ID: "t/x"}) }, "must be a struct"},
		{"tenant field not volatile", func() {
			plugin.Define(plugin.Spec[struct {
				U string `aiops:"tenant"`
			}]{ID: "t/x"})
		}, `aiops:"tenant" marks a tenant-writable field, which must be a plugin.Volatile`},
		{"tenant field inside a struct", func() {
			plugin.Define(plugin.Spec[struct {
				A struct {
					U []string `aiops:"tenant"`
				}
			}]{ID: "t/x"})
		}, "must be a plugin.Volatile"},
		{"unknown aiops tag", func() {
			plugin.Define(plugin.Spec[struct {
				U plugin.Volatile[string] `aiops:"secret"`
			}]{ID: "t/x"})
		}, `the only value is "tenant"`},
		{"embedded field", func() { plugin.Define(plugin.Spec[embedded]{ID: "t/x"}) }, "an embedded field is not config"},
		{"pointer", func() { plugin.Define(plugin.Spec[struct{ P *int }]{ID: "t/x"}) }, "kind ptr is not a config type"},
		{"interface", func() { plugin.Define(plugin.Spec[struct{ A any }]{ID: "t/x"}) }, "kind interface is not a config type"},
		{"array", func() { plugin.Define(plugin.Spec[struct{ A [2]int }]{ID: "t/x"}) }, "kind array is not a config type"},
		{"func", func() { plugin.Define(plugin.Spec[struct{ F func() }]{ID: "t/x"}) }, "kind func is not a config type"},
		{"chan", func() { plugin.Define(plugin.Spec[struct{ C chan int }]{ID: "t/x"}) }, "kind chan is not a config type"},
		{"complex", func() { plugin.Define(plugin.Spec[struct{ C complex128 }]{ID: "t/x"}) }, "kind complex128 is not a config type"},
		{"uintptr", func() { plugin.Define(plugin.Spec[struct{ U uintptr }]{ID: "t/x"}) }, "kind uintptr is not a config type"},
		{"pointer in a list", func() { plugin.Define(plugin.Spec[struct{ L []*int }]{ID: "t/x"}) }, "kind ptr is not a config type"},
		{"map with int keys", func() { plugin.Define(plugin.Spec[struct{ M map[int]string }]{ID: "t/x"}) }, "a map key must be a string"},
		{"recursive type", func() { plugin.Define(plugin.Spec[rec]{ID: "t/x"}) }, "a recursive type is not a config type"},
		{"two fields with one key", func() {
			plugin.Define(plugin.Spec[struct {
				URL string
				Url string
			}]{ID: "t/x"})
		}, `key "url" names two fields`},
		{"json tag with an option", func() {
			plugin.Define(plugin.Spec[struct {
				A string `json:"a,omitempty"`
			}]{ID: "t/x"})
		}, `json:"a,omitempty": give a name and no option`},
		{"key that is the wildcard", func() {
			plugin.Define(plugin.Spec[struct {
				Star plugin.Volatile[string] `json:"*"`
				Mode string
			}]{ID: "t/x"})
		}, `json:"*": a key is not "*" and holds no "."`},
		{"key with a dot", func() {
			plugin.Define(plugin.Spec[struct {
				A struct {
					B plugin.Volatile[string] `json:"x.b"`
				}
			}]{ID: "t/x"})
		}, `json:"x.b": a key is not "*" and holds no "."`},
		{"struct with no config field", func() { plugin.Define(plugin.Spec[struct{ Since time.Time }]{ID: "t/x"}) }, "time.Time has no config field"},
		{"empty struct field", func() { plugin.Define(plugin.Spec[struct{ A struct{} }]{ID: "t/x"}) }, "struct {} has no config field"},
		{"type defined over Volatile", func() { plugin.Define(plugin.Spec[struct{ Token secret }]{ID: "t/x"}) }, "plugin_test.secret is defined over plugin.Volatile"},
		{"list of a type defined over Volatile", func() { plugin.Define(plugin.Spec[struct{ L []secret }]{ID: "t/x"}) }, "is defined over plugin.Volatile"},
		{"json tag with no name", func() {
			plugin.Define(plugin.Spec[struct {
				A string `json:""`
			}]{ID: "t/x"})
		}, "give a name and no option"},
		{"default on a list", func() {
			plugin.Define(plugin.Spec[struct {
				L []string `default:"a"`
			}]{ID: "t/x"})
		}, "only a scalar field takes the tag default"},
		{"default on a struct", func() {
			plugin.Define(plugin.Spec[struct {
				S inner `default:"a"`
			}]{ID: "t/x"})
		}, "only a scalar field takes the tag default"},
		{"default out of range", func() {
			plugin.Define(plugin.Spec[struct {
				N int8 `default:"300"`
			}]{ID: "t/x"})
		}, `default:"300"`},
		{"default not an integer", func() {
			plugin.Define(plugin.Spec[struct {
				N int `default:"1.5"`
			}]{ID: "t/x"})
		}, `default:"1.5"`},
		{"default not a duration", func() {
			plugin.Define(plugin.Spec[struct {
				D time.Duration `default:"5"`
			}]{ID: "t/x"})
		}, "want a duration"},
		{"default bool not a word", func() {
			plugin.Define(plugin.Spec[struct {
				B bool `default:"1"`
			}]{ID: "t/x"})
		}, "a bool default is true or false"},
		{"example not an integer", func() {
			plugin.Define(plugin.Spec[struct {
				N int `example:"x"`
			}]{ID: "t/x"})
		}, `example:"x"`},
		{"example on a list", func() {
			plugin.Define(plugin.Spec[struct {
				L []string `example:"a"`
			}]{ID: "t/x"})
		}, "only a scalar field takes the tag example"},
		{"default float", func() {
			plugin.Define(plugin.Spec[struct {
				F float64 `default:"x"`
			}]{ID: "t/x"})
		}, `default:"x"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := func() (msg string) {
				defer func() { msg = fmt.Sprint(recover()) }()
				tc.def()
				return ""
			}()
			if !strings.HasPrefix(msg, "plugin.Define t/x: ") || !strings.Contains(msg, tc.want) {
				t.Fatalf("panic %q, want one with %q", msg, tc.want)
			}
		})
	}
}

// Volatile.Get returns the decoded value; the zero Volatile holds the zero value.
func TestVolatileGet(t *testing.T) {
	var v plugin.Volatile[[]int]
	if v.Get() != nil {
		t.Fatal("zero Volatile is not empty")
	}
	cfg, err := plugin.Define(plugin.Spec[config]{ID: "t/v"}).Decode(map[string]any{"url": "u"})
	if err != nil || plugin.ConfigOf(cfg).(*config).URL.Get() != "u" {
		t.Fatalf("Decode: %v, %v", cfg, err)
	}
}

// A change of Volatile values alone commits in place, with no restart of the row
// (07-c4-plugin-sdk.md:1375; ADR-048 decision 9). The test takes the loader's path
// for it: a change that loader.VolatileOnly finds volatile only over the paths
// Describe reports is committed with kernel.Fiber.Commit, any other restarts the row.
// Apply is not called again, and the running activation reads the committed value
// (07-c4-plugin-sdk.md:1382; ADR-034 G13).
func TestVolatileChangeCommitsInPlace(t *testing.T) {
	type cfg struct {
		URL  plugin.Volatile[string]
		Mode string
	}
	applied := make(chan *cfg, 2)
	p := plugin.Define(plugin.Spec[cfg]{ID: "test/volatile",
		Apply: func(_ *kernel.Context, c *cfg) error { applied <- c; return nil }})
	before, after := map[string]any{"url": "https://a"}, map[string]any{"url": "https://b"}
	f := kerneltest.New(t).Plugin(p, before)
	if s, err := await(t, f); s != kernel.Active {
		t.Fatalf("state %s, err %v", s, err)
	}
	running := <-applied
	info, err := plugin.Describe[cfg]()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, v := range info.Volatile {
		paths = append(paths, strings.Join(v, "."))
	}
	if _, only := loader.VolatileOnly(before, after, paths); !only {
		t.Fatalf("a change of url alone is not volatile over the paths %q", paths)
	}
	if _, only := loader.VolatileOnly(before, map[string]any{"url": "https://a", "mode": "m"}, paths); only {
		t.Fatalf("a change of mode is volatile over the paths %q", paths)
	}
	if err := f.Commit(t.Context(), after); err != nil {
		t.Fatal(err)
	}
	if s, err := await(t, f); s != kernel.Active {
		t.Fatalf("state after the commit %s, err %v", s, err)
	}
	if len(applied) != 0 {
		t.Fatalf("Apply ran again after a commit in place: %+v", <-applied)
	}
	if got := running.URL.Get(); got != "https://b" {
		t.Fatalf("the running activation reads URL %q, want the committed value", got)
	}
}

func TestAPIVersionIsMajorMinorAtZero(t *testing.T) {
	var minor int
	if n, err := fmt.Sscanf(plugin.APIVersion, "0.%d", &minor); n != 1 || err != nil || fmt.Sprintf("0.%d", minor) != plugin.APIVersion {
		t.Fatalf("APIVersion %q is not 0.y", plugin.APIVersion)
	}
}
