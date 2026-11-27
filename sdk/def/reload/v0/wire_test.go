// SPDX-License-Identifier: Apache-2.0

package reload_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loader "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

var update = flag.Bool("update", false, "append to testdata/wire-v1.golden the forms of version 1 it lacks")

const golden = "testdata/wire-v1.golden"

// errOf is the error of this definition that each of its causes names.
var errOf = map[string]error{
	reload.CauseQueueFull: reload.ErrQueueFull, reload.CauseClosed: reload.ErrClosed, reload.CauseNested: reload.ErrNested,
	reload.CauseUnknown: reload.ErrUnknown, reload.CauseAbandoned: reload.ErrAbandoned, reload.CauseVerdict: reload.ErrVerdict,
	reload.CauseDenied: reload.ErrDenied, reload.CauseRollback: reload.ErrRollback, reload.CauseStateUnknown: reload.ErrStateUnknown,
}

// transport lists the causes of the transport with a text the row may give.
var transport = map[string]string{
	reload.CausePeer:    "reload-socket: the peer runs as uid 0, not as the site user's uid 1000",
	reload.CauseBusy:    "reload-socket: 8 connections are open; try again",
	reload.CauseRequest: "reload-socket: unknown op \"purge\"; version 1 serves submit and await",
	reload.CauseVersion: "reload-socket: version 2 is not served; this row serves version 1",
}

var statuses = []reload.Status{reload.Queued, reload.Running, reload.Passed, reload.RolledBack,
	reload.RollbackFailed, reload.Rejected, reload.Abandoned}

func digestOf(b byte) launch.Digest {
	var d launch.Digest
	for i := range d {
		d[i] = b + byte(i)
	}
	return d
}

// result is a result of status s with every field a stage reaches set.
func result(s reload.Status) reload.Result {
	r := reload.Result{ID: 7, Op: reload.Op{Source: reload.SourcePrefix + reload.CommandEnable}, Status: s, Stage: reload.StageCompose}
	if s == reload.Queued || s == reload.Abandoned {
		if s == reload.Abandoned {
			r.Err = reload.ErrAbandoned
		}
		return r
	}
	r.Previous = launch.Composition{Rows: []launch.Row{{ID: "a"}}, Digest: digestOf(0x10)}
	r.Digest = digestOf(0xa0)
	r.Stage = reload.StageAdmit
	if s == reload.Rejected {
		r.Err = fmt.Errorf("%w: row extra", reload.ErrDenied)
		r.Admission = launch.Admission{Denied: []launch.Denial{{Row: "extra", Check: "7", Reason: "unlisted consumer of reload"}}}
		return r
	}
	r.Stage = reload.StageApply
	r.Plan = launch.Plan{From: digestOf(0x10), To: digestOf(0xa0), Closure: []string{"extra"},
		Entries: []launch.PlanEntry{{ID: "extra", Action: launch.ActionUpdate, Changed: []string{"enabled"}, ConfigPaths: []string{}}}}
	if s == reload.Running {
		return r
	}
	r.Stage, r.Generation = reload.StagePublish, 4
	r.Applied = []loader.Result{{ID: "extra", Outcome: loader.Created}}
	r.Report = composition.Report{Pass: true, Rows: []composition.RowReport{{ID: "extra", Outcome: "pass", State: kernel.Active}}}
	switch s {
	case reload.RolledBack, reload.RollbackFailed:
		r.Stage = reload.StageRollback
		r.Report = composition.Report{Rows: []composition.RowReport{{ID: "extra", Outcome: "fail", State: kernel.Failed, Detail: "apply: no"}}}
		r.Err = fmt.Errorf("%w: extra is FAILED", reload.ErrVerdict)
		r.Rollback = &reload.Rollback{Plan: launch.Plan{From: digestOf(0xa0), To: digestOf(0x10),
			Entries: []launch.PlanEntry{{ID: "extra", Action: launch.ActionRemove}}},
			Applied: []loader.Result{{ID: "extra", Outcome: loader.Removed}}, Generation: 5,
			Report: composition.Report{Pass: true}}
		if s == reload.RollbackFailed {
			r.Err = errors.Join(r.Err, reload.ErrRollback, reload.ErrStateUnknown)
			r.Rollback.Generation, r.Rollback.Err = 0, errors.New("restore: extra is FAILED")
			r.Rollback.Report = composition.Report{Rows: []composition.RowReport{{ID: "extra", Outcome: "fail", State: kernel.Failed}}}
		}
	}
	return r
}

type form struct {
	name string
	v    any
}

// forms are the forms of version 1 as this code writes them.
func forms() []form {
	var out []form
	for _, c := range reload.Commands() {
		out = append(out, form{"request submit " + c, reload.Request{V: 1, Op: reload.OpSubmit, Command: c}})
	}
	out = append(out, form{"request await", reload.Request{V: 1, Op: reload.OpAwait, ID: 7}})
	for _, s := range statuses {
		w := reload.EncodeResult(result(s))
		out = append(out, form{"reply result " + string(s), reload.Reply{V: 1, Result: &w}})
	}
	for _, c := range slices.Sorted(maps.Keys(errOf)) {
		out = append(out, form{"reply error " + c, reload.Reply{V: 1, Error: reload.EncodeError(errOf[c])}})
	}
	for _, c := range slices.Sorted(maps.Keys(transport)) {
		out = append(out, form{"reply error " + c, reload.Reply{V: 1, Error: reload.NewError(transport[c], c)}})
	}
	out = append(out, form{"status running", reload.ProcessStatus{Phase: "running", Profile: "dev"}})
	return out
}

// within reports whether every field of old is in cur with the same value: cur may
// hold fields old lacks, and nothing else may differ.
func within(old, cur any) bool {
	switch o := old.(type) {
	case map[string]any:
		c, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range o {
			if w, ok := c[k]; !ok || !within(v, w) {
				return false
			}
		}
		return true
	case []any:
		c, ok := cur.([]any)
		if !ok || len(c) != len(o) {
			return false
		}
		for i := range o {
			if !within(o[i], c[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(old, cur)
}

func tree(t *testing.T, line []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(line, &v); err != nil {
		t.Fatalf("%s: %v", line, err)
	}
	return v
}

// reread decodes a recorded line with the decoder of its kind and writes it again.
func reread(name string, line []byte) ([]byte, error) {
	switch kind, _, _ := strings.Cut(name, " "); kind {
	case "request":
		r, err := reload.DecodeRequest(line)
		return reload.Line(r), err
	case "reply":
		r, err := reload.DecodeReply(line)
		return reload.Line(r), err
	}
	var s reload.ProcessStatus
	err := json.Unmarshal(line, &s)
	return reload.Line(s), err
}

// Version 1 changes only by addition (ADR-062 decisions 6 and 18), as the handshake
// does (ADR-060 decision 42): every recorded form is still written, with every field
// it had and the same values, and the decoders still read it; a form this code writes
// that the file lacks fails until it is recorded, which freezes it too.
func TestWireVersion1ChangesOnlyByAddition(t *testing.T) {
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	cur := map[string][]byte{}
	var order []string
	for _, f := range forms() {
		cur[f.name] = bytes.TrimSuffix(reload.Line(f.v), []byte("\n"))
		order = append(order, f.name)
	}
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		name, line, ok := strings.Cut(sc.Text(), "\t")
		if !ok || seen[name] {
			t.Fatalf("%s: %q is not one <name>TAB<json> line of its own", golden, sc.Text())
		}
		seen[name] = true
		c, ok := cur[name]
		switch {
		case !ok:
			t.Errorf("form %q of version 1 is gone: a form, an op value or a command is never removed", name)
		case !within(tree(t, []byte(line)), tree(t, c)):
			t.Errorf("form %q changed other than by an added field:\nrecorded %s\nnow      %s", name, line, c)
		}
		again, err := reread(name, []byte(line+"\n"))
		if err != nil || !within(tree(t, []byte(line)), tree(t, again)) {
			t.Errorf("form %q as recorded does not read back: %s (%v)", name, again, err)
		}
	}
	var missing []string
	for _, n := range order {
		if !seen[n] {
			missing = append(missing, n+"\t"+string(cur[n])+"\n")
		}
	}
	if len(missing) == 0 {
		return
	}
	if *update {
		if err := os.WriteFile(golden, append(data, strings.Join(missing, "")...), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Errorf("forms not recorded in %s; run go test -run %s -update:\n%s", golden, t.Name(), strings.Join(missing, ""))
}

// A result of each status, with each cause, is encoded and decoded again unchanged.
func TestWireResultsRoundTrip(t *testing.T) {
	for _, s := range statuses {
		for cause, err := range errOf {
			r := result(s)
			r.Err = err
			want := reload.EncodeResult(r)
			got, derr := reload.DecodeReply(reload.Line(reload.Reply{V: 1, Result: &want}))
			if derr != nil || got.V != 1 || got.Error != nil || !reflect.DeepEqual(*got.Result, want) {
				t.Fatalf("%s with %s: decoded %+v, %v; want %+v", s, cause, got.Result, derr, want)
			}
			if got.Result.Status.Done() != s.Done() || !got.Result.Error.Has(cause) || got.Result.Error.Text != err.Error() {
				t.Errorf("%s with %s: error %+v", s, cause, got.Result.Error)
			}
		}
	}
}

func TestWireErrorsCarryEachCauseTheyMatch(t *testing.T) {
	for cause, err := range errOf {
		got, derr := reload.DecodeReply(reload.Line(reload.Reply{V: 1, Error: reload.EncodeError(fmt.Errorf("wrapped: %w", err))}))
		if derr != nil || got.Result != nil || !slices.Equal(got.Error.Causes, []string{cause}) {
			t.Errorf("%s: %+v, %v", cause, got.Error, derr)
		}
	}
	joined := errors.Join(reload.ErrStateUnknown, reload.ErrVerdict, reload.ErrRollback)
	if got := reload.EncodeError(joined).Causes; !slices.Equal(got, []string{reload.CauseVerdict, reload.CauseRollback, reload.CauseStateUnknown}) {
		t.Errorf("joined causes = %v, in the order of the closed list", got)
	}
	if got := reload.EncodeError(errors.New("other")); got.Text != "other" || got.Causes == nil || len(got.Causes) != 0 {
		t.Errorf("an error of no cause = %+v, want an empty list", got)
	}
	if reload.EncodeError(nil) != nil {
		t.Error("EncodeError(nil) is not nil")
	}
	if e := reload.NewError("busy", reload.CauseBusy); !e.Has(reload.CauseBusy) || e.Has(reload.CausePeer) || (*reload.WireError)(nil).Has(reload.CauseBusy) {
		t.Errorf("Has = %+v", e)
	}
}

func TestWireDigestsAreLowercaseHex(t *testing.T) {
	w := reload.EncodeResult(result(reload.RolledBack))
	for _, d := range []string{w.Digest, w.Previous, w.Plan.From, w.Plan.To, w.Rollback.Plan.From} {
		if len(d) != 64 || strings.ToLower(d) != d || strings.Trim(d, "0123456789abcdef") != "" {
			t.Errorf("digest %q is not 64 lowercase hex digits", d)
		}
	}
	if w.Digest != "a0a1a2a3a4a5a6a7a8a9aaabacadaeafb0b1b2b3b4b5b6b7b8b9babbbcbdbebf" || w.Previous != w.Plan.From {
		t.Errorf("digest %s, previous %s", w.Digest, w.Previous)
	}
	if !strings.Contains(string(reload.Line(w)), `"previous":"101112`) || strings.Contains(string(reload.Line(w)), `"rows"`+":null") {
		t.Errorf("line %s", reload.Line(w))
	}
}

func TestWireDecodersRefuseWhatIsNotOneLine(t *testing.T) {
	r, err := reload.DecodeRequest([]byte(`{"v":1,"op":"await","id":3,"later":true}` + "\n"))
	if err != nil || r != (reload.Request{V: 1, Op: reload.OpAwait, ID: 3}) {
		t.Errorf("a field it does not know is ignored: %+v, %v", r, err)
	}
	for _, bad := range []string{`{"v":1`, `[1]`, `{"v":1}{"v":1}`, ``} {
		if _, err := reload.DecodeRequest([]byte(bad)); err == nil {
			t.Errorf("DecodeRequest(%q) = nil error", bad)
		}
	}
	for _, bad := range []string{`{"v":1}`, `{"v":1,"result":{},"error":{"text":"x","causes":[]}}`, `{"v":`} {
		if _, err := reload.DecodeReply([]byte(bad)); err == nil {
			t.Errorf("DecodeReply(%q) = nil error", bad)
		}
	}
}
