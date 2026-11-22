// SPDX-License-Identifier: Apache-2.0

// Package teststate is the test-only row "test-state" of ADR-059 decision 9: it writes
// the committed state of the process as JSON to a file under the runtime directory of
// the home whenever it changes, so that a test drives the built binary and reads what
// it did. It is in no shipped distribution.
//
// The state is read by polling the loader and the readiness latch through the clock:
// no kernel event is needed and none is missed, a change is written within one
// interval, and an unchanged state writes nothing. The file is replaced atomically
// (runfile.Write), so a reader never sees a part of one.
//
// What is in the file, per ADR-059 decision 9: for each row id its mount state and
// fiber state, the readiness latch and a generation. At S1 no generation number
// exists (reload commits them and is not linked), so "generation" counts the changes
// this row has written, from 1, and "digest" is the digest of the composition the
// process booted. The last operation of reload is in "reload": its status, stage,
// error, the number and digest of the generation it applied and the verdict of each
// row, so that a harness reads what a submission did. The operations are found by
// asking reload for id 1, 2, 3 until it answers no, so any submitter is seen, not
// only test-submit.
package teststate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	clock "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
)

// ID is the plugin id. File is the default name of the state file.
const (
	ID   = "aiops-test/test-state"
	File = "test-state.json"
)

// Row is the state of one row.
type Row struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	Mount  string `json:"mount"` // mounted, disabled, other-role, parent-unmounted or unknown
	State  string `json:"state"` // the fiber state in capitals; empty unless mounted
}

// Verdict is the judged result of one row of an applied generation.
type Verdict struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	State   string `json:"state"`
	Detail  string `json:"detail"`
}

// Op is the last operation of reload.
type Op struct {
	ID         uint64 `json:"id"`
	Status     string `json:"status"` // queued, running, passed, rolled back, rollback failed, rejected or abandoned
	Stage      string `json:"stage"`
	Error      string `json:"error"`
	Generation uint64 `json:"generation"` // the number reload gave the applied generation; 0 until one is applied
	Digest     string `json:"digest"`
	// Previous is the composition the tree ran before the operation (Result.Previous),
	// which a later step restores: its digest and its number of rows.
	PreviousDigest string `json:"previousDigest"`
	PreviousRows   int    `json:"previousRows"`
	// Applied lists what the loader did, by outcome (created, restarted, committed...),
	// with the ids of the rows: Result.Applied.
	Applied map[string][]string `json:"applied,omitempty"`
	Denied  []string            `json:"denied"` // rows the gate denied: the generation is rejected
	Verdict []Verdict           `json:"verdict"`
	// Rollback is what restoring the previous composition did after a failed verdict
	// (Result.Rollback), absent when none was attempted.
	Rollback *Rollback `json:"rollback,omitempty"`
}

// Rollback is the outcome of a rollback: the number reload gave the restored generation, why
// it failed if it did, and the verdict on the restored rows.
type Rollback struct {
	Generation uint64    `json:"generation"`
	Error      string    `json:"error"`
	Verdict    []Verdict `json:"verdict"`
}

// State is the file.
type State struct {
	Generation uint64 `json:"generation"`
	Ready      bool   `json:"ready"`
	Closing    bool   `json:"closing"` // the last write, made as the row is disposed
	Digest     string `json:"digest"`
	Rows       []Row  `json:"rows"`
	Reload     *Op    `json:"reload,omitempty"` // the last operation, absent until one was submitted
}

// New returns the plugin. Config: interval (duration, default 25ms), dir (the
// directory of the file, default <home>/run) and file (default test-state.json).
func New() kernel.Plugin {
	type cfg struct {
		interval  time.Duration
		dir, file string
	}
	return kernel.Define(kernel.Plugin{
		ID: ID,
		Inject: []kernel.Dep{kernel.Need(clock.Key), kernel.Need(loaderdef.Key), kernel.Need(composition.Key),
			kernel.Need(reload.Key), kernel.Need(launch.ReadyKey), kernel.Need(launch.ProfileContextKey), kernel.Need(launch.CmdlineKey)},
		Decode: func(raw any) (any, error) {
			c := def.Parse(raw)
			out := cfg{interval: c.Duration("interval", 25*time.Millisecond), dir: c.String("dir", ""), file: c.String("file", File)}
			if out.interval <= 0 {
				return nil, errors.New("invalid config: interval must be positive")
			}
			return out, c.Err()
		},
		Apply: func(c *kernel.Context, raw any) error {
			cf := raw.(cfg)
			w := &writer{dir: cf.dir, file: cf.file, ready: launch.ReadyKey.Of(c), ld: loaderdef.Key.Of(c), rd: composition.Key.Of(c), rl: reload.Key.Of(c)}
			if w.dir == "" {
				home, ok := launch.CmdlineKey.Of(c).Flag("home")
				if !ok {
					return errors.New("test-state: no dir in the config and no --home")
				}
				w.dir = runfile.Dir(home)
			}
			comp, err := launch.ProfileContextKey.Of(c).Compose(c.Std())
			if err != nil {
				return fmt.Errorf("test-state: %w", err)
			}
			w.digest = fmt.Sprintf("%x", comp.Digest[:])
			for _, r := range comp.Rows {
				w.ids = append(w.ids, r.ID)
			}
			clk := clock.Key.Of(c)
			c.Defer("test-state-final", func(context.Context) error { w.closing = true; return w.write() })
			c.Go("test-state-poll", func(ctx context.Context) error {
				for {
					if err := w.write(); err != nil {
						return err
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

// writer is touched by the poll goroutine while the row is active and by the disposer
// after that goroutine ended, never by both at once: the kernel waits for tracked
// work before it runs the disposers.
type writer struct {
	dir, file, digest string
	ids               []string
	ready             launch.Ready
	ld                loaderdef.Loader
	rd                composition.Reader
	rl                reload.Reload
	lastOp            reload.ID
	last              []byte
	gen               uint64
	closing           bool
}

func (w *writer) snapshot() State {
	s := State{Closing: w.closing, Digest: w.digest}
	// The last operation is read first and the rows after it, so that the rows are never
	// older than the verdict beside them: a reader that sees a verdict must see the rows it
	// created. The latch is read last, for the same reason.
	for {
		if _, ok := w.rl.Result(w.lastOp + 1); !ok {
			break
		}
		w.lastOp++
	}
	if r, ok := w.rl.Result(w.lastOp); ok {
		s.Reload = opOf(r)
	}
	ids := slices.Clone(w.ids)
	if g, ok := w.rd.Committed(composition.ScopeProcess); ok {
		for _, r := range g.Rows {
			if !slices.Contains(ids, r.ID) {
				ids = append(ids, r.ID)
			}
		}
	}
	for _, id := range ids {
		row := Row{ID: id, Mount: "unknown"}
		if e, ok := w.ld.Locate(id); ok {
			row.Plugin, row.Mount, row.State = e.Spec.Plugin, string(e.Mount), string(e.State)
		}
		s.Rows = append(s.Rows, row)
	}
	s.Ready = w.ready.IsReady()
	return s
}

func opOf(r reload.Result) *Op {
	op := &Op{ID: uint64(r.ID), Status: string(r.Status), Stage: string(r.Stage), Generation: r.Generation,
		Digest: fmt.Sprintf("%x", r.Digest[:]), PreviousDigest: fmt.Sprintf("%x", r.Previous.Digest[:]), PreviousRows: len(r.Previous.Rows)}
	if r.Err != nil {
		op.Error = r.Err.Error()
	}
	for _, a := range r.Applied {
		if op.Applied == nil {
			op.Applied = map[string][]string{}
		}
		op.Applied[string(a.Outcome)] = append(op.Applied[string(a.Outcome)], a.ID)
	}
	for _, d := range r.Admission.Denied {
		op.Denied = append(op.Denied, d.Row)
	}
	op.Verdict = verdicts(r.Report)
	if rb := r.Rollback; rb != nil {
		op.Rollback = &Rollback{Generation: rb.Generation, Verdict: verdicts(rb.Report)}
		if rb.Err != nil {
			op.Rollback.Error = rb.Err.Error()
		}
	}
	return op
}

func verdicts(rep composition.Report) []Verdict {
	var out []Verdict
	for _, v := range rep.Rows {
		out = append(out, Verdict{ID: v.ID, Outcome: v.Outcome, State: string(v.State), Detail: v.Detail})
	}
	return out
}

// write writes the state if it differs from the last one written.
func (w *writer) write() error {
	s := w.snapshot()
	body, _ := json.Marshal(s) // a State holds nothing that cannot be marshalled
	if bytes.Equal(body, w.last) {
		return nil
	}
	w.last = body
	w.gen++
	s.Generation = w.gen
	out, _ := json.MarshalIndent(s, "", "  ")
	return runfile.Write(w.dir, w.file, append(out, '\n'))
}
