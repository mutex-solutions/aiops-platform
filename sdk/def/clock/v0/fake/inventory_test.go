// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The generated cycle test calls New with no argument and reads Inventory
// (ADR-060 decision 12).
var (
	_ func() (*fake.Clock, kernel.Plugin) = fake.New
	_ interface{ Inventory() []string }   = (*fake.Clock)(nil)
)

// New stands at a fixed instant, and NewAt where the test says.
func TestNewStandsAtAFixedInstant(t *testing.T) {
	a, _ := fake.New()
	b, _ := fake.New()
	at, _ := fake.NewAt(t0)
	if !a.Now().Equal(b.Now()) || !at.Now().Equal(t0) {
		t.Fatalf("New at %v and %v, NewAt at %v", a.Now(), b.Now(), at.Now())
	}
}

// Inventory lists the timers its callers left pending: one per timer until it
// fires or is stopped.
func TestInventoryListsThePendingTimers(t *testing.T) {
	f, c, cc := mount(t)
	if got := f.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory before a timer = %v", got)
	}
	soon, err := c.After(cc, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	later, err := c.After(cc, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Inventory(); len(got) != 2 {
		t.Fatalf("Inventory with two timers = %v", got)
	}
	f.Advance(time.Minute)
	if _, ok := fired(soon); !ok {
		t.Fatal("the first timer did not fire")
	}
	later.Stop()
	if got := f.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory after the timers ended = %v", got)
	}
}
