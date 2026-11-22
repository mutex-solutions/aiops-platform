// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var allKeys = []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey,
	launch.ReadyKey, launch.KillSwitchesKey, launch.AppExitKey}

func needAll() []kernel.Dep {
	var d []kernel.Dep
	d = append(d, kernel.Need(launch.ProfileContextKey), kernel.Need(launch.CatalogKey), kernel.Need(launch.CmdlineKey),
		kernel.Need(launch.ReadyKey), kernel.Need(launch.KillSwitchesKey), kernel.Need(launch.AppExitKey))
	return d
}

func TestConsumerCallsEveryMethod(t *testing.T) {
	l, plugin := fake.New()
	mod := kernel.Define(kernel.Plugin{ID: "aiops/mod", Apply: func(*kernel.Context, any) error { return nil }})
	l.ProfileName, l.RoleName = "self-host-full", "api"
	l.LayerList = []launch.Layer{{Number: 2, Source: "profile:self-host-full", File: "p.yml"}}
	l.Modules = []launch.CatalogEntry{{ID: "aiops/mod", Version: "1.0.0", Provides: []string{"k"}}}
	l.Plugins = map[string]kernel.Plugin{"aiops/mod": mod}
	l.Keys = map[string]kernel.AnyKey{"k": launch.CatalogKey}
	l.Argv, l.Flags = []string{"--role", "api"}, map[string]string{"role": "api"}
	l.Switches = map[string]bool{launch.AgentsPaused: true}
	l.Environ = map[string]string{"AIOPS_REF_X": "v"}
	l.Current = launch.Composition{Digest: launch.Digest{7}}
	l.Needed = launch.RequiredSet{Rows: []string{"r"}, Keys: []string{"sql"}}
	cand := launch.Composition{Rows: []launch.Row{{ID: "a"}}, Digest: launch.Digest{1}}
	l.ComposeFn = func(context.Context) (launch.Composition, error) { return cand, nil }
	l.AdmitFn = func(_ context.Context, c launch.Composition) (launch.Admission, error) {
		return launch.Admission{Denied: []launch.Denial{{Row: c.Rows[0].ID, Check: "3"}}}, nil
	}
	l.PlanFn = func(_ context.Context, from, to launch.Composition) (launch.Plan, error) {
		return launch.Plan{From: from.Digest, To: to.Digest, Entries: []launch.PlanEntry{{ID: "a", Action: launch.ActionCreate}}}, nil
	}

	ran := false
	consumer := kernel.Define(kernel.Plugin{ID: "aiops/consumer", Inject: needAll(), Apply: func(c *kernel.Context, _ any) error {
		ran = true
		ctx := context.Background()
		pc := launch.ProfileContextKey.Of(c)
		if pc.Profile() != "self-host-full" || pc.Role() != "api" || len(pc.Layers()) != 1 {
			t.Errorf("profile %q role %q layers %v", pc.Profile(), pc.Role(), pc.Layers())
		}
		got, err := pc.Compose(ctx)
		if err != nil || got.Digest != cand.Digest {
			t.Errorf("Compose = %v, %v", got, err)
		}
		adm, _ := pc.Admit(ctx, got)
		if len(adm.Denied) != 1 || adm.Denied[0].Row != "a" {
			t.Errorf("Admit = %v", adm)
		}
		plan, _ := pc.Plan(ctx, launch.Composition{}, got)
		if plan.To != cand.Digest || len(plan.Entries) != 1 {
			t.Errorf("Plan = %v", plan)
		}

		cat := launch.CatalogKey.Of(c)
		if len(cat.Entries()) != 1 {
			t.Errorf("Entries = %v", cat.Entries())
		}
		if e, ok := cat.Lookup("aiops/mod"); !ok || e.Version != "1.0.0" {
			t.Errorf("Lookup = %v, %v", e, ok)
		}
		if _, ok := cat.Lookup("nope"); ok {
			t.Error("Lookup found an unknown id")
		}
		if p, ok := cat.Plugin("aiops/mod"); !ok || p.ID != mod.ID {
			t.Errorf("Plugin = %v, %v", p.ID, ok)
		}
		if k, ok := cat.Key("k"); !ok || k.Name() != "catalog" {
			t.Errorf("Key = %v, %v", k, ok)
		}
		if _, ok := cat.Key("nope"); ok {
			t.Error("Key found an unknown name")
		}
		if _, ok := cat.Plugin("nope"); ok {
			t.Error("Plugin found an unknown id")
		}

		cl := launch.CmdlineKey.Of(c)
		if len(cl.Args()) != 2 {
			t.Errorf("Args = %v", cl.Args())
		}
		if v, ok := cl.Flag("role"); !ok || v != "api" {
			t.Errorf("Flag = %q, %v", v, ok)
		}
		if _, ok := cl.Flag("nope"); ok {
			t.Error("Flag found an unknown name")
		}

		if v, ok := cl.Env("AIOPS_REF_X"); !ok || v != "v" {
			t.Errorf("Env = %q, %v", v, ok)
		}
		if _, ok := cl.Env("nope"); ok {
			t.Error("Env found an unknown name")
		}
		if pc.Committed().Digest != (launch.Digest{7}) {
			t.Errorf("Committed = %v", pc.Committed())
		}
		boom := errors.New("refused")
		l.CommitFn = func(c launch.Composition) error {
			if c.Digest == (launch.Digest{9}) {
				return boom
			}
			return nil
		}
		if err := pc.Commit(launch.Composition{Digest: launch.Digest{9}}); !errors.Is(err, boom) || pc.Committed().Digest != (launch.Digest{7}) {
			t.Errorf("Commit of a refused composition = %v, committed %v", err, pc.Committed().Digest)
		}
		if err := pc.Commit(cand); err != nil || pc.Committed().Digest != cand.Digest {
			t.Errorf("Commit = %v, committed %v", err, pc.Committed().Digest)
		}
		l.CommitFn = nil
		if err := pc.Commit(launch.Composition{Digest: launch.Digest{7}}); err != nil || pc.Committed().Digest != (launch.Digest{7}) {
			t.Errorf("Commit with no script = %v", err)
		}
		if req := pc.Required(); len(req.Rows) != 1 || req.Rows[0] != "r" || req.Keys[0] != "sql" {
			t.Errorf("Required = %v", req)
		}

		rd := launch.ReadyKey.Of(c)
		if rd.IsReady() {
			t.Error("ready before Set")
		}
		rd.Set()
		if !rd.IsReady() {
			t.Error("not ready after Set")
		}
		rd.Drop()
		if rd.IsReady() {
			t.Error("ready after Drop")
		}

		ks := launch.KillSwitchesKey.Of(c)
		if !ks.On(launch.AgentsPaused) || ks.On(launch.TelemetryDisabled) || ks.On("unknown") {
			t.Error("switches")
		}

		launch.AppExitKey.Of(c).RequestExit(launch.ExitRequest{Reason: launch.ExitGaveUp, Detail: "sql"})
		return nil
	}})

	root := kerneltest.New(t)
	kerneltest.Mount(root, plugin, consumer)
	if !ran {
		t.Fatal("the consumer did not run")
	}
	if ex := l.Exits(); len(ex) != 1 || ex[0].Reason != launch.ExitGaveUp || ex[0].Detail != "sql" {
		t.Fatalf("exits = %v", ex)
	}
}

// Commit takes a composition that the launcher composed, the committed one or one it
// committed before, and refuses any other with nothing changed, as boot's does.
func TestCommitRefusesACompositionItDidNotCompose(t *testing.T) {
	l, _ := fake.New()
	boot, next := launch.Composition{Digest: launch.Digest{1}}, launch.Composition{Digest: launch.Digest{2}}
	l.Current = boot
	l.ComposeFn = func(context.Context) (launch.Composition, error) { return next, nil }
	if err := l.Commit(launch.Composition{Digest: launch.Digest{9}}); err == nil || !strings.Contains(err.Error(), "not composed by this launcher") {
		t.Fatalf("Commit of a stranger = %v", err)
	}
	if err := l.Commit(next); err == nil || l.Committed().Digest != boot.Digest {
		t.Fatalf("Commit before Compose returned it = %v, committed %x", err, l.Committed().Digest[0])
	}
	if _, err := l.Compose(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []launch.Composition{next, boot, boot} { // forward, rollback, and the committed one again
		if err := l.Commit(c); err != nil || l.Committed().Digest != c.Digest {
			t.Fatalf("Commit(%x) = %v, committed %x", c.Digest[0], err, l.Committed().Digest[0])
		}
	}
	l.ComposeFn = func(context.Context) (launch.Composition, error) {
		return launch.Composition{Digest: launch.Digest{3}}, errors.New("no")
	}
	if _, err := l.Compose(t.Context()); err == nil || l.Commit(launch.Composition{Digest: launch.Digest{3}}) == nil {
		t.Fatal("a composition whose Compose failed was committed")
	}
}

// Required is the distribution's set (Needed) plus the rows of the committed
// composition marked required, for the process role (ADR-059 decision 15): it
// follows Commit.
func TestRequiredFollowsTheCommittedComposition(t *testing.T) {
	l, _ := fake.New()
	l.RoleName = "api"
	l.Modules = []launch.CatalogEntry{{ID: "aiops/sqlp", Provides: []string{"sql"}}, {ID: "aiops/q", Provides: []string{"queue"}}}
	l.Needed = launch.RequiredSet{Rows: []string{"away", "dist"}, Keys: []string{"none", "queue", "sql"}}
	must, other, plain := launch.Row{ID: "must", Required: true}, launch.Row{ID: "other", Required: true, Roles: []string{"worker"}}, launch.Row{ID: "plain"}
	away := launch.Row{ID: "away", Roles: []string{"worker"}}
	sql := launch.Row{ID: "db", Plugin: "aiops/sqlp", Roles: []string{"worker"}}
	q := launch.Row{ID: "q", Plugin: "aiops/q", Roles: []string{"worker", "api"}}
	cand := launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{must, other, plain, away, sql, q}}
	l.ComposeFn = func(context.Context) (launch.Composition, error) { return cand, nil }
	if got := l.Required(); !slices.Equal(got.Rows, []string{"away", "dist"}) || !slices.Equal(got.Keys, []string{"none", "queue", "sql"}) {
		t.Fatalf("Required before the commit = %+v", got)
	}
	if _, err := l.Compose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := l.Commit(cand); err != nil {
		t.Fatal(err)
	}
	// "other" and "away" run in another role; so does every provider of sql. A key
	// with no provider row stays.
	if got := l.Required(); !slices.Equal(got.Rows, []string{"dist", "must"}) || !slices.Equal(got.Keys, []string{"none", "queue"}) {
		t.Errorf("Required after the commit = %+v", got)
	}
	l.RoleName = launch.RoleAll
	if got := l.Required(); !slices.Equal(got.Rows, []string{"away", "dist", "must", "other"}) || !slices.Equal(got.Keys, []string{"none", "queue", "sql"}) {
		t.Errorf("Required for every role = %+v", got)
	}
}

// RequiredOf is the required set of a composition that Commit would take, by the rule
// of Required, and refuses any other.
func TestRequiredOfACandidate(t *testing.T) {
	l, _ := fake.New()
	l.Needed = launch.RequiredSet{Rows: []string{"dist"}}
	l.Current = launch.Composition{Digest: launch.Digest{1}, Rows: []launch.Row{{ID: "old", Required: true}}}
	cand := launch.Composition{Digest: launch.Digest{2}, Rows: []launch.Row{{ID: "new", Required: true}}}
	l.ComposeFn = func(context.Context) (launch.Composition, error) { return cand, nil }
	if _, err := l.RequiredOf(cand); err == nil || !strings.Contains(err.Error(), "not composed by this launcher") {
		t.Fatalf("RequiredOf before Compose returned it = %v", err)
	}
	if _, err := l.Compose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, err := l.RequiredOf(cand); err != nil || !slices.Equal(got.Rows, []string{"dist", "new"}) {
		t.Errorf("RequiredOf(candidate) = %+v, %v", got, err)
	}
	if got := l.Required(); !slices.Equal(got.Rows, []string{"dist", "old"}) {
		t.Errorf("Required = %+v: the candidate is not committed", got)
	}
}

func TestUnscriptedComposeAdmitPlanReturnZero(t *testing.T) {
	l, _ := fake.New()
	ctx := context.Background()
	if c, err := l.Compose(ctx); err != nil || c.Rows != nil {
		t.Fatalf("Compose = %v, %v", c, err)
	}
	if a, err := l.Admit(ctx, launch.Composition{}); err != nil || a.Denied != nil {
		t.Fatalf("Admit = %v, %v", a, err)
	}
	if p, err := l.Plan(ctx, launch.Composition{}, launch.Composition{}); err != nil || p.Entries != nil {
		t.Fatalf("Plan = %v, %v", p, err)
	}
}

// The six keys are provider-bound: under kernel.New with binding maps only the
// listed entry, the launcher, provides them (03-c3-kernel.md:63).
func TestKeysAreProviderBound(t *testing.T) {
	_, plugin := fake.New()
	providers := kernel.Bindings{}
	for _, k := range allKeys {
		providers[k] = []kernel.Plugin{plugin}
	}
	root := deftest.Bound(t, deftest.Config(providers, nil))
	deftest.Active(t, root.Plugin(plugin, nil))

	rogues := map[string]func(*kernel.Context) error{
		"profileContext": func(c *kernel.Context) error { return launch.ProfileContextKey.Provide(c, nil) },
		"catalog":        func(c *kernel.Context) error { return launch.CatalogKey.Provide(c, nil) },
		"cmdline":        func(c *kernel.Context) error { return launch.CmdlineKey.Provide(c, nil) },
		"ready":          func(c *kernel.Context) error { return launch.ReadyKey.Provide(c, nil) },
		"killSwitches":   func(c *kernel.Context) error { return launch.KillSwitchesKey.Provide(c, nil) },
		"appExit":        func(c *kernel.Context) error { return launch.AppExitKey.Provide(c, nil) },
	}
	for name, provide := range rogues {
		t.Run(name, func(t *testing.T) {
			r := deftest.Bound(t, deftest.Config(providers, nil))
			rogue := deftest.Plugin("aiops/rogue-"+name, nil, allKeys, nil, provide)
			if err := deftest.Failure(t, r.Plugin(rogue, nil)); !errors.Is(err, kernel.ErrBinding) {
				t.Fatalf("err = %v, want ErrBinding", err)
			}
		})
	}
}

// appExit is also consumer-bound: it resolves only for the listed entries; the
// other five launch keys resolve for any entry (03-c3-kernel.md:63).
func TestAppExitIsConsumerBound(t *testing.T) {
	_, plugin := fake.New()
	providers := kernel.Bindings{}
	for _, k := range allKeys {
		providers[k] = []kernel.Plugin{plugin}
	}
	listed := deftest.Plugin("aiops/listed", []kernel.Dep{kernel.Need(launch.AppExitKey)}, nil, nil, func(c *kernel.Context) error {
		launch.AppExitKey.Of(c).RequestExit(launch.ExitRequest{Reason: launch.ExitRestoreFail})
		return nil
	})
	stranger := deftest.Plugin("aiops/stranger", []kernel.Dep{kernel.Need(launch.AppExitKey)}, nil, nil, func(*kernel.Context) error { return nil })
	others := deftest.Plugin("aiops/others", needAll()[:5], nil, nil, func(*kernel.Context) error { return nil })

	root := deftest.Bound(t, deftest.Config(providers, kernel.Bindings{launch.AppExitKey: {listed}}))
	deftest.Active(t, root.Plugin(plugin, nil))
	deftest.Active(t, root.Plugin(listed, nil))
	deftest.Active(t, root.Plugin(others, nil))
	if err := deftest.Failure(t, root.Plugin(stranger, nil)); !errors.Is(err, kernel.ErrBinding) {
		t.Fatalf("err = %v, want ErrBinding", err)
	}
}

// A second launcher cannot provide a key that is provided already.
func TestSecondLauncherFails(t *testing.T) {
	_, first := fake.New()
	_, second := fake.New()
	root := kerneltest.New(t)
	deftest.Active(t, root.Plugin(first, nil))
	if err := deftest.Failure(t, root.Plugin(second, nil)); err == nil {
		t.Fatal("no error")
	}
}
