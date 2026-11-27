// SPDX-License-Identifier: Apache-2.0

// Package caller is example-caller, the toy caller of the S2a test release (ADR-060
// decision 37, Q1 (a)): when a control file asks, it calls every echoer and every
// stamper through the dispatcher and writes what each answered to a result file that
// a test reads, as the control rows of ADR-059 decision 9 do. It consumes both toy
// registries and is in no shipped bundle.
//
// The control file is <dir>/call.json, and its content, any bytes, is the message.
// Each change of it is one round, and the first read counts, so a file that exists
// when the row starts is answered once. A test writes it atomically. The outcome of
// each round is written atomically to <dir>/call.result.json:
//
//	{"request": "<sha256 of the control file>",
//	 "echoers": {"<name>": {"reply": "..."}}, "stampers": {"<name>": {"error": "..."}}}
//
// Its config is Config, decoded by the rules of sdk/plugin.
package caller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id; Control and Result are the names of the control file and of
// the result file.
const (
	ID      = "aiops/example-caller"
	Control = "call.json"
	Result  = "call.result.json"
)

// Outcome is the content of the result file.
type Outcome struct {
	Request  string           `json:"request"`
	Echoers  map[string]Reply `json:"echoers"`
	Stampers map[string]Reply `json:"stampers"`
}

// Reply is what one contribution answered.
type Reply struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// Config is the row config.
type Config struct {
	// Dir holds the control file and the result file; empty, it is <home>/run.
	Dir string
	// Interval is how often the control file is read, positive.
	Interval time.Duration `default:"25ms"`
}

// Plugin is the caller.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID: ID,
	Inject: []kernel.Dep{kernel.Need(echoers.Key), kernel.Need(stampers.Key), kernel.Need(clock.Key),
		kernel.Need(launch.CmdlineKey)},
	Validate: func(cfg *Config) error {
		if cfg.Interval <= 0 {
			return errors.New("interval is not positive")
		}
		return nil
	},
	Apply: func(c *kernel.Context, cfg *Config) error {
		dir := cfg.Dir
		if dir == "" {
			home, ok := launch.CmdlineKey.Of(c).Flag("home")
			if !ok {
				return errors.New("example-caller: no dir in the config and no --home")
			}
			dir = filepath.Join(home, "run")
		}
		ec, sc, clk := echoers.Key.Of(c), stampers.Key.Of(c), clock.Key.Of(c)
		c.Go("example-caller-poll", func(ctx context.Context) error {
			var last [sha256.Size]byte // no content has the zero digest
			for {
				if msg, err := os.ReadFile(filepath.Join(dir, Control)); err == nil {
					if sum := sha256.Sum256(msg); sum != last {
						last = sum
						if err := write(dir, Result, call(ctx, ec, sc, sum, string(msg))); err != nil {
							return err
						}
					}
				}
				t, err := clk.After(c, cfg.Interval)
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

// call calls every registered echoer and stamper with msg and renders the outcome.
func call(ctx context.Context, ec echoers.Registry, sc stampers.Registry, sum [sha256.Size]byte, msg string) []byte {
	o := Outcome{Request: fmt.Sprintf("%x", sum), Echoers: map[string]Reply{}, Stampers: map[string]Reply{}}
	for _, name := range ec.Inventory() {
		o.Echoers[name] = reply(ec.Echo(ctx, name, msg))
	}
	for _, name := range sc.Inventory() {
		o.Stampers[name] = reply(sc.Stamp(ctx, name, msg))
	}
	b, _ := json.MarshalIndent(o, "", "  ")
	return append(b, '\n')
}

// write puts data in dir/name atomically: it writes a temporary file in dir and
// renames it, so a reader sees the old file or the whole new one. It creates dir.
func write(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, name))
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

func reply(s string, err error) Reply {
	if err != nil {
		return Reply{Error: err.Error()}
	}
	return Reply{Reply: s}
}
