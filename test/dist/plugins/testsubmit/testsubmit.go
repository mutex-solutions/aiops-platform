// SPDX-License-Identifier: Apache-2.0

// Package testsubmit is the test-only row "test-submit" of ADR-059 decision 9: it
// watches a control file and submits its content as a generation, so that a test
// drives a live recomposition of the built binary. It is in no shipped distribution.
//
// The control file is <home>/run/submit.json:
//
//	{"patches": ["/path/to/a.yml", "/path/to/b.yml"]}
//
// A test writes it atomically (write a temporary file in the directory and rename
// it). Each change of its content is one submission; the first read counts, so a
// file that exists at boot is submitted once. The outcome of each is written, again
// atomically, to <home>/run/submit.result.json:
//
//	{"request": "<sha256 of the control file>", "id": "<operation id>", "ok": true, "error": ""}
//
// The reload plugin, which is what Submit calls, is built in parallel and is not
// linked yet, so this row reaches it through Link, which the distribution gives it.
// Unlinked is the link of the distribution until then: it needs nothing and every
// submission fails with "reload not linked". THE ONE PLACE TO WIRE reload is the
// Link that test/dist passes to New (see Linked in this file for its shape).
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
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
)

// ID is the plugin id; Control and Result are the default file names.
const (
	ID      = "aiops-test/test-submit"
	Control = "submit.json"
	Result  = "submit.result.json"
)

// ErrNotLinked is what the unlinked Submit returns.
var ErrNotLinked = errors.New("reload not linked")

// SubmitFunc submits the layer 4 patch files as one generation and returns the
// operation id (reload.Submit, 03-c3-kernel.md:84).
type SubmitFunc func(patchFiles []string) (id string, err error)

// Link says how the row reaches reload: the dependencies it must declare and the
// function that makes its SubmitFunc from the context of its fiber.
type Link struct {
	Needs []kernel.Dep
	Bind  func(c *kernel.Context) SubmitFunc
}

// Unlinked is the link while reload is not linked: no dependency, and every
// submission fails with ErrNotLinked.
func Unlinked() Link {
	return Link{Bind: func(*kernel.Context) SubmitFunc {
		return func([]string) (string, error) { return "", ErrNotLinked }
	}}
}

// Linked shows the shape of the link to reload, in a comment because the key does
// not exist yet:
//
//	func Linked() Link {
//		return Link{
//			Needs: []kernel.Dep{kernel.Need(reloaddef.Key)},
//			Bind: func(c *kernel.Context) SubmitFunc {
//				r := reloaddef.Key.Of(c)
//				return func(files []string) (string, error) { return r.Submit(ctx, op(files)) }
//			},
//		}
//	}

// Outcome is the content of the result file.
type Outcome struct {
	Request string `json:"request"`
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
}

// New returns the plugin. Config: interval (duration, default 25ms), dir (default
// <home>/run), file (the control file name, default submit.json).
func New(link Link) kernel.Plugin {
	type cfg struct {
		interval  time.Duration
		dir, file string
	}
	return kernel.Define(kernel.Plugin{
		ID:     ID,
		Inject: append([]kernel.Dep{kernel.Need(clock.Key), kernel.Need(launch.CmdlineKey)}, link.Needs...),
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
			submit, clk := link.Bind(c), clock.Key.Of(c)
			var last [sha256.Size]byte
			c.Go("test-submit-poll", func(ctx context.Context) error {
				for {
					if data, err := os.ReadFile(filepath.Join(dir, cf.file)); err == nil {
						if sum := sha256.Sum256(data); sum != last {
							last = sum
							if err := runfile.Write(dir, Result, outcome(submit, data, sum)); err != nil {
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

// outcome submits the content of a control file and renders the result file.
func outcome(submit SubmitFunc, data []byte, sum [sha256.Size]byte) []byte {
	o := Outcome{Request: fmt.Sprintf("%x", sum[:])}
	var req struct {
		Patches []string `json:"patches"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		o.Error = "control file: " + err.Error()
	} else if id, err := submit(req.Patches); err != nil {
		o.Error = err.Error()
	} else {
		o.ID, o.OK = id, true
	}
	b, _ := json.MarshalIndent(o, "", "  ")
	return append(b, '\n')
}
