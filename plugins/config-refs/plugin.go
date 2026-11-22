// SPDX-License-Identifier: Apache-2.0

// Package configrefs is the config-refs row: a listener of the sealed waterfall
// internal/config that resolves the closed references ${env|file|secret|flag:value}
// of a row's config before the plugin sees it (03-c3-kernel.md:68;
// plugin-system-2026-11.md §9). A reference that does not resolve is an error of
// that fiber's config resolution, so the fiber is FAILED, or on a live recomposition
// the update is refused and the row keeps its instance (08-profiles-roadmap-adrs.md:468).
// The listener resolves what next returns, so every listener inward of it runs and
// sees the references unresolved (07-c4-plugin-sdk.md §3.4).
// The row also provides the dry run of the same resolution (sdk/def/configrefs/v0),
// which the launcher calls to reject such a generation at admission.
//
// The launcher puts a reference in the tree as a launch.Ref
// (sdk/def/launch/v0): it must convert boot.Ref to it when it hands the tree to the
// loader. The listener is sealed: the distribution lists this row for internal/config
// in Config.Listeners (ADR-034, "S1 additions"; 07-c4-plugin-sdk.md:463).
package configrefs

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/mutex-solutions/aiops-platform/plugins/config-refs/internal/refs"
	refsdef "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Config is the config of the row. The field is the plugin's own: the docs name a
// bound and give no number (08-profiles-roadmap-adrs.md:162).
type Config struct {
	// MaxFileBytes is the largest file a ${file:} reference reads. It must be positive.
	MaxFileBytes int64
}

// fileRootFlag names the "one directory named by flag" that ${file:} reads
// (08-profiles-roadmap-adrs.md:162): the server's --refs-dir and no other flag, so a
// file reference never reads the home or the bundles. Unset or empty, no file
// reference resolves.
const fileRootFlag = "refs-dir"

// ReadyKey is configRefsReady: the row provides it once its listener is on
// internal/config. The launcher gives every row whose config holds a reference a
// row-level inject of it, so such a row is applied only while its references can
// resolve, and unloads in the same owner turn as this row (ADR-059, "configRefsReady"). It is
// provider-bound to this row in dist.yaml.
var ReadyKey = kernel.NewKey[struct{}]("configRefsReady", 0, kernel.ProviderBound())

// Plugin is the config-refs row. It also provides the dry run of the configrefs
// definition, provider-bound to it in dist.yaml (ADR-059 decision 16).
var Plugin = kernel.Define(kernel.Plugin{
	ID:       "aiops/config-refs",
	Inject:   []kernel.Dep{kernel.Need(launch.CmdlineKey)},
	Provides: []kernel.AnyKey{ReadyKey, refsdef.Key},
	Hooks:    []kernel.HookUse{kernel.InternalConfig.Use(kernel.DefaultStage)},
	Decode:   decode,
	Apply:    apply,
})

// dryRun resolves a config with the listener's resolver, hands the resolved tree to
// decode, the row's own Decode, and drops it.
type dryRun struct{ r *refs.Resolver }

func (d dryRun) Check(_ context.Context, config any, decode func(any) error) error {
	out, _, err := d.r.Resolve(config)
	if err != nil || decode == nil {
		return err
	}
	return decode(out)
}

func apply(c *kernel.Context, cfg any) error {
	conf := cfg.(Config)
	cl := launch.CmdlineKey.Of(c)
	r := &refs.Resolver{Env: cl.Env, Flag: cl.Flag, RootFlag: fileRootFlag, MaxBytes: conf.MaxFileBytes}
	kernel.InternalConfig.On(c, kernel.DefaultStage, func(_ context.Context, _ any, next kernel.Next[any]) (any, error) {
		// What next returns may be the kernel's retained raw config: Resolve copies a
		// map or a list it changes and never writes to it.
		v, err := next()
		if err == nil {
			v, _, err = r.Resolve(v)
		}
		return v, err
	})
	return errors.Join(refsdef.Key.Provide(c, dryRun{r}), ReadyKey.Provide(c, struct{}{}))
}

func decode(raw any) (any, error) {
	var conf Config
	switch v := raw.(type) {
	case Config:
		conf = v
	case map[string]any:
		for k, e := range v {
			var err error
			switch k {
			case "maxFileBytes":
				conf.MaxFileBytes, err = integer(e)
			default:
				err = errors.New("unknown field")
			}
			if err != nil {
				return nil, fmt.Errorf("config-refs: %s: %w", k, err)
			}
		}
	default:
		return nil, fmt.Errorf("config-refs: config is %T, want Config or a map", raw)
	}
	if conf.MaxFileBytes <= 0 {
		return nil, errors.New("config-refs: maxFileBytes must be positive")
	}
	return conf, nil
}

// integer reads a whole number from a parsed config value.
func integer(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float64:
		if n == math.Trunc(n) && math.Abs(n) < 1<<53 {
			return int64(n), nil
		}
	}
	return 0, fmt.Errorf("want a whole number, got %T", v) // never the value: it may be a resolved reference
}
