// SPDX-License-Identifier: Apache-2.0

// Package s0typedops is an experimental closed record-operation surface, not an
// adopted SQL contract or a sandbox against hostile native Go. Only trusted host
// and provider code may hold Host or implement its private backend. Domain code
// receives a View and, inside Run, a Tx; neither carries an issuer or driver API.
package s0typedops

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrInvalid  = errors.New("s0typedops: invalid capability or argument")
	ErrInactive = errors.New("s0typedops: inactive capability or transaction")
	ErrForeign  = errors.New("s0typedops: foreign issuer")
	ErrCatalog  = errors.New("s0typedops: catalog mismatch")
)

// Entry and Scope are opaque issuer identities, not names supplied by a caller.
// Copying a valid value preserves its authority; copying a name creates none.
type Entry struct{ token *entryToken }
type Scope struct{ token *scopeToken }
type View struct{ token *viewToken }
type Tx struct{ state *transactionState }

type entryToken struct {
	owner        *Host
	name, schema string
	active       atomic.Bool
}
type scopeToken struct {
	owner      *Host
	id, tenant string
	active     atomic.Bool
}
type binding struct {
	entry  *entryToken
	scope  *scopeToken
	schema string
}
type viewToken struct {
	owner   *Host
	binding binding
	active  atomic.Bool
}

// backend is implementable only by trusted code in this package. The provider
// resolves the registered schema and fixed record table; key/value and the
// scope's id/tenant are data parameters, never executable text or identifiers.
// If begin fails or panics after acquiring resources, the trusted provider owns
// their cleanup before returning/unwinding, and must retain unexpected cleanup
// errors in its reported failure. Run cannot roll back a transaction not returned.
type backend interface {
	begin(context.Context, binding) (transactionBackend, error)
}
type transactionBackend interface {
	write(context.Context, string, string) error
	read(context.Context, string) (string, bool, error)
	commit(context.Context) error
	// rollback must normalize an expected already-closed transaction to nil;
	// unexpected cleanup failures remain observable to the trusted host.
	rollback(context.Context) error
}

// Host is trusted issuance/transaction authority. Never inject it into a domain
// plugin or derive MintScope inputs from untrusted job/request identity claims.
type Host struct {
	mu              sync.RWMutex
	driver          backend
	entries         map[*entryToken]struct{}
	scopes          map[*scopeToken]struct{}
	schemas         map[string]*entryToken
	views           map[*viewToken]binding
	runs            map[*activeRun]struct{}
	progress        chan struct{} // Closed/replaced under mu after actual Run cleanup.
	rollbackFailure error         // Most recent unexpected failure, retained across successes.
}

// activeRun lives only from admission until Run unwinds, including provider begin,
// commit/rollback and panic cleanup. It is not a completed-outcome history.
type activeRun struct {
	view           *viewToken
	binding        binding
	commitAdmitted bool // Arbitration winner, NOT a confirmed database outcome.
}

func NewHost(driver backend) (*Host, error) {
	if driver == nil {
		return nil, ErrInvalid
	}
	return &Host{driver: driver, entries: make(map[*entryToken]struct{}),
		scopes: make(map[*scopeToken]struct{}), schemas: make(map[string]*entryToken),
		views: make(map[*viewToken]binding), runs: make(map[*activeRun]struct{}),
		progress: make(chan struct{})}, nil
}

// LastRollbackError is a trusted-host diagnostic, including when Run panics.
// It retains the most recent unexpected cleanup failure, is not per-run or an
// exhaustive concurrent error log, and never exposes a transaction or driver.
func (h *Host) LastRollbackError() error {
	if h == nil {
		return ErrInvalid
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rollbackFailure
}

// Name is display metadata only; it is never an authorization lookup key.
func (e Entry) Name() string {
	if e.token == nil {
		return ""
	}
	return e.token.name
}

// RegisterEntry installs the sole closed record catalog for an entry. Schema
// syntax/63-byte bound prevents PostgreSQL identifier truncation aliases; this
// is trusted administration, not a SQL blacklist. A provider must still quote
// identifiers and separately control migration/DDL.
func (h *Host) RegisterEntry(name, schema string) (Entry, error) {
	if h == nil || h.driver == nil || name == "" || !schemaName(schema) {
		return Entry{}, ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.schemas[schema]; exists {
		return Entry{}, ErrCatalog
	}
	t := &entryToken{owner: h, name: name, schema: schema}
	t.active.Store(true)
	h.entries[t] = struct{}{}
	h.schemas[schema] = t
	return Entry{t}, nil
}

func schemaName(s string) bool {
	if len(s) == 0 || len(s) > 63 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// MintScope accepts already-authenticated host identity. Empty identity is not
// an authenticated scope; all nonempty contents are data, not catalog controls.
func (h *Host) MintScope(id, tenant string) (Scope, error) {
	if h == nil || h.driver == nil || id == "" || tenant == "" {
		return Scope{}, ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	t := &scopeToken{owner: h, id: id, tenant: tenant}
	t.active.Store(true)
	h.scopes[t] = struct{}{}
	return Scope{t}, nil
}

// Identity helpers require mu and deliberately accept registered revoked tokens.
func (h *Host) entryIdentityLocked(e *entryToken) error {
	if e == nil {
		return ErrInvalid
	}
	if e.owner != h {
		return ErrForeign
	}
	if _, ok := h.entries[e]; !ok || h.schemas[e.schema] != e {
		return ErrCatalog
	}
	return nil
}
func (h *Host) checkEntry(e *entryToken) error {
	if err := h.entryIdentityLocked(e); err != nil {
		return err
	}
	if !e.active.Load() {
		return ErrInactive
	}
	return nil
}
func (h *Host) scopeIdentityLocked(s *scopeToken) error {
	if s == nil {
		return ErrInvalid
	}
	if s.owner != h {
		return ErrForeign
	}
	if _, ok := h.scopes[s]; !ok {
		return ErrCatalog
	}
	return nil
}
func (h *Host) checkScope(s *scopeToken) error {
	if err := h.scopeIdentityLocked(s); err != nil {
		return err
	}
	if !s.active.Load() {
		return ErrInactive
	}
	return nil
}

func (h *Host) Bind(entry Entry, scope Scope) (View, error) {
	if h == nil {
		return View{}, ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.checkEntry(entry.token); err != nil {
		return View{}, err
	}
	if err := h.checkScope(scope.token); err != nil {
		return View{}, err
	}
	b := binding{entry.token, scope.token, entry.token.schema}
	t := &viewToken{owner: h, binding: b}
	t.active.Store(true)
	h.views[t] = b
	return View{t}, nil
}

func (h *Host) viewIdentityLocked(v View) (binding, error) {
	if v.token == nil {
		return binding{}, ErrInvalid
	}
	if v.token.owner != h {
		return binding{}, ErrForeign
	}
	b, ok := h.views[v.token]
	if !ok || b != v.token.binding {
		return binding{}, ErrCatalog
	}
	if err := h.entryIdentityLocked(b.entry); err != nil {
		return binding{}, err
	}
	if err := h.scopeIdentityLocked(b.scope); err != nil {
		return binding{}, err
	}
	if b.schema != b.entry.schema {
		return binding{}, ErrCatalog
	}
	return b, nil
}

// resolveLocked is shared by ordinary checks and atomic admission/arbitration.
// Its caller already holds mu; never acquire it recursively.
func (h *Host) resolveLocked(v View) (binding, error) {
	b, err := h.viewIdentityLocked(v)
	if err != nil {
		return binding{}, err
	}
	if !v.token.active.Load() || !b.entry.active.Load() || !b.scope.active.Load() {
		return binding{}, ErrInactive
	}
	return b, nil
}
func (h *Host) resolve(v View) (binding, error) {
	if h == nil {
		return binding{}, ErrInvalid
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.resolveLocked(v)
}

// RevokeEntry closes admission without waiting or aborting in-flight work.
// RevokeScope and Dispose have the same contract: success is not a drain ACK.
// A Run loses commit admission if revocation wins the shared Host lock first;
// if commit admission wins first, its database outcome is allowed to resolve.
// No Host lock is held across provider or domain calls. A domain callback may
// request revocation, but must not wait for its own Run (see WaitEntry).
func (h *Host) RevokeEntry(e Entry) error {
	if h == nil {
		return ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.checkEntry(e.token); err != nil {
		return err
	}
	e.token.active.Store(false)
	return nil
}
func (h *Host) RevokeScope(s Scope) error {
	if h == nil {
		return ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.checkScope(s.token); err != nil {
		return err
	}
	s.token.active.Store(false)
	return nil
}
func (h *Host) Dispose(v View) error {
	if h == nil {
		return ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.resolveLocked(v); err != nil {
		return err
	}
	v.token.active.Store(false)
	return nil
}

// WaitEntry is an explicit trusted-host quiescence wait for an already revoked
// entry; a live target is ErrInvalid. It returns only after all its admitted
// Runs, including begin, database outcome and cleanup, have unwound. A nil result
// is not a claim that every Run
// committed or rolled back successfully: Run/LastRollbackError retain failures.
// Waits are external-only: never wait from a callback/provider/participant for
// that callback's own Run. No goroutine-ID detector enforces this host contract.
// A canceled wait reports incomplete quiescence, never synthetic completion.
func (h *Host) WaitEntry(ctx context.Context, e Entry) error {
	return h.wait(ctx, e.token, nil, nil)
}

// WaitScope has WaitEntry's external-only contract and requires a revoked scope.
func (h *Host) WaitScope(ctx context.Context, s Scope) error {
	return h.wait(ctx, nil, s.token, nil)
}

// WaitView has WaitEntry's external-only contract. Its exact view must be disposed
// or its exact entry/scope revoked. An unrelated view never extends this wait.
func (h *Host) WaitView(ctx context.Context, v View) error {
	return h.wait(ctx, nil, nil, v.token)
}

func (h *Host) wait(ctx context.Context, e *entryToken, s *scopeToken, v *viewToken) error {
	if h == nil || ctx == nil {
		return ErrInvalid
	}
	h.mu.Lock()
	var err error
	switch {
	case e != nil:
		err = h.entryIdentityLocked(e)
		if err == nil && e.active.Load() {
			err = ErrInvalid
		}
	case s != nil:
		err = h.scopeIdentityLocked(s)
		if err == nil && s.active.Load() {
			err = ErrInvalid
		}
	case v != nil:
		var b binding
		b, err = h.viewIdentityLocked(View{v})
		if err == nil && v.active.Load() && b.entry.active.Load() && b.scope.active.Load() {
			err = ErrInvalid
		}
	default:
		err = ErrInvalid
	}
	if err != nil {
		h.mu.Unlock()
		return err
	}
	for {
		pending := false
		for run := range h.runs {
			if e != nil && run.binding.entry == e || s != nil && run.binding.scope == s || v != nil && run.view == v {
				pending = true
				break
			}
		}
		progress := h.progress // Snapshot with pending under the same mutex.
		h.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-progress:
		}
		h.mu.Lock()
	}
}

func (h *Host) admitRun(v View) (*activeRun, error) {
	if h == nil {
		return nil, ErrInvalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := h.resolveLocked(v)
	if err != nil {
		return nil, err
	}
	run := &activeRun{view: v.token, binding: b}
	h.runs[run] = struct{}{}
	return run, nil
}
func (h *Host) admitCommit(run *activeRun) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.runs[run]; !ok || run.commitAdmitted {
		return ErrInactive
	}
	b, err := h.resolveLocked(View{run.view})
	if err != nil {
		return err
	}
	if b != run.binding {
		return ErrCatalog
	}
	run.commitAdmitted = true
	return nil
}
func (h *Host) finishRun(run *activeRun) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.runs, run)
	close(h.progress)
	h.progress = make(chan struct{})
}

type transactionState struct {
	mu      sync.Mutex // The provider's transaction is never used concurrently.
	host    *Host
	view    View
	ctx     context.Context
	backend transactionBackend
	active  bool
	failed  error
}

// Run owns transaction completion. The callback receives only the closed Tx.
// Reusing an undisposed View opens a fresh transaction, never a completed Tx.
// Admission is tracked before provider begin and removed after cleanup unwinds.
// Commit admission is not a database outcome: provider commit errors, including
// cancellation/unknown outcomes, remain errors even if rollback is attempted.
func (h *Host) Run(ctx context.Context, v View, fn func(*Tx) error) error {
	return h.run(ctx, v, fn, nil)
}

// run enlists trusted infrastructure after domain work, before commit. Only
// same-package provider code may recover its private native transaction adapter
// here for unmodified River/outbox/audit. Never expose this callback to a plugin.
func (h *Host) run(ctx context.Context, v View, fn func(*Tx) error, participant func(transactionBackend) error) (result error) {
	if ctx == nil || fn == nil {
		return ErrInvalid
	}
	run, err := h.admitRun(v)
	if err != nil {
		return err
	}
	defer h.finishRun(run) // Registered first, so provider/Tx cleanup unwinds first.
	if err := ctx.Err(); err != nil {
		return err
	}
	native, err := h.driver.begin(ctx, run.binding)
	if err != nil {
		return err
	}
	if native == nil {
		return ErrInvalid
	}
	s := &transactionState{host: h, view: v, ctx: ctx, backend: native, active: true}
	committed := false
	defer func() {
		s.mu.Lock()
		s.active = false
		s.mu.Unlock()
		if !committed {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if cleanupErr := native.rollback(cleanup); cleanupErr != nil {
				h.mu.Lock()
				h.rollbackFailure = cleanupErr
				h.mu.Unlock()
				result = errors.Join(result, cleanupErr)
			}
		}
	}()
	if _, err := h.resolve(v); err != nil {
		return err
	}
	err = fn(&Tx{s}) // Deferred cleanup also runs on panic; the panic is not rewritten.
	s.mu.Lock()
	s.active = false
	failed := s.failed
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if failed != nil {
		return failed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := h.resolve(v); err != nil {
		return err
	}
	if participant != nil {
		if err := participant(native); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := h.admitCommit(run); err != nil {
		return err
	}
	if err := native.commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *transactionState) check(ctx context.Context) error {
	if !s.active {
		return ErrInactive
	}
	if s.failed != nil {
		return s.failed
	}
	if ctx == nil {
		return ErrInvalid
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.host.resolve(s.view)
	return err
}

// operationContext preserves caller values/deadlines while host cancellation
// also cancels provider work; a caller cannot detach work with Background.
func (s *transactionState) operationContext(ctx context.Context) (context.Context, func()) {
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	return work, func() { stop(); cancel() }
}

// Put stores any string key/value, including empty or SQL-looking data.
func (t *Tx) Put(ctx context.Context, key, value string) error {
	if t == nil || t.state == nil {
		return ErrInvalid
	}
	s := t.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return err
	}
	work, cancel := s.operationContext(ctx)
	defer cancel()
	err := s.backend.write(work, key, value)
	if err != nil {
		s.failed = err
	}
	return err
}

// Get returns found=false for an absent key. It has no query/identifier variant.
func (t *Tx) Get(ctx context.Context, key string) (string, bool, error) {
	if t == nil || t.state == nil {
		return "", false, ErrInvalid
	}
	s := t.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return "", false, err
	}
	work, cancel := s.operationContext(ctx)
	defer cancel()
	value, found, err := s.backend.read(work, key)
	if err != nil {
		s.failed = err
	}
	return value, found, err
}
