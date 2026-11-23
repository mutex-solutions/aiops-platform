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
// Config: dir (default <home>/run) and interval, how often the control file is read
// (a duration, default 25ms).
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

	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
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

type config struct {
	dir      string
	interval time.Duration
}

// Plugin is the caller.
var Plugin = kernel.Define(kernel.Plugin{
	ID: ID,
	Inject: []kernel.Dep{kernel.Need(echoers.Key), kernel.Need(stampers.Key), kernel.Need(clock.Key),
		kernel.Need(launch.CmdlineKey)},
	Decode: decode,
	Apply: func(c *kernel.Context, raw any) error {
		cfg := raw.(config)
		if cfg.dir == "" {
			home, ok := launch.CmdlineKey.Of(c).Flag("home")
			if !ok {
				return errors.New("example-caller: no dir in the config and no --home")
			}
			cfg.dir = runfile.Dir(home)
		}
		ec, sc, clk := echoers.Key.Of(c), stampers.Key.Of(c), clock.Key.Of(c)
		c.Go("example-caller-poll", func(ctx context.Context) error {
			var last [sha256.Size]byte // no content has the zero digest
			for {
				if msg, err := os.ReadFile(filepath.Join(cfg.dir, Control)); err == nil {
					if sum := sha256.Sum256(msg); sum != last {
						last = sum
						if err := runfile.Write(cfg.dir, Result, call(ctx, ec, sc, sum, string(msg))); err != nil {
							return err
						}
					}
				}
				t, err := clk.After(c, cfg.interval)
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

func reply(s string, err error) Reply {
	if err != nil {
		return Reply{Error: err.Error()}
	}
	return Reply{Reply: s}
}

func decode(raw any) (any, error) {
	m, ok := raw.(map[string]any)
	if raw != nil && !ok {
		return nil, errors.New("invalid config: want a map")
	}
	cfg := config{interval: 25 * time.Millisecond}
	for k, v := range m {
		s, isString := v.(string)
		var err error
		switch {
		case !isString:
			err = errors.New("not a string")
		case k == "dir":
			cfg.dir = s
		case k == "interval":
			if cfg.interval, err = time.ParseDuration(s); err == nil && cfg.interval <= 0 {
				err = errors.New("not positive")
			}
		default:
			err = errors.New("unknown field")
		}
		if err != nil {
			return nil, fmt.Errorf("invalid config: %s: %w", k, err)
		}
	}
	return cfg, nil
}
