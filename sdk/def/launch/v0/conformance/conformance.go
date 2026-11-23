// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the launch definition: what every provider of
// the six launch keys, the launcher, and the fake must do (07-c4-plugin-sdk.md §8.2,
// T2 "every provider and the fake behave the same"). The obligations half holds a
// launcher to the six services as the setup describes the process. The guard half
// holds it to what it refuses, its release and its method sets: Commit and
// RequiredOf refuse a composition it did not compose; after the launcher's release
// the consumer it served is released, and a read of any of the six keys fails with
// kernel.ErrInactive; no provided value has a method outside its interface. Each
// obligation has an id, the first word of its case's name. The package imports the
// standard library, sdk/kernel and definitions only.
package conformance

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Setup is the process a launcher is to describe.
type Setup struct {
	Profile, Role string
	Args          []string
	Flags         map[string]string
	// Env is the environment of the process.
	Env map[string]string
	// Switches names the kill switches that are on.
	Switches []string
	// Consumer is the plugin that needs the six keys. The launcher lists it in its
	// catalog and admits it as a consumer of profileContext and appExit, which are
	// consumer-bound.
	Consumer kernel.Plugin
}

// Mount starts a launcher that provides the six keys as s says, and returns the root
// on which the suite mounts s.Consumer and the function that releases the launcher,
// which for a launcher is the stop of the process.
type Mount func(t *testing.T, s Setup) (root *kernel.Root, release func())

const (
	refName   = launch.EnvPrefix + "CONFORMANCE"
	otherName = "CONFORMANCE_OTHER" // set, but without the prefix
)

func setup(consumer kernel.Plugin) Setup {
	return Setup{
		Profile: "conformance", Role: "api",
		Args:     []string{"--profile", "conformance", "--role", "api"},
		Flags:    map[string]string{"profile": "conformance", "role": "api"},
		Env:      map[string]string{refName: "ref-value", otherName: "other-value"},
		Switches: []string{launch.AgentsPaused},
		Consumer: consumer,
	}
}

var keys = []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey, launch.ReadyKey,
	launch.KillSwitchesKey, launch.AppExitKey}

// Run runs the suite, each case on a launcher of its own.
func Run(t *testing.T, mount Mount) {
	t.Run("obligations", func(t *testing.T) {
		t.Run("launch.O1 profileContext names the process", func(t *testing.T) {
			u := use(t, mount)
			if p, r := u.pc.Profile(), u.pc.Role(); p != u.s.Profile || r != u.s.Role {
				t.Errorf("Profile, Role = %q, %q; want %q, %q", p, r, u.s.Profile, u.s.Role)
			}
		})

		t.Run("launch.O2 a composed candidate is admitted, planned and committed", func(t *testing.T) {
			pc, ctx := use(t, mount).pc, t.Context()
			c, err := pc.Compose(ctx)
			if err != nil {
				t.Fatalf("Compose = %v", err)
			}
			if _, err := pc.Admit(ctx, c); err != nil {
				t.Errorf("Admit = %v", err)
			}
			if _, err := pc.Plan(ctx, pc.Committed(), c); err != nil {
				t.Errorf("Plan = %v", err)
			}
			if err := pc.Commit(c); err != nil {
				t.Fatalf("Commit = %v", err)
			}
			if got := pc.Committed().Digest; got != c.Digest {
				t.Errorf("Committed after Commit has the digest %x, want %x", got[:6], c.Digest[:6])
			}
			req, err := pc.RequiredOf(c)
			if have := pc.Required(); err != nil || !slices.Equal(req.Rows, have.Rows) || !slices.Equal(req.Keys, have.Keys) {
				t.Errorf("RequiredOf(committed) = %+v, %v; Required = %+v", req, err, have)
			}
		})

		t.Run("launch.O3 the catalog lists its entries and the keys they name", func(t *testing.T) {
			u := use(t, mount)
			es := u.cat.Entries()
			if !slices.IsSortedFunc(es, func(a, b launch.CatalogEntry) int { return strings.Compare(a.ID, b.ID) }) {
				t.Errorf("Entries are not sorted by id: %+v", es)
			}
			names := []string{}
			for _, k := range keys {
				names = append(names, k.Name())
			}
			for _, e := range es {
				if got, ok := u.cat.Lookup(e.ID); !ok || got.ID != e.ID {
					t.Errorf("Lookup(%s) = %+v, %v", e.ID, got, ok)
				}
				names = append(names, slices.Concat(e.Provides, e.Needs)...)
			}
			if _, ok := u.cat.Lookup(u.s.Consumer.ID); !ok {
				t.Errorf("Lookup of the consumer %s found nothing", u.s.Consumer.ID)
			}
			if e, ok := u.cat.Lookup("conformance/none"); ok {
				t.Errorf("Lookup of an unknown id = %+v", e)
			}
			for _, n := range names {
				if k, ok := u.cat.Key(n); !ok || k.Name() != n {
					t.Errorf("Key(%s) = %v, %v", n, k, ok)
				}
			}
			if k, ok := u.cat.Key("conformanceNone"); ok {
				t.Errorf("Key of an unknown name = %v", k)
			}
		})

		t.Run("launch.O4 cmdline serves argv, flags and only prefixed environment names", func(t *testing.T) {
			u := use(t, mount)
			args := u.cl.Args()
			if !slices.Equal(args, u.s.Args) {
				t.Errorf("Args = %q, want %q", args, u.s.Args)
			}
			args[0] = "changed"
			if again := u.cl.Args(); !slices.Equal(again, u.s.Args) {
				t.Errorf("Args after the caller changed what it got = %q", again)
			}
			for n, v := range u.s.Flags {
				if got, ok := u.cl.Flag(n); !ok || got != v {
					t.Errorf("Flag(%s) = %q, %v; want %q", n, got, ok, v)
				}
			}
			if got, ok := u.cl.Flag("conformance-none"); ok {
				t.Errorf("Flag of an unknown name = %q", got)
			}
			if got, ok := u.cl.Env(refName); !ok || got != u.s.Env[refName] {
				t.Errorf("Env(%s) = %q, %v; want %q", refName, got, ok, u.s.Env[refName])
			}
			for _, n := range []string{otherName, launch.EnvPrefix + "CONFORMANCE_NONE"} {
				if got, ok := u.cl.Env(n); ok {
					t.Errorf("Env(%s) = %q, want it not set", n, got)
				}
			}
		})

		t.Run("launch.O5 the ready latch follows Set and Drop", func(t *testing.T) {
			rd := use(t, mount).rd
			for _, step := range []struct {
				do   func()
				want bool
			}{{func() {}, false}, {rd.Set, true}, {rd.Set, true}, {rd.Drop, false}} {
				step.do()
				if got := rd.IsReady(); got != step.want {
					t.Fatalf("IsReady = %v, want %v", got, step.want)
				}
			}
		})

		t.Run("launch.O6 the kill switches are the setup's", func(t *testing.T) {
			ks := use(t, mount).ks
			for name, want := range map[string]bool{launch.AgentsPaused: true, launch.TelemetryDisabled: false, "conformance-none": false} {
				if got := ks.On(name); got != want {
					t.Errorf("On(%s) = %v, want %v", name, got, want)
				}
			}
		})

		t.Run("launch.O7 an exit request returns at once", func(t *testing.T) {
			ex := use(t, mount).ex
			ex.RequestExit(launch.ExitRequest{Reason: launch.ExitGaveUp, Detail: "conformance"})
			ex.RequestExit(launch.ExitRequest{Reason: launch.ExitDisposeLeak, Detail: "conformance"})
		})
	})

	t.Run("guard", func(t *testing.T) {
		t.Run("launch.G1 a composition the launcher did not compose is refused", func(t *testing.T) {
			pc := use(t, mount).pc
			before := pc.Committed().Digest
			foreign := launch.Composition{Rows: []launch.Row{{ID: "foreign", Plugin: "conformance/none", Enabled: true}}, Digest: launch.Digest{0xc0, 0xff, 0xee}}
			if err := pc.Commit(foreign); err == nil {
				t.Error("Commit of a foreign composition = nil")
			}
			if got := pc.Committed().Digest; got != before {
				t.Errorf("a refused Commit changed the committed digest to %x", got[:6])
			}
			if _, err := pc.RequiredOf(foreign); err == nil {
				t.Error("RequiredOf of a foreign composition = nil")
			}
		})

		t.Run("launch.G2 after release the keys fail with ErrInactive", func(t *testing.T) {
			u := use(t, mount)
			u.release()
			for _, read := range []func(){
				func() { launch.ProfileContextKey.Of(u.c) }, func() { launch.CatalogKey.Of(u.c) },
				func() { launch.CmdlineKey.Of(u.c) }, func() { launch.ReadyKey.Of(u.c) },
				func() { launch.KillSwitchesKey.Of(u.c) }, func() { launch.AppExitKey.Of(u.c) },
			} {
				if err := deftest.ReadError(read); !errors.Is(err, kernel.ErrInactive) {
					t.Errorf("a read of a launch key after release = %v, want ErrInactive", err)
				}
			}
		})

		t.Run("launch.G3 no side door", func(t *testing.T) {
			u := use(t, mount)
			for name, doors := range map[string][]string{
				"profileContext": deftest.SideDoors[launch.ProfileContext](u.pc),
				"catalog":        deftest.SideDoors[launch.Catalog](u.cat),
				"cmdline":        deftest.SideDoors[launch.Cmdline](u.cl),
				"ready":          deftest.SideDoors[launch.Ready](u.rd),
				"killSwitches":   deftest.SideDoors[launch.KillSwitches](u.ks),
				"appExit":        deftest.SideDoors[launch.AppExit](u.ex),
			} {
				if len(doors) > 0 {
					t.Errorf("the value provided under %s has methods outside its interface: %v", name, doors)
				}
			}
		})
	})
}

// user is the consumer of the six keys on a started launcher.
type user struct {
	s       Setup
	release func()
	c       *kernel.Context
	pc      launch.ProfileContext
	cat     launch.Catalog
	cl      launch.Cmdline
	rd      launch.Ready
	ks      launch.KillSwitches
	ex      launch.AppExit
}

func use(t *testing.T, mount Mount) *user {
	t.Helper()
	u := &user{}
	p := kernel.Plugin{ID: "conformance/launch-consumer", Apply: func(c *kernel.Context, _ any) error {
		u.c = c
		u.pc, u.cat, u.cl = launch.ProfileContextKey.Of(c), launch.CatalogKey.Of(c), launch.CmdlineKey.Of(c)
		u.rd, u.ks, u.ex = launch.ReadyKey.Of(c), launch.KillSwitchesKey.Of(c), launch.AppExitKey.Of(c)
		return nil
	}}
	for _, k := range keys {
		p.Inject = append(p.Inject, kernel.NeedKey(k))
	}
	u.s = setup(kernel.Define(p))
	root, release := mount(t, u.s)
	u.release = release
	deftest.Active(t, root.Plugin(u.s.Consumer, nil))
	return u
}
