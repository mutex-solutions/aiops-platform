// SPDX-License-Identifier: Apache-2.0

package concurrency

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func configSnapshotIs(t *testing.T, cell *Configuration, wantRaw, wantEffective any, wantVersion uint64) {
	t.Helper()
	raw, effective, version := cell.RawSnapshot()
	if raw != wantRaw || effective != wantEffective || version != wantVersion {
		t.Fatalf("snapshot = (%v, %v, %d), want (%v, %v, %d)", raw, effective, version, wantRaw, wantEffective, wantVersion)
	}
}

func configPrepare(t *testing.T, cell *Configuration) PreparedConfig {
	t.Helper()
	prepared, err := cell.Prepare(context.Background())
	if err != nil || !prepared.Published {
		t.Fatalf("Prepare = %+v, %v", prepared, err)
	}
	return prepared
}

func configAwait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("configuration callback did not finish (possible lock held across callback)")
		return nil
	}
}

func configSignal(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("configuration callback did not start")
	}
}

func TestConfigurationPreparePublishesBeforeApplyAndRetainsIdentity(t *testing.T) {
	// Non-serializable contents are deliberate: no schema, clone or JSON fallback
	// may replace the exact resolver result handed to the activation.
	raw := &struct{ Name string }{"input"}
	value := &struct {
		Invoke func() string
		Ready  chan struct{}
	}{func() string { return "ready" }, make(chan struct{})}
	calls := 0
	cell := NewConfiguration(raw, ConfigHooks{Resolve: func(ctx context.Context, got any) (any, error) {
		calls++
		if ctx == nil || got != raw {
			t.Fatalf("resolver input = %v", got)
		}
		return value, nil
	}})
	configSnapshotIs(t, cell, raw, nil, 0)
	prepared := configPrepare(t, cell)
	if prepared.Value != value || prepared.Version != 0 || calls != 1 {
		t.Fatalf("prepared = %+v, calls = %d", prepared, calls)
	}
	applyErr := errors.New("activation failed after publication")
	apply := func(got any) error {
		configSnapshotIs(t, cell, raw, value, 0)
		if got != value {
			t.Fatal("Apply lost resolver identity")
		}
		return applyErr
	}
	if err := apply(prepared.Value); !errors.Is(err, applyErr) {
		t.Fatal(err)
	}
	configSnapshotIs(t, cell, raw, value, 0)
}

func TestConfigurationCandidateAndRawRetentionDoNotPublishOrCallOtherHooks(t *testing.T) {
	resolveCalls := 0
	cell := NewConfiguration("initial", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) { resolveCalls++; return "resolved:" + raw.(string), nil },
		Commit:  func(context.Context, any) error { t.Fatal("unexpected Commit"); return nil },
		Update:  func(context.Context, any, func() error) error { t.Fatal("unexpected Update"); return nil },
	})
	configPrepare(t, cell)
	candidate, err := cell.ResolveCandidate(context.Background(), "candidate")
	if err != nil || candidate != "resolved:candidate" {
		t.Fatalf("candidate = %v, %v", candidate, err)
	}
	configSnapshotIs(t, cell, "initial", "resolved:initial", 0)
	cell.RetainRaw("retained")
	cell.RetainRaw("retained") // Replacement increments even for the same value.
	configSnapshotIs(t, cell, "retained", "resolved:initial", 2)
	if resolveCalls != 2 {
		t.Fatalf("resolve calls = %d", resolveCalls)
	}
	var identity Configuration
	identity.RetainRaw(candidate)
	if got := configPrepare(t, &identity); got.Value != candidate || got.Version != 1 {
		t.Fatalf("zero-value identity preparation = %+v", got)
	}
}

func TestConfigurationPrepareSupersededDoesNotPublishOrRetry(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	resolveCalls := 0
	cell := NewConfiguration("old", ConfigHooks{Resolve: func(_ context.Context, raw any) (any, error) {
		resolveCalls++
		close(entered)
		<-release
		return "resolved:" + raw.(string), nil
	}})
	type result struct {
		prepared PreparedConfig
		err      error
	}
	done := make(chan result, 1)
	go func() { p, err := cell.Prepare(context.Background()); done <- result{p, err} }()
	configSignal(t, entered)
	cell.RetainRaw("new")
	close(release)
	select {
	case got := <-done:
		if got.err != nil || got.prepared.Published || got.prepared.Value != "resolved:old" || got.prepared.Version != 0 {
			t.Fatalf("superseded Prepare = %+v, %v", got.prepared, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Prepare deadlocked")
	}
	configSnapshotIs(t, cell, "new", nil, 1)
	if resolveCalls != 1 {
		t.Fatalf("unexpected retry: %d resolves", resolveCalls)
	}
}

func TestConfigurationVetoCannotOverwriteSameVersionPreparation(t *testing.T) {
	candidate := &struct{ Name string }{"candidate"}
	activation := &struct{ Name string }{"activation"}
	var resolves atomic.Int32
	cell := NewConfiguration("old", ConfigHooks{
		Resolve: func(context.Context, any) (any, error) {
			if resolves.Add(1) == 1 {
				return candidate, nil
			}
			return activation, nil
		},
		Update: func(context.Context, any, func() error) error { return nil },
	})
	done := make(chan error, 1)
	go func() {
		done <- cell.ApplyUpdate(context.Background(), "new", true, true, nil, func() bool {
			// A runtime restart can prepare the retained raw after the runtime
			// binding check, without changing this cell's raw version.
			prepared, err := cell.Prepare(context.Background())
			if err != nil || !prepared.Published || prepared.Value != activation || prepared.Version != 1 {
				t.Errorf("new activation Prepare = %+v, %v", prepared, err)
				return false
			}
			return true
		}, nil)
	}()
	if err := configAwait(t, done); err != nil {
		t.Fatal(err)
	}
	configSnapshotIs(t, cell, "new", activation, 1)
	if resolves.Load() != 2 {
		t.Fatalf("resolve calls = %d, want 2 without retries", resolves.Load())
	}
}

func TestConfigurationOlderSameVersionPrepareCannotOverwriteNewerPublication(t *testing.T) {
	older := &struct{ Name string }{"older"}
	newer := &struct{ Name string }{"newer"}
	entered, release := make(chan struct{}), make(chan struct{})
	var resolves atomic.Int32
	cell := NewConfiguration("raw", ConfigHooks{Resolve: func(context.Context, any) (any, error) {
		if resolves.Add(1) == 1 {
			close(entered)
			<-release
			return older, nil
		}
		return newer, nil
	}})
	type result struct {
		prepared PreparedConfig
		err      error
	}
	done := make(chan result, 1)
	go func() { prepared, err := cell.Prepare(context.Background()); done <- result{prepared, err} }()
	configSignal(t, entered)
	later, err := cell.Prepare(context.Background())
	close(release)
	if err != nil || !later.Published || later.Value != newer || later.Version != 0 {
		t.Fatalf("newer Prepare = %+v, %v", later, err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.prepared.Published || got.prepared.Value != older || got.prepared.Version != 0 {
			t.Fatalf("older Prepare = %+v, %v; it must not publish after the newer preparation", got.prepared, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("older Prepare did not finish")
	}
	configSnapshotIs(t, cell, "raw", newer, 0)
	if resolves.Load() != 2 {
		t.Fatalf("resolve calls = %d, want 2 without retries", resolves.Load())
	}
}

func TestConfigurationActualCoreSameVersionPreparationSurvivesOldVeto(t *testing.T) {
	for name, makeRuntime := range map[string]func() Runtime{"reconciler": NewReconciler, "graph-lock": NewGraphLock} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r := makeRuntime()
			defer r.Close(ctx)
			initial := &struct{ Name string }{"initial"}
			candidate := &struct{ Name string }{"candidate"}
			activation := &struct{ Name string }{"activation"}
			var resolves atomic.Int32
			cell := NewConfiguration("old", ConfigHooks{
				Resolve: func(context.Context, any) (any, error) {
					switch resolves.Add(1) {
					case 1:
						return initial, nil
					case 2:
						return candidate, nil
					default:
						return activation, nil
					}
				},
				Update: func(context.Context, any, func() error) error { return nil },
			})
			applied := make(chan any, 3)
			id, err := r.Mount(ctx, Spec{ID: "publication-fence", Config: cell, Apply: func(c *Context) error { applied <- c.Config(); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Settle(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-applied:
				if got != initial {
					t.Fatalf("initial activation identity = %v", got)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var frameErr error
			done := make(chan error, 1)
			go func() {
				done <- cell.ApplyUpdate(ctx, "new", true, true, nil, func() bool {
					// Model the check-to-publish gap: the old binding was accepted,
					// then an actual core prepares/applies another activation using
					// the retained raw. Runtime.Update changes revision, not raw.
					if frameErr = r.Update(ctx, id); frameErr != nil {
						return false
					}
					frameErr = r.Settle(ctx)
					return frameErr == nil
				}, nil)
			}()
			if err := configAwait(t, done); err != nil {
				t.Fatal(err)
			}
			if frameErr != nil {
				t.Fatal(frameErr)
			}
			select {
			case got := <-applied:
				if got != activation {
					t.Fatalf("replacement activation identity = %v", got)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			configSnapshotIs(t, cell, "new", activation, 1)
			snapshots, err := r.Snapshots(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshots) != 1 || snapshots[0].ID != id || snapshots[0].State != Active || snapshots[0].Revision != 1 || snapshots[0].ConfigVersion != 1 || snapshots[0].EffectiveConfig != activation {
				t.Fatalf("new activation snapshot = %+v", snapshots)
			}
			if resolves.Load() != 3 {
				t.Fatalf("resolve calls = %d, want 3 without retries", resolves.Load())
			}
		})
	}
}

func TestConfigurationPrepareResolutionErrorPreservedAfterReentry(t *testing.T) {
	resolveErr := errors.New("invalid during stale preparation")
	var cell *Configuration
	cell = NewConfiguration("old", ConfigHooks{Resolve: func(context.Context, any) (any, error) {
		cell.RetainRaw("new")
		return nil, resolveErr
	}})
	done := make(chan error, 1)
	go func() {
		prepared, err := cell.Prepare(context.Background())
		if prepared.Published || prepared.Version != 0 {
			done <- errors.New("stale failure published or lost version")
			return
		}
		done <- err
	}()
	if err := configAwait(t, done); !errors.Is(err, resolveErr) {
		t.Fatalf("Prepare error = %v", err)
	}
	configSnapshotIs(t, cell, "new", nil, 1)
}

func TestConfigurationLoadedVetoOrderAndNoSave(t *testing.T) {
	for _, noSave := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "no-save"}[noSave], func(t *testing.T) {
			var cell *Configuration
			var order []string
			cell = NewConfiguration("old", ConfigHooks{
				Resolve: func(_ context.Context, raw any) (any, error) {
					order = append(order, "resolve")
					configSnapshotIs(t, cell, "old", "old", 0)
					return "resolved:" + raw.(string), nil
				},
				Commit: func(_ context.Context, raw any) error {
					order = append(order, "commit")
					if raw != "new" {
						t.Fatalf("Commit got effective instead of raw: %v", raw)
					}
					configSnapshotIs(t, cell, "old", "old", 0)
					return nil
				},
				Update: func(_ context.Context, value any, _ func() error) error {
					order = append(order, "update")
					if value != "resolved:new" {
						t.Fatalf("Update got %v", value)
					}
					configSnapshotIs(t, cell, "new", "old", 1)
					return nil // Retain the instance, not the old effective value.
				},
			})
			// Seed publication with the zero-hook path without changing hook setup.
			cell.effective = "old"
			err := cell.ApplyUpdate(context.Background(), "new", noSave, true,
				func() error { t.Fatal("veto restarted"); return nil },
				func() bool { order = append(order, "current"); return true },
				func() error { order = append(order, "clear"); configSnapshotIs(t, cell, "new", "old", 1); return nil })
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"resolve", "commit", "clear", "update", "current"}
			if noSave {
				want = []string{"resolve", "clear", "update", "current"}
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("order = %v, want %v", order, want)
			}
			configSnapshotIs(t, cell, "new", "resolved:new", 1)
		})
	}
}

func TestConfigurationDefaultAndPendingUpdateRestart(t *testing.T) {
	for _, loaded := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "loaded"}[loaded], func(t *testing.T) {
			resolveCalls, restartCalls, clearCalls := 0, 0, 0
			cell := NewConfiguration("old", ConfigHooks{
				Resolve: func(_ context.Context, raw any) (any, error) { resolveCalls++; return "resolved:" + raw.(string), nil },
			})
			configPrepare(t, cell)
			err := cell.ApplyUpdate(context.Background(), "new", false, loaded,
				func() error { restartCalls++; configSnapshotIs(t, cell, "new", "resolved:old", 1); return nil },
				func() bool { t.Fatal("restart must not check candidate publication"); return true },
				func() error { clearCalls++; return nil })
			wantResolves := 1
			if loaded {
				wantResolves++
			}
			if err != nil || restartCalls != 1 || clearCalls != 1 || resolveCalls != wantResolves {
				t.Fatalf("update = %v, resolve/restart/clear = %d/%d/%d", err, resolveCalls, restartCalls, clearCalls)
			}
			configSnapshotIs(t, cell, "new", "resolved:old", 1)
			prepared := configPrepare(t, cell)
			if prepared.Value != "resolved:new" || prepared.Version != 1 {
				t.Fatalf("restart preparation = %+v", prepared)
			}
		})
	}
}

func TestConfigurationPendingSkipsUpdateHookAndInvalidResolution(t *testing.T) {
	invalid := errors.New("invalid")
	cell := NewConfiguration("old", ConfigHooks{
		Resolve: func(context.Context, any) (any, error) { t.Fatal("pending update resolved early"); return nil, invalid },
		Update:  func(context.Context, any, func() error) error { t.Fatal("pending update dispatched hook"); return nil },
	})
	restarts := 0
	if err := cell.ApplyUpdate(context.Background(), "pending-raw", true, false, func() error { restarts++; return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("restarts = %d", restarts)
	}
	configSnapshotIs(t, cell, "pending-raw", nil, 1)
}

func TestConfigurationFailuresRespectRetentionBoundary(t *testing.T) {
	for _, stage := range []string{"resolve", "commit", "clear", "update", "restart"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New(stage + " failed")
			var order []string
			cell := NewConfiguration("old", ConfigHooks{
				Resolve: func(_ context.Context, raw any) (any, error) {
					order = append(order, "resolve")
					if stage == "resolve" {
						return nil, failure
					}
					return "resolved:" + raw.(string), nil
				},
				Commit: func(context.Context, any) error {
					order = append(order, "commit")
					if stage == "commit" {
						return failure
					}
					return nil
				},
				Update: func(_ context.Context, _ any, next func() error) error {
					order = append(order, "update")
					if stage == "update" {
						return failure
					}
					return next()
				},
			})
			cell.effective = "running"
			err := cell.ApplyUpdate(context.Background(), "new", false, true,
				func() error { order = append(order, "restart"); return failure }, nil,
				func() error {
					order = append(order, "clear")
					if stage == "clear" {
						return failure
					}
					return nil
				})
			if !errors.Is(err, failure) {
				t.Fatalf("update error = %v", err)
			}
			wantRaw, wantVersion := "new", uint64(1)
			if stage == "resolve" || stage == "commit" {
				wantRaw, wantVersion = "old", 0
			}
			configSnapshotIs(t, cell, wantRaw, "running", wantVersion)
			all := []string{"resolve", "commit", "clear", "update", "restart"}
			for i, name := range all {
				if name == stage {
					if !reflect.DeepEqual(order, all[:i+1]) {
						t.Fatalf("order = %v", order)
					}
					break
				}
			}
		})
	}
}

func TestConfigurationRestartFlagPreventsCandidatePublicationEvenWhenErrorHandled(t *testing.T) {
	restartErr := errors.New("restart failed")
	cell := NewConfiguration("old", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) { return "resolved:" + raw.(string), nil },
		Update: func(_ context.Context, _ any, next func() error) error {
			if err := next(); !errors.Is(err, restartErr) {
				return errors.New("restart error not passed to hook")
			}
			return nil // A hook may handle the error, but it cannot turn restart into veto.
		},
	})
	configPrepare(t, cell)
	if err := cell.ApplyUpdate(context.Background(), "new", true, true, func() error { return restartErr }, nil, nil); err != nil {
		t.Fatal(err)
	}
	configSnapshotIs(t, cell, "new", "resolved:old", 1)
}

func TestConfigurationDuplicateContinuationRejected(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "concurrent"}[concurrent], func(t *testing.T) {
			var restarts atomic.Int32
			cell := NewConfiguration("old", ConfigHooks{Update: func(_ context.Context, _ any, next func() error) error {
				if !concurrent {
					if err := next(); err != nil {
						return err
					}
					return next()
				}
				start := make(chan struct{})
				results := make(chan error, 2)
				for range 2 {
					go func() { <-start; results <- next() }()
				}
				close(start)
				a, b := <-results, <-results
				if a == nil {
					return b
				}
				return a
			}})
			err := cell.ApplyUpdate(context.Background(), "new", true, true, func() error { restarts.Add(1); return nil }, nil, nil)
			if !errors.Is(err, ErrAlreadyContinued) || restarts.Load() != 1 {
				t.Fatalf("error = %v, restarts = %d", err, restarts.Load())
			}
			configSnapshotIs(t, cell, "new", nil, 1)
		})
	}
}

func TestConfigurationUpdateReentrySuppressesStaleOuterPublication(t *testing.T) {
	var cell *Configuration
	cell = NewConfiguration("old", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) { return "resolved:" + raw.(string), nil },
		Update: func(ctx context.Context, value any, _ func() error) error {
			if value == "resolved:outer" {
				return cell.ApplyUpdate(ctx, "inner", true, true, nil, nil, nil)
			}
			return nil
		},
	})
	configPrepare(t, cell)
	done := make(chan error, 1)
	go func() {
		done <- cell.ApplyUpdate(context.Background(), "outer", true, true, nil, func() bool { t.Error("stale outer called runtime guard"); return true }, nil)
	}()
	if err := configAwait(t, done); err != nil {
		t.Fatal(err)
	}
	configSnapshotIs(t, cell, "inner", "resolved:inner", 2)
}

func TestConfigurationConcurrentUpdateSuppressesStaleOuterPublication(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	cell := NewConfiguration("old", ConfigHooks{
		Resolve: func(_ context.Context, raw any) (any, error) { return "resolved:" + raw.(string), nil },
		Update: func(_ context.Context, value any, _ func() error) error {
			if value == "resolved:outer" {
				close(entered)
				<-release
			}
			return nil
		},
	})
	configPrepare(t, cell)
	done := make(chan error, 1)
	go func() { done <- cell.ApplyUpdate(context.Background(), "outer", true, true, nil, nil, nil) }()
	configSignal(t, entered)
	if err := cell.ApplyUpdate(context.Background(), "inner", true, true, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := configAwait(t, done); err != nil {
		t.Fatal(err)
	}
	configSnapshotIs(t, cell, "inner", "resolved:inner", 2)
}

func TestConfigurationRuntimeGuardRunsUnlockedAndIsRechecked(t *testing.T) {
	for _, reenter := range []bool{false, true} {
		t.Run(map[bool]string{false: "epoch-changed", true: "raw-changed-in-guard"}[reenter], func(t *testing.T) {
			cell := NewConfiguration("old", ConfigHooks{Update: func(context.Context, any, func() error) error { return nil }})
			configPrepare(t, cell)
			done := make(chan error, 1)
			go func() {
				done <- cell.ApplyUpdate(context.Background(), "new", true, true, nil, func() bool {
					configSnapshotIs(t, cell, "new", "old", 1)
					if reenter {
						cell.RetainRaw("latest")
						return true
					}
					return false
				}, nil)
			}()
			if err := configAwait(t, done); err != nil {
				t.Fatal(err)
			}
			if reenter {
				configSnapshotIs(t, cell, "latest", "old", 2)
			} else {
				configSnapshotIs(t, cell, "new", "old", 1)
			}
		})
	}
}

func TestConfigurationCommitAndClearCallbacksMayReenter(t *testing.T) {
	for _, stage := range []string{"commit", "clear"} {
		t.Run(stage, func(t *testing.T) {
			var cell *Configuration
			cell = NewConfiguration("old", ConfigHooks{
				Commit: func(context.Context, any) error {
					cell.RawSnapshot()
					if stage == "commit" {
						cell.RetainRaw("intermediate")
					}
					return nil
				},
				Update: func(context.Context, any, func() error) error { return nil },
			})
			configPrepare(t, cell)
			done := make(chan error, 1)
			go func() {
				done <- cell.ApplyUpdate(context.Background(), "new", false, true, nil, nil, func() error {
					cell.RawSnapshot()
					if stage == "clear" {
						cell.RetainRaw("latest")
					}
					return nil
				})
			}()
			if err := configAwait(t, done); err != nil {
				t.Fatal(err)
			}
			if stage == "commit" {
				configSnapshotIs(t, cell, "new", "new", 2)
			} else {
				configSnapshotIs(t, cell, "latest", "old", 2)
			}
		})
	}
}

func TestConfigurationCancellationBoundaries(t *testing.T) {
	for _, stage := range []string{"before", "resolve", "commit", "clear", "update"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var order []string
			cell := NewConfiguration("old", ConfigHooks{
				Resolve: func(_ context.Context, raw any) (any, error) {
					order = append(order, "resolve")
					if stage == "resolve" {
						cancel()
					}
					return raw, nil
				},
				Commit: func(context.Context, any) error {
					order = append(order, "commit")
					if stage == "commit" {
						cancel()
					}
					return nil
				},
				Update: func(_ context.Context, _ any, next func() error) error {
					order = append(order, "update")
					if stage == "update" {
						cancel()
					}
					return next()
				},
			})
			cell.effective = "old"
			if stage == "before" {
				cancel()
			}
			err := cell.ApplyUpdate(ctx, "new", false, true, func() error { t.Error("canceled update restarted"); return nil }, nil,
				func() error {
					order = append(order, "clear")
					if stage == "clear" {
						cancel()
					}
					return nil
				})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v", err)
			}
			wantRaw, wantVersion := "new", uint64(1)
			if stage == "before" || stage == "resolve" {
				wantRaw, wantVersion = "old", 0
			}
			configSnapshotIs(t, cell, wantRaw, "old", wantVersion)
			want := map[string][]string{
				"before": nil, "resolve": {"resolve"}, "commit": {"resolve", "commit", "clear"},
				"clear": {"resolve", "commit", "clear"}, "update": {"resolve", "commit", "clear", "update"},
			}[stage]
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("order = %v, want %v", order, want)
			}
		})
	}
}

func TestConfigurationCanceledCandidateAndPrepareDoNotPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cell := NewConfiguration("raw", ConfigHooks{Resolve: func(context.Context, any) (any, error) { t.Fatal("canceled resolver called"); return nil, nil }})
	if _, err := cell.ResolveCandidate(ctx, "candidate"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if prepared, err := cell.Prepare(ctx); !errors.Is(err, context.Canceled) || prepared.Published {
		t.Fatalf("Prepare = %+v, %v", prepared, err)
	}
	configSnapshotIs(t, cell, "raw", nil, 0)
}

func TestConfigurationUpdatePreservesIdentityAndRestartPreparationWins(t *testing.T) {
	for _, shouldRestart := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained-instance", true: "restart"}[shouldRestart], func(t *testing.T) {
			raw := &struct{ Name string }{"new"}
			candidate := &struct{ Payload chan struct{} }{make(chan struct{})}
			activation := &struct{ Payload chan struct{} }{make(chan struct{})}
			resolveCalls, commitCalls, restartCalls := 0, 0, 0
			var cell *Configuration
			cell = NewConfiguration("old", ConfigHooks{
				Resolve: func(_ context.Context, got any) (any, error) {
					if got != raw {
						t.Fatalf("resolver lost raw identity: %v", got)
					}
					resolveCalls++
					if resolveCalls == 1 {
						return candidate, nil
					}
					return activation, nil
				},
				Commit: func(_ context.Context, got any) error {
					commitCalls++
					if got != raw {
						t.Fatalf("commit lost raw identity: %v", got)
					}
					configSnapshotIs(t, cell, "old", nil, 0)
					return nil
				},
				Update: func(_ context.Context, got any, next func() error) error {
					if got != candidate {
						t.Fatalf("hook lost candidate identity: %v", got)
					}
					if shouldRestart {
						return next()
					}
					return nil
				},
			})
			err := cell.ApplyUpdate(context.Background(), raw, false, true, func() error {
				restartCalls++
				configSnapshotIs(t, cell, raw, nil, 1)
				prepared := configPrepare(t, cell)
				if prepared.Value != activation {
					t.Fatal("restart used old candidate instead of preparing raw")
				}
				return nil
			}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			wantValue, wantResolveCalls, wantRestartCalls := any(candidate), 1, 0
			if shouldRestart {
				wantValue, wantResolveCalls, wantRestartCalls = activation, 2, 1
			}
			configSnapshotIs(t, cell, raw, wantValue, 1)
			if commitCalls != 1 || resolveCalls != wantResolveCalls || restartCalls != wantRestartCalls {
				t.Fatalf("commit/resolve/restart calls = %d/%d/%d", commitCalls, resolveCalls, restartCalls)
			}
		})
	}
}

func TestConfigurationSuccessfulHookNotRetroactivelyCanceled(t *testing.T) {
	for _, runtimeCurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "runtime-stale", true: "runtime-current"}[runtimeCurrent], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cell := NewConfiguration("old", ConfigHooks{Update: func(context.Context, any, func() error) error { cancel(); return nil }})
			configPrepare(t, cell)
			err := cell.ApplyUpdate(ctx, "new", true, true, nil, func() bool { return runtimeCurrent }, nil)
			if err != nil {
				t.Fatalf("successful hook retroactively failed: %v", err)
			}
			wantEffective := "old"
			if runtimeCurrent {
				wantEffective = "new"
			}
			configSnapshotIs(t, cell, "new", wantEffective, 1)
		})
	}
}

func TestConfigurationMissingRestartReturnsErrorAfterRetention(t *testing.T) {
	cell := NewConfiguration("old", ConfigHooks{})
	configPrepare(t, cell)
	cleared := false
	err := cell.ApplyUpdate(context.Background(), "new", true, true, nil, nil, func() error { cleared = true; return nil })
	if !errors.Is(err, ErrConfigRestart) || !cleared {
		t.Fatalf("missing restart = %v, cleared = %v", err, cleared)
	}
	configSnapshotIs(t, cell, "new", "old", 1)
}

func TestConfigurationSnapshotDistinguishesPublishedNil(t *testing.T) {
	for _, throughUpdate := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepare", true: "veto"}[throughUpdate], func(t *testing.T) {
			cell := NewConfiguration("raw", ConfigHooks{
				Resolve: func(context.Context, any) (any, error) { return nil, nil },
				Update:  func(context.Context, any, func() error) error { return nil },
			})
			if cell.Prepared() || cell.Snapshot().Prepared {
				t.Fatal("new cell claims a published value")
			}
			if value, err := cell.ResolveCandidate(context.Background(), "candidate"); err != nil || value != nil {
				t.Fatalf("candidate = %v, %v", value, err)
			}
			if cell.Prepared() || cell.Snapshot().Prepared {
				t.Fatal("candidate marked cell prepared")
			}
			cell.RetainRaw("raw")
			if cell.Prepared() {
				t.Fatal("raw retention marked cell prepared")
			}
			if throughUpdate {
				if err := cell.ApplyUpdate(context.Background(), "raw", true, true, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
			} else {
				configPrepare(t, cell)
			}
			got := cell.Snapshot()
			if !got.Prepared || !cell.Prepared() || got.Effective != nil || got.Raw != "raw" {
				t.Fatalf("published nil snapshot = %+v", got)
			}
			cell.RetainRaw("later")
			got = cell.Snapshot()
			if !got.Prepared || got.Effective != nil || got.Raw != "later" {
				t.Fatalf("retention forgot published nil: %+v", got)
			}
		})
	}
}

func TestConfigurationParallelRawSnapshotsRemainCoherent(t *testing.T) {
	cell := NewConfiguration(uint64(0), ConfigHooks{})
	var writers sync.WaitGroup
	writers.Add(1)
	go func() {
		defer writers.Done()
		for n := uint64(1); n <= 1000; n++ {
			cell.RetainRaw(n)
		}
	}()
	for range 1000 {
		raw, effective, version := cell.RawSnapshot()
		if raw != version || effective != nil {
			t.Fatalf("incoherent snapshot = (%v, %v, %d)", raw, effective, version)
		}
	}
	writers.Wait()
	configSnapshotIs(t, cell, uint64(1000), nil, 1000)
}
