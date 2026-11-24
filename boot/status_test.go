// SPDX-License-Identifier: Apache-2.0

package boot

import (
	"errors"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The status carries each row's state and what it waits for, at settle and at every
// committed generation (ADR-059 decision 31 as amended; ADR-061 decision 21).

func rowsByID(rows []RowStatus) map[string]RowStatus {
	m := map[string]RowStatus{}
	for _, r := range rows {
		m[r.ID] = r
	}
	return m
}

func TestRunPublishesTheRowsAtSettle(t *testing.T) {
	bad := toy(toySpec{id: "t/bad", apply: func(*kernel.Context, any) error { return errors.New("boom") }})
	f := newFixture(t, `
- insert:
    - {id: store, plugin: t/store, roles: [api]}
    - {id: bad, plugin: t/bad, roles: [api]}
    - {id: waits, plugin: t/user, roles: [api], inject: [cache]}
    - {id: off, plugin: t/user, roles: [api], enabled: false}
`, bad)
	f.dist.RequiredRows = nil
	done := f.start(t.Context())
	f.waitReady(t, done)
	got := rowsByID(f.lastStatus().Rows)
	for id, want := range map[string]RowStatus{
		LoaderRowID: {ID: LoaderRowID, State: StateActive},
		"store":     {ID: "store", State: StateActive},
		"bad":       {ID: "bad", State: StateFailed},
		"waits":     {ID: "waits", State: StatePending, WaitsFor: []string{"cache"}},
	} {
		if g := got[id]; g.ID != want.ID || g.State != want.State || !slices.Equal(g.WaitsFor, want.WaitsFor) {
			t.Errorf("row %s = %+v, want %+v", id, g, want)
		}
	}
	if r, ok := got["off"]; ok || len(got) != 4 {
		t.Errorf("rows %+v: a disabled row is listed (%+v)", got, r)
	}
	stopAndWait(t, f, done)
}

func TestCommitPublishesTheRows(t *testing.T) {
	var l launched
	f := newFixture(t, "- insert:\n    - {id: store, plugin: t/store, roles: [api]}\n    - {id: probe, plugin: t/probe, roles: [api]}\n", probe(&l))
	bindLaunch(f, "t/probe")
	done := f.start(t.Context())
	f.waitReady(t, done)
	l.mu.Lock()
	pc := l.profile
	l.mu.Unlock()
	f.files["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte("- insert:\n    - {id: store, plugin: t/store, roles: [api]}\n    - {id: probe, plugin: t/probe, roles: [api]}\n    - {id: later, plugin: t/user, roles: [api]}\n")}
	next, err := pc.Compose(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.Commit(next); err != nil {
		t.Fatal(err)
	}
	// The toy loader mounted nothing of the new row: it has no fiber yet.
	got := rowsByID(f.lastStatus().Rows)
	if g := got["later"]; g.ID != "later" || g.State != "" || len(g.WaitsFor) != 0 || got["store"].State != StateActive {
		t.Errorf("rows after the commit = %+v", got)
	}
	stopAndWait(t, f, done)
}
