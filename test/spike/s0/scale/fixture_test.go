// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"reflect"
	"testing"
)

func TestFrozenFixtureCountsOnly(t *testing.T) {
	fixtures := FrozenFixtures()
	if len(fixtures) != 9 {
		t.Fatalf("fixtures = %d", len(fixtures))
	}
	for _, f := range fixtures {
		if err := f.Validate(); err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if f.Topology == Trees {
			if f.Shared != 97 || len(f.Nodes) != 97+f.TreeCount*f.TreeWidth || (f.TreeCount != 300 && f.TreeCount != 500) {
				t.Fatalf("bad tree dimensions: %s", f.Name)
			}
			if len(f.Nodes[0].Provides) != 1 || len(f.Nodes[0].Needs) != 0 {
				t.Fatal("missing shared root")
			}
			for i := 1; i < 97; i++ {
				if !reflect.DeepEqual(f.Nodes[i].Needs, f.Nodes[0].Provides) || len(f.Nodes[i].Provides) != 0 {
					t.Fatalf("shared node %d has incorrect edge", i)
				}
			}
			for tree := 0; tree < f.TreeCount; tree++ {
				provider := 97 + tree*f.TreeWidth
				if !reflect.DeepEqual(f.Nodes[provider].Needs, f.Nodes[0].Provides) || len(f.Nodes[provider].Provides) != 1 {
					t.Fatalf("tree %d provider missing shared edge", tree)
				}
				for i := provider; i < provider+f.TreeWidth; i++ {
					if f.Nodes[i].Tree != tree || f.Nodes[i].Realm != tree {
						t.Fatalf("tree %d scope mismatch", tree)
					}
					if i > provider && (!reflect.DeepEqual(f.Nodes[i].Needs, f.Nodes[provider].Provides) || len(f.Nodes[i].Provides) != 0) {
						t.Fatalf("tree %d consumer %d missing local edge", tree, i)
					}
				}
			}
			continue
		}
		if len(f.Nodes) != 10_000 {
			t.Fatalf("%s fibers = %d", f.Name, len(f.Nodes))
		}
		var needs, provisions int
		for _, node := range f.Nodes {
			needs += len(node.Needs)
			provisions += len(node.Provides)
		}
		switch f.Topology {
		case Independent:
			if needs != 0 || provisions != 1 {
				t.Fatalf("independent: %d needs, %d provisions", needs, provisions)
			}
		case Chain:
			if needs != 9_999 || provisions != 10_000 {
				t.Fatalf("chain: %d needs, %d provisions", needs, provisions)
			}
		case FanOut:
			if needs != 9_999 || provisions != 1 {
				t.Fatalf("fanout: %d needs, %d provisions", needs, provisions)
			}
		case Diamonds:
			if needs != 13_332 || provisions != 6_667 {
				t.Fatalf("diamonds: %d needs, %d provisions", needs, provisions)
			}
		case Realms:
			if needs != 9_900 || provisions != 100 || len(f.RootProviders) != 100 {
				t.Fatalf("realms: %d needs, %d provisions, %d roots", needs, provisions, len(f.RootProviders))
			}
		}
	}
}

func TestSmallFixtureShapes(t *testing.T) {
	for _, topology := range []Topology{Independent, Chain, FanOut, Diamonds} {
		count := 12
		if topology == Diamonds {
			count = 10
		}
		a, err := Graph(topology, count)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Graph(topology, count)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("nondeterministic %s", topology)
		}
	}
	if _, err := Graph(Diamonds, 12); err == nil {
		t.Fatal("partial diamond accepted")
	}
	f, err := RealmFixture(3, 3)
	if err != nil || len(f.Nodes) != 12 {
		t.Fatalf("small realms: %+v %v", f, err)
	}
	bad, _ := Graph(Chain, 12)
	bad.Nodes[1].Provides = append(bad.Nodes[1].Provides, bad.Nodes[0].Provides[0])
	if err := bad.Validate(); err == nil {
		t.Fatal("duplicate provider accepted")
	}
	bad, _ = Graph(Chain, 12)
	bad.Nodes[1].Needs = []RawKey{{Name: "missing", Realm: -1}}
	if err := bad.Validate(); err == nil {
		t.Fatal("missing provision accepted")
	}
}
