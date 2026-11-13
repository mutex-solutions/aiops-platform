// SPDX-License-Identifier: Apache-2.0

// Package properties is an S0 test fixture, not a production SDK or host.
// Its model never calls, reads, or delegates expected states to a runtime.
package properties

import (
	"fmt"
	"slices"
)

const (
	providerSlots = 8
	consumerSlots = 8
	totalSlots    = providerSlots + consumerSlots
	missingNeed   = providerSlots // An address that no generated spec provides.
	firstSeed     = uint64(0x53000000)
)

// Operation is a complete candidate-independent command. Needs contains provider
// indexes, or missingNeed; no runtime-assigned fiber/provision UID is generated.
type Operation struct {
	Index       int    `json:"index"`
	Kind        string `json:"kind"`
	Slot        int    `json:"slot"`
	Incarnation uint64 `json:"incarnation"`
	Generation  uint64 `json:"inputGeneration"`
	Value       uint64 `json:"value"`
	Fail        bool   `json:"fail"`
	Needs       []int  `json:"needs,omitempty"`
}

// splitMix64 fixes both the PRNG and its exact arithmetic independently of Go's
// math/rand implementation. Next uses the public-domain SplitMix64 algorithm.
type splitMix64 struct{ state uint64 }

func (r *splitMix64) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

type generatedSlot struct {
	alive       bool
	incarnation uint64
	generation  uint64
	value       uint64
	fail        bool
}

// generate emits exactly count commands, including a fixed coverage prefix:
// consumers-before-providers, a chain/diamond, partial provider failure, sticky
// failure across a dependency restart, explicit recovery, disposal and remount.
// All remaining choices are drawn from the currently legal command set.
func generate(seed uint64, count int) []Operation {
	rng := splitMix64{state: seed}
	var slots [totalSlots]generatedSlot
	out := make([]Operation, 0, count)
	appendOp := func(kind string, slot int) {
		s := &slots[slot]
		op := Operation{Index: len(out), Kind: kind, Slot: slot}
		switch kind {
		case "mount":
			s.alive = true
			s.incarnation++
			s.generation = 1
			s.value, s.fail = rng.next(), false
			op.Needs = generateNeeds(&rng, slot)
		case "dispose":
			s.alive = false
		case "update", "value", "fail":
			s.generation++
			if kind == "value" {
				s.value = rng.next()
			}
			if kind == "fail" {
				s.fail = !s.fail
			}
		default:
			panic("unknown generated operation")
		}
		op.Incarnation, op.Generation, op.Value, op.Fail = s.incarnation, s.generation, s.value, s.fail
		out = append(out, op)
	}
	prefix := []struct {
		kind string
		slot int
	}{
		{"mount", 8}, {"mount", 9}, {"mount", 10}, {"mount", 11},
		{"mount", 12}, {"mount", 13}, {"mount", 14}, {"mount", 15},
		{"mount", 0}, {"mount", 1}, {"mount", 2}, {"mount", 3},
		{"mount", 4}, {"mount", 5}, {"mount", 6}, {"mount", 7},
		{"fail", 1}, {"value", 0}, {"fail", 1}, {"dispose", 0},
		{"mount", 0}, {"update", 4}, {"dispose", 8}, {"mount", 8},
	}
	for _, command := range prefix {
		if len(out) == count {
			return out
		}
		appendOp(command.kind, command.slot)
	}
	for len(out) < count {
		type choice struct {
			kind string
			slot int
		}
		choices := make([]choice, 0, totalSlots*4)
		for slot, s := range slots {
			if !s.alive {
				choices = append(choices, choice{"mount", slot})
				continue
			}
			choices = append(choices, choice{"dispose", slot}, choice{"update", slot}, choice{"fail", slot})
			if slot < providerSlots {
				choices = append(choices, choice{"value", slot})
			}
		}
		chosen := choices[rng.next()%uint64(len(choices))]
		appendOp(chosen.kind, chosen.slot)
	}
	return out
}

func generateNeeds(rng *splitMix64, slot int) []int {
	// A deterministic chain and diamond give every seed meaningful transitive
	// invalidation. Other provider slots retain generated DAG edges/missing needs.
	switch slot {
	case 0:
		return nil
	case 1, 2, 3:
		if slot == 1 {
			return []int{0}
		}
		return []int{1}
	case 4:
		return []int{2, 3}
	case 7:
		return []int{missingNeed}
	case 8:
		return []int{0}
	case 9:
		return []int{4}
	}
	limit := providerSlots
	if slot < providerSlots {
		limit = slot
	}
	needs := make([]int, 0, 4)
	for provider := 0; provider < limit; provider++ {
		if rng.next()%3 == 0 && len(needs) < 3 {
			needs = append(needs, provider)
		}
	}
	if len(needs) == 0 && limit > 0 {
		needs = append(needs, int(rng.next()%uint64(limit)))
	}
	if rng.next()%7 == 0 {
		needs = append(needs, missingNeed)
	}
	return needs
}

// modelIdentity is an abstract activation identity, not a guessed runtime UID.
// It is used only to decide when a declaration's dependencies have changed.
type modelIdentity struct {
	Slot        int    `json:"slot"`
	Incarnation uint64 `json:"incarnation"`
	Attempt     int64  `json:"attempt"`
}

type modelFiber struct {
	Slot              int             `json:"slot"`
	Incarnation       uint64          `json:"incarnation"`
	Needs             []int           `json:"needs"`
	Generation        uint64          `json:"inputGeneration"`
	Value             uint64          `json:"value"`
	Fail              bool            `json:"failInput"`
	Alive             bool            `json:"alive"`
	Revision          uint64          `json:"revision"`
	State             string          `json:"state"`
	Applications      int64           `json:"applications"`
	Disposals         int64           `json:"disposals"`
	FailureGeneration uint64          `json:"stickyFailureGeneration,omitempty"`
	LastGeneration    uint64          `json:"lastAttemptGeneration,omitempty"`
	LastValue         uint64          `json:"lastAttemptValue"`
	ActiveRevision    uint64          `json:"activeRevision"`
	Bindings          []modelIdentity `json:"bindings"`
	Identity          modelIdentity   `json:"identity"`
}

func (f *modelFiber) name() string {
	return fmt.Sprintf("slot-%02d/incarnation-%d", f.Slot, f.Incarnation)
}

type model struct {
	Current [totalSlots]*modelFiber `json:"-"`
	Fibers  []*modelFiber           `json:"fibers"`
}

func (m *model) apply(op Operation) error {
	if op.Slot < 0 || op.Slot >= totalSlots {
		return fmt.Errorf("invalid slot %d", op.Slot)
	}
	f := m.Current[op.Slot]
	if op.Kind == "mount" {
		if f != nil || op.Incarnation == 0 || op.Generation != 1 {
			return fmt.Errorf("invalid mount %+v", op)
		}
		for i, need := range op.Needs {
			if need < 0 || need > missingNeed || (op.Slot < providerSlots && need != missingNeed && need >= op.Slot) || (i > 0 && op.Needs[i-1] >= need) {
				return fmt.Errorf("invalid DAG needs %+v", op)
			}
		}
		f = &modelFiber{Slot: op.Slot, Incarnation: op.Incarnation, Needs: slices.Clone(op.Needs), Alive: true, State: "PENDING"}
		m.Current[op.Slot] = f
		m.Fibers = append(m.Fibers, f)
	} else {
		if f == nil || !f.Alive || f.Incarnation != op.Incarnation {
			return fmt.Errorf("command targets an unmounted incarnation: %+v", op)
		}
		switch op.Kind {
		case "dispose":
			f.Alive = false
			m.Current[op.Slot] = nil
		case "update", "value", "fail":
			if op.Generation != f.Generation+1 || (op.Kind == "value" && op.Slot >= providerSlots) {
				return fmt.Errorf("invalid update %+v", op)
			}
			f.Revision++
			f.FailureGeneration = 0 // Only an explicit update clears a failure.
		default:
			return fmt.Errorf("unknown operation %q", op.Kind)
		}
	}
	f.Generation, f.Value, f.Fail = op.Generation, op.Value, op.Fail
	m.settle()
	return nil
}

// settle evaluates the small DAG in declaration order. It contains no scheduler
// queue, timing, retry, actual UID, or observed-runtime input. Stable state and
// callback/resource counts follow exclusively from the commands and dependency
// identities. Sticky startup rejection is checked before dependency availability.
func (m *model) settle() {
	for _, f := range m.Fibers {
		if !f.Alive && f.State != "DISPOSED" {
			if f.State == "ACTIVE" {
				f.Disposals++
			}
			f.State = "DISPOSED"
		}
	}
	for _, f := range m.Current {
		if f == nil {
			continue
		}
		bindings := make([]modelIdentity, 0, len(f.Needs))
		ready := true
		for _, need := range f.Needs {
			if need == missingNeed || m.Current[need] == nil || m.Current[need].State != "ACTIVE" {
				ready = false
				break
			}
			bindings = append(bindings, m.Current[need].Identity)
		}
		if f.State == "ACTIVE" {
			if ready && f.ActiveRevision == f.Revision && slices.Equal(f.Bindings, bindings) {
				continue
			}
			f.Disposals++
		}
		if f.FailureGeneration != 0 {
			f.State = "FAILED"
			continue
		}
		if !ready {
			f.State = "PENDING"
			continue
		}
		f.Applications++
		f.LastGeneration, f.LastValue = f.Generation, f.Value
		if f.Fail {
			f.Disposals++ // The failed body still acquired its owned effect.
			f.FailureGeneration = f.Generation
			f.State = "FAILED"
			continue
		}
		f.State = "ACTIVE"
		f.ActiveRevision = f.Revision
		f.Bindings = bindings
		f.Identity = modelIdentity{Slot: f.Slot, Incarnation: f.Incarnation, Attempt: f.Applications}
	}
}
