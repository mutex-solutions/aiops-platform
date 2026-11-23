// SPDX-License-Identifier: Apache-2.0

// Package toyspine is the heavy spine provider of the test distribution. It and the
// toy provider and consumer stand for the infrastructure providers and policy services
// of a real profile, so that the launcher, the loader and the kernel are exercised
// with a realistic dependency chain: spine <- provider <- consumer, and spine <-
// consumer.
//
// Every delay is config data, never a default sleep: a row with no config applies and
// disposes at once.
package toyspine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	spine "github.com/mutex-solutions/aiops-platform/test/dist/def/spine/v0"
)

// ID is the plugin id.
const ID = "aiops-test/toy-spine"

// SpineReport is what the spine writes to <home>/run/<report>: the tally of every
// incarnation of the row since the process started, so that a test of the built binary
// reads what happened to calls while the provider restarted in place.
type SpineReport struct {
	Incarnations int64 `json:"incarnations"` // Apply calls that provided the spine
	Calls        int64 `json:"calls"`        // calls that ran to the end inside a lease of an active provider
	Refused      int64 `json:"refused"`      // calls refused because the provider had begun to unload
	HalfDisposed int64 `json:"halfDisposed"` // calls that saw the disposer run, or still ran, inside their lease: always 0
	InFlight     int64 `json:"inFlight"`     // calls open right now
	AtDispose    int64 `json:"atDispose"`    // calls open when a disposer began: always 0, the unload drains leases first
	OpenAtUnload int64 `json:"openAtUnload"` // calls of the outside caller open when their incarnation began to unload
	Active       bool  `json:"active"`
}

// ErrHalfDisposed is what a call returns when it ran while the provider's disposer did.
var ErrHalfDisposed = errors.New("toy-spine: a call ran while the provider was being disposed")

// New returns the heavy spine provider: it provides toy.spine, which the provider and
// the consumers need, holds a number of tracked workers that end only when their
// context does, and has a slow but bounded unload. The value it provides has Call, a
// call under a lease of the provider's fiber that records what it saw.
//
// Config: applyDelay (default 0), workers (whole number, default 2), drainDelay (how
// long each worker takes to end once its context is cancelled, default 0),
// disposeDelay (how long its disposer runs, default 0), report (the name of a file
// in the runtime directory that the row keeps up to date with its SpineReport; default
// none) and outsideHold (default 0, none; see below). A delay that is longer than the
// matching deadline of the kernel is cut at the deadline by the context it waits on:
// the spine is slow, never a leak. The leaks are the fault plugins. The tally is kept
// per plugin value, which a distribution creates once, so it spans the restarts of the
// row.
//
// With outsideHold each incarnation has an outside caller: a goroutine that no fiber
// tracks and no dependant owns, as an admin call or the dispatcher of another row has
// one, so that only its lease keeps the disposer from running under its call
// (07-c4-plugin-sdk.md:373). It takes a lease at once and holds it until the unload
// begins and for outsideHold more, which the drain waits for, then calls again, which
// is refused. A disposer that began within outsideHold is a half-disposed call.
func New() kernel.Plugin {
	type cfg struct {
		applyDelay, drainDelay, disposeDelay, outsideHold time.Duration
		workers                                           int
		report                                            string
	}
	var (
		incarnations, calls, refused, half, inFlight, atDispose, openAtUnload atomic.Int64
		active                                                                atomic.Bool
		reportMu                                                              sync.Mutex
	)
	snapshot := func() SpineReport {
		return SpineReport{Incarnations: incarnations.Load(), Calls: calls.Load(), Refused: refused.Load(),
			HalfDisposed: half.Load(), InFlight: inFlight.Load(), AtDispose: atDispose.Load(),
			OpenAtUnload: openAtUnload.Load(), Active: active.Load()}
	}
	return kernel.Define(kernel.Plugin{
		ID:       ID,
		Inject:   []kernel.Dep{kernel.Need(launch.CmdlineKey)},
		Provides: []kernel.AnyKey{spine.Key},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{applyDelay: c.Duration("applyDelay", 0), drainDelay: c.Duration("drainDelay", 0),
				disposeDelay: c.Duration("disposeDelay", 0), outsideHold: c.Duration("outsideHold", 0),
				workers: c.Int("workers", 2), report: c.String("report", "")}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			if !def.Wait(cf.applyDelay, c.Std().Done()) {
				return c.Std().Err()
			}
			var write func() error
			if cf.report != "" {
				home, ok := launch.CmdlineKey.Of(c).Flag("home")
				if !ok {
					return errors.New("toy-spine: report needs --home")
				}
				dir := runfile.Dir(home)
				var last SpineReport
				write = func() error {
					reportMu.Lock()
					defer reportMu.Unlock()
					r := snapshot()
					if r == last {
						return nil
					}
					last = r
					b, _ := json.Marshal(r)
					return runfile.Write(dir, cf.report, append(b, '\n'))
				}
			}
			var disposed atomic.Bool
			disposing := make(chan struct{}) // closed when the disposer begins
			call := func(hold time.Duration) error {
				leave, ok := c.Enter()
				if !ok {
					refused.Add(1)
					return kernel.ErrInactive
				}
				defer leave()
				inFlight.Add(1)
				defer inFlight.Add(-1)
				if disposed.Load() {
					half.Add(1)
					return ErrHalfDisposed
				}
				def.Wait(hold, nil) // bounded: the lease is held for hold, whatever the unload does
				if disposed.Load() {
					half.Add(1)
					return ErrHalfDisposed
				}
				calls.Add(1)
				return nil
			}
			for i := range cf.workers {
				c.Go("spine-worker-"+string(rune('a'+i%26)), func(ctx context.Context) error {
					<-ctx.Done()
					def.Wait(cf.drainDelay, nil)
					return nil
				})
			}
			c.Defer("spine-dispose", func(ctx context.Context) error {
				atDispose.Add(inFlight.Load())
				disposed.Store(true)
				close(disposing)
				active.Store(false)
				def.Wait(cf.disposeDelay, ctx.Done())
				if write != nil {
					return write()
				}
				return nil
			})
			if write != nil {
				c.Go("spine-report", func(ctx context.Context) error {
					for {
						if err := write(); err != nil {
							return err
						}
						if !def.Wait(10*time.Millisecond, ctx.Done()) {
							return nil
						}
					}
				})
			}
			if cf.outsideHold > 0 {
				std := c.Std()
				go func() { // the outside caller: deliberately untracked, see New
					leave, ok := c.Enter()
					if !ok {
						refused.Add(1)
						return
					}
					inFlight.Add(1)
					<-std.Done()
					openAtUnload.Add(1)
					def.Wait(cf.outsideHold, disposing)
					if disposed.Load() {
						half.Add(1)
					} else {
						calls.Add(1)
					}
					inFlight.Add(-1)
					leave()
					_ = call(0) // the unload began: refused
				}()
			}
			incarnations.Add(1)
			active.Store(true)
			return spine.Key.Provide(c, spine.New(cf.workers, call))
		},
	})
}
