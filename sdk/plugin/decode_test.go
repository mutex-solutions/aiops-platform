// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"reflect"
	"strings"
	"testing"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

type mode string

type window struct {
	Name  string
	Limit int8 `default:"3"`
	Until Volatile[time.Duration]
}

// rules is a config type with a field of every kind the decode rules name.
type rules struct {
	Timeout  time.Duration    `default:"50ms"`
	URL      Volatile[string] `aiops:"tenant"`
	Count    int
	Small    int8
	Size     uint16
	Big      uint64
	Ratio    float32
	Scale    float64 `default:"0.5"`
	On       bool    `default:"true"`
	Tags     []string
	Labels   map[string]string
	Auth     window
	Windows  []window
	IDs      []int `json:"ids"`
	URLPath  string
	Mode     mode   `default:"fast"`
	Skipped  string `json:"-"`
	Settings Volatile[window]
	hidden   int
}

// defaults is what an empty config decodes to.
func defaults() rules {
	return rules{Timeout: 50 * time.Millisecond, Scale: 0.5, On: true, Mode: "fast",
		Auth: window{Limit: 3}, Settings: vol(window{Limit: 3})}
}

// vol is a Volatile holding v.
func vol[T any](v T) Volatile[T] {
	var x Volatile[T]
	p, _ := x.alloc()
	*p.(*T) = v
	return x
}

// sameValue is reflect.DeepEqual that reads a Volatile by its value: each decoded
// Volatile has a cell of its own.
func sameValue(a, b reflect.Value) bool {
	switch t := a.Type(); {
	case isVolatile(t):
		return sameValue(a.MethodByName("Get").Call(nil)[0], b.MethodByName("Get").Call(nil)[0])
	case t.Kind() == reflect.Pointer:
		return a.IsNil() == b.IsNil() && (a.IsNil() || sameValue(a.Elem(), b.Elem()))
	case t.Kind() == reflect.Struct:
		for i := range t.NumField() {
			if !sameValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case t.Kind() == reflect.Slice:
		if a.IsNil() != b.IsNil() || a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !sameValue(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case t.Kind() == reflect.Map:
		return reflect.DeepEqual(a.Interface(), b.Interface()) // no map of the table holds a Volatile
	}
	return a.Equal(b)
}

// TestDecodeRules is the table of the decode rules of the package documentation.
// Each case runs Define's Decode, and the validator and the decoder each on their
// own: the two agree on every case, so the schema says what the decoder does.
func TestDecodeRules(t *testing.T) {
	m := func(kv ...any) map[string]any {
		out := map[string]any{}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	l := func(v ...any) []any { return v }
	for _, tc := range []struct {
		name string
		tree any
		want func(*rules) // edits defaults() into the expected value; nil with err
		err  string       // substring of the error; empty for success
	}{
		{"nil config is the defaults", nil, func(*rules) {}, ""},
		{"empty map is the defaults", m(), func(*rules) {}, ""},
		{"every field", m("timeout", "1h30m", "url", "https://h", "count", int64(-7), "small", int64(-128),
			"size", int64(65535), "big", int64(1<<62), "ratio", 1.5, "scale", int64(2), "on", false,
			"tags", l("a", "b"), "labels", m("k", "v"), "auth", m("name", "n", "limit", int64(9), "until", "1s"),
			"windows", l(m(), m("name", "w")), "ids", l(int64(1), int64(2)), "urlPath", "/p", "mode", "slow",
			"settings", m("limit", int64(1))),
			func(r *rules) {
				r.Timeout, r.URL, r.Count, r.Small, r.Size, r.Big = 90*time.Minute, vol("https://h"), -7, -128, 65535, 1<<62
				r.Ratio, r.Scale, r.On, r.Tags, r.Labels = 1.5, 2, false, []string{"a", "b"}, map[string]string{"k": "v"}
				r.Auth = window{Name: "n", Limit: 9, Until: vol(time.Second)}
				r.Windows = []window{{Limit: 3}, {Name: "w", Limit: 3}}
				r.IDs, r.URLPath, r.Mode, r.Settings = []int{1, 2}, "/p", "slow", vol(window{Limit: 1})
			}, ""},
		{"empty list and map stay empty, not nil", m("tags", l(), "labels", m()), func(r *rules) { r.Tags, r.Labels = []string{}, map[string]string{} }, ""},
		{"explicit false beats a true default", m("on", false), func(r *rules) { r.On = false }, ""},
		{"float field takes an integer", m("ratio", int64(2)), func(r *rules) { r.Ratio = 2 }, ""},
		{"zero duration", m("timeout", "0"), func(r *rules) { r.Timeout = 0 }, ""},

		{"root is not a map", "x", nil, `config: want a map, got a string`},
		{"unknown field", m("extra", int64(1)), nil, "config.extra: unknown field"},
		{"unknown nested field", m("auth", m("x", true)), nil, "config.auth.x: unknown field"},
		{"unknown field in a list element", m("windows", l(m("y", true))), nil, "config.windows.0.y: unknown field"},
		{"the Go name is not the key", m("URL", "x"), nil, "config.URL: unknown field"},
		{"a json tag replaces the key", m("iDs", l()), nil, "config.iDs: unknown field"},
		{"json:\"-\" is not config", m("skipped", "x"), nil, "config.skipped: unknown field"},
		{"an unexported field is not config", m("hidden", int64(1)), nil, "config.hidden: unknown field"},
		{"null is not the default", m("count", nil), nil, "config.count: want an integer, got null"},
		{"null struct", m("auth", nil), nil, "config.auth: want a map, got null"},
		// A whole float is shown as written, so it is seen why it is no integer: S1's
		// decoders took factor: 2.0 as 2.
		{"float into an integer", m("count", 5.0), nil, "config.count: want an integer, got 5.0, a float"},
		{"float with a fraction into an integer", m("count", 5.5), nil, "config.count: want an integer, got 5.5, a float"},
		{"string into an integer", m("count", "5"), nil, `config.count: want an integer, got a string`},
		{"int8 above range", m("small", int64(128)), nil, "config.small: 128 is above the maximum 127"},
		{"int8 below range", m("small", int64(-129)), nil, "config.small: -129 is below the minimum -128"},
		{"uint16 above range", m("size", int64(65536)), nil, "config.size: 65536 is above the maximum 65535"},
		{"uint negative", m("big", int64(-1)), nil, "config.big: -1 is below the minimum 0"},
		{"float32 above range", m("ratio", 1e39), nil, "config.ratio: 1e+39 is above the maximum"},
		{"bool from a string", m("on", "yes"), nil, `config.on: want true or false, got a string`},
		{"duration from a number", m("timeout", int64(5)), nil, "config.timeout: want a duration such as 50ms or 1h30m, got 5"},
		{"duration with no unit", m("timeout", "5"), nil, `config.timeout: want a duration such as 50ms or 1h30m, got a string`},
		{"duration in words", m("timeout", "5 minutes"), nil, "config.timeout: want a duration"},
		{"volatile takes its value type", m("url", int64(1)), nil, "config.url: want a string, got 1"},
		{"volatile struct refuses an unknown field", m("settings", m("z", int64(1))), nil, "config.settings.z: unknown field"},
		{"list from a scalar", m("tags", "a"), nil, `config.tags: want a list, got a string`},
		{"list element of the wrong type", m("tags", l("a", int64(1))), nil, "config.tags.1: want a string, got 1"},
		{"map value of the wrong type", m("labels", m("a", int64(1))), nil, "config.labels.a: want a string, got 1"},
		{"map from a list", m("labels", l()), nil, "config.labels: want a map, got a list"},
		{"duration in a list element", m("windows", l(m("until", "soon"))), nil, `config.windows.0.until: want a duration such as 50ms or 1h30m, got a string`},
		{"an unresolved reference is not a string", m("url", launch.Ref{Scheme: "secret", Name: "u"}), nil, "config.url: want a string, got {secret u} of type launch.Ref"},
		{"a Go int is not a tree integer", m("count", 5), nil, "config.count: want an integer, got 5 of type int"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Define(Spec[rules]{ID: "test/rules"})
			got, err := p.Decode(tc.tree)
			if tc.err != "" {
				if err == nil || !strings.HasPrefix(err.Error(), "invalid config: ") || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Decode: want an error with %q, got %v", tc.err, err)
				}
			} else {
				if err != nil {
					t.Fatalf("Decode: %v", err)
				}
				want := defaults()
				tc.want(&want)
				if !sameValue(reflect.ValueOf(ConfigOf(got)), reflect.ValueOf(&want)) {
					t.Fatalf("Decode:\n got %+v\nwant %+v", got, &want)
				}
			}
			d, err := describe(reflect.TypeFor[rules]())
			if err != nil {
				t.Fatal(err)
			}
			v, err := NewValidator(d.schema)
			if err != nil {
				t.Fatal(err)
			}
			tree := tc.tree
			if tree == nil {
				tree = map[string]any{}
			}
			verr := v.Validate(tree)
			derr := (&decoder{d, map[string]volatileCell{}}).decode(nil, tree, reflect.New(reflect.TypeFor[rules]()).Elem())
			if (verr == nil) != (tc.err == "") || (derr == nil) != (tc.err == "") {
				t.Fatalf("the validator and the decoder disagree: validator %v, decoder %v", verr, derr)
			}
		})
	}
}

// What the schema cannot say is refused by the decoder alone.
func TestDecodeRefusesWhatTheSchemaCannotSay(t *testing.T) {
	d, err := describe(reflect.TypeFor[rules]())
	if err != nil {
		t.Fatal(err)
	}
	tree := map[string]any{"timeout": "9999999999h"}
	if v, _ := NewValidator(d.schema); v.Validate(tree) != nil {
		t.Fatal("the schema refuses a duration past the range of time.Duration; this test needs another value")
	}
	err = (&decoder{d, map[string]volatileCell{}}).decode(nil, tree, reflect.New(reflect.TypeFor[rules]()).Elem())
	if err == nil || !strings.Contains(err.Error(), "config.timeout: want a duration") {
		t.Fatalf("decode: %v", err)
	}
	if _, err := Define(Spec[rules]{ID: "test/rules"}).Decode(tree); err == nil || !strings.HasPrefix(err.Error(), "invalid config: config.timeout") {
		t.Fatalf("Decode: %v", err)
	}
}

func TestKeyOf(t *testing.T) {
	for name, want := range map[string]string{
		"Timeout": "timeout", "URL": "url", "MaxLines": "maxLines", "URLPath": "urlPath", "ID": "id",
		"A": "a", "HTTP2": "http2", "IDs": "iDs", "X1Y": "x1Y",
	} {
		if got := keyOf(name); got != want {
			t.Errorf("keyOf(%q) = %q, want %q", name, got, want)
		}
	}
}
