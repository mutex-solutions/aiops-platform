// SPDX-License-Identifier: Apache-2.0

// Package plugin is the authoring surface of the SDK: an author writes a Spec[C]
// and Define turns it into the kernel.Plugin value of the module table
// (07-c4-plugin-sdk.md:212-226; ADR-060 decisions 4 and 8):
//
//	var Plugin = plugin.Define(plugin.Spec[Config]{
//		ID:        "acme/notifier-webhook",
//		Inject:    []kernel.Dep{kernel.Need(notifiers.Key)},
//		Registers: []kernel.AnyKey{notifiers.Key},
//		Apply:     func(c *kernel.Context, cfg *Config) error { ... },
//	})
//
// Generate reads the generated half of the plugin's manifest from that value
// without running Apply, and Manifest is manifest schema 1.0 as Go data (ADR-048).
//
// The package imports the standard library and sdk/kernel only, so the require
// list of the SDK module stays the one 00-decisions.md:262 allows.
//
// # Config
//
// C is the plugin's config type, a struct. Define derives from it a JSON Schema
// 2020-12 document and the volatile paths (Describe), and decodes the row's
// parsed config tree into a new *C at every activation, on every candidate update
// and at every commit (see Volatile). A parsed tree holds nil, bool, int64,
// float64, string, []any and map[string]any (launch.Row); the launcher keeps
// integers and floats apart. The rules below are part of the author API,
// versioned by APIVersion, and the table tests of this package prove them.
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
//   - Volatile. A Volatile[T] field is decoded as T. The schema marks it with
//     "x-aiops-volatile": true, and its path is one of the volatile paths: a
//     change of volatile values alone commits in place, with no restart of the
//     row (07-c4-plugin-sdk.md:1375; ADR-048 decision 9). See Volatile for what
//     the running activation reads.
//   - Tenant. aiops:"tenant" marks a field tenant-writable, and such a field
//     must be a Volatile[T] (07-c4-plugin-sdk.md:137, :473). The schema marks it
//     with "x-aiops-tenant": true, beside its volatile mark. The overlay that
//     writes it arrives at S4b. No other value of the aiops tag exists.
//   - Validate. Spec.Validate runs on the decoded value. Every error, from the
//     tree or from Validate, is returned as "invalid config: ...", and the row
//     fails with it.
//
// A reference such as ${secret:name} is resolved before Decode runs; an
// unresolved one is not a string, and decoding it into any field is an error. A
// resolved one may be a secret, so an error names the field and what it wants and
// never echoes a string value.
package plugin

import (
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// APIVersion is pluginApi, the version of the author-facing surface of
// sdk/kernel and of this package, as major.minor (ADR-060 decision 8; 08:918).
// While it is 0.y a minor may break, and gate check 1 needs the exact minor.
// tools/pluginapi fails when the exported API of either package changed and this
// constant did not.
const APIVersion = "0.3"

// Spec is what an author writes (07-c4-plugin-sdk.md:215-223). Durable joins at
// S3 with the durable definition (ADR-060 decision 29).
type Spec[C any] struct {
	ID       string          // "<owner>/<name>", equal to the manifest id.
	Inject   []kernel.Dep    // Data: profile render and the gate read it without running code.
	Provides []kernel.AnyKey // Provide of an undeclared key fails.
	// Registers lists every registry Apply contributes to (ADR-048 decision 21), as
	// Provides lists what it provides: plugingen writes registers from it, and the
	// generated cycle test fails when the plugin's fiber holds a registration in a
	// registry it does not list. kernel.Plugin does not carry it, so nothing refuses
	// an unlisted Register at run time.
	Registers []kernel.AnyKey
	Hooks     []kernel.HookUse // Every hook Apply may listen on, with its stage; an undeclared On fails.
	// Validate checks the decoded config. Optional; it must be pure, since it
	// runs at every activation, on every candidate update and at every commit.
	Validate func(cfg *C) error
	// Apply is the only entry point; everything it registers is an effect.
	Apply func(c *kernel.Context, cfg *C) error
}

// Define returns the kernel.Plugin of s, defined with kernel.Define, whose
// Decode turns the parsed config tree into a new *C by the rules of the package
// documentation and whose Apply calls s.Apply with it. When C has a Volatile
// field, Decode returns instead a record of this package that holds the *C and
// the cell of each Volatile field, Apply passes the *C of that record to s.Apply,
// and the Plugin has a hand-off (kernel.WithCommit) that stores the cells of a
// committed config into those of the running activation. It panics when C breaks
// a rule of that documentation, so a wrong config type fails at the package's
// initialisation, before any row of it mounts.
func Define[C any](s Spec[C]) kernel.Plugin {
	d, err := describe(reflect.TypeFor[C]())
	if err != nil {
		panic(fmt.Sprintf("plugin.Define %s: %v", s.ID, err))
	}
	p := kernel.Plugin{ID: s.ID, Inject: s.Inject, Provides: s.Provides, Hooks: s.Hooks,
		Decode: func(raw any) (any, error) {
			if probe, ok := raw.(*declared); ok { // Generate, never the kernel
				*probe = declared{registers: slices.Clone(s.Registers), schema: d.schema}
				return nil, nil
			}
			if raw == nil {
				raw = map[string]any{}
			}
			cfg := new(C)
			dec := &decoder{described: d}
			if len(d.volatile) != 0 {
				dec.cells = map[string]volatileCell{}
			}
			err := d.validator.Validate(raw)
			if err == nil {
				err = dec.decode(nil, raw, reflect.ValueOf(cfg).Elem())
			}
			if err == nil && s.Validate != nil {
				err = s.Validate(cfg)
			}
			if err != nil {
				return nil, fmt.Errorf("invalid config: %w", err)
			}
			if dec.cells != nil {
				return &decoded{cfg: cfg, cells: dec.cells}, nil
			}
			return cfg, nil
		}}
	if s.Apply != nil {
		p.Apply = func(c *kernel.Context, cfg any) error {
			if r, ok := cfg.(*decoded); ok {
				cfg = r.cfg
			}
			return s.Apply(c, cfg.(*C))
		}
	}
	if len(d.volatile) != 0 {
		p = kernel.WithCommit(p, handOff)
	}
	return kernel.Define(p)
}

// Volatile is a config field whose change alone commits in place, with no restart
// of the row (07-c4-plugin-sdk.md:473, :1375). Every tenant-writable field is one.
// Its path is derived from the config type and carried in configSchema, so no list
// is written by hand (ADR-048 decision 9). The loader commits such a change with
// kernel.Fiber.Commit: Apply is not called again, and the kernel hands the committed
// config to the running activation (ADR-034 G13), whose Volatile fields then read
// the committed values (07-c4-plugin-sdk.md:1382). A field that is not Volatile
// keeps the value Apply received, and the next activation of the row, whatever
// starts it, applies the committed config. A Volatile field that holds a reference
// such as ${secret:name} reads the value the reference resolves to at the commit.
//
// Each Volatile field has a cell of its own, which a copy of the config shares, and
// a commit stores the cells one after another: two reads may return values of two
// commits, or of one commit partly stored. Fields that must change together go in
// one Volatile of a struct type. A value that holds a slice or a map is shared by the
// commit and every reader, and a reader must not modify it.
type Volatile[T any] struct{ c *cell[T] }

// Get returns the value committed last, or the decoded value: two dependent loads
// and a dereference, with no lock and no allocation. The zero Volatile returns the
// zero T.
func (v Volatile[T]) Get() T {
	if v.c == nil {
		var zero T
		return zero
	}
	return *v.c.Load()
}

// cell holds the value of one Volatile field.
type cell[T any] struct{ atomic.Pointer[T] }

// volatileCell is a cell of any T: take stores the value of from, a cell of the same
// field of another decoded config.
type volatileCell interface{ take(from volatileCell) }

func (c *cell[T]) take(from volatileCell) { c.Store(from.(*cell[T]).Load()) }

// alloc gives v a new cell and returns the address of the value it holds, which the
// decoder sets, and the cell.
func (v *Volatile[T]) alloc() (any, volatileCell) {
	v.c = new(cell[T])
	v.c.Store(new(T))
	return v.c.Load(), v.c
}

// elem returns T.
func (Volatile[T]) elem() reflect.Type { return reflect.TypeFor[T]() }

// decoded is what Decode returns for a config type with a Volatile field (ADR-034
// G13): the *C that Apply receives and, by path, the cell of each Volatile field.
type decoded struct {
	cfg   any
	cells map[string]volatileCell
}

// handOff is the hand-off of a config type with a Volatile field: it stores the
// value of each cell of committed into the cell of applied at the same path. It
// reads the two records only, never a *C, whose slices and maps the author may
// change while it runs. A path in one record only is skipped. The loader commits in
// place only a change that keeps the length of each list and the keys of each map
// that holds a Volatile value (loader.VolatileOnly), and the decoder gives a cell to
// each Volatile field of a struct, present or absent, so the two records of such a
// commit have the same paths.
func handOff(applied, committed any) {
	to := applied.(*decoded).cells
	for path, c := range committed.(*decoded).cells {
		if a, ok := to[path]; ok {
			a.take(c)
		}
	}
}

// ConfigInfo is what the manifest carries of a config type.
type ConfigInfo struct {
	// Schema is the JSON Schema 2020-12 document of the type: configSchema.
	Schema []byte
	// Volatile lists the paths whose change alone commits in place, sorted, each
	// as its list of keys, the shape of loader.VolatileUpdateArgs.Paths (ADR-059
	// decision 44). The segment "*" stands for any list index or map key: the
	// paths of the subschemas of Schema marked "x-aiops-volatile".
	Volatile [][]string
}

// Describe derives the schema and the volatile paths of C, or says which rule of
// the package documentation C breaks.
func Describe[C any]() (ConfigInfo, error) {
	d, err := describe(reflect.TypeFor[C]())
	if err != nil {
		return ConfigInfo{}, err
	}
	return ConfigInfo{Schema: d.schema, Volatile: d.volatile}, nil
}
