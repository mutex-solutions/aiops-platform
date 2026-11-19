// SPDX-License-Identifier: Apache-2.0

package properties

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

type intentionalFailure struct {
	Name       string
	Generation uint64
}

func (e *intentionalFailure) Error() string {
	return fmt.Sprintf("intentional S0 body failure %s input-%d", e.Name, e.Generation)
}

// input is replaced only at a stable Settle barrier, before Update is submitted.
// Apply loads exactly one immutable input; neither callbacks nor the model read
// a mutable failure flag belonging to a later activation.
type activationInput struct {
	Generation uint64
	Value      uint64
	Failure    *intentionalFailure
}

type ownedToken struct{ alive atomic.Bool }

type providedValue struct {
	Slot        int
	Incarnation uint64
	Generation  uint64
	Value       uint64
	Attempt     int64
	Owner       *ownedToken
}

type activation struct {
	Context   *concurrency.Context
	Input     *activationInput
	Token     *ownedToken
	Attempt   int64
	Bindings  map[string]*providedValue
	Provided  *providedValue
	Validated bool // Test goroutine only, after this activation has been released.
}

type actualFiber struct {
	ID            uint64
	Expected      *modelFiber
	Input         atomic.Pointer[activationInput]
	Running       atomic.Int64
	Peak          atomic.Int64
	Applications  atomic.Int64
	Disposals     atomic.Int64
	Owned         atomic.Int64
	mu            sync.Mutex
	Activations   []*activation
	Violations    []string
	AwaitChecked  bool // Stable inspection goroutine only, not a callback input.
	AwaitState    string
	AwaitRevision uint64
	AwaitFailure  uint64
}

func key(slot int) string { return fmt.Sprintf("provider/%02d", slot) }

func (f *actualFiber) violation(format string, values ...any) {
	f.mu.Lock()
	f.Violations = append(f.Violations, fmt.Sprintf(format, values...))
	f.mu.Unlock()
}

func (f *actualFiber) enterCallback(kind string) func() {
	n := f.Running.Add(1)
	for old := f.Peak.Load(); n > old && !f.Peak.CompareAndSwap(old, n); old = f.Peak.Load() {
	}
	if n != 1 {
		f.violation("overlapping %s callbacks: %d", kind, n)
	}
	return func() {
		if remaining := f.Running.Add(-1); remaining != 0 {
			f.violation("%s exit leaves %d callback writers", kind, remaining)
		}
	}
}

func (f *actualFiber) apply(c *concurrency.Context) error {
	leave := f.enterCallback("Apply")
	defer leave()
	input := f.Input.Load()
	if input == nil {
		return errors.New("fixture input was not installed before Mount")
	}
	a := &activation{Context: c, Input: input, Token: &ownedToken{}, Attempt: f.Applications.Add(1), Bindings: make(map[string]*providedValue)}
	a.Token.alive.Store(true)
	f.Owned.Add(1)
	f.mu.Lock()
	f.Activations = append(f.Activations, a)
	f.mu.Unlock()
	if err := c.Defer("owned", func(cleanup context.Context) error {
		leave := f.enterCallback("dispose")
		defer leave()
		if err := cleanup.Err(); err != nil {
			f.violation("unexpected cleanup deadline/cancellation: %v", err)
		}
		if c.Std().Err() == nil || c.Enter() {
			f.violation("disposer entered before cancellation/quiescence")
		}
		for address, expected := range a.Bindings {
			value, err := c.Read(address)
			if err != nil || value != expected || !expected.Owner.alive.Load() {
				f.violation("retained dependency %s was released before consumer cleanup: value=%v error=%v", address, value, err)
			}
		}
		if !a.Token.alive.CompareAndSwap(true, false) {
			f.violation("owned effect disposed twice")
		}
		if remaining := f.Owned.Add(-1); remaining != 0 {
			f.violation("owned effects after dispose = %d", remaining)
		}
		f.Disposals.Add(1)
		return nil
	}); err != nil {
		// Rejected registration is a test failure, not an effect attributed to the
		// runtime. Undo the fixture acquisition while preserving the diagnostic.
		a.Token.alive.Store(false)
		f.Owned.Add(-1)
		f.violation("owned Defer rejected: %v", err)
		return err
	}
	for _, need := range f.Expected.Needs {
		address := key(need)
		value, err := c.Read(address)
		if err != nil {
			f.violation("declared Read %s: %v", address, err)
			return err
		}
		provided, ok := value.(*providedValue)
		if !ok || provided.Slot != need || !provided.Owner.alive.Load() {
			f.violation("invalid live dependency at %s: %v", address, value)
			return errors.New("invalid real dependency identity")
		}
		a.Bindings[address] = provided
	}
	if f.Expected.Slot < providerSlots {
		a.Provided = &providedValue{Slot: f.Expected.Slot, Incarnation: f.Expected.Incarnation, Generation: input.Generation, Value: input.Value, Attempt: a.Attempt, Owner: a.Token}
		if err := c.Provide(key(f.Expected.Slot), a.Provided); err != nil {
			f.violation("valid provision rejected: %v", err)
			return err
		}
	}
	// Fail only after registering the real owned effect and provision. A failed
	// attempt must unwind both, without becoming available to a consumer.
	if input.Failure != nil {
		return input.Failure
	}
	return nil
}

type runHarness struct {
	Runtime    concurrency.Runtime
	Inspector  concurrency.Inspector
	Waiter     concurrency.Waiter
	Model      model
	Current    [totalSlots]*actualFiber
	Fibers     []*actualFiber
	Errors     map[string]map[uint64]*intentionalFailure
	SeenUIDs   map[uint64]*providedValue
	ValueUIDs  map[*providedValue]uint64
	Last       concurrency.Inspection
	LastEvents []concurrency.Event
}

func newHarness(r concurrency.Runtime) (*runHarness, error) {
	inspector, ok := r.(concurrency.Inspector)
	if !ok {
		return nil, errors.New("real runtime has no Inspector")
	}
	waiter, ok := r.(concurrency.Waiter)
	if !ok {
		return nil, errors.New("real runtime has no native AwaitFiber")
	}
	return &runHarness{Runtime: r, Inspector: inspector, Waiter: waiter, Errors: make(map[string]map[uint64]*intentionalFailure), SeenUIDs: make(map[uint64]*providedValue), ValueUIDs: make(map[*providedValue]uint64)}, nil
}

func (h *runHarness) input(f *actualFiber, op Operation) *activationInput {
	input := &activationInput{Generation: op.Generation, Value: op.Value}
	if op.Fail {
		input.Failure = &intentionalFailure{Name: f.Expected.name(), Generation: op.Generation}
		if h.Errors[f.Expected.name()] == nil {
			h.Errors[f.Expected.name()] = make(map[uint64]*intentionalFailure)
		}
		h.Errors[f.Expected.name()][op.Generation] = input.Failure
	}
	return input
}

func (h *runHarness) step(op Operation) error {
	previous := h.Current[op.Slot]
	if err := h.Model.apply(op); err != nil {
		return fmt.Errorf("independent model: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch op.Kind {
	case "mount":
		f := &actualFiber{Expected: h.Model.Current[op.Slot]}
		f.Input.Store(h.input(f, op))
		needs := make([]string, len(op.Needs))
		for i, need := range op.Needs {
			needs[i] = key(need)
		}
		spec := concurrency.Spec{ID: f.Expected.name(), Needs: needs, Apply: f.apply}
		if op.Slot < providerSlots {
			spec.Provides = []string{key(op.Slot)}
		}
		id, err := h.Runtime.Mount(ctx, spec)
		if err != nil {
			return fmt.Errorf("real Mount: %w", err)
		}
		if id == 0 {
			return errors.New("real Mount returned zero fiber identity")
		}
		for _, old := range h.Fibers {
			if id == old.ID {
				return fmt.Errorf("reused fiber identity %d", id)
			}
		}
		f.ID = id
		h.Current[op.Slot] = f
		h.Fibers = append(h.Fibers, f)
	case "dispose":
		if err := h.Runtime.Dispose(ctx, previous.ID); err != nil {
			return fmt.Errorf("real Dispose: %w", err)
		}
		h.Current[op.Slot] = nil
	default:
		previous.Input.Store(h.input(previous, op))
		if err := h.Runtime.Update(ctx, previous.ID); err != nil {
			return fmt.Errorf("real Update: %w", err)
		}
	}
	settleErr := h.Runtime.Settle(ctx)
	inspection, err := h.Inspector.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("real Inspect after Settle (%v): %w", settleErr, err)
	}
	h.Last = inspection
	if err := h.assertStable(ctx, inspection); err != nil {
		return fmt.Errorf("stable-state invariant (Settle=%v): %w", settleErr, err)
	}
	// GraphLock's Settle is only a join; Reconciler's also reports startup
	// rejection. Only exact known failures of currently FAILED fibers may be
	// unwrapped here. AwaitFiber below independently checks every error identity.
	allowed := make(map[error]bool)
	for _, f := range h.Fibers {
		if f.Expected.State == "FAILED" {
			allowed[h.Errors[f.Expected.name()][f.Expected.FailureGeneration]] = true
		}
	}
	if !onlyKnownErrors(settleErr, allowed) {
		return fmt.Errorf("unexpected Settle error: %w", settleErr)
	}
	return nil
}

// onlyKnownErrors rejects unknown leaves even when an aggregate also contains an
// intentional failure. No string matching, blanket errors.Is, or deadline/panic
// exemption can turn an unexpected runtime error into a pass.
func onlyKnownErrors(err error, allowed map[error]bool) bool {
	if err == nil {
		return true
	}
	if allowed[err] {
		return true
	}
	if many, ok := err.(interface{ Unwrap() []error }); ok {
		children := many.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if child == nil || !onlyKnownErrors(child, allowed) {
				return false
			}
		}
		return true
	}
	if one, ok := err.(interface{ Unwrap() error }); ok && one.Unwrap() != nil {
		return onlyKnownErrors(one.Unwrap(), allowed)
	}
	return false
}

func (h *runHarness) assertStable(ctx context.Context, inspection concurrency.Inspection) error {
	if len(inspection.Fibers) != len(h.Fibers) {
		return fmt.Errorf("inspected fibers=%d want %d including archives", len(inspection.Fibers), len(h.Fibers))
	}
	byID := make(map[uint64]concurrency.Snapshot, len(inspection.Fibers))
	for _, s := range inspection.Fibers {
		if _, duplicate := byID[s.ID]; duplicate {
			return fmt.Errorf("duplicate snapshot fiber identity %d", s.ID)
		}
		byID[s.ID] = s
	}
	byKey := make(map[string]concurrency.ProvisionSnapshot, len(inspection.Provisions))
	seenUIDs := make(map[uint64]bool)
	for _, p := range inspection.Provisions {
		if _, duplicate := byKey[p.Key]; duplicate || p.UID == 0 || seenUIDs[p.UID] {
			return fmt.Errorf("duplicate address or zero/repeated actual provision UID: %+v", p)
		}
		byKey[p.Key], seenUIDs[p.UID] = p, true
		value, ok := p.Value.(*providedValue)
		if !ok || !p.Active || !value.Owner.alive.Load() {
			return fmt.Errorf("non-active or unowned provision at stable barrier: %+v", p)
		}
		if previous, exists := h.SeenUIDs[p.UID]; exists && previous != value {
			return fmt.Errorf("provision UID %d reused for another actual identity", p.UID)
		}
		if previous, exists := h.ValueUIDs[value]; exists && previous != p.UID {
			return fmt.Errorf("actual provision identity changed its UID from %d to %d", previous, p.UID)
		}
		h.SeenUIDs[p.UID], h.ValueUIDs[value] = value, p.UID
	}
	wantProvisions, wantOwned, aliveTokens := 0, int64(0), int64(0)
	for _, f := range h.Fibers {
		m := f.Expected
		s, ok := byID[f.ID]
		if !ok || s.Name != m.name() || string(s.State) != m.State || s.Alive != m.Alive || s.Revision != m.Revision {
			return fmt.Errorf("%s stable snapshot=%+v; model state=%s alive=%t revision=%d", m.name(), s, m.State, m.Alive, m.Revision)
		}
		if s.Configured || s.ConfigPrepared || s.RawConfig != nil || s.EffectiveConfig != nil || s.ConfigVersion != 0 {
			return fmt.Errorf("%s acquired an undeclared configuration", m.name())
		}
		f.mu.Lock()
		violations := slices.Clone(f.Violations)
		activations := slices.Clone(f.Activations)
		f.mu.Unlock()
		if len(violations) != 0 {
			return fmt.Errorf("%s callback invariant: %s", m.name(), strings.Join(violations, "; "))
		}
		if f.Running.Load() != 0 || f.Peak.Load() > 1 || f.Applications.Load() != m.Applications || f.Disposals.Load() != m.Disposals {
			return fmt.Errorf("%s callbacks: running=%d peak=%d apply=%d/%d dispose=%d/%d", m.name(), f.Running.Load(), f.Peak.Load(), f.Applications.Load(), m.Applications, f.Disposals.Load(), m.Disposals)
		}
		owned := int64(0)
		if m.State == "ACTIVE" {
			owned = 1
		}
		wantOwned += owned
		if f.Owned.Load() != owned || f.Applications.Load()-f.Disposals.Load() != owned {
			return fmt.Errorf("%s owned ledger=%d want %d", m.name(), f.Owned.Load(), owned)
		}
		var expectedError *intentionalFailure
		if m.FailureGeneration != 0 {
			expectedError = h.Errors[m.name()][m.FailureGeneration]
			if expectedError == nil {
				return fmt.Errorf("%s has model failure without a generated error identity", m.name())
			}
		}
		if expectedError == nil {
			if s.Error != "" {
				return fmt.Errorf("%s unexpected snapshot diagnostic %q", m.name(), s.Error)
			}
		} else {
			// Reconciler exposes startup and retained Await diagnostics separately;
			// both must be exactly the same generated error, with nothing unknown.
			if s.Error == "" {
				return fmt.Errorf("%s lost its sticky diagnostic", m.name())
			}
			for _, line := range strings.Split(s.Error, "\n") {
				if line != expectedError.Error() {
					return fmt.Errorf("%s unknown snapshot diagnostic %q", m.name(), s.Error)
				}
			}
		}
		// Immutable archives are still inspected on every operation, but need
		// not exchange another Await command on every unrelated later update.
		// Every new native state/revision/sticky-error generation is checked.
		if !f.AwaitChecked || f.AwaitState != m.State || f.AwaitRevision != m.Revision || f.AwaitFailure != m.FailureGeneration {
			awaitErr := h.Waiter.AwaitFiber(ctx, f.ID)
			if expectedError == nil {
				if awaitErr != nil {
					return fmt.Errorf("%s unexpected native AwaitFiber error: %w", m.name(), awaitErr)
				}
			} else if awaitErr == nil || !errors.Is(awaitErr, expectedError) || !onlyKnownErrors(awaitErr, map[error]bool{expectedError: true}) {
				return fmt.Errorf("%s native AwaitFiber lost known error identity: %v", m.name(), awaitErr)
			}
			f.AwaitChecked, f.AwaitState, f.AwaitRevision, f.AwaitFailure = true, m.State, m.Revision, m.FailureGeneration
		}
		if m.State == "ACTIVE" {
			if len(activations) == 0 {
				return fmt.Errorf("%s ACTIVE without Apply", m.name())
			}
			a := activations[len(activations)-1]
			if a.Input.Generation != m.LastGeneration || a.Input.Value != m.LastValue || a.Input.Failure != nil || a.Attempt != m.Applications {
				return fmt.Errorf("%s applied stale/non-immutable input", m.name())
			}
			wantUIDs := make([]uint64, len(m.Needs))
			for i, need := range m.Needs {
				p, exists := byKey[key(need)]
				provider := h.Current[need]
				if !exists || provider == nil || p.Owner != provider.ID || p.Name != provider.Expected.name() || a.Bindings[key(need)] != p.Value {
					return fmt.Errorf("%s dependency %d is not bound to the real active provision identity", m.name(), need)
				}
				wantUIDs[i] = p.UID
			}
			if s.Target == nil || s.Target.Revision != m.Revision || !slices.Equal(s.Target.UIDs, wantUIDs) || !slices.Equal(s.Epoch, wantUIDs) {
				return fmt.Errorf("%s actual target/committed epoch=%+v/%v want actual active-provider UIDs %v", m.name(), s.Target, s.Epoch, wantUIDs)
			}
			if m.Slot < providerSlots {
				wantProvisions++
				p, exists := byKey[key(m.Slot)]
				if !exists || p.Owner != f.ID || p.Name != m.name() || p.Value != a.Provided || a.Provided.Value != m.Value || a.Provided.Generation != m.Generation || a.Provided.Attempt != m.Identity.Attempt {
					return fmt.Errorf("%s missing/stale provision or value: %+v", m.name(), p)
				}
			}
		} else if s.Target != nil || len(s.Effects) != 0 {
			return fmt.Errorf("%s inactive target/effects remain: %+v", m.name(), s)
		}
		ownedLabels := 0
		for _, effect := range s.Effects {
			if effect == "owned" {
				ownedLabels++
			} else if m.Slot >= providerSlots || effect != "provide:"+key(m.Slot) {
				return fmt.Errorf("%s undeclared effect %q", m.name(), effect)
			}
		}
		if int64(ownedLabels) != owned {
			return fmt.Errorf("%s owned effect labels=%d want %d", m.name(), ownedLabels, owned)
		}
		for i, a := range activations {
			if a.Token.alive.Load() {
				aliveTokens++
				if m.State != "ACTIVE" || i != len(activations)-1 || a.Context.Std().Err() != nil || !a.Context.Enter() {
					return fmt.Errorf("%s inactive activation retained a live proof", m.name())
				}
				a.Context.Leave()
				continue
			}
			if !a.Validated {
				if a.Context.Std().Err() == nil || a.Context.Enter() || !errors.Is(a.Context.Defer("post-release", func(context.Context) error { return nil }), concurrency.ErrInactive) || !errors.Is(a.Context.Provide("post-release", nil), concurrency.ErrInactive) {
					return fmt.Errorf("%s released context accepts activity", m.name())
				}
				for _, need := range m.Needs {
					if _, err := a.Context.Read(key(need)); !errors.Is(err, concurrency.ErrInactive) {
						return fmt.Errorf("%s released context Read = %v", m.name(), err)
					}
				}
				if a.Context.Owns(key(m.Slot)) {
					return fmt.Errorf("%s released context still owns an address", m.name())
				}
				a.Validated = true
			}
		}
	}
	if len(inspection.Provisions) != wantProvisions || aliveTokens != wantOwned {
		return fmt.Errorf("stable ledgers: actual provisions=%d want=%d; alive owned proofs=%d want=%d", len(inspection.Provisions), wantProvisions, aliveTokens, wantOwned)
	}
	return nil
}

func (h *runHarness) finish() error {
	// Inspect the actual archives before Close: Reconciler's native Close joins
	// and terminates its owner, so post-Close inspection is intentionally invalid.
	for slot := totalSlots - 1; slot >= 0; slot-- {
		if f := h.Current[slot]; f != nil {
			m := f.Expected
			if err := h.step(Operation{Kind: "dispose", Slot: slot, Incarnation: m.Incarnation, Generation: m.Generation, Value: m.Value, Fail: m.Fail}); err != nil {
				return fmt.Errorf("final reverse-topological dispose: %w", err)
			}
		}
	}
	if len(h.Last.Provisions) != 0 {
		return errors.New("active/reserved provisions remain after complete disposal")
	}
	for _, s := range h.Last.Fibers {
		if s.State != concurrency.Disposed || s.Alive || s.Target != nil || len(s.Effects) != 0 {
			return fmt.Errorf("incomplete disposed archive: %+v", s)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, err := h.Runtime.Events(ctx)
	if err != nil {
		return fmt.Errorf("final actual Events: %w", err)
	}
	h.LastEvents = events
	for i, event := range events {
		if event.Sequence != uint64(i+1) || strings.Contains(event.Kind, "deadline") {
			return fmt.Errorf("missing/duplicate event or unexpected deadline: %+v", event)
		}
	}
	if err := h.Runtime.Close(ctx); err != nil {
		return fmt.Errorf("Close after actual empty-ledger inspection: %w", err)
	}
	return nil
}

type fileIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type evidenceIdentity struct {
	CriteriaSHA256   string         `json:"criteriaSHA256"`
	BaselineCommit   string         `json:"baselineCommit"`
	SourceSHA256     string         `json:"sourceSHA256"`
	Sources          []fileIdentity `json:"sources"`
	ExecutableSHA256 string         `json:"executableSHA256"`
	GoVersion        string         `json:"goVersion"`
	GOMAXPROCS       int            `json:"gomaxprocs"`
}

func repositoryRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
			return cwd, nil
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", errors.New("cannot locate module root")
		}
		cwd = parent
	}
}

func digestFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func identity(root string) (evidenceIdentity, error) {
	result := evidenceIdentity{GoVersion: runtime.Version(), GOMAXPROCS: runtime.GOMAXPROCS(0)}
	criteria, err := digestFile(filepath.Join(root, "docs/design/s0-spike-criteria.md"))
	if err != nil {
		return result, err
	}
	result.CriteriaSHA256 = criteria
	freezeBytes, err := os.ReadFile(filepath.Join(root, "docs/design/s0-criteria-freeze.json"))
	if err != nil {
		return result, err
	}
	var freeze struct {
		Criteria struct {
			SHA256 string `json:"sha256"`
		} `json:"criteria"`
	}
	if err := json.Unmarshal(freezeBytes, &freeze); err != nil {
		return result, err
	}
	if criteria != freeze.Criteria.SHA256 {
		return result, fmt.Errorf("criteria bytes differ from frozen receipt: %s != %s", criteria, freeze.Criteria.SHA256)
	}
	command := exec.Command("git", "rev-parse", "HEAD")
	command.Dir = root
	commit, err := command.Output()
	if err != nil {
		return result, err
	}
	result.BaselineCommit = strings.TrimSpace(string(commit))
	paths := []string{"go.mod", "go.sum", "docs/design/s0-spike-criteria.md", "docs/design/s0-criteria-freeze.json"}
	// Freeze the actual local package closure, not unrelated S0 probes that do
	// not enter this executable. The native observer queue is now linked by the
	// runtime; its source and leak footprint must join this exact local closure.
	for _, directory := range []string{"test/spike/s0/concurrency", "test/spike/s0/properties", "test/spike/s0/internal/eventqueue"} {
		if err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".go") {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				paths = append(paths, filepath.ToSlash(relative))
			}
			return nil
		}); err != nil {
			return result, err
		}
	}
	slices.Sort(paths)
	hash := sha256.New()
	for _, path := range paths {
		digest, err := digestFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return result, err
		}
		result.Sources = append(result.Sources, fileIdentity{Path: path, SHA256: digest})
		fmt.Fprintf(hash, "%s\x00%s\n", path, digest)
	}
	result.SourceSHA256 = hex.EncodeToString(hash.Sum(nil))
	executable, err := os.Executable()
	if err != nil {
		return result, err
	}
	result.ExecutableSHA256, err = digestFile(executable)
	return result, err
}

type failureRecord struct {
	Kind         string                 `json:"kind"`
	Backend      string                 `json:"backend"`
	Seed         uint64                 `json:"seed"`
	PRNG         string                 `json:"prng"`
	Started      string                 `json:"startedUTC"`
	Finished     string                 `json:"finishedUTC"`
	Identity     evidenceIdentity       `json:"identity"`
	Failure      string                 `json:"failure"`
	FirstFailing int                    `json:"firstFailingOperation"`
	Operations   []Operation            `json:"operations"`
	Model        model                  `json:"independentModel"`
	Inspection   concurrency.Inspection `json:"actualInspection"`
	Events       []concurrency.Event    `json:"actualEvents"`
}

func saveJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func goroutines() map[uint64]string {
	buffer := make([]byte, 16384)
	for {
		n := runtime.Stack(buffer, true)
		if n < len(buffer) {
			result := make(map[uint64]string)
			for _, stack := range strings.Split(strings.TrimSpace(string(buffer[:n])), "\n\n") {
				fields := strings.Fields(stack)
				if len(fields) >= 2 && fields[0] == "goroutine" {
					id, err := strconv.ParseUint(fields[1], 10, 64)
					if err == nil {
						result[id] = stack
					}
				}
			}
			return result
		}
		buffer = make([]byte, len(buffer)*2)
	}
}

func ownedGoroutine(stack string) bool {
	return strings.Contains(stack, "/test/spike/s0/concurrency.") ||
		strings.Contains(stack, "/test/spike/s0/internal/eventqueue.") ||
		strings.Contains(stack, "/test/spike/s0/properties.(*actualFiber).")
}

func assertNoGoroutineGrowth(baseline map[uint64]string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		var remaining []string
		for id, stack := range goroutines() {
			if _, existed := baseline[id]; !existed && ownedGoroutine(stack) {
				remaining = append(remaining, stack)
			}
		}
		if len(remaining) == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("new fixture/runtime goroutines remain after Close (unrelated Go runtime/testing goroutines are not count-offset exemptions):\n%s", strings.Join(remaining, "\n\n"))
		}
		// Close acknowledges just before its state-owner goroutine returns. A
		// scheduler yield joins that finite tail without sleeping or count slack.
		runtime.Gosched()
	}
}

func TestRuntimeProperties(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	before, err := identity(root)
	if err != nil {
		t.Fatal(err)
	}
	full := os.Getenv("S0_PROPERTY_FULL") == "1"
	if full && os.Getenv("S0_PROPERTY_REPLAY") != "" {
		t.Fatal("a saved-prefix replay cannot count as the full gate")
	}
	seeds, operations, mode := 16, 128, "smoke-NOT-full-gate"
	if full {
		seeds, operations, mode = 1000, 1000, "full-1000-seeds-x-1000-operations-per-real-candidate"
		if os.Getenv("S0_PROPERTY_SOURCE_SHA256") != before.SourceSHA256 {
			t.Fatalf("full corpus suspended: parent must freeze exact source and set S0_PROPERTY_SOURCE_SHA256=%s", before.SourceSHA256)
		}
		if runtime.GOMAXPROCS(0) != 8 {
			t.Fatal("full corpus requires frozen GOMAXPROCS=8")
		}
	}
	t.Logf("mode=%s PRNG=SplitMix64 seed=0x%x+index seeds=%d operations=%d providers=%d consumers=%d criteria=%s baseline=%s source=%s executable=%s go=%s GOMAXPROCS=%d", mode, firstSeed, seeds, operations, providerSlots, consumerSlots, before.CriteriaSHA256, before.BaselineCommit, before.SourceSHA256, before.ExecutableSHA256, before.GoVersion, before.GOMAXPROCS)
	baseline := goroutines()
	defer func() {
		if err := assertNoGoroutineGrowth(baseline); err != nil {
			t.Error(err)
		}
		after, err := identity(root)
		if err != nil {
			t.Error(err)
		} else if after.SourceSHA256 != before.SourceSHA256 {
			t.Errorf("INVALID evidence: source changed during corpus: %s -> %s; rerun after parent freeze", before.SourceSHA256, after.SourceSHA256)
		}
	}()
	backends := []struct {
		name string
		make func() concurrency.Runtime
	}{{"reconciler", concurrency.NewRecordingReconciler}, {"graph-lock", concurrency.NewRecordingGraphLock}}
	var replay *failureRecord
	if path := os.Getenv("S0_PROPERTY_REPLAY"); path != "" {
		if full {
			t.Fatal("a saved-prefix replay cannot count as the full gate")
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		replay = &failureRecord{}
		if err := json.Unmarshal(bytes, replay); err != nil {
			t.Fatal(err)
		}
		if replay.Kind != "s0-property-first-failing-prefix" || (replay.Backend != "reconciler" && replay.Backend != "graph-lock") || len(replay.Operations) == 0 {
			t.Fatal("invalid replay: expected a nonempty S0 first-failing prefix for an actual candidate")
		}
		seeds, mode = 1, "replay-NOT-full-gate"
		t.Logf("replaying saved commands seed=0x%x operations=%d; NOT a full gate", replay.Seed, len(replay.Operations))
	}
	var completedBackends []string
	for _, backend := range backends {
		if replay != nil && replay.Backend != backend.name {
			continue
		}
		if full {
			current, err := identity(root)
			if err != nil || current.SourceSHA256 != before.SourceSHA256 {
				t.Fatalf("full corpus suspended before %s: source changed after freeze (identity error=%v)", backend.name, err)
			}
		}
		t.Run(backend.name, func(t *testing.T) {
			for index := 0; index < seeds; index++ {
				seed := firstSeed + uint64(index)
				commands := generate(seed, operations)
				if replay != nil {
					seed, commands = replay.Seed, replay.Operations
				}
				started := time.Now().UTC()
				h, err := newHarness(backend.make())
				if err != nil {
					t.Fatal(err)
				}
				failureAt := -1
				for i, command := range commands {
					if err = h.step(command); err != nil {
						failureAt = i
						break
					}
				}
				if err == nil {
					err = h.finish()
					if err != nil {
						failureAt = len(commands)
					}
				}
				if err != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					if inspection, inspectErr := h.Inspector.Inspect(ctx); inspectErr == nil {
						h.Last = inspection
					}
					if events, eventsErr := h.Runtime.Events(ctx); eventsErr == nil {
						h.LastEvents = events
					}
					closeErr := h.Runtime.Close(ctx)
					cancel()
					prefix := commands
					if failureAt >= 0 && failureAt < len(commands) {
						prefix = commands[:failureAt+1]
					}
					record := failureRecord{Kind: "s0-property-first-failing-prefix", Backend: backend.name, Seed: seed, PRNG: "SplitMix64", Started: started.Format(time.RFC3339Nano), Finished: time.Now().UTC().Format(time.RFC3339Nano), Identity: before, Failure: fmt.Sprintf("%v; emergency Close=%v", err, closeErr), FirstFailing: failureAt, Operations: prefix, Model: h.Model, Inspection: h.Last, Events: h.LastEvents}
					directory := os.Getenv("S0_PROPERTY_RESULTS")
					if directory == "" {
						directory = filepath.Join(root, "test/spike/s0/properties/evidence")
					}
					path := filepath.Join(directory, fmt.Sprintf("failure-%s-%08x-%d.json", backend.name, seed, time.Now().UTC().UnixNano()))
					if saveErr := saveJSON(path, record); saveErr != nil {
						t.Errorf("cannot preserve failing commands: %v", saveErr)
					}
					encoded, _ := json.Marshal(prefix)
					t.Fatalf("seed=0x%x operation=%d: %v\nreproduction=%s\ncommandStream=%s", seed, failureAt, record.Failure, path, encoded)
				}
				if err := assertNoGoroutineGrowth(baseline); err != nil {
					t.Fatalf("seed=0x%x %v", seed, err)
				}
			}
		})
		if !t.Failed() {
			completedBackends = append(completedBackends, backend.name)
		}
	}
	// Validate before writing a pass record (the deferred validation also runs
	// on failure paths). Changed source must never leave a misleading pass JSON.
	after, identityErr := identity(root)
	if identityErr != nil || after.SourceSHA256 != before.SourceSHA256 {
		t.Errorf("INVALID evidence: source changed before result publication (%s -> %s; identity error=%v)", before.SourceSHA256, after.SourceSHA256, identityErr)
	}
	if directory := os.Getenv("S0_PROPERTY_RESULTS"); directory != "" && !t.Failed() {
		record := struct {
			Kind     string           `json:"kind"`
			Mode     string           `json:"mode"`
			Seeds    int              `json:"seedsPerCandidate"`
			Ops      int              `json:"operationsPerSeed"`
			PRNG     string           `json:"prng"`
			First    uint64           `json:"firstSeed"`
			Backends []string         `json:"realBackends"`
			Identity evidenceIdentity `json:"identity"`
		}{"s0-property-result", mode, seeds, operations, "SplitMix64", firstSeed, completedBackends, before}
		if err := saveJSON(filepath.Join(directory, fmt.Sprintf("pass-%s-%d.json", mode, time.Now().UTC().UnixNano())), record); err != nil {
			t.Error(err)
		}
	}
}

// TestPropertyIdentity prints a freeze receipt without running any runtime or
// requiring a deliberately failed full-mode preflight to discover its digest.
func TestPropertyIdentity(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	value, err := identity(root)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("freezeIdentity=%s", encoded)
}

func TestFullPropertyGate(t *testing.T) {
	if os.Getenv("S0_PROPERTY_FULL") != "1" {
		t.Skip("NOT RUN: frozen 1000 seeds x 1000 operations per real backend under -race; set S0_PROPERTY_FULL=1 only after parent source freeze")
	}
	// TestRuntimeProperties owns the corpus so running ./properties does not run
	// it twice. Selecting this marker alone is deliberately not a full-gate pass.
	t.Skip("full corpus is TestRuntimeProperties; this marker is not acceptance evidence")
}
