// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"context"
	"errors"
	"testing"
)

// Quiescence can seal an admitted scope between its scheduling and the worker's
// entry. The worker then ran no setup, so it reports success; a scope that merely
// started elsewhere keeps its error.
func TestScopeWorkerReportsASlotSealedBeforeEntryAsSuccess(t *testing.T) {
	factory := func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
	setup := func(*EffectScope) error { t.Error("setup ran"); return nil }

	sealed := newScopeEffect("scope", setup)
	if !sealed.cancelPending(ErrInactive) {
		t.Fatal("a pending scope was not sealed")
	}
	if err := scopeSetupResult(sealed, sealed.runSetup(context.Background(), factory)); err != nil {
		t.Fatalf("sealed slot reported %v", err)
	}

	started := newScopeEffect("scope", setup)
	started.started.Store(true)
	if err := scopeSetupResult(started, started.runSetup(context.Background(), factory)); !errors.Is(err, errScopeStarted) {
		t.Fatalf("slot whose setup runs elsewhere reported %v", err)
	}
	other := errors.New("setup failed")
	if err := scopeSetupResult(started, other); err != other {
		t.Fatalf("scopeSetupResult changed an unrelated error to %v", err)
	}
}
