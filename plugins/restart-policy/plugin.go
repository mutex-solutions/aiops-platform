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
// generation and until it is final (reload/operation), a give-up waits, and so does a
// retry of a row that the generation changes or restarts: a failure of a row that is
// not yet judged is rolled back, not given up on (08-profiles-roadmap-adrs.md:473), and
// the verdict sees it (ADR-059).
//
// The listener is sealed: the distribution lists this row for internal/status in
// Config.Listeners, and for the registry key, which is consumer-bound, in
// Config.Consumers (ADR-034, "S1 additions"). appExit is consumer-bound as well
// (03-c3-kernel.md:63).
package restartpolicy

import (
	"context"
	"errors"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/restart-policy/internal/retry"
	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// Config is the config of the row; the docs name "N failures in a window" and "a
// backoff" and give no number (03-c3-kernel.md:66). It is decoded by the rules of
// sdk/plugin: the durations are strings in the form of time.ParseDuration.
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

// New returns the restart-policy row. Its activations share one retry.Memory, so a
// restart of the row keeps the hold of the reload operation in flight (ADR-059 decision
// 19). Each activation starts its records afresh: a fiber given up on, or waiting for a
// retry, before a restart of the row is counted again from its first failure (ADR-059
// decision 22, an accepted limit until durable records). A binary makes one value for
// its one kernel, which its module table holds and its binding maps list.
func New() kernel.Plugin {
	m := retry.NewMemory()
	return plugin.Define(plugin.Spec[Config]{
		ID: "aiops/restart-policy",
		Inject: []kernel.Dep{kernel.Need(clock.Key), kernel.Need(launch.CatalogKey), kernel.Need(launch.AppExitKey),
			kernel.Need(kernel.RegistryKey), kernel.Need(composition.Key), kernel.Need(kernel.LoggerKey)},
		Hooks:    []kernel.HookUse{kernel.InternalStatus.Use(kernel.DefaultStage), reload.Operation.Use(kernel.DefaultStage)},
		Validate: validate,
		Apply:    func(c *kernel.Context, cfg *Config) error { return apply(c, *cfg, m) },
	})
}

func apply(c *kernel.Context, conf Config, m *retry.Memory) error {
	r := retry.New(retry.Policy{MaxFailures: conf.MaxFailures, Window: conf.Window, BackoffMin: conf.BackoffMin,
		BackoffMax: conf.BackoffMax, Factor: conf.Factor, Stable: conf.Stable, Infrastructure: conf.Infrastructure},
		retry.Deps{Context: c, Clock: clock.Key.Of(c), Fibers: kernel.RegistryKey.Of(c), Catalog: launch.CatalogKey.Of(c),
			Exit: launch.AppExitKey.Of(c), Rows: composition.Key.Of(c), Log: kernel.LoggerKey.Of(c).For(c)}, m)
	kernel.InternalStatus.On(c, kernel.DefaultStage, func(ctx context.Context, e kernel.LedgerEvent) (any, error) {
		r.Status(ctx, e)
		return nil, nil
	})
	// A give-up, and a retry of a row of the generation, wait while reload applies a
	// generation and judges it.
	reload.Operation.On(c, kernel.DefaultStage, func(ctx context.Context, res reload.Result) (any, error) {
		r.Operation(ctx, res)
		return nil, nil
	})
	// Every row activates with no order, so a fiber may have failed before this
	// one listened, and its event reached no one; the records and the retries of an
	// earlier activation went with it. Catch up as for the notice that events were
	// lost: read the tree and count the FAILED fibers in it.
	r.Status(c.Std(), kernel.LedgerEvent{Kind: "overflow"})
	return nil
}

// validate holds the numbers to what the rule needs. Its errors name the field and
// never the value.
func validate(conf *Config) error {
	switch {
	case conf.MaxFailures < 1:
		return errors.New("restart-policy: maxFailures must be at least 1")
	case conf.Window <= 0:
		return errors.New("restart-policy: window must be positive")
	case conf.BackoffMin <= 0:
		return errors.New("restart-policy: backoffMin must be positive")
	case conf.BackoffMax < conf.BackoffMin:
		return errors.New("restart-policy: backoffMax must not be below backoffMin")
	case conf.Factor < 1:
		return errors.New("restart-policy: factor must be at least 1")
	case conf.Stable < 0:
		return errors.New("restart-policy: stable must not be negative")
	case len(conf.Infrastructure) == 0:
		return errors.New("restart-policy: infrastructure must name at least one key")
	}
	return nil
}
