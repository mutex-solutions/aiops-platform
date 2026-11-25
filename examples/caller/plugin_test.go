// SPDX-License-Identifier: Apache-2.0

package caller_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/examples/caller"
	clockfake "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	echofake "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/fake"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	stampfake "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// reply is an echoer and a stamper of the test, which answers its prefix and the message.
type reply string

func (r reply) Echo(_ context.Context, msg string) (string, error) { return string(r) + msg, nil }
func (r reply) Stamp(_ context.Context, body string) (string, error) {
	return string(r) + strings.ToUpper(body), nil
}

// contribution registers r under name with the registry of k, as a contribution row does;
// a plugin imports no other plugin, so the caller's tests call through the fakes of the
// two dispatchers (07-c4-plugin-sdk.md:34).
func contribution[T any](id string, k kernel.Key[T], register func(T, *kernel.Context) error) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: id, Inject: []kernel.Dep{kernel.Need(k)},
		Apply: func(c *kernel.Context, _ any) error { return register(k.Of(c), c) }})
}

// When the control file changes, the caller calls every echoer and every stamper
// through the dispatcher and writes what each answered (ADR-060 decision 37).
func TestCallsBothRegistriesWhenAsked(t *testing.T) {
	home := t.TempDir()
	run := filepath.Join(home, "run")
	control(t, run, "hi")
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	l.Flags = map[string]string{"home": home}
	clk, cp := clockfake.NewAt(time.Unix(0, 0))
	_, ep := echofake.New()
	_, sp := stampfake.New()
	kerneltest.Mount(root, lp, cp, ep, sp,
		contribution("test/echo", echoers.Key, func(r echoers.Registry, c *kernel.Context) error { return r.Register(c, "example", reply("example: ")) }),
		contribution("test/stamp", stampers.Key, func(r stampers.Registry, c *kernel.Context) error { return r.Register(c, "example", reply("example:")) }))
	f := root.Plugin(contribution("test/loud", echoers.Key, func(r echoers.Registry, c *kernel.Context) error { return r.Register(c, "loud", reply("LOUD ")) }), nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	kerneltest.Mount(root, caller.Plugin)

	got := result(t, run, clk, "hi")
	want := caller.Outcome{Request: sum("hi"),
		Echoers:  map[string]caller.Reply{"example": {Reply: "example: hi"}, "loud": {Reply: "LOUD hi"}},
		Stampers: map[string]caller.Reply{"example": {Reply: "example:HI"}}}
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
	for _, tc := range []struct {
		raw  any
		want string
	}{
		{"text", `invalid config: config: want a map, got a string`},
		{map[string]any{"interval": "soon"}, `invalid config: config.interval: want a duration such as 50ms or 1h30m, got a string`},
		{map[string]any{"interval": "-1s"}, "invalid config: interval is not positive"},
		{map[string]any{"dir": int64(1)}, "invalid config: config.dir: want a string, got 1"},
		{map[string]any{"color": "red"}, "invalid config: config.color: unknown field"},
	} {
		if _, err := caller.Plugin.Decode(tc.raw); err == nil || err.Error() != tc.want {
			t.Errorf("Decode(%v) = %v, want %q", tc.raw, err, tc.want)
		}
	}
	if _, err := plugin.Describe[caller.Config](); err != nil {
		t.Error(err)
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
