// SPDX-License-Identifier: Apache-2.0

package s0typedops

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Unit-only contract double: not SQL, PostgreSQL/RLS, River or authority evidence.
type unitRoute struct {
	entry *entryToken
	scope *scopeToken
	key   string
}
type unitDriver struct {
	rows                                                    map[unitRoute]string
	begins, writes, reads, commits, rollbacks, participants int
	bindings                                                []binding
	failWrite, failRead, failCommit, failRollback           error
	cleanupCanceled                                         bool
}
type unitTransaction struct {
	driver  *unitDriver
	binding binding
	pending map[unitRoute]string
}

func (d *unitDriver) begin(_ context.Context, b binding) (transactionBackend, error) {
	d.begins++
	d.bindings = append(d.bindings, b)
	return &unitTransaction{d, b, make(map[unitRoute]string)}, nil
}
func (u *unitTransaction) write(_ context.Context, k, v string) error {
	u.driver.writes++
	if u.driver.failWrite != nil {
		return u.driver.failWrite
	}
	u.pending[unitRoute{u.binding.entry, u.binding.scope, k}] = v
	return nil
}
func (u *unitTransaction) read(_ context.Context, k string) (string, bool, error) {
	u.driver.reads++
	if u.driver.failRead != nil {
		return "", false, u.driver.failRead
	}
	route := unitRoute{u.binding.entry, u.binding.scope, k}
	if v, ok := u.pending[route]; ok {
		return v, true, nil
	}
	v, ok := u.driver.rows[route]
	return v, ok, nil
}
func (u *unitTransaction) commit(context.Context) error {
	u.driver.commits++
	if u.driver.failCommit != nil {
		return u.driver.failCommit
	}
	for k, v := range u.pending {
		u.driver.rows[k] = v
	}
	return nil
}
func (u *unitTransaction) rollback(ctx context.Context) error {
	u.driver.rollbacks++
	u.driver.cleanupCanceled = ctx.Err() != nil
	return u.driver.failRollback
}
func unitHost(t *testing.T) (*Host, *unitDriver, Entry, Scope, View) {
	t.Helper()
	d := &unitDriver{rows: make(map[unitRoute]string)}
	h, err := NewHost(d)
	if err != nil {
		t.Fatal(err)
	}
	e, err := h.RegisterEntry("alpha", "alpha_records")
	if err != nil {
		t.Fatal(err)
	}
	s, err := h.MintScope("actor-a", "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.Bind(e, s)
	if err != nil {
		t.Fatal(err)
	}
	return h, d, e, s, v
}
func unitError(t *testing.T, got, want error) {
	t.Helper()
	if got != want {
		t.Fatalf("error = %v, want exact %v", got, want)
	}
}
func unitFollowup(t *testing.T, h *Host, v View) {
	t.Helper()
	if err := h.Run(context.Background(), v, func(tx *Tx) error {
		if err := tx.Put(context.Background(), "followup", "ok"); err != nil {
			return err
		}
		got, found, err := tx.Get(context.Background(), "followup")
		if err == nil && (!found || got != "ok") {
			t.Fatal("followup read did not match write")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnitClosedSurfaceAndInertData(t *testing.T) {
	h, d, e, _, v := unitHost(t)
	for _, typ := range []reflect.Type{reflect.TypeOf(Tx{}), reflect.TypeOf(&Tx{})} {
		for i := 0; i < typ.NumMethod(); i++ {
			name := typ.Method(i).Name
			if name != "Put" && name != "Get" {
				t.Fatalf("caller Tx exposes %s", name)
			}
		}
	}
	if reflect.TypeOf(&Tx{}).NumMethod() != 2 {
		t.Fatal("expected exactly Put/Get")
	}
	for _, value := range []any{Entry{}, Scope{}, View{}, Tx{}} {
		typ := reflect.TypeOf(value)
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).IsExported() {
				t.Fatalf("%s exposes field", typ.Name())
			}
		}
	}
	if reflect.TypeOf(View{}).NumMethod() != 0 || reflect.TypeOf(&View{}).NumMethod() != 0 {
		t.Fatal("view exposes authority methods")
	}
	if e.Name() != "alpha" || (Entry{}).Name() != "" {
		t.Fatal("display name mismatch")
	}
	// SQL-looking strings deliberately remain arbitrary data; no parser/blacklist.
	key, value := "'; SET ROLE jobs; --", "SELECT set_config('scope','foreign',true)"
	if err := h.Run(context.Background(), v, func(tx *Tx) error {
		if err := tx.Put(context.Background(), key, value); err != nil {
			return err
		}
		if err := tx.Put(context.Background(), "", ""); err != nil {
			return err
		}
		got, found, err := tx.Get(context.Background(), key)
		if got != value || !found || err != nil {
			t.Fatalf("data changed: %q %v %v", got, found, err)
		}
		_, found, err = tx.Get(context.Background(), "absent")
		if found || err != nil {
			t.Fatal("absent key reported present")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if d.commits != 1 || d.rollbacks != 0 || d.bindings[0].schema != "alpha_records" {
		t.Fatal("routing/completion mismatch")
	}
	unitFollowup(t, h, v)
}

func TestUnitIdentityAndCatalogRefusals(t *testing.T) {
	h, d, e, s, v := unitHost(t)
	_, _, foreignEntry, foreignScope, foreignView := unitHost(t)
	spoofEntry := &entryToken{owner: h, name: e.Name(), schema: e.token.schema}
	spoofEntry.active.Store(true)
	spoofScope := &scopeToken{owner: h, id: s.token.id, tenant: s.token.tenant}
	spoofScope.active.Store(true)
	for _, tc := range []struct {
		name string
		e    Entry
		s    Scope
		want error
	}{
		{"zero entry", Entry{}, s, ErrInvalid}, {"zero scope", e, Scope{}, ErrInvalid},
		{"foreign entry", foreignEntry, s, ErrForeign}, {"foreign scope", e, foreignScope, ErrForeign},
		{"same-name unissued", Entry{spoofEntry}, s, ErrCatalog}, {"unissued scope", e, Scope{spoofScope}, ErrCatalog},
	} {
		t.Run(tc.name, func(t *testing.T) { _, err := h.Bind(tc.e, tc.s); unitError(t, err, tc.want); unitFollowup(t, h, v) })
	}
	for _, tc := range []struct {
		e    Entry
		s    Scope
		want error
	}{
		{Entry{}, s, ErrInvalid}, {foreignEntry, s, ErrForeign},
		{Entry{spoofEntry}, s, ErrCatalog}, {e, Scope{}, ErrInvalid}, {e, foreignScope, ErrForeign},
	} {
		if tc.e.token != e.token {
			unitError(t, h.RevokeEntry(tc.e), tc.want)
		} else {
			unitError(t, h.RevokeScope(tc.s), tc.want)
		}
		unitFollowup(t, h, v)
	}
	for _, tc := range []struct {
		view View
		want error
	}{{View{}, ErrInvalid}, {foreignView, ErrForeign}} {
		before := d.begins
		unitError(t, h.Run(context.Background(), tc.view, func(*Tx) error { t.Fatal("invalid callback ran"); return nil }), tc.want)
		if d.begins != before {
			t.Fatal("invalid view opened provider")
		}
		unitFollowup(t, h, v)
	}
	before := d.begins
	bad := &viewToken{owner: h, binding: v.token.binding}
	bad.active.Store(true)
	unitError(t, h.Run(context.Background(), View{bad}, func(*Tx) error { return nil }), ErrCatalog)
	original := v.token.binding
	v.token.binding.schema = "other_records" // Trusted-package mutation simulates malformed metadata only.
	unitError(t, h.Run(context.Background(), v, func(*Tx) error { return nil }), ErrCatalog)
	v.token.binding = original
	if d.begins != before {
		t.Fatal("catalog refusal reached backend")
	}
	for _, schema := range []string{"", "Bad", "x.y", "x;select", "x\"", "9x", strings.Repeat("a", 64)} {
		_, err := h.RegisterEntry("bad", schema)
		unitError(t, err, ErrInvalid)
	}
	_, err := h.RegisterEntry("duplicate", e.token.schema)
	unitError(t, err, ErrCatalog)
	unitFollowup(t, h, v)
}

func TestUnitEntryScopeRoutingAndLifetime(t *testing.T) {
	h, d, e, s, v := unitHost(t)
	e2, err := h.RegisterEntry(e.Name(), "other_records")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := h.MintScope("actor-b", "tenant-b")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := h.Bind(e2, s)
	if err != nil {
		t.Fatal(err)
	}
	v3, err := h.Bind(e, s2)
	if err != nil {
		t.Fatal(err)
	}
	unitFollowup(t, h, v)
	for _, isolated := range []View{v2, v3} {
		if err := h.Run(context.Background(), isolated, func(tx *Tx) error {
			_, found, err := tx.Get(context.Background(), "followup")
			if found {
				t.Fatal("cross-binding read")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	var old *Tx
	if err := h.Run(context.Background(), v, func(tx *Tx) error { old = tx; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := h.Run(context.Background(), v, func(tx *Tx) error {
		before := d.writes
		unitError(t, old.Put(context.Background(), "reuse", "bad"), ErrInactive)
		if d.writes != before {
			t.Fatal("completed transaction reached provider")
		}
		return tx.Put(context.Background(), "reuse", "new")
	}); err != nil {
		t.Fatal(err)
	}
	unitError(t, h.Dispose(v), nil)
	unitError(t, h.Run(context.Background(), v, func(*Tx) error { return nil }), ErrInactive)
	unitFollowup(t, h, v2)
	unitError(t, h.RevokeEntry(e2), nil)
	unitError(t, h.Run(context.Background(), v2, func(*Tx) error { return nil }), ErrInactive)
	unitFollowup(t, h, v3)
	unitError(t, h.RevokeScope(s2), nil)
	unitError(t, h.Run(context.Background(), v3, func(*Tx) error { return nil }), ErrInactive)
	fresh, err := h.Bind(e, s)
	if err != nil {
		t.Fatal(err)
	}
	unitFollowup(t, h, fresh)
}

func TestUnitRollbackFailuresPanicAndCancellation(t *testing.T) {
	failure := errors.New("unit failure")
	for _, mode := range []string{"callback", "write", "read", "participant", "commit", "panic", "cancel", "revoke", "dispose"} {
		t.Run(mode, func(t *testing.T) {
			h, d, _, s, v := unitHost(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "write" {
				d.failWrite = failure
			}
			if mode == "read" {
				d.failRead = failure
			}
			if mode == "commit" {
				d.failCommit = failure
			}
			var captured *Tx
			var result error
			func() {
				defer func() {
					if p := recover(); p != nil {
						if mode != "panic" || p != failure {
							panic(p)
						}
						result = failure
					}
				}()
				result = h.run(ctx, v, func(tx *Tx) error {
					captured = tx
					err := tx.Put(ctx, "pending", "value")
					if mode != "write" && err != nil {
						return err
					}
					if mode == "read" {
						_, _, _ = tx.Get(ctx, "pending")
					}
					switch mode {
					case "callback":
						return failure
					case "panic":
						panic(failure)
					case "cancel":
						cancel()
					case "revoke":
						return h.RevokeScope(s)
					case "dispose":
						return h.Dispose(v)
					}
					return nil
				}, func(native transactionBackend) error {
					d.participants++
					if native.(*unitTransaction).pending[unitRoute{v.token.binding.entry, s.token, "pending"}] != "value" {
						t.Fatal("participant not on domain transaction")
					}
					unitError(t, captured.Put(context.Background(), "late", "no"), ErrInactive)
					if mode == "participant" {
						return failure
					}
					return nil
				})
			}()
			want := failure
			if mode == "cancel" {
				want = context.Canceled
			}
			if mode == "revoke" || mode == "dispose" {
				want = ErrInactive
			}
			unitError(t, result, want)
			if d.rollbacks != 1 || len(d.rows) != 0 || d.cleanupCanceled {
				t.Fatal("rollback/cleanup contract failed")
			}
			if mode != "commit" && d.commits != 0 {
				t.Fatal("failed work committed")
			}
			unitError(t, captured.Put(context.Background(), "late", "no"), ErrInactive)
			d.failWrite, d.failRead, d.failCommit = nil, nil, nil
			safe, err := h.MintScope("followup", "tenant")
			if err != nil {
				t.Fatal(err)
			}
			good, err := h.Bind(Entry{v.token.binding.entry}, safe)
			if err != nil {
				t.Fatal(err)
			}
			unitFollowup(t, h, good)
		})
	}
}

func TestUnitRollbackErrorObservability(t *testing.T) {
	primary := errors.New("unit primary failure")
	cleanupFailure := errors.New("unit cleanup failure")
	for _, mode := range []string{"callback", "write", "commit", "cancel", "panic"} {
		t.Run(mode, func(t *testing.T) {
			h, d, _, _, v := unitHost(t)
			unitError(t, h.LastRollbackError(), nil)
			d.failRollback = cleanupFailure
			if mode == "write" {
				d.failWrite = primary
			}
			if mode == "commit" {
				d.failCommit = primary
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var result error
			var payload any
			func() {
				defer func() { payload = recover() }()
				result = h.Run(ctx, v, func(tx *Tx) error {
					if err := tx.Put(ctx, "pending", "value"); err != nil {
						return err
					}
					switch mode {
					case "callback":
						return primary
					case "cancel":
						cancel()
					case "panic":
						panic(primary)
					}
					return nil
				})
			}()
			if mode == "panic" {
				if payload != primary {
					t.Fatalf("panic payload changed: %v", payload)
				}
			} else {
				if payload != nil {
					t.Fatalf("unexpected panic: %v", payload)
				}
				want := primary
				if mode == "cancel" {
					want = context.Canceled
				}
				if !errors.Is(result, want) || !errors.Is(result, cleanupFailure) {
					t.Fatalf("lost primary/cleanup error: %v", result)
				}
			}
			unitError(t, h.LastRollbackError(), cleanupFailure)
			if d.rollbacks != 1 || len(d.rows) != 0 || d.cleanupCanceled {
				t.Fatal("cleanup contract failed")
			}
			d.failWrite, d.failCommit, d.failRollback = nil, nil, nil
			unitError(t, h.Run(context.Background(), v, func(tx *Tx) error {
				if err := tx.Put(context.Background(), "ordinary-rollback", "value"); err != nil {
					return err
				}
				return primary
			}), primary)
			if d.rollbacks != 2 || len(d.rows) != 0 {
				t.Fatal("positive rollback follow-up failed")
			}
			unitFollowup(t, h, v)
			unitError(t, h.LastRollbackError(), cleanupFailure) // Successful cleanup does not hide a failure.
		})
	}
}

func TestUnitParticipantCommitAndConcurrentCalls(t *testing.T) {
	h, d, _, _, v := unitHost(t)
	if err := h.run(context.Background(), v, func(tx *Tx) error {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); errs <- tx.Put(context.Background(), "key", "value") }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				return err
			}
		}
		return nil
	}, func(native transactionBackend) error {
		d.participants++
		return native.write(context.Background(), "trusted", "participant")
	}); err != nil {
		t.Fatal(err)
	}
	if d.writes != 9 || d.participants != 1 || d.commits != 1 || d.rollbacks != 0 || len(d.rows) != 2 {
		t.Fatal("participant/serialization contract failed")
	}
	unitFollowup(t, h, v)
}

func TestUnitInvalidContexts(t *testing.T) {
	h, d, _, _, v := unitHost(t)
	_, err := NewHost(nil)
	unitError(t, err, ErrInvalid)
	_, err = (&Host{}).RegisterEntry("entry", "schema")
	unitError(t, err, ErrInvalid)
	unitError(t, h.Run(nil, v, func(*Tx) error { return nil }), ErrInvalid)
	unitError(t, h.Run(context.Background(), v, nil), ErrInvalid)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	unitError(t, h.Run(ctx, v, func(*Tx) error { return nil }), context.Canceled)
	if d.begins != 0 {
		t.Fatal("invalid/canceled context began transaction")
	}
	if err := h.Run(context.Background(), v, func(tx *Tx) error {
		unitError(t, tx.Put(nil, "", ""), ErrInvalid)
		unitError(t, tx.Put(ctx, "", ""), context.Canceled)
		_, _, err := tx.Get(nil, "")
		unitError(t, err, ErrInvalid)
		return tx.Put(context.Background(), "valid", "ok")
	}); err != nil {
		t.Fatal(err)
	}
	unitError(t, (&Tx{}).Put(context.Background(), "", ""), ErrInvalid)
	unitFollowup(t, h, v)
}

// These barriers are unit-only lifecycle doubles. They establish wrapper ordering,
// NOT PostgreSQL, RLS, River participation, catalog admission or SQL authority.
// No sleep or scheduler timing establishes either arbitration winner.
type unitBarrier struct {
	entered, release chan struct{}
	enter, leave     sync.Once
}

func newUnitBarrier(t *testing.T) *unitBarrier {
	t.Helper()
	b := &unitBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(b.open)
	return b
}
func (b *unitBarrier) block() {
	if b != nil {
		b.enter.Do(func() { close(b.entered) })
		<-b.release
	}
}
func (b *unitBarrier) open() {
	if b != nil {
		b.leave.Do(func() { close(b.release) })
	}
}

type unitBarrierCounts struct{ begins, commits, committed, rollbacks int }
type unitBarrierDriver struct {
	mu                                      sync.Mutex
	counts                                  unitBarrierCounts
	beginGate, beginCleanupGate             *unitBarrier
	commitGate, rollbackGate                *unitBarrier
	beginError, beginCleanupError           error
	commitError, rollbackError              error
	commitUsesContext, commitOutcomeUnknown bool
}
type unitBarrierTransaction struct{ driver *unitBarrierDriver }

func (d *unitBarrierDriver) snapshot() unitBarrierCounts {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.counts
}
func (d *unitBarrierDriver) begin(context.Context, binding) (transactionBackend, error) {
	d.mu.Lock()
	d.counts.begins++
	d.mu.Unlock()
	d.beginGate.block()
	if d.beginError != nil {
		// A trusted provider owns begin-failure cleanup before reporting failure.
		d.beginCleanupGate.block()
		return nil, errors.Join(d.beginError, d.beginCleanupError)
	}
	return &unitBarrierTransaction{d}, nil
}
func (*unitBarrierTransaction) write(context.Context, string, string) error { return nil }
func (*unitBarrierTransaction) read(context.Context, string) (string, bool, error) {
	return "", false, nil
}
func (u *unitBarrierTransaction) commit(ctx context.Context) error {
	d := u.driver
	d.mu.Lock()
	d.counts.commits++
	d.mu.Unlock()
	d.commitGate.block()
	if d.commitOutcomeUnknown {
		// Unit model of a server-side effect followed by an outcome error. A
		// later rollback attempt cannot establish that the effect was absent.
		d.mu.Lock()
		d.counts.committed++
		d.mu.Unlock()
		return d.commitError
	}
	if d.commitError != nil {
		return d.commitError
	}
	if d.commitUsesContext {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	d.mu.Lock()
	d.counts.committed++
	d.mu.Unlock()
	return nil
}
func (u *unitBarrierTransaction) rollback(context.Context) error {
	d := u.driver
	d.mu.Lock()
	d.counts.rollbacks++
	d.mu.Unlock()
	d.rollbackGate.block()
	return d.rollbackError
}
func unitBarrierHost(t *testing.T, d *unitBarrierDriver) (*Host, Entry, Scope, View) {
	t.Helper()
	h, err := NewHost(d)
	if err != nil {
		t.Fatal(err)
	}
	e, err := h.RegisterEntry("alpha", "alpha_records")
	if err != nil {
		t.Fatal(err)
	}
	s, err := h.MintScope("actor-a", "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.Bind(e, s)
	if err != nil {
		t.Fatal(err)
	}
	return h, e, s, v
}

type unitRevoker struct {
	name   string
	revoke func(*Host, Entry, Scope, View) error
	wait   func(context.Context, *Host, Entry, Scope, View) error
}

var unitRevokers = []unitRevoker{
	{"entry", func(h *Host, e Entry, _ Scope, _ View) error { return h.RevokeEntry(e) },
		func(ctx context.Context, h *Host, e Entry, _ Scope, _ View) error { return h.WaitEntry(ctx, e) }},
	{"scope", func(h *Host, _ Entry, s Scope, _ View) error { return h.RevokeScope(s) },
		func(ctx context.Context, h *Host, _ Entry, s Scope, _ View) error { return h.WaitScope(ctx, s) }},
	{"view", func(h *Host, _ Entry, _ Scope, v View) error { return h.Dispose(v) },
		func(ctx context.Context, h *Host, _ Entry, _ Scope, v View) error { return h.WaitView(ctx, v) }},
}

// Observing Done proves a wait reached its blocking select, rather than relying
// on a goroutine having been scheduled. Context methods must run outside Host.mu.
type unitObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *unitObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}
func unitBlockedWait(t *testing.T, wait func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &unitObservedContext{Context: ctx, observed: make(chan struct{})}
	result := make(chan error, 1)
	go func() { result <- wait(observed) }()
	select {
	case <-observed.observed:
		cancel()
		unitError(t, <-result, context.Canceled)
	case err := <-result:
		t.Fatalf("wait completed before the held Run/cleanup returned: %v", err)
	}
}
func unitDrainedWait(t *testing.T, wait func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := &unitObservedContext{Context: ctx, observed: make(chan struct{})}
	result := make(chan error, 1)
	go func() { result <- wait(observed) }()
	select {
	case err := <-result:
		unitError(t, err, nil)
	case <-observed.observed:
		cancel()
		<-result
		t.Fatal("targeted wait included an unrelated active Run")
	}
}
func unitNoActiveRuns(t *testing.T, h *Host) {
	t.Helper()
	h.mu.RLock()
	count := len(h.runs)
	h.mu.RUnlock()
	if count != 0 {
		t.Fatalf("retained %d completed Runs", count)
	}
}

func TestUnitRevocationWinsFinalCommitAdmission(t *testing.T) {
	for _, tc := range unitRevokers {
		t.Run(tc.name, func(t *testing.T) {
			participant, rollback := newUnitBarrier(t), newUnitBarrier(t)
			d := &unitBarrierDriver{rollbackGate: rollback}
			h, e, s, v := unitBarrierHost(t, d)
			wait := func(ctx context.Context) error { return tc.wait(ctx, h, e, s, v) }
			result := make(chan error, 1)
			go func() {
				result <- h.run(context.Background(), v, func(tx *Tx) error {
					return tx.Put(context.Background(), "pending", "value")
				}, func(transactionBackend) error { participant.block(); return nil })
			}()
			<-participant.entered // Immediately before final commit arbitration.
			unitError(t, tc.revoke(h, e, s, v), nil)
			unitBlockedWait(t, wait)
			participant.open()
			<-rollback.entered
			unitBlockedWait(t, wait) // Rollback entry is not cleanup completion.
			if got := d.snapshot(); got.commits != 0 || got.rollbacks != 1 {
				t.Fatalf("revocation-first completion = %+v", got)
			}
			rollback.open()
			unitError(t, <-result, ErrInactive)
			unitError(t, wait(context.Background()), nil)
			unitError(t, wait(context.Background()), nil)
			unitError(t, h.Run(context.Background(), v, func(*Tx) error { return nil }), ErrInactive)
			unitNoActiveRuns(t, h)
		})
	}
}

func TestUnitCommitAdmissionWinsAndWaitClosesOldCheckCommitGap(t *testing.T) {
	for _, tc := range unitRevokers {
		t.Run(tc.name, func(t *testing.T) {
			commit := newUnitBarrier(t)
			d := &unitBarrierDriver{commitGate: commit}
			h, e, s, v := unitBarrierHost(t, d)
			wait := func(ctx context.Context) error { return tc.wait(ctx, h, e, s, v) }
			result := make(chan error, 1)
			go func() { result <- h.Run(context.Background(), v, func(*Tx) error { return nil }) }()
			// The old implementation's final resolve has returned here, but the
			// provider has not committed. Revocation alone never closed this gap.
			<-commit.entered
			unitError(t, tc.revoke(h, e, s, v), nil)
			unitBlockedWait(t, wait)
			if got := d.snapshot(); got.committed != 0 {
				t.Fatalf("barrier did not hold commit: %+v", got)
			}
			commit.open()
			unitError(t, <-result, nil)
			unitError(t, wait(context.Background()), nil)
			unitError(t, wait(context.Background()), nil)
			if got := d.snapshot(); got.commits != 1 || got.committed != 1 || got.rollbacks != 0 {
				t.Fatalf("commit-first completion = %+v", got)
			}
			unitNoActiveRuns(t, h)
		})
	}
}

func TestUnitWaitIncludesBeginAndProviderBeginFailureCleanup(t *testing.T) {
	primary, cleanupFailure := errors.New("unit begin failed"), errors.New("unit begin cleanup failed")
	for _, tc := range unitRevokers {
		for _, mode := range []string{"success", "error", "cleanup-error"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				begin, providerCleanup := newUnitBarrier(t), newUnitBarrier(t)
				d := &unitBarrierDriver{beginGate: begin, beginCleanupGate: providerCleanup}
				if mode != "success" {
					d.beginError = primary
				}
				if mode == "cleanup-error" {
					d.beginCleanupError = cleanupFailure
				}
				h, e, s, v := unitBarrierHost(t, d)
				wait := func(ctx context.Context) error { return tc.wait(ctx, h, e, s, v) }
				result := make(chan error, 1)
				called := make(chan struct{}, 1)
				go func() {
					result <- h.Run(context.Background(), v, func(*Tx) error { called <- struct{}{}; return nil })
				}()
				<-begin.entered
				unitError(t, tc.revoke(h, e, s, v), nil)
				unitBlockedWait(t, wait) // Admission must precede provider begin.
				begin.open()
				if mode != "success" {
					<-providerCleanup.entered
					unitBlockedWait(t, wait)
					providerCleanup.open()
				}
				err := <-result
				if mode == "success" {
					unitError(t, err, ErrInactive)
				} else if !errors.Is(err, primary) || mode == "cleanup-error" && !errors.Is(err, cleanupFailure) {
					t.Fatalf("lost provider begin/cleanup error: %v", err)
				}
				select {
				case <-called:
					t.Fatal("revoked begin reached domain callback")
				default:
				}
				unitError(t, wait(context.Background()), nil)
				wantRollbacks := 0
				if mode == "success" {
					wantRollbacks = 1
				}
				if got := d.snapshot(); got.begins != 1 || got.commits != 0 || got.rollbacks != wantRollbacks {
					t.Fatalf("begin lifecycle = %+v", got)
				}
				unitNoActiveRuns(t, h)
			})
		}
	}
}

func TestUnitCallbackCanRequestRevocationAndPanicCleanupStaysVisible(t *testing.T) {
	cleanupFailure := errors.New("unit rollback failed")
	payload := &struct{ message string }{"original panic identity"}
	for _, tc := range unitRevokers {
		for _, panics := range []bool{false, true} {
			name := tc.name + "/return"
			if panics {
				name = tc.name + "/panic"
			}
			t.Run(name, func(t *testing.T) {
				callback, rollback := newUnitBarrier(t), newUnitBarrier(t)
				d := &unitBarrierDriver{rollbackGate: rollback, rollbackError: cleanupFailure}
				h, e, s, v := unitBarrierHost(t, d)
				wait := func(ctx context.Context) error { return tc.wait(ctx, h, e, s, v) }
				type outcome struct {
					err     error
					payload any
				}
				result := make(chan outcome, 1)
				request := make(chan error, 1)
				var captured *Tx
				go func() {
					var got outcome
					defer func() { got.payload = recover(); result <- got }()
					got.err = h.Run(context.Background(), v, func(tx *Tx) error {
						captured = tx
						request <- tc.revoke(h, e, s, v) // Request, never a self-wait.
						callback.block()
						if panics {
							panic(payload)
						}
						return nil
					})
				}()
				unitError(t, <-request, nil)
				<-callback.entered
				unitBlockedWait(t, wait)
				callback.open()
				<-rollback.entered
				unitBlockedWait(t, wait)
				rollback.open()
				got := <-result
				if panics {
					if got.payload != payload {
						t.Fatalf("panic identity changed: %v", got.payload)
					}
				} else if got.payload != nil || !errors.Is(got.err, ErrInactive) || !errors.Is(got.err, cleanupFailure) {
					t.Fatalf("lost revoke/cleanup outcome: %+v", got)
				}
				unitError(t, h.LastRollbackError(), cleanupFailure)
				unitError(t, captured.Put(context.Background(), "late", "no"), ErrInactive)
				unitError(t, wait(context.Background()), nil)
				unitError(t, wait(context.Background()), nil)
				if got := d.snapshot(); got.commits != 0 || got.rollbacks != 1 {
					t.Fatalf("panic/return completion = %+v", got)
				}
				unitNoActiveRuns(t, h)
			})
		}
	}
}

func TestUnitWaitValidatesExactClosedTarget(t *testing.T) {
	h, d, e, s, v := unitHost(t)
	_, _, foreignEntry, foreignScope, foreignView := unitHost(t)
	spoofEntry := Entry{&entryToken{owner: h, name: e.token.name, schema: e.token.schema}}
	spoofScope := Scope{&scopeToken{owner: h, id: s.token.id, tenant: s.token.tenant}}
	spoofView := View{&viewToken{owner: h, binding: v.token.binding}}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		wait func() error
		want error
	}{
		{"zero-entry", func() error { return h.WaitEntry(ctx, Entry{}) }, ErrInvalid},
		{"zero-scope", func() error { return h.WaitScope(ctx, Scope{}) }, ErrInvalid},
		{"zero-view", func() error { return h.WaitView(ctx, View{}) }, ErrInvalid},
		{"foreign-entry", func() error { return h.WaitEntry(ctx, foreignEntry) }, ErrForeign},
		{"foreign-scope", func() error { return h.WaitScope(ctx, foreignScope) }, ErrForeign},
		{"foreign-view", func() error { return h.WaitView(ctx, foreignView) }, ErrForeign},
		{"unissued-entry", func() error { return h.WaitEntry(ctx, spoofEntry) }, ErrCatalog},
		{"unissued-scope", func() error { return h.WaitScope(ctx, spoofScope) }, ErrCatalog},
		{"unissued-view", func() error { return h.WaitView(ctx, spoofView) }, ErrCatalog},
		{"live-entry", func() error { return h.WaitEntry(ctx, e) }, ErrInvalid},
		{"live-scope", func() error { return h.WaitScope(ctx, s) }, ErrInvalid},
		{"live-view", func() error { return h.WaitView(ctx, v) }, ErrInvalid},
		{"nil-context-entry", func() error { return h.WaitEntry(nil, e) }, ErrInvalid},
		{"nil-context-scope", func() error { return h.WaitScope(nil, s) }, ErrInvalid},
		{"nil-context-view", func() error { return h.WaitView(nil, v) }, ErrInvalid},
		{"nil-host-entry", func() error { return (*Host)(nil).WaitEntry(ctx, e) }, ErrInvalid},
		{"nil-host-scope", func() error { return (*Host)(nil).WaitScope(ctx, s) }, ErrInvalid},
		{"nil-host-view", func() error { return (*Host)(nil).WaitView(ctx, v) }, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := d.begins
			unitError(t, tc.wait(), tc.want)
			if d.begins != before {
				t.Fatal("wait invoked provider")
			}
			unitFollowup(t, h, v)
		})
	}
	original := v.token.binding
	v.token.binding.schema = "malformed_records"
	unitError(t, h.WaitView(ctx, v), ErrCatalog)
	v.token.binding = original
	unitFollowup(t, h, v)
	unitError(t, h.RevokeEntry(e), nil)
	unitError(t, h.WaitEntry(ctx, e), nil)
	unitError(t, h.WaitScope(ctx, s), ErrInvalid) // Closing an entry does not revoke its scope.
	unitError(t, h.WaitView(ctx, v), nil)         // The exact revoked ancestor closes this view.
	unitError(t, h.RevokeScope(s), nil)
	unitError(t, h.WaitScope(ctx, s), nil)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	unitError(t, h.WaitEntry(canceled, e), context.Canceled)
	unitError(t, h.WaitScope(canceled, s), context.Canceled)
	unitError(t, h.WaitView(canceled, v), context.Canceled)
	unitError(t, h.WaitView(ctx, v), nil)
	unitNoActiveRuns(t, h)
}

func TestUnitTargetedWaitIgnoresUnrelatedViewAndWakesAllWaiters(t *testing.T) {
	for _, tc := range unitRevokers {
		t.Run(tc.name, func(t *testing.T) {
			target, unrelated := newUnitBarrier(t), newUnitBarrier(t)
			d := &unitBarrierDriver{}
			h, e, s, v := unitBarrierHost(t, d)
			otherEntry, otherScope := e, s
			if tc.name != "view" {
				var err error
				otherEntry, err = h.RegisterEntry(e.Name(), "other_records")
				if err != nil {
					t.Fatal(err)
				}
				// Same display identity, distinct exact registered scope token.
				otherScope, err = h.MintScope(s.token.id, s.token.tenant)
				if err != nil {
					t.Fatal(err)
				}
			}
			otherView, err := h.Bind(otherEntry, otherScope)
			if err != nil {
				t.Fatal(err)
			}
			firstResult, otherResult := make(chan error, 1), make(chan error, 1)
			go func() {
				firstResult <- h.Run(context.Background(), v, func(*Tx) error { target.block(); return nil })
			}()
			go func() {
				otherResult <- h.Run(context.Background(), otherView, func(*Tx) error { unrelated.block(); return nil })
			}()
			<-target.entered
			<-unrelated.entered
			unitError(t, tc.revoke(h, e, s, v), nil)
			wait := func(ctx context.Context) error { return tc.wait(ctx, h, e, s, v) }
			unitBlockedWait(t, wait)
			waitCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			waiters := make([]chan error, 4)
			for i := range waiters {
				observed := &unitObservedContext{Context: waitCtx, observed: make(chan struct{})}
				result := make(chan error, 1)
				waiters[i] = result
				go func() { result <- wait(observed) }()
				select {
				case <-observed.observed:
				case err := <-result:
					t.Fatalf("waiter returned before target callback completed: %v", err)
				}
			}
			target.open()
			unitError(t, <-firstResult, ErrInactive)
			for _, result := range waiters {
				unitError(t, <-result, nil)
			}
			unitDrainedWait(t, wait)
			unitError(t, wait(context.Background()), nil)
			unitError(t, h.WaitView(context.Background(), v), nil)
			h.mu.RLock()
			remaining := len(h.runs)
			h.mu.RUnlock()
			if remaining != 1 {
				t.Fatalf("active unrelated Runs = %d, want 1", remaining)
			}
			unrelated.open()
			unitError(t, <-otherResult, nil)
			unitNoActiveRuns(t, h)
		})
	}
}

func TestUnitCommitAdmissionDoesNotConfirmDatabaseOutcome(t *testing.T) {
	unknown, cleanupFailure := errors.New("unit commit outcome unknown"), errors.New("unit cleanup failed")
	for _, mode := range []string{"commit-error", "unknown-after-effect", "caller-canceled"} {
		t.Run(mode, func(t *testing.T) {
			commit, rollback := newUnitBarrier(t), newUnitBarrier(t)
			d := &unitBarrierDriver{commitGate: commit, rollbackGate: rollback, rollbackError: cleanupFailure}
			switch mode {
			case "commit-error":
				d.commitError = unknown
			case "unknown-after-effect":
				d.commitOutcomeUnknown, d.commitError = true, unknown
			case "caller-canceled":
				d.commitUsesContext = true
			}
			h, _, _, v := unitBarrierHost(t, d)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- h.Run(ctx, v, func(*Tx) error { return nil }) }()
			<-commit.entered
			unitError(t, h.Dispose(v), nil)
			wait := func(ctx context.Context) error { return h.WaitView(ctx, v) }
			unitBlockedWait(t, wait)
			if mode == "caller-canceled" {
				cancel() // The actual caller context reaches provider commit.
			}
			commit.open()
			<-rollback.entered
			unitBlockedWait(t, wait)
			rollback.open()
			err := <-result
			want := unknown
			if mode == "caller-canceled" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || !errors.Is(err, cleanupFailure) {
				t.Fatalf("lost commit/cancel/cleanup outcome: %v", err)
			}
			unitError(t, h.LastRollbackError(), cleanupFailure)
			unitError(t, wait(context.Background()), nil)
			wantEffects := 0
			if mode == "unknown-after-effect" {
				wantEffects = 1
			}
			if got := d.snapshot(); got.commits != 1 || got.rollbacks != 1 || got.committed != wantEffects {
				t.Fatalf("commit outcome = %+v", got)
			}
			unitNoActiveRuns(t, h)
		})
	}
}
