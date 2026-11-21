// SPDX-License-Identifier: Apache-2.0

// Package testsubmit is the test-only row "test-submit" of ADR-059 decision 9: it
// watches a control file and submits a generation to reload when the file changes, so
// that a test drives a live recomposition of the built binary. It is in no shipped
// distribution.
//
// A generation is what the layer files say when reload composes again: the profile,
// the bundles, the site layer and the --patch files are all read from disk at each
// submission (boot.Compose). A test therefore writes a layer file first, then the
// control file. The content of the control file is an opaque marker, any bytes: each
// change of it is one submission, and the first read counts, so a file that exists at
// boot is submitted once. A test writes it atomically (a temporary file in the
// directory, then a rename).
//
// The control file is <home>/run/submit.json. The outcome of each submission is
// written, again atomically, to <home>/run/submit.result.json:
//
//	{"request": "<sha256 of the control file>", "id": 3, "ok": true, "error": "", "leased": false, "ran": false,
//	 "openAtUnload": false, "disposedOpen": false}
//
// "ok" says that reload accepted the operation, not that the generation passed: the
// verdict is the last operation of reload in the state file of test-state.
//
// A control file that starts with "leased" is a submission from a leased call, as an
// admin call makes one (plugin-system-2026-11.md §9), and on a goroutine that this row
// does not track, as an admin call runs on: a lease of the row's fiber, the submission,
// and the lease is held until reload has begun the operation ("ran"), which a Submit that
// waited for the apply could never see, because the apply of a generation that restarts
// this row waits for this lease. The lease is held on into the unload of the row, when
// the generation restarts it ("openAtUnload"), and a while past its start: only the lease
// holds the drain back, so the row's disposer must not begin while it is open
// ("disposedOpen" says it did). Then the call leaves and writes the outcome.
package testsubmit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
)

// ID is the plugin id; Control and Result are the default file names; Source is the
// source field of the operations it submits.
const (
	ID      = "aiops-test/test-submit"
	Control = "submit.json"
	Result  = "submit.result.json"
	Source  = "test-submit"
)

// Outcome is the content of the result file.
type Outcome struct {
	Request string `json:"request"`
	ID      uint64 `json:"id"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Leased  bool   `json:"leased"` // the submission was made inside a lease of this row
	Ran     bool   `json:"ran"`    // leased: reload had begun the operation while the lease was held
	// OpenAtUnload (leased): the lease was open when the unload of this row began.
	OpenAtUnload bool `json:"openAtUnload"`
	// DisposedOpen (leased): the disposer of this row began while the lease was open.
	DisposedOpen bool `json:"disposedOpen"`
}

// New returns the plugin. Config: interval (duration, default 25ms), dir (default
// <home>/run) and file (the control file name, default submit.json).
func New() kernel.Plugin {
	type cfg struct {
		interval  time.Duration
		dir, file string
	}
	// The marker last submitted is kept per plugin value, which a distribution creates
	// once, so that a restart of this row (an epoch change, a new config) does not
	// submit the same control file again.
	var (
		mu   sync.Mutex
		last [sha256.Size]byte
	)
	return kernel.Define(kernel.Plugin{
		ID:     ID,
		Inject: []kernel.Dep{kernel.Need(clock.Key), kernel.Need(launch.CmdlineKey), kernel.Need(reload.Key)},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{interval: c.Duration("interval", 25*time.Millisecond), dir: c.String("dir", ""), file: c.String("file", Control)}
			if out.interval <= 0 {
				return nil, errors.New("invalid config: interval must be positive")
			}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			dir := cf.dir
			if dir == "" {
				home, ok := launch.CmdlineKey.Of(c).Flag("home")
				if !ok {
					return errors.New("test-submit: no dir in the config and no --home")
				}
				dir = runfile.Dir(home)
			}
			rl, clk := reload.Key.Of(c), clock.Key.Of(c)
			disposing := make(chan struct{})
			c.Defer("test-submit-dispose", func(context.Context) error { close(disposing); return nil })
			c.Go("test-submit-poll", func(ctx context.Context) error {
				for {
					if data, err := os.ReadFile(filepath.Join(dir, cf.file)); err == nil {
						mu.Lock()
						sum := sha256.Sum256(data)
						fresh := sum != last
						last = sum
						mu.Unlock()
						if fresh && bytes.HasPrefix(data, []byte("leased")) {
							// A failed write leaves no outcome, which the test reports.
							go func() { _ = runfile.Write(dir, Result, outcome(ctx, rl, sum, c.Enter, disposing)) }()
						} else if fresh {
							if err := runfile.Write(dir, Result, outcome(ctx, rl, sum, nil, nil)); err != nil {
								return err
							}
						}
					}
					t, err := clk.After(c, cf.interval)
					if err != nil {
						return nil // the row is unloading
					}
					select {
					case <-t.C():
					case <-ctx.Done():
						return nil
					}
				}
			})
			return nil
		},
	})
}

// outcome submits one generation and renders the result file. A leased submission (enter
// is not nil) is made inside the lease that enter takes, and keeps it until the operation
// has begun or the row is unloading, which the apply of the operation itself may be the
// cause of, then into that unload as the package says. ctx is the row's, which ends when
// the unload begins; disposing is closed when the row's disposer begins.
func outcome(ctx context.Context, rl reload.Reload, sum [sha256.Size]byte, enter func() (func(), bool), disposing <-chan struct{}) []byte {
	leased := enter != nil
	o := Outcome{Request: fmt.Sprintf("%x", sum[:]), Leased: leased}
	if leased {
		leave, ok := enter()
		if !ok {
			o.Error = "test-submit: no lease: the row is unloading"
			return render(o)
		}
		defer leave()
	}
	id, err := rl.Submit(ctx, reload.Op{Source: Source})
	if err != nil {
		o.Error = err.Error()
		return render(o)
	}
	o.ID, o.OK = uint64(id), true
	if leased {
		for i := 0; i < 500; i++ { // bounded: five seconds
			if r, ok := rl.Result(id); ok && r.Status != reload.Queued {
				break
			}
			if !def.Wait(10*time.Millisecond, ctx.Done()) {
				break
			}
		}
		r, ok := rl.Result(id)
		o.Ran = ok && r.Status != reload.Queued
		if !def.Wait(5*time.Second, ctx.Done()) { // bounded: a generation may leave the row alone
			o.OpenAtUnload = true
			o.DisposedOpen = !def.Wait(300*time.Millisecond, disposing) // well under the drain deadline
		}
	}
	return render(o)
}

func render(o Outcome) []byte {
	b, _ := json.MarshalIndent(o, "", "  ")
	return append(b, '\n')
}
