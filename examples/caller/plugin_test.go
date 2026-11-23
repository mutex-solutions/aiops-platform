// SPDX-License-Identifier: Apache-2.0

package caller_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/examples/caller"
	"github.com/mutex-solutions/aiops-platform/examples/dispatch"
	"github.com/mutex-solutions/aiops-platform/examples/echoer"
	"github.com/mutex-solutions/aiops-platform/examples/stamper"
	clockfake "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// When the control file changes, the caller calls every echoer and every stamper
// through the dispatcher and writes what each answered (ADR-060 decision 37).
func TestCallsBothRegistriesWhenAsked(t *testing.T) {
	home := t.TempDir()
	run := filepath.Join(home, "run")
	control(t, run, "hi")
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	l.Flags = map[string]string{"home": home}
	clk, cp := clockfake.New(time.Unix(0, 0))
	kerneltest.Mount(root, lp, cp, dispatch.Plugin, echoer.Plugin, stamper.Plugin)
	f := root.Plugin(echoer.Plugin, map[string]any{"name": "loud", "prefix": "LOUD "})
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	kerneltest.Mount(root, caller.Plugin)

	got := result(t, run, clk, "hi")
	want := caller.Outcome{Request: sum("hi"),
		Echoers:  map[string]caller.Reply{"example": {Reply: "example: hi"}, "loud": {Reply: "LOUD hi"}},
		Stampers: map[string]caller.Reply{"example": {Reply: "example:d8932aac"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("result %+v, want %+v", got, want)
	}
	if err := f.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = f.Await(t.Context())
	control(t, run, "again")
	got = result(t, run, clk, "again")
	if _, ok := got.Echoers["loud"]; ok || got.Echoers["example"].Reply != "example: again" {
		t.Errorf("after the loud row left: %+v", got)
	}
}

func TestConfigIsChecked(t *testing.T) {
	if _, err := caller.Plugin.Decode(map[string]any{"dir": "/run", "interval": "1s"}); err != nil {
		t.Errorf("a good config: %v", err)
	}
	for _, raw := range []any{"text", map[string]any{"interval": "soon"}, map[string]any{"interval": "-1s"},
		map[string]any{"dir": 1}, map[string]any{"color": "red"}} {
		if _, err := caller.Plugin.Decode(raw); err == nil {
			t.Errorf("Decode(%v) accepted it", raw)
		}
	}
}

func sum(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

func control(t *testing.T, run, msg string) {
	t.Helper()
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, caller.Control), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// result waits until the result file answers msg, moving the manual clock so that
// the caller polls again.
func result(t *testing.T, run string, clk *clockfake.Clock, msg string) caller.Outcome {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var o caller.Outcome
		if b, err := os.ReadFile(filepath.Join(run, caller.Result)); err == nil && json.Unmarshal(b, &o) == nil && o.Request == sum(msg) {
			return o
		}
		if time.Now().After(deadline) {
			t.Fatalf("no result for %q", msg)
		}
		clk.Advance(time.Second)
		time.Sleep(5 * time.Millisecond)
	}
}
