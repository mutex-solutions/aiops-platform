// SPDX-License-Identifier: Apache-2.0
package s0workers

// TestSection10CatchesDeliberateLeaks leaks each resource class once in a live
// application and shows the section 10 checkpoint FAILS naming the right owner and
// generation, and passes again once the leak is removed (the check is not sticky).
// It needs the fixture environment (it skips without S0_ADMIN_DSN) and takes a
// few seconds: no load window, a 2 s warm-up.

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// s10LeakParkedWorker is the named function a leaked goroutine is parked in.
func s10LeakParkedWorker(stop <-chan struct{}) { <-stop }

func TestSection10CatchesDeliberateLeaks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f := openFixture(t, ctx)
	app := newFullApp(f, 0)
	t.Cleanup(app.teardown)
	x := newS10Arm(t, app, "pool", 1, false)
	x.harness(ctx)
	must(t, safeError("leak test app start", app.start(ctx)))
	report.WorkerStarted = true
	floor, err := app.idFloor(ctx)
	must(t, err)
	x.floor = floor
	app.warmPools(ctx)
	app.warmTraffic(ctx, "lk-"+f.id+"-", 2*time.Second)

	base, findings := x.take(ctx, "warmed_baseline", 0, "baseline")
	x.rep.Baseline = base
	if len(findings) != 0 {
		t.Fatalf("a clean application must meet its declared background set: %v", findings)
	}
	clean := func(name string) {
		t.Helper()
		cp, findings := x.take(ctx, name, len(x.rep.Checkpoint), "checkpoint")
		if len(findings) != 0 {
			t.Fatalf("%s: expected a clean checkpoint, got %v", name, findings)
		}
		x.rep.Checkpoint = append(x.rep.Checkpoint, cp)
	}
	failing := func(name, bound string, wants ...string) {
		t.Helper()
		cp, findings := x.take(ctx, name, 100+len(x.rep.Checkpoint), "checkpoint")
		if cp.Attempts != s10MaxAttempts {
			t.Errorf("%s: a persistent leak must survive every re-take (%d attempts)", name, cp.Attempts)
		}
		for _, want := range wants {
			if !hasFinding(findings, bound, want) {
				t.Errorf("%s: expected %s to name %q, got %v", name, bound, want, findings)
			}
		}
	}
	clean("clean at the warmed baseline")

	// goroutine parked in a named function (a plain block: a subtest would add its own tRunner goroutine to the measured set)
	func() {
		stop := make(chan struct{})
		go s10LeakParkedWorker(stop)
		failing("parked goroutine", bGoroutines, "harness-coordinator role=- entry=s0workers.s10LeakParkedWorker: expected 0, observed 1")
		close(stop)
		clean("after the parked goroutine exits")
	}()

	// goroutine of a disposed River client generation (a plain block: a subtest would add its own tRunner goroutine to the measured set)
	func() {
		oldGen := app.gens.liveMap()[roleWorkerClient]
		_, err := app.recreate(ctx, false) // client recreation: a new worker client generation
		must(t, err)
		if app.gens.liveMap()[roleWorkerClient] == oldGen {
			t.Fatal("recreation must open a new generation")
		}
		clean("after a client recreation")
		stop := make(chan struct{})
		labelled(ctx, roleWorkerClient, oldGen, func(context.Context) { go s10LeakParkedWorker(stop) })
		failing("old client generation goroutine", bDisposed, "river-worker#"+strconv.Itoa(oldGen)+" (disposed) owns 1 goroutine(s) of harness-coordinator s0workers.s10LeakParkedWorker")
		close(stop)
		clean("after the old generation's goroutine exits")
	}()

	// unclosed pgx connection from an old pool generation (a plain block: a subtest would add its own tRunner goroutine to the measured set)
	func() {
		oldGen := app.gens.liveMap()[roleCallerPool]
		pc, err := app.cur.pool.Acquire(ctx)
		must(t, safeError("acquire", err))
		hijacked := pc.Hijack() // no longer counted by the pool; nobody closes it
		_, err = app.recreate(ctx, true)
		must(t, err)
		failing("unclosed connection of an old pool generation", bDisposed, "pool-caller#"+strconv.Itoa(oldGen)+" (disposed) owns 1 server session(s)")
		must(t, safeError("close hijacked", hijacked.Close(ctx)))
		clean("after the connection is closed")
	}()

	// session left idle in transaction (a plain block: a subtest would add its own tRunner goroutine to the measured set)
	func() {
		gen := app.gens.liveMap()[roleCallerPool]
		tx, err := app.cur.pool.Begin(ctx)
		must(t, safeError("begin", err))
		_, err = tx.Exec(ctx, "SELECT 1")
		must(t, safeError("select", err))
		failing("idle in transaction", bOpenTx, "pool-caller#"+strconv.Itoa(gen)+" has 1 session(s) idle in transaction")
		cp, findings := x.take(ctx, "acquired connection", 200, "checkpoint")
		_ = cp
		if !hasFinding(findings, bConns, "pool-caller#"+strconv.Itoa(gen)+" acquired connections: expected 0, observed 1") {
			t.Errorf("the held transaction also holds an acquired connection: %v", findings)
		}
		must(t, safeError("rollback", tx.Rollback(ctx)))
		clean("after the transaction ends")
	}()

	// extra listener (a plain block: a subtest would add its own tRunner goroutine to the measured set)
	func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{ReadHeaderTimeout: time.Second}
		gen := app.gens.liveMap()[roleListener]
		labelled(ctx, roleListener, gen, func(context.Context) { go func() { _ = srv.Serve(ln) }() })
		failing("extra listener", bFDs, "listen-socket-unexpected: expected 0, observed 1")
		_, findings := x.take(ctx, "extra listener goroutine", 300, "checkpoint")
		if !hasFinding(findings, bGoroutines, "harness-coordinator role=http-server entry=s0workers.TestSection10CatchesDeliberateLeaks") {
			t.Errorf("the extra accept loop is an unexpected goroutine labelled with the live listener generation: %v", findings)
		}
		_ = srv.Close()
		clean("after the extra listener is closed")
	}()

	// a live gate stays clean through restarts (a plain block: a subtest would add its own tRunner goroutine to the measured set)
	func() {
		for _, kind := range []string{"listener", "client", "pool", "combined"} {
			_, err := app.restart(ctx, kind)
			must(t, err)
			clean("after " + kind + " restart")
		}
	}()

	x.teardown() // fails the test itself when counts do not return to the harness baseline
	if x.rep.Teardown == nil || len(x.rep.Failed) != 0 {
		t.Fatalf("teardown: %+v", x.rep.Failed)
	}
	if strings.Contains(strings.Join(x.rep.Failed, ""), "teardown") {
		t.Fatal("teardown must return to the harness baseline")
	}
}
