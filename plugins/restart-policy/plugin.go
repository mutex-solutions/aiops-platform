// SPDX-License-Identifier: Apache-2.0

// Package restartpolicy is the restart-policy row: it listens on the sealed event
// internal/status and retries a FAILED fiber in place, through the registry's fiber
// lookup and Fiber.Update, after a backoff. It gives up after more than N failures in
// a window, and then leaves the fiber FAILED, logs "fiber.gave-up" and, for an
// infrastructure provider, asks the launcher to replace the process
// (03-c3-kernel.md:66, :88, :372; 07-c4-plugin-sdk.md:345, :360;
// 08-profiles-roadmap-adrs.md:485).
//
// Every number comes from the row's config. Time comes from the clock key, so a test
// moves it by hand. The rule itself is in internal/retry. While reload applies a
// generation and until it is final (reload/operation), a give-up waits: a failure of
// a row that is not yet judged is rolled back, not given up on
// (08-profiles-roadmap-adrs.md:473).
//
// The listener is sealed: the distribution lists this row for internal/status in
// Config.Listeners, and for the registry key, which is consumer-bound, in
// Config.Consumers (ADR-034, "S1 additions"). appExit is consumer-bound as well
// (03-c3-kernel.md:63).
package restartpolicy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/restart-policy/internal/retry"
	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Config is the config of the row; the docs name "N failures in a window" and "a
// backoff" and give no number (03-c3-kernel.md:66). In a parsed tree the durations are
// strings in the form of time.ParseDuration.
type Config struct {
	MaxFailures int           // a fiber is given up on at the failure after this many in Window; at least 1
	Window      time.Duration // failures older than this no longer count; positive
	BackoffMin  time.Duration // the delay before the first retry; positive
	BackoffMax  time.Duration // the delay never grows past this; at least BackoffMin
	Factor      int           // each further failure in the window multiplies the delay; at least 1
	Stable      time.Duration // a fiber ACTIVE this long has its failures forgotten; 0 never: only Window counts
	// Infrastructure lists the key names whose providers are infrastructure providers
	// (03-c3-kernel.md:371 names sql, jobs, events and http); giving up on one asks the
	// launcher to replace the process. It must not be empty: an empty list would turn
	// that documented exit off without a word.
	Infrastructure []string
}

// Plugin is the restart-policy row.
var Plugin = kernel.Define(kernel.Plugin{
	ID: "aiops/restart-policy",
	Inject: []kernel.Dep{kernel.Need(clock.Key), kernel.Need(launch.CatalogKey), kernel.Need(launch.AppExitKey),
		kernel.Need(kernel.RegistryKey), kernel.Need(composition.Key), kernel.Need(kernel.LoggerKey)},
	Hooks:  []kernel.HookUse{kernel.InternalStatus.Use(kernel.DefaultStage), reload.Operation.Use(kernel.DefaultStage)},
	Decode: decode,
	Apply: func(c *kernel.Context, cfg any) error {
		conf := cfg.(Config)
		r := retry.New(retry.Policy{MaxFailures: conf.MaxFailures, Window: conf.Window, BackoffMin: conf.BackoffMin,
			BackoffMax: conf.BackoffMax, Factor: conf.Factor, Stable: conf.Stable, Infrastructure: conf.Infrastructure},
			retry.Deps{Context: c, Clock: clock.Key.Of(c), Fibers: kernel.RegistryKey.Of(c), Catalog: launch.CatalogKey.Of(c),
				Exit: launch.AppExitKey.Of(c), Rows: composition.Key.Of(c), Log: kernel.LoggerKey.Of(c).For(c)})
		kernel.InternalStatus.On(c, kernel.DefaultStage, func(ctx context.Context, e kernel.LedgerEvent) (any, error) {
			r.Status(ctx, e)
			return nil, nil
		})
		// A give-up waits while reload applies a generation and judges it.
		reload.Operation.On(c, kernel.DefaultStage, func(ctx context.Context, res reload.Result) (any, error) {
			r.Operation(ctx, res.Status.Done())
			return nil, nil
		})
		// Every row activates with no order, so a fiber may have failed before this
		// one listened, and its event reached no one. Catch up as for the notice that
		// events were lost: read the tree and count the FAILED fibers in it.
		r.Status(c.Std(), kernel.LedgerEvent{Kind: "overflow"})
		return nil
	},
})

func decode(raw any) (any, error) {
	var conf Config
	switch v := raw.(type) {
	case Config:
		conf = v
	case map[string]any:
		for k, e := range v {
			var err error
			switch k {
			case "maxFailures":
				conf.MaxFailures, err = integer(e)
			case "factor":
				conf.Factor, err = integer(e)
			case "window":
				conf.Window, err = duration(e)
			case "backoffMin":
				conf.BackoffMin, err = duration(e)
			case "backoffMax":
				conf.BackoffMax, err = duration(e)
			case "stable":
				conf.Stable, err = duration(e)
			case "infrastructure":
				conf.Infrastructure, err = names(e)
			default:
				err = errors.New("unknown field")
			}
			if err != nil {
				return nil, fmt.Errorf("restart-policy: %s: %w", k, err)
			}
		}
	default:
		return nil, fmt.Errorf("restart-policy: config is %T, want Config or a map", raw)
	}
	switch {
	case conf.MaxFailures < 1:
		return nil, errors.New("restart-policy: maxFailures must be at least 1")
	case conf.Window <= 0:
		return nil, errors.New("restart-policy: window must be positive")
	case conf.BackoffMin <= 0:
		return nil, errors.New("restart-policy: backoffMin must be positive")
	case conf.BackoffMax < conf.BackoffMin:
		return nil, errors.New("restart-policy: backoffMax must not be below backoffMin")
	case conf.Factor < 1:
		return nil, errors.New("restart-policy: factor must be at least 1")
	case conf.Stable < 0:
		return nil, errors.New("restart-policy: stable must not be negative")
	case len(conf.Infrastructure) == 0:
		return nil, errors.New("restart-policy: infrastructure must name at least one key")
	}
	conf.Infrastructure = slices.Clone(conf.Infrastructure)
	return conf, nil
}

// integer reads a whole number from a parsed config value. A refusal names the type
// and never the value, which may be the text a reference resolved to (a secret).
func integer(v any) (int, error) {
	switch n := v.(type) {
	case int64:
		return int(n), nil
	case int:
		return n, nil
	case float64:
		if n == float64(int(n)) {
			return int(n), nil
		}
	}
	return 0, fmt.Errorf("want a whole number, got %T", v)
}

// duration reads a duration string. As for integer, a refusal does not quote the value,
// so the error of time.ParseDuration, which does, is not wrapped.
func duration(v any) (time.Duration, error) {
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("want a duration string such as \"30s\", got %T", v)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New("not a duration such as \"30s\"")
	}
	return d, nil
}

func names(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("want a list of key names, got %T", v)
	}
	out := make([]string, len(list))
	for i, e := range list {
		if out[i], ok = e.(string); !ok {
			return nil, fmt.Errorf("item %d: want a key name, got %T", i, e)
		}
	}
	return out, nil
}
