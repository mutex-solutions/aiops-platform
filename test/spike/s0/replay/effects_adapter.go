// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

// effectActivation is deliberately not part of activation: only this fixture
// operation requires native Effect and tracked Go. Neither a private effect
// ledger nor an untracked worker can stand in for these lifecycle capabilities.
type effectActivation interface {
	Effect(string, func() (cc.Disposer, error)) error
	Go(string, func(context.Context) error) error
}

type scopeActivation interface {
	RequestEffectScope(string, func(*cc.EffectScope) error) (*cc.EffectHandle, error)
}

// effectRequests stores references and outstanding completion observations only.
// No disposer or resource is owned here: the actual native scope tree owns them.
// Parent-owned external Settle and inspect seams flush these observations only
// after Apply has returned. See docs/design/s0-d23-request-mapping.md.
type effectRequests struct {
	mu      sync.Mutex
	handles map[string]*cc.EffectHandle
	pending []effectCompletion
}

type effectCompletion struct {
	handle   *cc.EffectHandle
	label    string
	to       string
	catch    bool
	withdraw bool
}

func (f *composedFrontend) effectRequests() *effectRequests {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.effects == nil {
		f.effects = &effectRequests{handles: make(map[string]*cc.EffectHandle)}
	}
	return f.effects
}

func (f *composedFrontend) validateEffect(a differential.Action) (bool, error) {
	switch a.Op {
	case "inspect":
		if a.Mode != "effects" {
			return false, nil
		}
		return true, nil
	case "withdraw":
		if a.Task == "" || a.To != "" || a.Mode != "" {
			return true, unsupported(a.Op, "only native effect handle withdrawal without a completion task is mapped")
		}
		return true, nil
	case "effect", "lateEffect":
	default:
		return false, nil
	}
	if a.Task != "" {
		return true, unsupported(a.Op, "effect setup task handles are not implemented")
	}
	if a.Mode != "" && a.Mode != "partial" || a.Op == "lateEffect" && a.Mode != "" {
		return true, unsupported(a.Op, "unknown effect collection mode")
	}
	if a.Mode == "partial" {
		if !a.Catch || len(a.Program) != 0 || a.Await != nil && !*a.Await {
			return true, unsupported(a.Op, "partial setup requires caught failure and external completion observation, without a setup program/task")
		}
	} else if a.Op == "effect" && a.To != "" && (len(a.Program) != 0 || a.Await != nil && !*a.Await) {
		return true, unsupported(a.Op, "native withdrawable setup currently maps only an immediate disposer, not a source async continuation")
	}
	if err := f.validate(a.Program); err != nil {
		return true, err
	}
	return true, f.validate(a.Cleanup)
}

func (f *composedFrontend) executeEffect(a differential.Action, env environment) (bool, error) {
	// This hook sees every action, not just effect instructions. A source action
	// cannot consume a caught output that the native callback lane has not yet
	// completed and the external observer has not actually published.
	if err := f.rejectPendingEffectOutput(a); err != nil {
		return true, err
	}
	if a.Op == "withdraw" {
		return true, f.requestEffectWithdraw(a, env)
	}
	if a.Op == "inspect" && a.Mode == "effects" {
		return true, f.inspectNativeEffects(a, env)
	}
	if a.Op != "effect" && a.Op != "lateEffect" {
		return false, nil
	}
	if a.Op == "effect" && (a.Mode == "partial" || a.To != "") {
		return true, f.requestNativeEffect(a, env)
	}
	if a.Op == "lateEffect" {
		// The template in frontend.contexts can already describe a replacement.
		// Refusal must come from the actual captured old activation, including
		// during its disposer and after native disposal has completed.
		f.r.mu.Lock()
		target := f.r.fibers[a.Fiber]
		var captured activation
		if target != nil {
			captured = target.ctx
		}
		f.r.mu.Unlock()
		if captured == nil {
			return true, f.catchEffect(a, fmt.Errorf("unknown captured fiber %q", a.Fiber))
		}
		native, ok := captured.(effectActivation)
		if !ok {
			return true, unsupported(a.Op, "native Effect and tracked Go required")
		}
		err := native.Effect(a.Label, func() (cc.Disposer, error) {
			return func(context.Context) error { return nil }, nil
		})
		return true, f.catchEffect(a, err)
	}
	caller, err := f.selected(a, env)
	if err != nil {
		return true, f.catchEffect(a, err)
	}
	env.ctx = caller
	env.std = caller.Std()
	return true, f.catchEffect(a, f.r.executeNativeEffect(a, env))
}

func (f *composedFrontend) requestNativeEffect(a differential.Action, env environment) error {
	caller, err := f.selected(a, env)
	if err != nil {
		return f.catchEffect(a, err)
	}
	native, ok := any(caller).(scopeActivation)
	if !ok {
		return unsupported(a.Op, "native RequestEffectScope required")
	}
	env.ctx, env.std = caller, caller.Std()
	handle, err := native.RequestEffectScope(a.Label, func(scope *cc.EffectScope) error {
		cleanup := func(actions []differential.Action) cc.Disposer {
			return func(ctx context.Context) error {
				current := env
				current.std = ctx // The actual native rollback/withdrawal budget.
				return f.r.run(actions, current)
			}
		}
		if a.Mode == "partial" {
			// Each generator yield transfers a distinct disposer to native
			// incremental ownership. Only the native tree performs rollback.
			for index, action := range a.Cleanup {
				label := action.Label
				if label == "" {
					label = fmt.Sprintf("%s cleanup %d", a.Label, index+1)
				}
				if err := scope.Defer(label, cleanup([]differential.Action{action})); err != nil {
					return err
				}
			}
			message := a.Error
			if message == "" {
				message = "partial-effect-failed"
			}
			return errors.New(message)
		}
		return scope.Defer(a.Label+" cleanup", cleanup(a.Cleanup))
	})
	if err != nil {
		return f.catchEffect(a, err)
	}
	requests := f.effectRequests()
	requests.mu.Lock()
	if a.Mode != "partial" && a.To != "" {
		requests.handles[a.To] = handle
	}
	requests.pending = append(requests.pending, effectCompletion{handle: handle, label: a.Label, to: a.To, catch: a.Catch})
	requests.mu.Unlock()
	// A request from Apply is acknowledgement-only. Completion is externally
	// observed after Apply returns; awaiting here would deadlock its own lane.
	return nil
}

func (f *composedFrontend) requestEffectWithdraw(a differential.Action, env environment) error {
	requests := f.effectRequests()
	requests.mu.Lock()
	handle := requests.handles[a.Task]
	requests.mu.Unlock()
	if handle == nil {
		return fmt.Errorf("unknown disposer %s", a.Task)
	}
	// Do not suppress duplicate requests: both frozen instructions must call
	// the same real native handle. Single-shot cleanup is a core guarantee.
	if err := handle.RequestWithdraw(); err != nil {
		return err
	}
	requests.mu.Lock()
	requests.pending = append(requests.pending, effectCompletion{handle: handle, label: a.Task, withdraw: true})
	requests.mu.Unlock()
	if env.fiber == nil && (a.Await == nil || *a.Await) {
		return f.flushEffectCompletions()
	}
	return nil
}

// flushEffectCompletions is external-only. It observes actual work; it never
// invokes setup/cleanup callbacks, changes ownership or fabricates a result.
func (f *composedFrontend) flushEffectCompletions() error {
	requests := f.effectRequests()
	for {
		requests.mu.Lock()
		if len(requests.pending) == 0 {
			requests.mu.Unlock()
			return nil
		}
		observation := requests.pending[0]
		requests.mu.Unlock()
		var err error
		operation := "effect setup " + observation.label
		if observation.withdraw {
			operation = "effect withdraw " + observation.label
			err = observation.handle.Await(f.r.ctx)
		} else {
			err = observation.handle.AwaitSetup(f.r.ctx)
		}
		if err != nil {
			f.r.mu.Lock()
			f.r.result.RuntimeErrors = append(f.r.result.RuntimeErrors, RuntimeFailure{operation, err})
			f.r.mu.Unlock()
			// These are actual wait/callback causes, not source catch values.
			// Keep the outstanding observation on interrupted external waits.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnsupported) {
				return err
			}
		}
		requests.mu.Lock()
		requests.pending = requests.pending[1:]
		requests.mu.Unlock()
		if err == nil {
			continue
		}
		if observation.withdraw || !observation.catch {
			return err
		}
		// Native AwaitSetup includes the actual rollback result. Preserve the
		// entire error; do not replace it with action.Error or a known sentinel.
		if err := f.catchEffect(differential.Action{Op: "effect", Catch: true, To: observation.to}, err); err != nil {
			return err
		}
	}
}

func (f *composedFrontend) inspectNativeEffects(a differential.Action, env environment) error {
	if env.fiber != nil {
		return unsupported(a.Op, "effect inspection completion waits are external-only")
	}
	if err := f.flushEffectCompletions(); err != nil {
		return err
	}
	f.r.mu.Lock()
	target := f.r.fibers[a.Fiber]
	var id uint64
	if target != nil {
		id = target.id
	}
	f.r.mu.Unlock()
	if target == nil {
		return fmt.Errorf("unknown fiber %q", a.Fiber)
	}
	inspection, err := f.r.runtime.(cc.Inspector).Inspect(f.r.ctx)
	if err != nil {
		return err
	}
	for _, snapshot := range inspection.Fibers {
		if snapshot.ID != id {
			continue
		}
		labels := make([]string, 0, len(snapshot.EffectTree))
		for _, effect := range snapshot.EffectTree {
			labels = append(labels, effect.Label)
		}
		f.r.put(a.To, labels)
		return nil
	}
	return unsupported(a.Op, "native Inspector omitted the mounted fiber's actual effect tree")
}

func (f *composedFrontend) rejectPendingEffectOutput(a differential.Action) error {
	requests := f.effectRequests()
	requests.mu.Lock()
	var pending []string
	for _, observation := range requests.pending {
		if !observation.withdraw && observation.catch && observation.to != "" {
			pending = append(pending, observation.to)
		}
	}
	requests.mu.Unlock()
	var inspect func(any) bool
	inspect = func(value any) bool {
		switch value := value.(type) {
		case map[string]any:
			if path, ok := value["$output"]; ok {
				path := fmt.Sprint(path)
				for _, name := range pending {
					if path == "" || path == name || strings.HasPrefix(path, name+".") {
						return true
					}
				}
			}
			for _, child := range value {
				if inspect(child) {
					return true
				}
			}
		case []any:
			for _, child := range value {
				if inspect(child) {
					return true
				}
			}
		}
		return false
	}
	for _, value := range []any{a.Value, a.Config, a.Meta, a.Base, a.Head} {
		if inspect(value) {
			return unsupported(a.Op, "source continuation reads a pending native effect catch output before external completion observation")
		}
	}
	return nil
}

// executeNativeEffect is also the narrow bridge for root-only replay's ordinary
// effect instruction. The native activation owns every setup lease and disposer.
func (r *interpreter) executeNativeEffect(a differential.Action, env environment) error {
	native, ok := env.ctx.(effectActivation)
	if !ok {
		return unsupported(a.Op, "native Effect and tracked Go required")
	}
	setup := func(current environment, prefix func()) error {
		return native.Effect(a.Label, func() (cc.Disposer, error) {
			if err := r.runEffectSetup(a.Program, current, prefix); err != nil {
				return nil, err
			}
			return func(ctx context.Context) error {
				cleanup := current
				cleanup.std = ctx // Actual fresh native cleanup deadline, not Std.
				return r.run(a.Cleanup, cleanup)
			}, nil
		})
	}
	if a.Await == nil || *a.Await {
		return setup(env, nil)
	}

	// Source run() reaches its first gate synchronously before returning a
	// Promise. Join precisely that observable prefix on the real tracked worker;
	// this channel is a scheduling barrier, never an ownership/completion ledger.
	admittedPrefix := make(chan struct{})
	var once sync.Once
	prefix := func() { once.Do(func() { close(admittedPrefix) }) }
	err := native.Go("effect setup "+a.Label, func(ctx context.Context) error {
		defer prefix() // No gate, failure, or panic must strand the prefix waiter.
		current := env
		current.std = ctx
		err := setup(current, prefix)
		if inactiveEffectOnly(err) {
			// A late resource has already been synchronously cleaned by native
			// Effect. Refused registration on the quiesced old activation is
			// handled shutdown, not a new FAILED transition. Preserve its exact
			// native diagnostic without emitting a source fixture failure.
			r.mu.Lock()
			r.result.RuntimeErrors = append(r.result.RuntimeErrors, RuntimeFailure{"effect " + a.Label, err})
			r.mu.Unlock()
			return nil
		}
		if err != nil {
			if current.fiber != nil {
				return r.capture(current.fiber, err)
			}
			r.mu.Lock()
			r.failures = append(r.failures, err)
			if errors.Is(err, ErrUnsupported) {
				r.fatal = append(r.fatal, err)
			}
			r.eventLocked("error", map[string]any{"op": a.Op, "error": errorValue(err)})
			r.mu.Unlock()
		}
		return err
	})
	if err != nil {
		return err
	}
	select {
	case <-admittedPrefix:
		return nil
	case <-r.ctx.Done():
		return r.ctx.Err()
	}
}

// runEffectSetup records the first real gate before releasing the prefix waiter.
// Fixture gates are explicit script barriers, not plugin I/O: cancellation of
// Std cannot manufacture setup/cleanup completion. The external harness context
// remains the escape hatch. All ordinary instructions use the shared action engine.
func (r *interpreter) runEffectSetup(actions []differential.Action, env environment, prefix func()) error {
	for _, a := range actions {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		switch a.Op {
		case "gate":
			r.mark(a.Label, "gate", r.evaluate(a.Value, env))
			r.mu.Lock()
			done := r.getGate(a.Label).done
			r.mu.Unlock()
			if prefix != nil {
				prefix()
			}
			select {
			case <-done:
			case <-r.ctx.Done():
				return r.ctx.Err()
			}
		case "when":
			if reflect.DeepEqual(r.evaluate(a.Base, env), r.evaluate(a.Value, env)) {
				if err := r.runEffectSetup(a.Program, env, prefix); err != nil {
					return err
				}
			}
		default:
			if err := r.run([]differential.Action{a}, env); err != nil {
				return err
			}
		}
	}
	return nil
}

// Native Effect returns errors.Join of these exact sentinel leaves. Admit only
// the standard join container, not arbitrary Unwrap/Is implementations, formatting
// wrappers or panic payloads. An extra cleanup/deadline cause must remain visible.
var effectJoinType = reflect.TypeOf(errors.Join(cc.ErrInactiveEffect))

func inactiveEffectOnly(err error) bool {
	var visit func(error) (known, effect bool)
	visit = func(err error) (bool, bool) {
		if err == cc.ErrInactiveEffect {
			return true, true
		}
		if err == cc.ErrInactive {
			return true, false
		}
		if err == nil || reflect.TypeOf(err) != effectJoinType {
			return false, false
		}
		children := err.(interface{ Unwrap() []error }).Unwrap()
		if len(children) == 0 {
			return false, false
		}
		hasEffect := false
		for _, child := range children {
			known, effect := visit(child)
			if !known {
				return false, false
			}
			hasEffect = hasEffect || effect
		}
		return true, hasEffect
	}
	known, effect := visit(err)
	return known && effect
}

func effectErrorValue(err error) map[string]any {
	if inactiveEffectOnly(err) {
		return map[string]any{"name": "Error", "message": cc.ErrInactiveEffect.Error(), "code": "INACTIVE_EFFECT"}
	}
	return errorValue(err)
}

func (f *composedFrontend) catchEffect(a differential.Action, err error) error {
	if err == nil || !a.Catch || errors.Is(err, ErrUnsupported) {
		return err
	}
	value := effectErrorValue(err)
	f.r.event("error", map[string]any{"op": a.Op, "error": value})
	f.r.put(a.To, value)
	return nil
}
