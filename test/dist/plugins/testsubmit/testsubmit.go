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
//	{"request": "<sha256 of the control file>", "id": 3, "ok": true, "error": ""}
//
// "ok" says that reload accepted the operation, not that the generation passed: the
// verdict is the last operation of reload in the state file of test-state.
package testsubmit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

// New returns the plugin. Config: interval (duration, default 25ms), dir (default
// <home>/run) and file (the control file name, default submit.json).
func New() kernel.Plugin {
	type cfg struct {
		interval  time.Duration
		dir, file string
	}
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
			var last [sha256.Size]byte
			c.Go("test-submit-poll", func(ctx context.Context) error {
				for {
					if data, err := os.ReadFile(filepath.Join(dir, cf.file)); err == nil {
						if sum := sha256.Sum256(data); sum != last {
							last = sum
							if err := runfile.Write(dir, Result, outcome(ctx, rl, sum)); err != nil {
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

// outcome submits one generation and renders the result file.
func outcome(ctx context.Context, rl reload.Reload, sum [sha256.Size]byte) []byte {
	o := Outcome{Request: fmt.Sprintf("%x", sum[:])}
	if id, err := rl.Submit(ctx, reload.Op{Source: Source}); err != nil {
		o.Error = err.Error()
	} else {
		o.ID, o.OK = uint64(id), true
	}
	b, _ := json.MarshalIndent(o, "", "  ")
	return append(b, '\n')
}
