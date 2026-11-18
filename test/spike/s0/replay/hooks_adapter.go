// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	co "github.com/mutex-solutions/aiops-platform/test/spike/s0/composition"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
	hk "github.com/mutex-solutions/aiops-platform/test/spike/s0/hooks"
)

// This is an S0 fixture-language bridge, not another dispatcher or service store.
// Each definition and listener below belongs to the actual hooks engine. One
// frontend installs one real, root-effect-owned bundle; no user fiber is minted.
type hookIdentity struct {
	text   string
	symbol bool
}

type hookDefinition struct {
	event     *hk.Event[any]
	waterfall *hk.Waterfall[any, any]
}

type hookBundle struct {
	key         cm.Key[*hookBundle]
	owner       *co.Context
	binding     hk.Binding
	definitions map[hookIdentity]*hookDefinition
	closed      atomic.Bool
	mu          sync.Mutex
	tasks       map[string]*hk.Completion
	taskIDs     map[string]bool
	setupErr    error
}

// hookKeys runs before NewRuntime so the typed builtin key is genuinely compiled
// into every composition context. Event strings and symbol keys are NOT services.
func (f *composedFrontend) hookKeys() []cm.AnyKey {
	b := &hookBundle{key: cm.NewKey[*hookBundle]("hooks", 0), definitions: make(map[hookIdentity]*hookDefinition),
		tasks: make(map[string]*hk.Completion), taskIDs: make(map[string]bool)}
	var actions []differential.Action
	for _, plugin := range f.script.Plugins {
		actions = append(actions, plugin.Apply...)
		actions = append(actions, plugin.Cleanup...)
	}
	actions = append(actions, f.script.Steps...)
	var collect func([]differential.Action)
	collect = func(program []differential.Action) {
		for _, a := range program {
			if isUpdateListener(a) {
				if f.updates == nil {
					f.updates = newUpdateHookAdapter()
				}
			} else if a.Op == "listen" || a.Op == "dispatch" {
				id := hookIdentity{a.Key, a.Symbol}
				if b.definitions[id] == nil {
					spec := hk.Definition{Name: a.Key, Owner: b.key.Any(), Stages: []string{"default"}}
					event, eventErr := hk.NewEvent[any](spec)
					waterfall, waterfallErr := hk.NewWaterfall[any, any](spec)
					b.setupErr = errors.Join(b.setupErr, eventErr, waterfallErr)
					b.definitions[id] = &hookDefinition{event: event, waterfall: waterfall}
				}
			}
			if a.Task != "" && a.Op != "awaitTask" {
				if b.taskIDs[a.Task] {
					b.setupErr = errors.Join(b.setupErr, unsupported("task", "duplicate task labels across hook and lifecycle operations"))
				}
				b.taskIDs[a.Task] = true
			}
			collect(a.Program)
			collect(a.Cleanup)
		}
	}
	collect(actions)
	if len(b.definitions) == 0 && f.updates == nil {
		return nil
	}
	f.hooks = b
	if _, exists := f.keys["hooks"]; exists {
		b.setupErr = errors.Join(b.setupErr, unsupported("hooks service", "ordinary string service key collides with the typed builtin hooks name"))
		return nil // Let initHooks report the adapter limit before any installation.
	}
	return []cm.AnyKey{b.key.Any()}
}

func (f *composedFrontend) initHooks() error {
	b := f.hooks
	if b == nil {
		return nil
	}
	if b.setupErr != nil {
		return b.setupErr
	}
	// String service keys are Key[any], not the builtin Key[*hookBundle]. Do not
	// turn a textual collision into implicit authority over the actual bundle.
	if _, exists := f.keys["hooks"]; exists {
		return unsupported("hooks service", "ordinary string service key collides with the typed builtin hooks name")
	}
	owner, err := co.InstallBuiltin(f.r.ctx, f.root, b.key, b, func(_ context.Context, value *hookBundle) error {
		value.closed.Store(true)
		value.mu.Lock()
		defer value.mu.Unlock()
		value.tasks = nil
		value.definitions = nil
		return nil
	})
	if err != nil {
		return err
	}
	binding, err := hk.Bind(owner)
	if err != nil {
		return err
	}
	b.owner, b.binding = owner, binding
	return nil
}

func (f *composedFrontend) validateHook(a differential.Action) (bool, error) {
	if a.Op != "listen" && a.Op != "dispatch" && a.Op != "return" && a.Op != "next" {
		return false, nil
	}
	if a.Op == "return" || a.Op == "next" {
		return true, unsupported(a.Op, "only supported inside a hook listener program")
	}
	if len(f.script.Plugins) != 0 || len(f.script.Contexts) != 0 || a.Fiber != "" || a.Context != "" && a.Context != "root" {
		return true, unsupported(a.Op, "hook replay currently supports genuine host-root listeners only")
	}
	if a.Global || a.Prepend {
		return true, unsupported(a.Op, "global or prepend hooks have no accepted stage declaration")
	}
	if a.Op == "dispatch" {
		switch a.Mode {
		case "emit", "parallel", "serial", "bail", "waterfall":
		default:
			return true, unsupported(a.Op, "unknown hook dispatch mode")
		}
		return true, nil
	}
	for _, step := range a.Program {
		switch step.Op {
		case "mark", "gate", "output", "fail", "return", "next":
		default:
			return true, unsupported(step.Op, "not implemented inside hook listener program")
		}
		if step.Context != "" || step.Fiber != "" || len(step.Program) != 0 || len(step.Cleanup) != 0 {
			return true, unsupported(step.Op, "nested or retargeted hook instructions are not implemented")
		}
	}
	return true, nil
}

func (f *composedFrontend) executeHook(a differential.Action, env environment) (bool, error) {
	b := f.hooks
	if b == nil {
		return false, nil
	}
	if b.closed.Load() {
		return true, hk.ErrInactive
	}
	if isUpdateListener(a) {
		return true, f.catchHook(a, f.updates.listen(f, a, env))
	}
	if a.Op == "awaitTask" {
		b.mu.Lock()
		task := b.tasks[a.Task]
		b.mu.Unlock()
		if task == nil {
			return false, nil
		}
		_, err := task.Await(env.std)
		return true, f.catchHook(a, err)
	}
	if a.Op != "listen" && a.Op != "dispatch" {
		return false, nil
	}
	definition := b.definitions[hookIdentity{a.Key, a.Symbol}]
	if definition == nil {
		return true, unsupported(a.Op, "hook definition was not collected before root construction")
	}
	if a.Op == "listen" {
		return true, f.catchHook(a, f.listenHook(a, env, definition))
	}
	call := func(ctx context.Context) (hk.Decision, error) {
		audience := cm.Audience{}
		switch a.Mode {
		case "emit":
			return hk.Decision{}, definition.event.Emit(ctx, b.binding, audience, nil)
		case "parallel":
			return hk.Decision{}, definition.event.Parallel(ctx, b.binding, audience, nil)
		case "serial":
			return definition.event.Serial(ctx, b.binding, audience, nil)
		case "bail":
			return definition.event.Bail(ctx, b.binding, audience, nil)
		case "waterfall":
			result, err := definition.waterfall.Run(ctx, b.binding, audience, nil, func(context.Context, any) (any, error) {
				f.r.event("inner", map[string]any{"key": a.Key})
				return f.r.evaluate(a.Value, env), nil
			})
			return hk.Decision{Value: result.Value, Stop: result.Present}, err
		default:
			return hk.Decision{}, unsupported(a.Op, "unknown hook dispatch mode")
		}
	}
	finish := func(value hk.Decision, err error) (hk.Decision, error) {
		if err == nil {
			out := any(undefined())
			if value.Stop {
				out = value.Value
			}
			f.r.put(a.To, out)
		}
		return value, err
	}
	if a.Await != nil && !*a.Await && a.Mode != "emit" && a.Mode != "bail" && a.Mode != "waterfall" {
		completion, complete := hk.NewCompletion()
		if a.Task != "" {
			b.mu.Lock()
			b.tasks[a.Task] = completion
			b.mu.Unlock()
		}
		err := b.owner.Go("hook dispatch "+a.Key, func(ctx context.Context) error {
			value, err := call(ctx)
			value, err = finish(value, err)
			complete(value, err)
			return nil // Dispatch failure belongs to its Future, not the host fiber.
		})
		if err != nil {
			complete(hk.Decision{}, err)
		}
		return true, f.catchHook(a, err)
	}
	value, err := call(env.std)
	_, err = finish(value, err)
	return true, f.catchHook(a, err)
}

func (f *composedFrontend) listenHook(a differential.Action, env environment, d *hookDefinition) error {
	b := f.hooks
	eventUse, err := d.event.Use("default")
	if err != nil {
		return err
	}
	waterfallUse, err := d.waterfall.Use("default")
	if err != nil {
		return err
	}
	binding, err := hk.Bind(b.owner, eventUse, waterfallUse)
	if err != nil {
		return err
	}
	_, err = d.event.On(binding, "default", func(ctx context.Context, _ any) (hk.Decision, error) {
		f.r.event("listener", map[string]any{"label": a.Label})
		current := env
		current.std = ctx
		value, pending, err := f.hookProgram(a.Program, current, nil)
		if err != nil || pending == nil {
			return hookDecision(value), err
		}
		completion, complete := hk.NewCompletion()
		err = b.owner.Go("hook listener "+a.Label, func(ctx context.Context) error {
			value, err := pending(ctx)
			complete(hookDecision(value), err)
			return nil
		})
		if err != nil {
			complete(hk.Decision{}, err)
			return hk.Decision{}, err
		}
		return hk.Decision{Future: completion}, nil
	})
	if err != nil {
		return err
	}
	_, err = d.waterfall.On(binding, "default", func(ctx context.Context, _ any, next hk.Next[any]) (hk.Result[any], error) {
		f.r.event("listener", map[string]any{"label": a.Label})
		current := env
		current.std = ctx
		value, pending, err := f.hookProgram(a.Program, current, next)
		if pending != nil {
			return hk.Result[any]{}, unsupported("waterfall", "asynchronous listener continuation is not represented by the synchronous Go engine")
		}
		return hk.Result[any]{Value: value, Present: true}, err
	})
	return err
}

// hookProgram executes the exact synchronous prefix, including the first gate
// observation. Only the remainder is passed to the real activation's tracked Go.
// No waiter goroutine, fake lease, or expected-output value participates.
func (f *composedFrontend) hookProgram(actions []differential.Action, env environment, next hk.Next[any]) (any, func(context.Context) (any, error), error) {
	var value any = undefined()
	for index, a := range actions {
		value = undefined()
		var err error
		switch a.Op {
		case "return":
			return f.r.evaluate(a.Value, env), nil, nil
		case "fail":
			message := a.Error
			if message == "" {
				message = "fixture-failed"
			}
			err = &hookFixtureError{message: message}
		case "next":
			if next == nil {
				err = &hookFixtureError{message: "next outside waterfall"}
			} else {
				var result hk.Result[any]
				result, err = next()
				if result.Present {
					value = result.Value
				}
				if err == nil {
					f.r.put(a.To, value)
				}
			}
		case "gate":
			if next != nil {
				return nil, nil, unsupported("waterfall", "asynchronous listener continuation is not represented by the synchronous Go engine")
			}
			f.r.mark(a.Label, "gate", f.r.evaluate(a.Value, env))
			f.r.mu.Lock()
			done := f.r.getGate(a.Label).done
			f.r.mu.Unlock()
			return value, func(ctx context.Context) (any, error) {
				select {
				case <-done:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				continued := env
				continued.std = ctx
				value, pending, err := f.hookProgram(actions[index+1:], continued, nil)
				for pending != nil && err == nil {
					value, err = pending(ctx)
					pending = nil
				}
				return value, err
			}, nil
		default:
			err = f.r.execute(a, env)
		}
		if err != nil {
			if !a.Catch || errors.Is(err, ErrUnsupported) {
				return nil, nil, err
			}
			f.catchHook(a, err)
		}
	}
	return value, nil, nil
}

func hookDecision(value any) hk.Decision {
	stop := value != nil
	if boolean, ok := value.(bool); ok && !boolean {
		stop = false
	}
	if object, ok := value.(map[string]any); ok && len(object) == 1 && object["$undefined"] == true {
		stop = false
	}
	return hk.Decision{Value: value, Stop: stop}
}

// Only errors created at this adapter's explicit fixture fail instruction carry
// source Error identity. Unknown runtime, cancellation, panic and next errors are
// never string-stripped, normalized or mislabeled as a fixture failure.
type hookFixtureError struct{ message string }

func (e *hookFixtureError) Error() string { return e.message }

func hookErrorValue(err error) map[string]any {
	// Only explicitly typed native provenance is transparent. Unknown wrappers,
	// panic payloads and generic joined errors remain their actual diagnostics.
	switch cause := err.(type) {
	case *hk.ListenerError:
		return hookErrorValue(cause.Cause)
	case *hk.ParallelAggregateError:
		causes := cause.Unwrap()
		members := make([]any, len(causes))
		for i, member := range causes {
			members[i] = hookErrorValue(member)
		}
		return map[string]any{"name": "AggregateError", "message": "", "errors": members}
	case *hookFixtureError:
		return map[string]any{"name": "Error", "message": cause.message}
	}
	if err == hk.ErrNext {
		return map[string]any{"name": "Error", "message": hk.ErrNext.Error()}
	}
	return errorValue(err)
}

func (f *composedFrontend) catchHook(a differential.Action, err error) error {
	if err == nil || !a.Catch || errors.Is(err, ErrUnsupported) {
		return err
	}
	value := hookErrorValue(err)
	f.r.event("error", map[string]any{"op": a.Op, "error": value})
	f.r.put(a.To, value)
	return nil
}
