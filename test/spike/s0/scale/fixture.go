// SPDX-License-Identifier: Apache-2.0

// Package scale is a test-only S0 measurement harness, not a selected runtime or SDK.
package scale

import (
	"fmt"
	"slices"
)

const (
	GraphFibers               = 10_000
	SharedFibers              = 97
	MinimumSamples            = 1_000
	MinimumRepetitions        = 10
	FirstSeed          uint64 = 0x53000000
)

type Topology string

const (
	Independent Topology = "independent"
	Chain       Topology = "chain"
	FanOut      Topology = "fanout"
	Diamonds    Topology = "diamonds"
	Realms      Topology = "realms"
	Trees       Topology = "trees"
)

// RawKey is shallow fixture data. Realm=-1 is shared; nonnegative realm numbers
// select different opaque labels at the composition boundary. Name alone never
// grants authority. No per-node registry or recursive type is generated.
type RawKey struct {
	Name  string `json:"name"`
	Realm int    `json:"realm"`
}

// Node declares every read used by its body and cleanup. Index is the immutable
// value provided by that body; a consumer validates the actual injected value.
type Node struct {
	ID       string   `json:"id"`
	Index    int      `json:"index"`
	Realm    int      `json:"realm"`
	Tree     int      `json:"tree"` // -1 outside a local tree.
	Needs    []RawKey `json:"needs,omitempty"`
	Provides []RawKey `json:"provides,omitempty"`
}

// Fixture is deterministic declarative data, including all measured fibers.
// The backend's root is not a fixture fiber. Treat returned data as immutable.
type Fixture struct {
	Name          string   `json:"name"`
	Topology      Topology `json:"topology"`
	Nodes         []Node   `json:"nodes"`
	RootProviders []int    `json:"rootProviders"`
	Leaf          int      `json:"leaf"`
	Shared        int      `json:"shared"`
	TreeCount     int      `json:"treeCount,omitempty"`
	TreeWidth     int      `json:"treeWidth,omitempty"`
	Seed          uint64   `json:"seed"`
}

func key(index int) RawKey { return RawKey{Name: fmt.Sprintf("value-%05d", index), Realm: -1} }

func fixture(topology Topology, count int) Fixture {
	f := Fixture{Name: string(topology), Topology: topology, Leaf: count - 1, Seed: FirstSeed}
	for i := 0; i < count; i++ {
		f.Nodes = append(f.Nodes, Node{ID: fmt.Sprintf("fiber-%05d", i), Index: i, Realm: -1, Tree: -1})
	}
	return f
}

// Graph builds exact graph shapes. Realms uses 100 groups in the frozen 10,000
// case; use RealmFixture for deliberately small, explicit author-test shapes.
func Graph(topology Topology, count int) (Fixture, error) {
	if count < 2 {
		return Fixture{}, fmt.Errorf("at least two fibers required")
	}
	if topology == Realms {
		if count != GraphFibers {
			return Fixture{}, fmt.Errorf("use RealmFixture for a non-frozen realm shape")
		}
		return RealmFixture(100, 99)
	}
	if topology != Independent && topology != Chain && topology != FanOut && topology != Diamonds {
		return Fixture{}, fmt.Errorf("unknown graph topology %q", topology)
	}
	if topology == Diamonds && (count-1)%3 != 0 {
		return Fixture{}, fmt.Errorf("diamond count must be 1+3*n, got %d", count)
	}
	f := fixture(topology, count)
	f.RootProviders = []int{0}
	f.Nodes[0].Provides = []RawKey{key(0)}
	switch topology {
	case Independent:
		// The separate restart provider is node 0, counted within the total.
		// No independent leaf depends on it.
	case Chain:
		for i := 1; i < count; i++ {
			f.Nodes[i].Needs = []RawKey{key(i - 1)}
			f.Nodes[i].Provides = []RawKey{key(i)}
		}
	case FanOut:
		for i := 1; i < count; i++ {
			f.Nodes[i].Needs = []RawKey{key(0)}
		}
	case Diamonds:
		for i := 1; i < count; i += 3 {
			f.Nodes[i].Needs, f.Nodes[i+1].Needs = []RawKey{key(0)}, []RawKey{key(0)}
			f.Nodes[i].Provides, f.Nodes[i+1].Provides = []RawKey{key(i)}, []RawKey{key(i + 1)}
			f.Nodes[i+2].Needs = []RawKey{key(i), key(i + 1)}
		}
	}
	return f, f.Validate()
}

func RealmFixture(groups, consumers int) (Fixture, error) {
	if groups < 1 || consumers < 1 {
		return Fixture{}, fmt.Errorf("realms require positive group and consumer counts")
	}
	width := consumers + 1
	f := fixture(Realms, groups*width)
	for group := 0; group < groups; group++ {
		local := RawKey{Name: "value", Realm: group}
		provider := group * width
		f.RootProviders = append(f.RootProviders, provider)
		for i := provider; i < provider+width; i++ {
			f.Nodes[i].Realm = group
			if i == provider {
				f.Nodes[i].Provides = []RawKey{local}
			} else {
				f.Nodes[i].Needs = []RawKey{local}
			}
		}
	}
	return f, f.Validate()
}

func TreeFixture(trees, width int) (Fixture, error) {
	if trees < 1 || (width != 2 && width != 4) {
		return Fixture{}, fmt.Errorf("trees require positive count and width 2 or 4")
	}
	f := fixture(Trees, SharedFibers+trees*width)
	f.Name = fmt.Sprintf("trees-%d-width-%d", trees, width)
	f.Shared, f.TreeCount, f.TreeWidth = SharedFibers, trees, width
	f.RootProviders = []int{0}
	shared := RawKey{Name: "shared", Realm: -1}
	f.Nodes[0].Provides = []RawKey{shared}
	for i := 1; i < SharedFibers; i++ {
		f.Nodes[i].Needs = []RawKey{shared}
	}
	for tree := 0; tree < trees; tree++ {
		provider := SharedFibers + tree*width
		local := RawKey{Name: "value", Realm: tree}
		for i := provider; i < provider+width; i++ {
			f.Nodes[i].Realm, f.Nodes[i].Tree = tree, tree
			if i == provider {
				f.Nodes[i].Needs, f.Nodes[i].Provides = []RawKey{shared}, []RawKey{local}
			} else {
				f.Nodes[i].Needs = []RawKey{local}
			}
		}
	}
	return f, f.Validate()
}

// FrozenFixtures returns the five exact 10k graphs and both 500/300-tree widths.
// Generating this data does not start a runtime or execute a performance trial.
func FrozenFixtures() []Fixture {
	var out []Fixture
	for _, topology := range []Topology{Independent, Chain, FanOut, Diamonds, Realms} {
		f, err := Graph(topology, GraphFibers)
		if err != nil {
			panic(err)
		}
		out = append(out, f)
	}
	for _, count := range []int{500, 300} {
		for _, width := range []int{2, 4} {
			f, err := TreeFixture(count, width)
			if err != nil {
				panic(err)
			}
			out = append(out, f)
		}
	}
	return out
}

// Validate refuses a missing/duplicate provider and undeclared/double reads.
// It is deliberately iterative: even the 10k chain adds no recursive Go calls.
func (f Fixture) Validate() error {
	if len(f.Nodes) == 0 || f.Leaf < 0 || f.Leaf >= len(f.Nodes) || len(f.RootProviders) == 0 {
		return fmt.Errorf("invalid fixture dimensions")
	}
	providers, names := make(map[RawKey]int), make(map[string]bool)
	for i, node := range f.Nodes {
		if node.ID == "" || names[node.ID] || node.Index != i {
			return fmt.Errorf("invalid/repeated node %d", i)
		}
		names[node.ID] = true
		for _, provided := range node.Provides {
			if _, exists := providers[provided]; exists || provided.Name == "" {
				return fmt.Errorf("duplicate/empty provider key at node %d: %+v", i, provided)
			}
			providers[provided] = i
		}
	}
	for i, node := range f.Nodes {
		seen := make(map[RawKey]bool)
		for _, need := range node.Needs {
			provider, exists := providers[need]
			if !exists || provider >= i || seen[need] {
				return fmt.Errorf("missing, forward/cyclic or duplicate need at node %d: %+v", i, need)
			}
			seen[need] = true
		}
	}
	for _, root := range f.RootProviders {
		if root < 0 || root >= len(f.Nodes) || len(f.Nodes[root].Provides) == 0 {
			return fmt.Errorf("invalid root provider %d", root)
		}
	}
	if len(slices.Compact(slices.Sorted(slices.Values(f.RootProviders)))) != len(f.RootProviders) {
		return fmt.Errorf("duplicate root provider")
	}
	return nil
}
