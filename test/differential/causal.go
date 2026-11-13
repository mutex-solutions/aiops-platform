// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

const CausalWitnessSchema = "s0-causal-witness/v1"

// ErrCausalUnsupported means that independence or an observation's ownership
// cannot be proved by this version. It is not a successful comparison.
var ErrCausalUnsupported = errors.New("causal comparison unsupported")

// CausalWitness binds an observed checkpoint sidecar to exact, retained input
// bytes. Hashes are lowercase SHA-256 hex; they are not executable provenance or
// signatures. The replay recorder, not this comparator, must observe the cuts.
type CausalWitness struct {
	Schema       string      `json:"schema"`
	TraceSHA256  string      `json:"traceSHA256"`
	ScriptSHA256 string      `json:"scriptSHA256"`
	Checkpoints  []CausalCut `json:"checkpoints"`
}

// CausalCut counts the events already recorded when the checkpoint was observed.
// Zero is valid; a cut is a boundary, not an event's zero-based array index.
type CausalCut struct {
	Label      string `json:"label"`
	AfterEvent int    `json:"afterEvent"`
}

// CompareCausal compares only statically justified independent interleavings.
// It neither calls Compare nor replaces its strict diagnostic. ParseTrace makes
// private copies; input bytes and complete, totally ordered raw events remain
// untouched. No event is sorted, dropped, coalesced, or given a new raw sequence.
//
// Version 1 is deliberately narrow: static ordinary realms, fixed root mounts,
// read-only Needs, synchronous bodies, and witnessed checkpoint boundaries.
// Gates, marks, tasks, availability, children, callbacks and dynamic contexts do
// not silently become independent; the compiler returns ErrCausalUnsupported.
// Script expectations are never consulted to produce or compare observations.
func CompareCausal(scriptJSON, expectedTraceJSON, actualTraceJSON []byte, expectedWitness, actualWitness CausalWitness) error {
	model, err := compileCausalModel(scriptJSON)
	if err != nil {
		return err
	}
	left, err := ParseTrace(expectedTraceJSON)
	if err != nil {
		return fmt.Errorf("expected: %w", err)
	}
	right, err := ParseTrace(actualTraceJSON)
	if err != nil {
		return fmt.Errorf("actual: %w", err)
	}
	for _, item := range []struct {
		name    string
		raw     []byte
		trace   *Trace
		witness CausalWitness
	}{{"expected", expectedTraceJSON, left, expectedWitness}, {"actual", actualTraceJSON, right, actualWitness}} {
		if item.trace.Scenario != model.script.ID {
			return fmt.Errorf("%s script/trace scenario mismatch", item.name)
		}
		if err := causalVerifyWitness(scriptJSON, item.raw, item.trace, item.witness, model); err != nil {
			return fmt.Errorf("%s: %w", item.name, err)
		}
		causalBuiltinNames(item.trace)
	}
	leftEvents, err := model.observe(left, expectedWitness)
	if err != nil {
		return fmt.Errorf("expected: %w", err)
	}
	rightEvents, err := model.observe(right, actualWitness)
	if err != nil {
		return fmt.Errorf("actual: %w", err)
	}
	// Compare every segment, including the tail after the final checkpoint. The
	// multiset is only a matching index; the pair test retains all conflict order.
	li, ri := 0, 0
	for segment := 0; segment <= len(left.Checkpoints); segment++ {
		le, re := len(leftEvents), len(rightEvents)
		if segment < len(left.Checkpoints) {
			le = expectedWitness.Checkpoints[segment].AfterEvent
			re = actualWitness.Checkpoints[segment].AfterEvent
		}
		if err := causalCompareSequence(leftEvents[li:le], rightEvents[ri:re], model.eventsConflict); err != nil {
			return fmt.Errorf("event segment %d: %w", segment, err)
		}
		li, ri = le, re
	}
	if err := model.identities(left, expectedWitness); err != nil {
		return fmt.Errorf("expected identities: %w", err)
	}
	if err := model.identities(right, actualWitness); err != nil {
		return fmt.Errorf("actual identities: %w", err)
	}
	if !causalEqual(left.Diagnostic, right.Diagnostic) {
		return fmt.Errorf("diagnostic mismatch")
	}
	for i := range left.Checkpoints {
		l, r := &left.Checkpoints[i], &right.Checkpoints[i]
		if !causalEqual(l.States, r.States) || !causalEqual(l.Outputs, r.Outputs) {
			return fmt.Errorf("checkpoint %q state/config/epoch/output mismatch", l.Label)
		}
		lp, rp := make([]causalObservation, len(l.Provisions)), make([]causalObservation, len(r.Provisions))
		for j, p := range l.Provisions {
			lp[j] = causalObservation{fingerprint: causalJSON(p), actor: p.Provider}
		}
		for j, p := range r.Provisions {
			rp[j] = causalObservation{fingerprint: causalJSON(p), actor: p.Provider}
		}
		if err := causalCompareSequence(lp, rp, func(a, b causalObservation) bool { return model.actorsConflict(a.actor, b.actor) }); err != nil {
			return fmt.Errorf("checkpoint %q provisions: %w", l.Label, err)
		}
	}
	return nil
}

func causalDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func causalVerifyWitness(scriptJSON, traceJSON []byte, trace *Trace, w CausalWitness, m *causalModel) error {
	if w.Schema != CausalWitnessSchema || w.ScriptSHA256 != causalDigest(scriptJSON) || w.TraceSHA256 != causalDigest(traceJSON) {
		return fmt.Errorf("missing/invalid causal witness schema or exact-byte SHA-256")
	}
	if w.Checkpoints == nil || len(w.Checkpoints) != len(trace.Checkpoints) || len(w.Checkpoints) != len(m.cuts) {
		return fmt.Errorf("causal witness checkpoint count mismatch")
	}
	previous := 0
	for i, cut := range w.Checkpoints {
		if cut.Label != trace.Checkpoints[i].Label || cut.Label != m.cuts[i].label || cut.AfterEvent < previous || cut.AfterEvent > len(trace.Events) {
			return fmt.Errorf("invalid causal checkpoint cut %d", i)
		}
		previous = cut.AfterEvent
	}
	return nil
}

func causalBuiltin(key string) string {
	if key == "events" {
		return "hooks"
	}
	return key
}

func causalRealm(key, realm string) string {
	if key == "events" && realm == "root:events" {
		return "root:hooks"
	}
	return realm
}

// Only the pre-existing structural built-in rename is permitted. In particular,
// payload properties named events, timestamp or provisionUID are never walked.
func causalBuiltinNames(trace *Trace) {
	for i := range trace.Events {
		e := &trace.Events[i]
		if e.Kind == "service" {
			key, _ := e.Data["key"].(string)
			if realm, ok := e.Data["realm"].(string); ok {
				e.Data["realm"] = causalRealm(key, realm)
			}
			e.Data["key"] = causalBuiltin(key)
		}
	}
	for ci := range trace.Checkpoints {
		cp := &trace.Checkpoints[ci]
		for si := range cp.States {
			if cp.States[si].Epoch != nil {
				for ni := range cp.States[si].Epoch.Needs {
					binding := &cp.States[si].Epoch.Needs[ni]
					binding.Key = causalBuiltin(binding.Key)
				}
			}
		}
		for pi := range cp.Provisions {
			p := &cp.Provisions[pi]
			p.Realm = causalRealm(p.Key, p.Realm)
			p.Key = causalBuiltin(p.Key)
		}
	}
}

func causalJSON(value any) string {
	data, _ := json.Marshal(value) // All callers use already validated JSON data.
	return string(data)
}

func causalEqual(a, b any) bool { return bytes.Equal([]byte(causalJSON(a)), []byte(causalJSON(b))) }

type causalObservation struct {
	fingerprint           string
	actor                 string
	rootLane              bool
	pureMount             bool
	startupLoading        bool
	publication           bool
	serviceAddress        causalAddress
	publicationActivation int
	startupProviders      map[causalAddress]int
	unloadingActive       bool
	unloadingRunning      bool
	unloadingActivation   int
	teardownProviders     map[causalAddress]int
}

// Identical occurrences are matched in arrival order, never by global first UID
// observation. Equal event multiplicity plus preservation of every conflicting
// pair is the trace-monoid projection test. O(n^2) is intentional for small traces.
func causalCompareSequence(left, right []causalObservation, conflict func(causalObservation, causalObservation) bool) error {
	if len(left) != len(right) {
		return fmt.Errorf("record count mismatch: %d != %d", len(left), len(right))
	}
	positions, used := make(map[string][]int), make(map[string]int)
	for i, e := range right {
		positions[e.fingerprint] = append(positions[e.fingerprint], i)
	}
	matched := make([]int, len(left))
	for i, e := range left {
		n := used[e.fingerprint]
		if n >= len(positions[e.fingerprint]) {
			return fmt.Errorf("content/multiplicity mismatch at record %d: %s", i+1, e.fingerprint)
		}
		matched[i] = positions[e.fingerprint][n]
		used[e.fingerprint] = n + 1
	}
	for i := range left {
		for j := i + 1; j < len(left); j++ {
			// A conditional exception needs proof in BOTH independently validated
			// streams; differing trigger metadata cannot erase a real conflict.
			conflicting := conflict(left[i], left[j]) || conflict(right[matched[i]], right[matched[j]])
			if conflicting && matched[i] > matched[j] {
				return fmt.Errorf("conflicting order reversed between %s and %s", left[i].fingerprint, left[j].fingerprint)
			}
		}
	}
	return nil
}
