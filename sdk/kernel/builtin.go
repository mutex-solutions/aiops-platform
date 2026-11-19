// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import (
	"fmt"
	"log/slog"
)

// The kernel has exactly four built-in services and no other service name:
// reflect, registry, hooks and logger (03-c3-kernel.md:62; 07-c4-plugin-sdk.md:27,
// :1346; plugin record :59). The root installs them at construction as
// provisions of its own context, at major 0 and in the open class, so any plugin may
// Need one and none can provide or replace one: the root provides each key, and a
// second provision of it is refused as for any key. The documents name the four and
// say what two of them hold ("plugin records, the fiber index"; "a log/slog front
// with fiber attributes"). Only logger has an interface; the others are opaque
// values with no methods, and their surface is an open point.

// The keys of the four built-in services. The documents name no Go identifier for
// them: these names are the kernel's own and an open point.
var (
	// ReflectKey names the built-in service store (03-c3-kernel.md:62;
	// plugin record :59). Its value has no methods.
	ReflectKey = NewKey[*reflectService]("reflect", 0)
	// RegistryKey names the built-in plugin registry (03-c3-kernel.md:62;
	// plugin record :59). Its value has no methods.
	RegistryKey = NewKey[*registry]("registry", 0)
	// HooksKey names the built-in hook service, Cordis "events" renamed because
	// "events" is the outbox (03-c3-kernel.md:62; plugin record :59). Its
	// value has no methods.
	HooksKey = NewKey[*hooksService]("hooks", 0)
	// LoggerKey names the built-in logger (03-c3-kernel.md:62; plugin record
	// section 6 and :179).
	LoggerKey = NewKey[Logger]("logger", 0)
)

// reflectService and hooksService are the opaque values of two built-ins.
type (
	reflectService struct{}
	hooksService   struct{}
)

// installBuiltins is replaced in tests only, to refuse an installation.
var installBuiltins = (*Root).installBuiltins

// installBuiltins provides the four built-in services from the root context. An
// error leaves the root half installed: the caller closes it.
func (r *Root) installBuiltins(h slog.Handler) error {
	if h == nil {
		h = slog.DiscardHandler
	}
	for _, provide := range []func() error{
		func() error { return ReflectKey.Provide(r.ctx, &reflectService{}) },
		func() error { return RegistryKey.Provide(r.ctx, &registry{}) },
		func() error { return HooksKey.Provide(r.ctx, &hooksService{}) },
		func() error { return LoggerKey.Provide(r.ctx, newLogger(h)) },
	} {
		if err := provide(); err != nil {
			return fmt.Errorf("install built-in service: %w", err)
		}
	}
	return nil
}
