// SPDX-License-Identifier: Apache-2.0

package s0typedops

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Trusted test-only forwarding provider: the catalog, role setup, operations,
// participants and database completion still belong to the real PG fixture.
// This gate is reached only AFTER Host.admitCommit; it never simulates COMMIT.
type typedGateBackend struct {
	base                   *typedPGFixture
	commitEntered, release chan struct{}
	entered                sync.Once
}

type typedGateTransaction struct {
	inner transactionBackend
	gate  *typedGateBackend
}

func (g *typedGateBackend) begin(ctx context.Context, b binding) (transactionBackend, error) {
	inner, err := g.base.begin(ctx, b)
	if err != nil {
		return nil, err
	}
	return &typedGateTransaction{inner: inner, gate: g}, nil
}
func (p *typedGateTransaction) write(ctx context.Context, key, value string) error {
	return p.inner.write(ctx, key, value)
}
func (p *typedGateTransaction) read(ctx context.Context, key string) (string, bool, error) {
	return p.inner.read(ctx, key)
}
func (p *typedGateTransaction) rollback(ctx context.Context) error {
	return p.inner.rollback(ctx)
}
func (p *typedGateTransaction) commit(ctx context.Context) error {
	p.gate.entered.Do(func() { close(p.gate.commitEntered) })
	select {
	case <-p.gate.release:
		return p.inner.commit(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Done is evaluated by Host.wait only on its pending path, outside Host.mu.
// Observing it proves the asynchronous wait reached the blocking select; an
// already-canceled context alone would not distinguish a drained target.
type typedPGWaitContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *typedPGWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}
func typedPGAwait(t *testing.T, label string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

// Approved spike evidence only, not adoption or a raw-SQL authority claim.
// PostgreSQL provisioning, the exact fixture DSN and execution isolation remain
// parent-owned. Every schedule gets a fresh openTypedPG fixture; no sleeps.
func TestPgRevocationArbitration(t *testing.T) {
	typedTrack(t)
	for _, target := range []string{"entry", "scope", "view"} {
		for _, schedule := range []string{"revoke-first", "commit-first"} {
			t.Run(target+"/"+schedule, func(t *testing.T) {
				ctx := typedContext(t)
				f := openTypedPG(t, ctx)
				v, scope := f.view(t, 0, "arbitration", "tenant-a")
				commitFirst := schedule == "commit-first"
				revoke := func() error {
					switch target {
					case "entry":
						return f.host.RevokeEntry(f.entries[0])
					case "scope":
						return f.host.RevokeScope(scope)
					default:
						return f.host.Dispose(v)
					}
				}
				wait := func(c context.Context) error {
					switch target {
					case "entry":
						return f.host.WaitEntry(c, f.entries[0])
					case "scope":
						return f.host.WaitScope(c, scope)
					default:
						return f.host.WaitView(c, v)
					}
				}
				gate := &typedGateBackend{base: f, commitEntered: make(chan struct{}), release: make(chan struct{})}
				if commitFirst {
					f.host.driver = gate // Once, before Run; f.catalog is unchanged.
				}
				holdingDomain, releaseDomain := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(releaseDomain); close(gate.release) }) }
				work, cancelWork := context.WithCancel(ctx)
				waitCtx, cancelWait := context.WithCancel(ctx)
				runDone := make(chan struct{})
				var runErr, waitErr error
				var waitDone chan struct{}
				// Registered after openTypedPG: release/drain before its pool-close
				// and counter-report cleanups, including on a parent-goroutine Fatal.
				t.Cleanup(func() {
					release()
					cancelWait()
					cancelWork()
					typedPGAwait(t, "Run cleanup", runDone)
					if waitDone != nil {
						typedPGAwait(t, "wait cleanup", waitDone)
					}
				})
				before := f.counts // Plain fixture counters: never read during Run.
				go func() {
					defer close(runDone)
					domain := func(tx *Tx) error {
						if err := tx.Put(work, "arbitration", "pending"); err != nil {
							return err
						}
						if !commitFirst {
							close(holdingDomain) // Real INSERT completed, not committed.
							select {
							case <-releaseDomain:
							case <-work.Done():
								return work.Err()
							}
						}
						return nil
					}
					if !commitFirst {
						runErr = f.host.Run(work, v, domain)
						return
					}
					runErr = f.host.run(work, v, domain, func(native transactionBackend) error {
						wrapped, ok := native.(*typedGateTransaction)
						if !ok {
							return ErrInvalid
						}
						p, ok := wrapped.inner.(*typedPGTx) // Trusted participant only.
						if !ok {
							return ErrInvalid
						}
						for boundary := 1; boundary <= 3; boundary++ {
							if err := p.participant(work, boundary); err != nil {
								return err
							}
						}
						return nil
					})
				}()
				held := holdingDomain
				if commitFirst {
					held = gate.commitEntered
				}
				select {
				case <-held:
				case <-runDone:
					t.Fatalf("Run ended before its hold: SQLSTATE=%s", typedSQLState(runErr))
				case <-time.After(10 * time.Second):
					t.Fatal("Run did not reach its real transaction hold")
				}
				typedMust(t, revoke()) // External request, never a self-wait.
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				typedWantError(t, wait(canceled), context.Canceled)
				observed := &typedPGWaitContext{Context: waitCtx, observed: make(chan struct{})}
				waitDone = make(chan struct{})
				go func() { defer close(waitDone); waitErr = wait(observed) }()
				select {
				case <-observed.observed:
				case <-waitDone:
					t.Fatal("wait reported quiescence before held provider completion")
				case <-time.After(10 * time.Second):
					t.Fatal("wait did not reach its pending select")
				}
				f.host.mu.RLock()
				active, admitted := len(f.host.runs), false
				for run := range f.host.runs {
					admitted = run.view == v.token && run.commitAdmitted
				}
				f.host.mu.RUnlock()
				if active != 1 || admitted != commitFirst {
					t.Fatal("held Run/arbitration winner not represented in Host")
				}
				select {
				case <-waitDone:
					t.Fatal("wait escaped while the transaction was still held")
				default:
				}
				if f.total(t, ctx) != 0 {
					t.Fatal("independent PostgreSQL observer saw uncommitted rows")
				}
				release()
				typedPGAwait(t, "real database outcome and Run cleanup", runDone)
				if commitFirst {
					typedMust(t, runErr)
				} else {
					typedWantError(t, runErr, ErrInactive)
				}
				typedPGAwait(t, "post-cleanup quiescence", waitDone)
				typedMust(t, waitErr)
				typedMust(t, wait(ctx))
				typedMust(t, f.host.LastRollbackError())
				f.host.mu.RLock()
				remaining := len(f.host.runs)
				f.host.mu.RUnlock()
				if remaining != 0 {
					t.Fatal("quiescence retained a completed Run")
				}
				wantCommits, wantRollbacks, wantRows := 0, 1, 0
				if commitFirst {
					wantCommits, wantRollbacks, wantRows = 1, 0, 4
				}
				if f.counts.begins != before.begins+1 || f.counts.writes != before.writes+1 || f.counts.commits != before.commits+wantCommits || f.counts.rollbacks != before.rollbacks+wantRollbacks {
					t.Fatal("real provider completion counters disagree with arbitration")
				}
				var xid string
				for _, spec := range []struct{ schema, name string }{{f.entries[0].token.schema, "records"}, {f.jobs, "river_job"}, {f.outbox, "outbox"}, {f.audit, "audit"}} {
					var count int
					var writer string
					typedMust(t, f.admin.QueryRow(ctx, "SELECT count(*),coalesce(min(writer_xid::text),'') FROM "+typedTable(spec.schema, spec.name)+" WHERE tenant=$1 AND scope_id=$2", scope.token.tenant, scope.token.id).Scan(&count, &writer))
					if count != wantCommits || commitFirst && (writer == "" || xid != "" && writer != xid) {
						t.Fatal("durable participant rows or shared writer XID mismatch")
					}
					xid = writer
				}
				var exactRecord int
				typedMust(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+typedTable(f.entries[0].token.schema, "records")+" WHERE tenant=$1 AND scope_id=$2 AND key=$3 AND value=$4", scope.token.tenant, scope.token.id, "arbitration", "pending").Scan(&exactRecord))
				if exactRecord != wantCommits || f.total(t, ctx) != wantRows {
					t.Fatal("arbitration lost its exact record or left other-scope rows")
				}
				entry := 0
				if target == "entry" {
					entry = 1
				}
				good, goodScope := f.view(t, entry, "arbitration-followup", "tenant-b")
				typedMust(t, f.host.Run(ctx, good, func(tx *Tx) error {
					_, found, err := tx.Get(ctx, "arbitration")
					if err != nil {
						return err
					}
					if found {
						return errors.New("fresh scope saw revoked-scope data")
					}
					return tx.Put(ctx, "followup", "ok")
				}))
				var own int
				typedMust(t, f.admin.QueryRow(ctx, "SELECT count(*) FROM "+typedTable(f.entries[entry].token.schema, "records")+" WHERE tenant=$1 AND scope_id=$2 AND key=$3 AND value=$4", goodScope.token.tenant, goodScope.token.id, "followup", "ok").Scan(&own))
				if own != 1 || f.total(t, ctx) != wantRows+1 {
					t.Fatal("fresh live control missing or changed other scopes")
				}
			})
		}
	}
}
