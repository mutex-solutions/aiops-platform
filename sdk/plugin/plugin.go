// SPDX-License-Identifier: Apache-2.0

// Package plugin is the authoring surface of the SDK: an author writes a Spec[C]
// and Define turns it into the kernel.Plugin value of the module table
// (07-c4-plugin-sdk.md:212-226; ADR-060 decisions 4 and 8):
//
//	var Plugin = plugin.Define(plugin.Spec[Config]{
//		ID:     "acme/notifier-webhook",
//		Inject: []kernel.Dep{kernel.Need(notifiers.Key)},
//		Apply:  func(c *kernel.Context, cfg *Config) error { ... },
//	})
//
// The package imports the standard library and sdk/kernel only, so the require
// list of the SDK module stays the one 00-decisions.md:262 allows.
//
// # Config
//
// C is the plugin's config type, a struct. Define derives from it a JSON Schema
// 2020-12 document and the volatile paths (Describe), and at every activation
// decodes the row's parsed config tree into a new *C. A parsed tree holds nil,
// bool, int64, float64, string, []any and map[string]any (launch.Row); the
// launcher keeps integers and floats apart. The rules below are part of the
// author API, versioned by APIVersion, and the table tests of this package prove
// them.
//
//   - Keys. Each exported field of a struct is one key: the name in its json tag
//     when it has one, else the field name with its leading capitals lowered and
//     the last of them kept when a lower-case letter follows, so Timeout is
//     timeout, URL is url, MaxLines is maxLines and URLPath is urlPath, but IDs
//     is iDs, so such a field takes a json tag. A json tag takes a name and no
//     option; json:"-" leaves the field out. A key is not "*" and holds no ".":
//     a config path, as the loader writes it, reads "*" as any key and "." as a
//     separator (loader.VolatileOnly). Unexported fields are not config. An
//     embedded field, and two fields with one key, are refused.
//   - Unknown fields. A key that names no field is an error, at every depth.
//   - Absence. A nil config is an empty map. An absent key leaves its field at
//     the default: the value of its default tag, else the zero value. An absent
//     struct is decoded from an empty map, so its own fields take their defaults.
//     A key whose value is null is an error: null never means "the default".
//   - Types. bool takes true or false; string a string; time.Duration a string
//     that time.ParseDuration accepts, such as "50ms" or "1h30m", never a number,
//     which has no unit; a signed or unsigned integer type an int64 inside the
//     type's range, never a float64, not even 5.0; float32 and float64 an int64 or
//     a float64 inside the type's range; a slice a list; a map with string keys a
//     map; a struct a map. A type defined over one of these kinds, such as
//     type Mode string, follows its kind. Define refuses every other kind:
//     pointer, interface, array, channel, function, complex and uintptr. It
//     refuses a struct other than C itself that has no config field, such as
//     time.Time, and a type defined over Volatile, which keeps none of its
//     methods.
//   - Defaults. default:"..." sets the default of a field of a scalar type,
//     written as the field takes it: default:"50ms", default:"3",
//     default:"0.5", default:"true", default:"text". Define refuses a default
//     the field could not take, and one on a list, a map or a struct. The schema
//     carries it as the field's default.
//   - Examples. example:"..." gives a field of a scalar type an example, written
//     and checked as a default is. The schema carries it as the field's examples,
//     the annotation from which a template takes a row config (ADR-060 decision
//     24). It changes nothing the decoder does.
//   - Volatile. A Volatile[T] field is decoded as T. Its path is one of the
//     volatile paths: a change of volatile values alone is to commit in place,
//     with no restart of the row (07-c4-plugin-sdk.md:1375). Until the kernel can
//     hand such a value to a running field, Describe reports no volatile path and
//     the change restarts the row (see Volatile).
//   - Tenant. aiops:"tenant" marks a field tenant-writable, and such a field
//     must be a Volatile[T] (07-c4-plugin-sdk.md:137, :473). The schema marks it
//     with "x-aiops-tenant": true. The overlay that writes it arrives at S4b. No
//     other value of the aiops tag exists.
//   - Validate. Spec.Validate runs on the decoded value. Every error, from the
//     tree or from Validate, is returned as "invalid config: ...", and the row
//     fails with it.
//
// A reference such as ${secret:name} is resolved before Decode runs; an
// unresolved one is not a string, and decoding it into any field is an error.
package plugin

import (
	"fmt"
	"reflect"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// APIVersion is pluginApi, the version of the author-facing surface of
// sdk/kernel and of this package, as major.minor (ADR-060 decision 8; 08:918).
// While it is 0.y a minor may break, and gate check 1 needs the exact minor.
// tools/pluginapi fails when the exported API of either package changed and this
// constant did not.
const APIVersion = "0.1"

// Spec is what an author writes (07-c4-plugin-sdk.md:215-223). Durable joins at
// S3 with the durable definition (ADR-060 decision 29).
type Spec[C any] struct {
	ID       string           // "<owner>/<name>", equal to the manifest id.
	Inject   []kernel.Dep     // Data: profile render and the gate read it without running code.
	Provides []kernel.AnyKey  // Provide of an undeclared key fails.
	Hooks    []kernel.HookUse // Every hook Apply may listen on, with its stage; an undeclared On fails.
	// Validate checks the decoded config. Optional; it must be pure, since it
	// runs at every activation and on every candidate update.
	Validate func(cfg *C) error
	// Apply is the only entry point; everything it registers is an effect.
	Apply func(c *kernel.Context, cfg *C) error
}

// Define returns the kernel.Plugin of s, defined with kernel.Define, whose
// Decode turns the parsed config tree into a new *C by the rules of the package
// documentation and whose Apply calls s.Apply with it. It panics when C breaks
// a rule of that documentation, so a wrong config type fails at the package's
// initialisation, before any row of it mounts.
func Define[C any](s Spec[C]) kernel.Plugin {
	d, err := describe(reflect.TypeFor[C]())
	if err != nil {
		panic(fmt.Sprintf("plugin.Define %s: %v", s.ID, err))
	}
	p := kernel.Plugin{ID: s.ID, Inject: s.Inject, Provides: s.Provides, Hooks: s.Hooks,
		Decode: func(raw any) (any, error) {
			if raw == nil {
				raw = map[string]any{}
			}
			cfg := new(C)
			err := d.validator.Validate(raw)
			if err == nil {
				err = d.decode(nil, raw, reflect.ValueOf(cfg).Elem())
			}
			if err == nil && s.Validate != nil {
				err = s.Validate(cfg)
			}
			if err != nil {
				return nil, fmt.Errorf("invalid config: %w", err)
			}
			return cfg, nil
		}}
	if s.Apply != nil {
		p.Apply = func(c *kernel.Context, cfg any) error { return s.Apply(c, cfg.(*C)) }
	}
	return kernel.Define(p)
}

// Volatile is a config field whose change alone is to commit in place, with no
// restart of the row (07-c4-plugin-sdk.md:473, :1375). Every tenant-writable field
// is one. Its path is derived from the config type, so no list is written by hand.
// Get returns the value of the activation: the kernel cannot hand a value committed
// in place to a running field until the ADR-034 addition that ADR-060 decision 3
// asks for, so until then Describe reports no volatile path, the loader restarts
// the row on a change of the field, and the new value reaches Apply.
type Volatile[T any] struct{ v T }

// Get returns the value.
func (v Volatile[T]) Get() T { return v.v }

// ptr gives the decoder the address of the value.
func (v *Volatile[T]) ptr() any { return &v.v }

// ConfigInfo is what the manifest carries of a config type.
type ConfigInfo struct {
	// Schema is the JSON Schema 2020-12 document of the type: configSchema.
	Schema []byte
	// Volatile lists the paths whose change alone commits in place, sorted, each
	// as its list of keys, the shape of loader.VolatileUpdateArgs.Paths (ADR-059
	// decision 44). The segment "*" stands for any list index or map key. It is
	// empty until the kernel can hand a value committed in place to a running
	// Volatile field (see Volatile).
	Volatile [][]string
}

// Describe derives the schema and the volatile paths of C, or says which rule of
// the package documentation C breaks. The paths of the Volatile fields are derived
// and checked, and Describe reports none of them until the kernel can hand a value
// committed in place to a running field: a change of one restarts the row and
// reaches Apply.
func Describe[C any]() (ConfigInfo, error) {
	d, err := describe(reflect.TypeFor[C]())
	if err != nil {
		return ConfigInfo{}, err
	}
	return ConfigInfo{Schema: d.schema}, nil
}
