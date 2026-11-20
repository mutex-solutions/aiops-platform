package boot

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

func TestParseLimits(t *testing.T) {
	l, err := ParseLimits("limits.yaml", []byte("loadDeadline: 250ms\ndrainDeadline: 1s\ndisposeDeadline: 2s\nsettleBound: 3s\nshutdownBound: 4s\nleakGrace: 5s\narchiveLimit: 7\neventWindow: 8\nobserverQueue: 9\nownerInbox: 10\nterminalLimit: 11\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := Limits{LoadDeadline: 250 * time.Millisecond, DrainDeadline: time.Second, DisposeDeadline: 2 * time.Second,
		SettleBound: 3 * time.Second, ShutdownBound: 4 * time.Second, LeakGrace: 5 * time.Second,
		ArchiveLimit: 7, EventWindow: 8, ObserverQueue: 9, OwnerInbox: 10, TerminalLimit: 11}
	if l != want {
		t.Fatalf("got %+v", l)
	}
	if l.settleBound() != 3*time.Second || l.leakGrace() != 5*time.Second {
		t.Fatal("bounds")
	}
	// A subset keeps the defaults; the derived bounds follow the deadlines.
	l, err = ParseLimits("limits.yaml", []byte("loadDeadline: 1s\n"))
	d := DefaultLimits()
	if err != nil || l.LoadDeadline != time.Second || l.DrainDeadline != d.DrainDeadline || l.ShutdownBound != d.ShutdownBound {
		t.Fatalf("subset: %v %+v", err, l)
	}
	if l.settleBound() != time.Second+d.DrainDeadline+d.DisposeDeadline || l.leakGrace() != d.DrainDeadline {
		t.Fatalf("derived bounds %v %v", l.settleBound(), l.leakGrace())
	}
	if err := l.check(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []func(*Limits){
		func(l *Limits) { l.LoadDeadline = 0 }, func(l *Limits) { l.DrainDeadline = 0 }, func(l *Limits) { l.DisposeDeadline = 0 },
		func(l *Limits) { l.ArchiveLimit = 0 }, func(l *Limits) { l.EventWindow = 0 }, func(l *Limits) { l.ObserverQueue = 0 },
		func(l *Limits) { l.OwnerInbox = 0 }, func(l *Limits) { l.TerminalLimit = 0 }, func(l *Limits) { l.ShutdownBound = 0 },
	} {
		bad := DefaultLimits()
		f(&bad)
		if bad.check() == nil {
			t.Fatalf("%+v passed", bad)
		}
	}
}

func TestParseLimitsErrors(t *testing.T) {
	for name, in := range map[string]string{
		"empty":         "",
		"not a map":     "[1]",
		"unknown":       "speed: 1s",
		"duration type": "loadDeadline: 5",
		"duration text": "loadDeadline: soon",
		"duration sign": "loadDeadline: -1s",
		"count type":    "archiveLimit: lots",
		"count zero":    "archiveLimit: 0",
		"count float":   "archiveLimit: 1.5",
		"count alias":   "archiveLimit: &a 5",
	} {
		if _, err := ParseLimits("limits.yaml", []byte(in)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestPlainConvertsEveryKind(t *testing.T) {
	v := Value{Kind: KindMap, Map: []Field{
		{Key: "a", Value: Value{Kind: KindBool, Bool: true}},
		{Key: "b", Value: Value{Kind: KindInt, Int: 3}},
		{Key: "c", Value: Value{Kind: KindFloat, Float: 1.5}},
		{Key: "d", Value: Value{Kind: KindString, Str: "s"}},
		{Key: "e", Value: Value{Kind: KindNull}},
		{Key: "f", Value: Value{Kind: KindRef, Ref: Ref{Scheme: RefEnv, Name: "AIOPS_REF_X"}}},
		{Key: "g", Value: Value{Kind: KindList, List: []Value{{Kind: KindInt, Int: 1}, {Kind: KindString, Str: "x"}}}},
	}}
	want := map[string]any{"a": true, "b": int64(3), "c": 1.5, "d": "s", "e": nil, "f": launch.Ref{Scheme: "env", Name: "AIOPS_REF_X"},
		"g": []any{int64(1), "x"}}
	if got := plain(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	if plainFields(nil) != nil || len(plainFields([]Field{{Key: "k", Value: Value{Kind: KindBool}}})) != 1 {
		t.Fatal("plainFields")
	}
}

func TestEntrySpecsAndLaunchRows(t *testing.T) {
	c := comp(t, `
- insert:
    - id: g
      plugin: core:group
      roles: [api]
      config:
        - {id: kid, plugin: t/store, roles: [api], inject: [store], isolate: [cache], intercept: {policy: {n: 1}}}
    - {id: s, plugin: t/store, roles: [api, worker], config: {ref: "${env:AIOPS_REF_X}"}, required: true}
`)
	rows := entrySpecs(WithLoader(c))
	if len(rows) != len(c.Rows) {
		t.Fatalf("the loader row must not be mounted: %d rows", len(rows))
	}
	for _, r := range rows {
		if r.ID == LoaderRowID {
			t.Fatal("loader row given to the loader")
		}
	}
	byID := map[string]int{}
	for i, r := range rows {
		byID[r.ID] = i
	}
	g, kid, s := rows[byID["g"]], rows[byID["g:kid"]], rows[byID["s"]]
	if g.Config != nil || g.Plugin != "core:group" || kid.Parent != "g" || !slices.Equal(kid.Inject, []string{"store"}) || !slices.Equal(kid.Isolate, []string{"cache"}) {
		t.Fatalf("group %+v kid %+v", g, kid)
	}
	if !reflect.DeepEqual(s.Config, map[string]any{"ref": launch.Ref{Scheme: "env", Name: "AIOPS_REF_X"}}) || !slices.Equal(s.Roles, []string{"api", "worker"}) {
		t.Fatalf("row %+v", s)
	}
	if _, ok := kid.Intercept["policy"]; !ok {
		t.Fatalf("intercept %+v", kid.Intercept)
	}
	lc := launchComposition(c)
	if len(lc.Rows) != len(c.Rows) || lc.Digest != launch.Digest(c.Digest) {
		t.Fatal("launchComposition")
	}
	if r := lc.Rows[byID["s"]]; !r.Required || !r.Enabled || r.Group {
		t.Fatalf("launch row %+v", r)
	}
}

func TestLaunchPlanConvertsEveryField(t *testing.T) {
	p := Plan{From: Digest{1}, To: Digest{2}, Closure: []string{"x"}, Entries: []PlanEntry{
		{ID: "a", Action: ActionUpdate, Changed: []FieldName{FieldConfig, FieldEnabled}, ConfigPaths: []string{"config.k"}, Effect: EffectRestart},
	}}
	got := launchPlan(p)
	want := launch.Plan{From: launch.Digest{1}, To: launch.Digest{2}, Closure: []string{"x"}, Entries: []launch.PlanEntry{
		{ID: "a", Action: launch.ActionUpdate, Changed: []string{"config", "enabled"}, ConfigPaths: []string{"config.k"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestVolatilePathsAreWrittenFromTheConfig(t *testing.T) {
	got := volatilePaths([]string{"config.timeout", "config.auth.token", "config.allow[0]", "config"})
	if want := []string{"allow.0", "auth.token", "timeout"}; !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestLayersOfIsInLayerOrder(t *testing.T) {
	c := comp(t, rows("id: a, plugin: t/store, roles: [api]"), "- {id: a, enabled: false}")
	got := layersOf(c)
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 4 || !strings.HasPrefix(got[1].Source, "patch:") {
		t.Fatalf("layers %+v", got)
	}
}

func TestLayersOfBreaksTiesBySource(t *testing.T) {
	files := fstestMap(map[string]string{
		"profiles/p/profile.yaml": "bundles: [z, a]",
		"bundles/z/bundle.yaml":   rows("id: z, plugin: t/store, roles: [api]"),
		"bundles/a/bundle.yaml":   rows("id: a, plugin: t/store, roles: [api]"),
	})
	c, _, err := Compose(Sources{Profile: "p", Home: files, Bundles: files}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := layersOf(c)
	if len(got) != 2 || got[0].Source != "bundle:a" || got[1].Source != "bundle:z" {
		t.Fatalf("layers %+v", got)
	}
}

func TestCheckName(t *testing.T) {
	if checkName(0) != "module table" || checkName(CheckKeyHookClass) != "7 Key and hook classes" {
		t.Fatalf("%q %q", checkName(0), checkName(7))
	}
}

func TestLatchPublishesOnlyChanges(t *testing.T) {
	var seen []bool
	l := &latch{changed: func(v bool) { seen = append(seen, v) }}
	l.Drop()
	l.Set()
	l.Set()
	l.Drop()
	if !slices.Equal(seen, []bool{true, false}) || l.IsReady() {
		t.Fatalf("seen %v", seen)
	}
}

func TestAppExitKeepsTheFirstRequest(t *testing.T) {
	a := appExit{reqs: make(chan launch.ExitRequest, 1)}
	a.RequestExit(launch.ExitRequest{Reason: launch.ExitGaveUp})
	a.RequestExit(launch.ExitRequest{Reason: launch.ExitRestoreFail})
	if got := <-a.reqs; got.Reason != launch.ExitGaveUp {
		t.Fatalf("got %v", got)
	}
	if exitCodeOf(launch.ExitRestoreFail) != ExitRestore {
		t.Fatal("code")
	}
}

func TestSystemClockFiresAndStops(t *testing.T) {
	var n atomic.Int32
	fired := make(chan struct{})
	SystemClock().AfterFunc(time.Millisecond, func() { n.Add(1); close(fired) })
	<-fired
	stop := SystemClock().AfterFunc(time.Hour, func() { n.Add(1) })
	if !stop() || n.Load() != 1 {
		t.Fatalf("stop or count: %d", n.Load())
	}
}

func TestPlumbing(t *testing.T) {
	// A nil writer and a nil environment are valid; Publish is optional.
	var w syncWriter
	if n, err := w.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatal("nil writer")
	}
	var buf bytes.Buffer
	w = syncWriter{w: &buf}
	_, _ = w.Write([]byte("x"))
	if buf.String() != "x" {
		t.Fatal("writer")
	}
	if envOf(nil)("X") != "" || envOf(func(string) string { return "v" })("X") != "v" {
		t.Fatal("env")
	}
	p := &publisher{}
	p.update(func(s *ProcessStatus) { s.Ready = true })
	if !p.st.Ready {
		t.Fatal("publisher")
	}
	if role, roles, err := parseRunRole(""); role != "all" || roles != nil || err != nil {
		t.Fatal("role all")
	}
	if role, roles, err := parseRunRole("api"); role != "api" || !slices.Equal(roles, []Role{RoleAPI}) || err != nil {
		t.Fatal("role api")
	}
}

func fstestMap(m map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for k, v := range m {
		out[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return out
}

// Regression: the launcher provides the six launch keys, so a required row whose
// manifest needs one is not "a need with no provider" in the static graph.
func TestGraphLeavesLaunchKeysOut(t *testing.T) {
	mods := mustMods(t, `
aiops/needy:
  needs: [profileContext, catalog, appExit]
`)
	c := comp(t, rows("id: needy, plugin: aiops/needy, roles: [api], required: true"))
	g, errs, warns := BuildGraph(c, mods, DistManifest{}, nil)
	if len(errs) != 0 || len(warns) != 0 || len(g.Edges) != 0 {
		t.Fatalf("errs %v warns %v edges %v", errs, warns, g.Edges)
	}
	if got := AuditStatic(c, mods, DistManifest{}, g, nil); len(got) != 0 {
		t.Fatalf("audit %v", got)
	}
}
