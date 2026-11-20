package boot

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// fakeRefs stands in for the config-refs row: it signals that it entered Apply, waits
// for gate, and resolves a ${env:X} reference to "resolved-X" and fails a secret one.
func fakeRefs(entered chan<- struct{}, gate <-chan struct{}) ModuleEntry {
	p := kernel.Define(kernel.Plugin{
		ID:    ConfigRefsID,
		Hooks: []kernel.HookUse{kernel.InternalConfig.Use(kernel.DefaultStage)},
		Apply: func(c *kernel.Context, _ any) error {
			entered <- struct{}{}
			select {
			case <-gate:
			case <-c.Std().Done():
				return c.Std().Err()
			}
			kernel.InternalConfig.On(c, kernel.DefaultStage, func(_ context.Context, raw any, _ kernel.Next[any]) (any, error) {
				return resolveRefs(raw)
			})
			return nil
		},
	})
	return ModuleEntry{ID: ConfigRefsID, Module: "example.test/refs", Version: "v0.1.0", Plugin: p,
		Hooks: []HookDecl{{Hook: "internal/config", StageName: "default"}}}
}

func resolveRefs(v any) (any, error) {
	switch x := v.(type) {
	case launch.Ref:
		if x.Scheme != "env" {
			return nil, errors.New("reference " + x.Scheme + ":" + x.Name + " does not resolve")
		}
		return "resolved-" + x.Name, nil
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			r, err := resolveRefs(e)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	}
	return v, nil
}

// recorder is a plugin that sends the config it was applied with.
func recorder(id string, got chan<- any) ModuleEntry {
	return toy(toySpec{id: id, apply: func(_ *kernel.Context, cfg any) error { got <- cfg; return nil }})
}

func refsFixture(t *testing.T, bundle string, extra ...ModuleEntry) *fixture {
	t.Helper()
	f := newFixture(t, bundle, extra...)
	f.dist.SealedHooks = []HookListeners{{Hook: "internal/config", Plugins: []string{ConfigRefsID}}}
	return f
}

// A row whose config holds a reference is not applied before config-refs is ACTIVE,
// and then sees the resolved string.
func TestRowWithAReferenceWaitsForConfigRefs(t *testing.T) {
	entered, gate := make(chan struct{}, 1), make(chan struct{})
	plain, withRef := make(chan any, 1), make(chan any, 1)
	f := refsFixture(t, `
- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: plain, plugin: t/plain, roles: [api]}
    - {id: user, plugin: t/rec, roles: [api], config: {note: "${env:AIOPS_REF_NOTE}", n: 1}}
`, fakeRefs(entered, gate), recorder("t/plain", plain), recorder("t/rec", withRef))
	done := f.start(t.Context())
	<-entered
	<-plain // a row with no reference applied; the row with one has had the same chance
	select {
	case cfg := <-withRef:
		t.Fatalf("the row with a reference was applied before config-refs was ACTIVE, with %v", cfg)
	default:
	}
	close(gate)
	cfg := <-withRef
	if want := map[string]any{"note": "resolved-AIOPS_REF_NOTE", "n": int64(1)}; !mapsEqual(cfg, want) {
		t.Fatalf("config %v", cfg)
	}
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
	}
	// The observer of the root sees the events of the shutdown with the context of the
	// shutdown: config-refs unloading is not a failure of the observer.
	if strings.Contains(f.errb.String(), "warning: observer") {
		t.Errorf("the shutdown made the observer fail:\n%s", f.errb.String())
	}
}

func mapsEqual(got any, want map[string]any) bool {
	m, ok := got.(map[string]any)
	if !ok || len(m) != len(want) {
		return false
	}
	for k, v := range want {
		if m[k] != v {
			return false
		}
	}
	return true
}

// A reference that does not resolve fails the row: an optional row is one grouped
// warning and boot goes on, a required row stops boot (08-profiles-roadmap-adrs.md:468).
func TestUnresolvedReference(t *testing.T) {
	for _, tc := range []struct {
		name     string
		required string
		code     int
	}{{"optional", "", -1}, {"required", ", required: true", ExitRequired}} {
		t.Run(tc.name, func(t *testing.T) {
			entered, gate := make(chan struct{}, 1), make(chan struct{})
			close(gate)
			got := make(chan any, 1)
			f := refsFixture(t, `
- insert:
    - {id: refs, plugin: aiops/config-refs, roles: [api]}
    - {id: user, plugin: t/rec, roles: [api], config: {token: "${secret:x}"}`+tc.required+`}
`, fakeRefs(entered, gate), recorder("t/rec", got))
			if tc.code < 0 {
				done := f.start(t.Context())
				f.waitReady(t, done)
				if code := stopAndWait(t, f, done); code != ExitOK {
					t.Fatalf("exit %d", code)
				}
			} else if code := f.run(t.Context()); code != tc.code {
				t.Fatalf("exit %d, stderr:\n%s", code, f.errb.String())
			}
			if !strings.Contains(f.errb.String(), `"user" is FAILED`) && !strings.Contains(f.errb.String(), `required row "user" is FAILED`) {
				t.Fatalf("stderr:\n%s", f.errb.String())
			}
			select {
			case cfg := <-got:
				t.Fatalf("the plugin was applied with %v", cfg)
			default:
			}
		})
	}
}

// With no config-refs row at all, a row with a reference stays PENDING: it is never
// applied with an unresolved reference in its config.
func TestReferenceWithoutConfigRefsStaysPending(t *testing.T) {
	got := make(chan any, 1)
	f := newFixture(t, "- insert:\n    - {id: user, plugin: t/rec, roles: [api], config: {note: \"${env:AIOPS_REF_X}\"}}\n", recorder("t/rec", got))
	done := f.start(t.Context())
	f.waitReady(t, done)
	if code := stopAndWait(t, f, done); code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(f.errb.String(), `"user" is PENDING`) {
		t.Fatalf("stderr:\n%s", f.errb.String())
	}
	select {
	case cfg := <-got:
		t.Fatalf("applied with %v", cfg)
	default:
	}
}

func TestRefsInject(t *testing.T) {
	c := comp(t, `
- insert:
    - {id: plain, plugin: t/store, roles: [api], inject: [b]}
    - {id: ref, plugin: t/store, roles: [api], inject: [b, a], config: {x: "${env:AIOPS_REF_X}"}}
    - {id: again, plugin: t/store, roles: [api], inject: [configRefsReady], config: {x: "${env:AIOPS_REF_X}"}}
    - {id: refs, plugin: aiops/config-refs, roles: [api], config: {x: "${env:AIOPS_REF_X}"}}
`)
	got := map[string][]string{}
	for _, r := range c.Rows {
		got[r.ID] = refsInject(r)
	}
	want := map[string][]string{"plain": {"b"}, "ref": {"a", "b", "configRefsReady"}, "again": {"configRefsReady"}, "refs": nil}
	for id, w := range want {
		if !slices.Equal(got[id], w) && !(len(w) == 0 && len(got[id]) == 0) {
			t.Errorf("%s: got %v, want %v", id, got[id], w)
		}
	}
	// The loader gets it through the entry, reload through the launch row.
	for _, e := range entrySpecs(c) {
		if e.ID == "ref" && !slices.Contains(e.Inject, "configRefsReady") {
			t.Errorf("entry %+v", e)
		}
	}
	for _, r := range launchComposition(c).Rows {
		if r.ID == "ref" && !slices.Contains(r.Inject, "configRefsReady") {
			t.Errorf("launch row %+v", r)
		}
	}
}
