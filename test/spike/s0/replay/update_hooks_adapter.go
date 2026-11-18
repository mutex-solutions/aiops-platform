// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	cm "github.com/mutex-solutions/aiops-platform/test/spike/s0/contextmodel"
	hk "github.com/mutex-solutions/aiops-platform/test/spike/s0/hooks"
)

// updateHookAdapter belongs to the same actual root-installed hooks bundle as
// ordinary replay events. Each fiber has a distinct real Waterfall definition;
// listener callbacks and their removal are owned by the registering activation.
// It is not a callback list or a second configuration cell.
type updateHookAdapter struct {
	mu          sync.Mutex
	definitions map[*fiber]*hk.Waterfall[updateHookInput, any]
}

type updateHookInput struct {
	candidate any
	prefix    func()
}

func newUpdateHookAdapter() *updateHookAdapter {
	return &updateHookAdapter{definitions: make(map[*fiber]*hk.Waterfall[updateHookInput, any])}
}

func (u *updateHookAdapter) definition(b *hookBundle, target *fiber) (*hk.Waterfall[updateHookInput, any], error) {
	if b == nil || b.owner == nil || target == nil {
		return nil, unsupported("internal/update", "actual builtin hooks owner and target activation are required")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if definition := u.definitions[target]; definition != nil {
		return definition, nil
	}
	// The fiber name is source data, but the pointer-scoped map is the identity.
	// Equal event text on another mounted fiber never shares this definition.
	definition, err := hk.NewWaterfall[updateHookInput, any](hk.Definition{
		Name:  fmt.Sprintf("fiber/%s/internal/update", target.name),
		Owner: b.key.Any(), Stages: []string{"default"},
	})
	if err != nil {
		return nil, err
	}
	u.definitions[target] = definition
	return definition, nil
}

func isUpdateListener(a differential.Action) bool {
	return a.Op == "listen" && a.Key == "internal/update" && !a.Symbol
}

func (f *composedFrontend) validateUpdateHook(a differential.Action) (bool, error) {
	if !isUpdateListener(a) {
		return false, nil
	}
	if a.Global || a.Prepend || a.Context != "" || a.Fiber != "" {
		return true, unsupported(a.Op, "internal/update requires the actual current activation and default stage")
	}
	var validateProgram func([]differential.Action) error
	validateProgram = func(program []differential.Action) error {
		for _, step := range program {
			if step.Context != "" || step.Fiber != "" || len(step.Cleanup) != 0 {
				return unsupported(step.Op, "retargeted update listener instructions are not implemented")
			}
			switch step.Op {
			case "mark", "gate", "output", "fail", "return", "next":
				if len(step.Program) != 0 {
					return unsupported(step.Op, "nested update listener instructions require when")
				}
			case "when":
				if err := validateProgram(step.Program); err != nil {
					return err
				}
			default:
				return unsupported(step.Op, "not implemented inside an internal/update listener")
			}
		}
		return nil
	}
	return true, validateProgram(a.Program)
}

// listen binds the real composed activation, never the builtin dispatcher, to
// the listener. hk.On registers native Defer and takes a native setup lease.
func (u *updateHookAdapter) listen(f *composedFrontend, a differential.Action, env environment) error {
	caller, ok := env.ctx.(*composedActivation)
	if !ok || caller.trusted || env.fiber == nil || caller.owner != env.fiber {
		return unsupported(a.Op, "internal/update listeners must be installed by their real plugin Apply")
	}
	definition, err := u.definition(f.hooks, env.fiber)
	if err != nil {
		return err
	}
	use, err := definition.Use("default")
	if err != nil {
		return err
	}
	// This bounded internal operation obtains its declared use from the actual
	// source listener. It does not claim final Spec.Hooks/public-class admission.
	binding, err := hk.Bind(caller.context, use)
	if err != nil {
		return err
	}
	_, err = definition.On(binding, "default", func(ctx context.Context, input updateHookInput, next hk.Next[any]) (hk.Result[any], error) {
		f.r.event("listener", map[string]any{"label": a.Label})
		current := env
		current.std, current.config = ctx, input.candidate
		value, _, err := f.updateHookProgram(a.Program, current, next, input.prefix)
		return hk.Result[any]{Value: value, Present: true}, err
	})
	return err
}

// request starts only the listener phase. The configuration cell has already
// synchronously resolved, committed, retained raw and cleared failure before it
// invokes this callback. The handshake returns only after the real listener's
// first gate observation or its complete synchronous execution. Waterfall.Run
// keeps its real callback lease until the entire continuation actually returns.
func (u *updateHookAdapter) request(f *composedFrontend, target *fiber, ctx context.Context, candidate any, next func() error) (func(context.Context) error, error) {
	definition, err := u.definition(f.hooks, target)
	if err != nil {
		return nil, err
	}
	ready, done := make(chan struct{}), make(chan struct{})
	var once sync.Once
	prefix := func() { once.Do(func() { close(ready) }) }
	var resultErr error
	err = f.hooks.owner.Go("configuration hooks "+target.name, func(_ context.Context) error {
		_, resultErr = definition.Run(ctx, f.hooks.binding, cm.Audience{}, updateHookInput{candidate: candidate, prefix: prefix}, func(context.Context, updateHookInput) (any, error) {
			return undefined(), next()
		})
		close(done)
		prefix()
		return nil // A rejected update belongs to its request, not the root fiber.
	})
	if err != nil {
		return nil, err
	}
	select {
	case <-ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	wait := func(wait context.Context) error {
		select {
		case <-done:
			return resultErr
		case <-wait.Done():
			return wait.Err()
		}
	}
	return wait, nil
}

// updateHookProgram is a small source-language evaluator, not a hook engine.
// Actual callback selection, ordering, ownership, next guards and leases belong
// to hooks.Waterfall. A gate yields the prefix handshake, then blocks only that
// actual callback. Return is propagated through nested when bodies.
func (f *composedFrontend) updateHookProgram(program []differential.Action, env environment, next hk.Next[any], prefix func()) (any, bool, error) {
	var value any = undefined()
	for _, a := range program {
		value = undefined()
		var err error
		switch a.Op {
		case "return":
			return f.r.evaluate(a.Value, env), true, nil
		case "when":
			if reflect.DeepEqual(f.r.evaluate(a.Base, env), f.r.evaluate(a.Value, env)) {
				var returned bool
				value, returned, err = f.updateHookProgram(a.Program, env, next, prefix)
				if returned {
					return value, true, err
				}
			}
		case "fail":
			message := a.Error
			if message == "" {
				message = "fixture-failed"
			}
			err = &hookFixtureError{message: message}
		case "next":
			var result hk.Result[any]
			result, err = next()
			if result.Present {
				value = result.Value
			}
			if err == nil {
				f.r.put(a.To, value)
			}
		case "gate":
			f.r.mark(a.Label, "gate", f.r.evaluate(a.Value, env))
			f.r.mu.Lock()
			done := f.r.getGate(a.Label).done
			f.r.mu.Unlock()
			prefix()
			select {
			case <-done:
			case <-env.std.Done():
				err = env.std.Err()
			}
		default:
			err = f.r.execute(a, env)
		}
		if err != nil {
			if !a.Catch || errors.Is(err, ErrUnsupported) {
				return nil, false, err
			}
			if err := f.catchHook(a, err); err != nil {
				return nil, false, err
			}
		}
	}
	return value, false, nil
}
