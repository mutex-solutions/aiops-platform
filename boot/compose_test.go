package boot

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

var update = flag.Bool("update", false, "rewrite golden files")

// dump renders a composition as text: one block per row, with the origin of
// every field. It is the shape of a golden dump.
func dump(c Composition, warns []Warning) string {
	var b strings.Builder
	fmt.Fprintf(&b, "digest %s\n", c.Digest)
	for _, w := range warns {
		fmt.Fprintf(&b, "warning %s\n", w)
	}
	for _, r := range c.Rows {
		fmt.Fprintf(&b, "\nrow %s plugin=%s parent=%q enabled=%v required=%v group=%v roles=%v inject=%v isolate=%v\n",
			r.ID, r.Plugin, r.Parent, r.Enabled, r.Required, r.Group, r.Roles, r.Inject, r.Isolate)
		fmt.Fprintf(&b, "  config %s\n", show(r.Config))
		if len(r.Intercept) > 0 {
			fmt.Fprintf(&b, "  intercept %s\n", show(Value{Kind: KindMap, Map: r.Intercept}))
		}
		for _, u := range r.Refs {
			fmt.Fprintf(&b, "  ref %s = %s:%s\n", u.Path, u.Ref.Scheme, u.Ref.Name)
		}
		ins := r.Provenance.Fields[0].Origin
		fmt.Fprintf(&b, "  inserted L%d %s %s:%d\n", ins.Layer, ins.Source, ins.File, ins.Line)
		for _, fo := range r.Provenance.Fields[1:] {
			if o := fo.Origin; o != ins {
				fmt.Fprintf(&b, "  %s set by L%d %s %s:%d\n", fo.Field, o.Layer, o.Source, o.File, o.Line)
			}
		}
	}
	return b.String()
}

func show(v Value) string {
	switch v.Kind {
	case KindBool:
		return fmt.Sprint(v.Bool)
	case KindInt:
		return fmt.Sprintf("int(%d)", v.Int)
	case KindFloat:
		return fmt.Sprintf("float(%g)", v.Float)
	case KindString:
		return fmt.Sprintf("%q", v.Str)
	case KindRef:
		return "${" + string(v.Ref.Scheme) + ":" + v.Ref.Name + "}"
	case KindList:
		parts := make([]string, len(v.List))
		for i, x := range v.List {
			parts[i] = show(x)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case KindMap:
		parts := make([]string, len(v.Map))
		for i, f := range v.Map {
			parts[i] = f.Key + ": " + show(f.Value)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return "null"
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file; run with -update and review the diff\n--- got\n%s", name, got)
	}
}

func TestComposeGolden(t *testing.T) {
	root := os.DirFS("testdata/basic")
	home, err := fs.Sub(root, "home")
	if err != nil {
		t.Fatal(err)
	}
	src := Sources{
		Profile: "demo",
		Home:    home,
		Bundles: root,
		Patches: []File{{FS: root, Path: "patch1.yml"}, {FS: root, Path: "patch2.yml"}},
	}
	opt := Options{Server: true, Modules: ModuleTable{"aiops/loader": {ID: "aiops/loader", Module: "m/loader", Version: "v1", Sum: "h1:x"}}}
	c, warns, err := Compose(src, opt)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "basic.golden", dump(c, warns))

	c2, _, err := Compose(src, opt)
	if err != nil || c2.Digest != c.Digest {
		t.Fatalf("second composition differs: %v", err)
	}
	if o, ok := c.Rows[0].Provenance.Of(FieldConfig); !ok || o.Layer != 1 {
		t.Errorf("Of(config) = %v %v", o, ok)
	}
	if _, ok := c.Rows[0].Provenance.Of("nope"); ok {
		t.Error("Of accepted a name that is not a field")
	}
}

// files builds a home and a bundle tree for one profile "p" with one bundle
// "b" whose text is b, and optional layer texts.
type files struct {
	profile, bundle, l2, l3 string
	patches                 []string
}

func (f files) compose(opt Options) (Composition, []Warning, error) {
	home := fstest.MapFS{}
	bund := fstest.MapFS{}
	if f.profile != "-" {
		home["profiles/p/profile.yaml"] = &fstest.MapFile{Data: []byte(orDefault(f.profile, "bundles: [b]"))}
	}
	if f.bundle != "-" {
		bund["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte(f.bundle)}
	}
	if f.l2 != "" {
		home["profiles/p/aiops.patch.yml"] = &fstest.MapFile{Data: []byte(f.l2)}
	}
	if f.l3 != "" {
		home["aiops.patch.yml"] = &fstest.MapFile{Data: []byte(f.l3)}
	}
	src := Sources{Profile: "p", Home: home, Bundles: bund}
	for i, p := range f.patches {
		name := fmt.Sprintf("patch%d.yml", i)
		bund[name] = &fstest.MapFile{Data: []byte(p)}
		src.Patches = append(src.Patches, File{FS: bund, Path: name})
	}
	return Compose(src, opt)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

const base = `
- insert:
    - id: a
      plugin: o/a
      roles: [api]
      config: {k: 1}
    - id: req
      plugin: o/req
      roles: [api]
      required: true
`

func TestComposeErrors(t *testing.T) {
	tests := []struct {
		name string
		f    files
		rule Rule
		row  string
		line int // 0: not checked
	}{
		{"profile missing", files{profile: "-", bundle: base}, RuleMissing, "", 0},
		{"bundle missing", files{bundle: "-"}, RuleMissing, "", 0},
		{"empty bundle", files{bundle: "# nothing\n"}, RuleEmpty, "", 0},
		{"empty profile layer", files{bundle: base, l2: "\n\n"}, RuleEmpty, "", 0},
		{"empty site layer", files{bundle: base, l3: "# x\n"}, RuleEmpty, "", 0},
		{"empty patch", files{bundle: base, patches: []string{""}}, RuleEmpty, "", 0},
		{"profile empty", files{profile: "# x\n", bundle: base}, RuleEmpty, "", 0},
		{"profile no bundles", files{profile: "{}", bundle: base}, RuleType, "", 1},
		{"profile unknown field", files{profile: "bundles: []\nx: 1", bundle: base}, RuleUnknownField, "", 2},
		{"profile bundles not list", files{profile: "bundles: x", bundle: base}, RuleType, "", 1},
		{"profile bundle not string", files{profile: "bundles: [1]", bundle: base}, RuleType, "", 1},
		{"profile bundle bad name", files{profile: "bundles: ['a b']", bundle: base}, RuleSyntaxID, "", 1},
		{"profile bundle bad second part", files{profile: "bundles: ['a/b c']", bundle: base}, RuleSyntaxID, "", 1},
		{"profile bundle twice", files{profile: "bundles: [b, b]", bundle: base}, RuleSyntaxID, "", 1},
		{"profile not mapping", files{profile: "[b]", bundle: base}, RuleType, "", 1},
		{"syntax error", files{bundle: "- [\n"}, RuleSyntax, "", 0},
		{"two documents", files{bundle: "[]\n---\n[]\n"}, RuleDocument, "", 2},
		{"alias", files{bundle: "- &x {id: a}\n- *x\n"}, RuleAlias, "", 1},
		{"anchor in config", files{bundle: "- insert:\n  - id: a\n    plugin: o/a\n    roles: [api]\n    config: {k: &x 1}\n"}, RuleAlias, "a", 5},
		{"merge key", files{bundle: "- insert:\n  - id: a\n    plugin: o/a\n    roles: [api]\n    config: {<<: {k: 1}}\n"}, RuleAlias, "a", 5},
		{"explicit tag", files{bundle: "- insert:\n  - id: a\n    plugin: o/a\n    roles: [api]\n    config: {k: !!str 1}\n"}, RuleTag, "a", 5},
		{"duplicate key", files{bundle: "- insert:\n  - id: a\n    plugin: o/a\n    roles: [api]\n    config: {k: 1, k: 2}\n"}, RuleDuplicateKey, "a", 5},
		{"duplicate row key", files{bundle: "- id: a\n  id: b\n"}, RuleDuplicateKey, "", 2},
		{"non scalar key", files{bundle: "- {[a]: 1}\n"}, RuleType, "", 1},
		{"reference as key", files{bundle: "- {'${env:X}': 1}\n"}, RuleReference, "", 1},
		{"root not list", files{bundle: "{id: a}"}, RuleType, "", 1},
		{"root alias", files{bundle: "*x"}, RuleSyntax, "", 0},
		{"item not mapping", files{bundle: "- x\n"}, RuleType, "", 1},
		{"item without id", files{bundle: "- {enabled: true}\n"}, RuleType, "", 1},
		{"insert not exclusive", files{bundle: "- {insert: [], id: a}\n"}, RuleUnknownField, "", 1},
		{"insert not list", files{bundle: "- insert: x\n"}, RuleType, "", 1},
		{"insert alias list", files{bundle: "- insert: &x []\n"}, RuleAlias, "", 1},
		{"insert row not mapping", files{bundle: "- insert: [x]\n"}, RuleType, "", 1},
		{"insert without plugin", files{bundle: "- insert: [{id: a}]\n"}, RuleType, "a", 1},
		{"insert bad id", files{bundle: "- insert: [{id: 'a:b', plugin: o/a, roles: [api]}]\n"}, RuleSyntaxID, "a:b", 1},
		{"insert bad plugin", files{bundle: "- insert: [{id: a, plugin: nope, roles: [api]}]\n"}, RuleSyntaxID, "a", 1},
		{"insert no roles", files{bundle: "- insert: [{id: a, plugin: o/a}]\n"}, RuleRoles, "a", 1},
		{"insert bad role", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [admin]}]\n"}, RuleRoles, "a", 1},
		{"insert unknown field", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], tenant: x}]\n"}, RuleUnknownField, "a", 1},
		{"insert id not string", files{bundle: "- insert: [{id: 5, plugin: o/a, roles: [api]}]\n"}, RuleType, "5", 1},
		{"insert enabled quoted", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], enabled: 'true'}]\n"}, RuleType, "a", 1},
		{"insert enabled yes", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], enabled: yes}]\n"}, RuleType, "a", 1},
		{"insert required not bool", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], required: 1}]\n"}, RuleType, "a", 1},
		{"insert group not bool", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], group: x}]\n"}, RuleType, "a", 1},
		{"insert roles not list", files{bundle: "- insert: [{id: a, plugin: o/a, roles: api}]\n"}, RuleType, "a", 1},
		{"insert roles twice", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api, api]}]\n"}, RuleSyntaxID, "a", 1},
		{"insert role item bad", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [1]}]\n"}, RuleType, "a", 1},
		{"insert role name bad", files{bundle: "- insert: [{id: a, plugin: o/a, roles: ['a b']}]\n"}, RuleSyntaxID, "a", 1},
		{"insert inject bad", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], inject: x}]\n"}, RuleType, "a", 1},
		{"insert isolate bad", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], isolate: x}]\n"}, RuleType, "a", 1},
		{"insert intercept not map", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], intercept: x}]\n"}, RuleType, "a", 1},
		{"insert intercept bad key", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], intercept: {'a b': 1}}]\n"}, RuleSyntaxID, "a", 1},
		{"insert intercept bad value", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], intercept: {k: 0755}}]\n"}, RuleType, "a", 1},
		// config-refs resolves the config of a row only, so a reference in an intercept would reach the provider raw.
		{"insert intercept reference", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], intercept: {k: {m: [1, '${secret:x}']}}}]\n"}, RuleReference, "a", 1},
		{"patch intercept reference", files{bundle: base, l2: "- {id: a, intercept: {k: '${env:AIOPS_REF_X}'}}\n"}, RuleReference, "a", 1},
		{"insert config not map", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], config: [1]}]\n"}, RuleType, "a", 1},
		{"insert plugin not string", files{bundle: "- insert: [{id: a, plugin: 5, roles: [api]}]\n"}, RuleType, "a", 1},
		{"id exists in layer", files{bundle: base + "- insert: [{id: a, plugin: o/b, roles: [api]}]\n"}, RuleDuplicateID, "a", 0},
		{"id exists in later layer", files{bundle: base, l2: "- insert: [{id: a, plugin: o/b, roles: [api]}]\n"}, RuleDuplicateID, "a", 1},
		{"patch sets plugin", files{bundle: base, l2: "- {id: a, plugin: o/z}\n"}, RuleFixedField, "a", 1},
		{"patch sets roles", files{bundle: base, l3: "- {id: a, roles: [edge]}\n"}, RuleFixedField, "a", 1},
		{"patch lowers required", files{bundle: base, patches: []string{"- {id: req, required: false}\n"}}, RuleRequiredLower, "req", 1},
		{"patch inject on required", files{bundle: base, l2: "- {id: req, inject: [x]}\n"}, RuleRequiredRow, "req", 1},
		{"patch isolate on required", files{bundle: base, l2: "- {id: req, isolate: [x]}\n"}, RuleRequiredRow, "req", 1},
		{"patch group on required", files{bundle: base, l2: "- {id: req, group: true}\n"}, RuleRequiredRow, "req", 1},
		{"patch config not map", files{bundle: base, l2: "- {id: a, config: [1]}\n"}, RuleType, "a", 1},
		{"patch config bad scalar", files{bundle: base, l2: "- {id: a, config: {k: 0x10}}\n"}, RuleType, "a", 1},
		{"patch bad id", files{bundle: base, l2: "- {id: 'a b'}\n"}, RuleSyntaxID, "a b", 1},
		{"patch bad reference", files{bundle: base, l2: "- {id: a, config: {k: '${env:'}}\n"}, RuleReference, "a", 1},
		{"group config not list", files{bundle: "- insert: [{id: g, plugin: core:group, roles: [api], config: {a: 1}}]\n"}, RuleGroup, "g", 1},
		{"group config alias", files{bundle: "- insert: [{id: g, plugin: core:group, roles: [api], config: &x []}]\n"}, RuleAlias, "g", 1},
		{"group child bad", files{bundle: "- insert: [{id: g, plugin: core:group, roles: [api], config: [{id: c}]}]\n"}, RuleType, "c", 1},
		{"group child roles", files{bundle: "- insert: [{id: g, plugin: core:group, roles: [api], config: [{id: c, plugin: o/c}]}]\n"}, RuleRoles, "c", 1},
		{"group duplicate child", files{bundle: "- insert: [{id: g, plugin: core:group, roles: [api], config: [{id: c, plugin: o/c, roles: [api]}, {id: c, plugin: o/c, roles: [api]}]}]\n"}, RuleDuplicateID, "g:c", 1},
		{"group patch config", files{bundle: "- insert: [{id: g, plugin: core:group, roles: [api]}]\n", l2: "- {id: g, config: {a: 1}}\n"}, RuleGroup, "g", 1},
		{"include without config", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api]}]\n"}, RuleGroup, "i", 1},
		{"include extra key", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api], config: {path: x, y: 1}}]\n"}, RuleGroup, "i", 1},
		{"include path not string", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api], config: {path: 1}}]\n"}, RuleType, "i", 1},
		{"include empty path", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api], config: {path: ''}}]\n"}, RuleGroup, "i", 1},
		{"include config not map", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api], config: [1]}]\n"}, RuleType, "i", 1},
		{"include escapes", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api], config: {path: ../x.yaml}}]\n"}, RuleInclude, "i", 1},
		{"include dot", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api], config: {path: .}}]\n"}, RuleInclude, "i", 1},
		{"include absent file", files{bundle: "- insert: [{id: i, plugin: core:include, roles: [api], config: {path: nope.yaml}}]\n"}, RuleInclude, "i", 1},
		{"config bad reference scheme", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], config: {k: '${vault:x}'}}]\n"}, RuleReference, "a", 1},
		{"config reference with text around", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], config: {k: 'x${env:A}'}}]\n"}, RuleReference, "a", 1},
		{"anchor on enabled", files{bundle: base, l2: "- {id: a, enabled: &x true}\n"}, RuleAlias, "a", 1},
		{"anchor on inject", files{bundle: base, l2: "- {id: a, inject: &x []}\n"}, RuleAlias, "a", 1},
		{"anchor on key", files{bundle: "- {&x id: a}\n"}, RuleAlias, "", 1},
		{"tag on key", files{bundle: "- {!!str id: a}\n"}, RuleTag, "", 1},
		{"merge as value", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], config: {k: <<}}]\n"}, RuleType, "a", 1},
		{"anchor on id", files{bundle: "- insert: [{id: &x a, plugin: o/a, roles: [api]}]\n"}, RuleAlias, "a", 1},
		{"anchor on root", files{bundle: "&x []\n"}, RuleAlias, "", 1},
		{"anchor on bundles", files{profile: "bundles: &x []", bundle: base}, RuleAlias, "", 1},
		{"patch unknown field", files{bundle: base, l2: "- {id: a, tenant: x}\n"}, RuleUnknownField, "a", 1},
		{"patch with only roles list bad", files{bundle: base, l2: "- {id: a, inject: [1]}\n"}, RuleType, "a", 1},
		{"config nested too deep", files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api], config: " + strings.Repeat("[", 40) + strings.Repeat("]", 40) + "}]\n"}, RuleLimit, "a", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.f.compose(Options{Server: true})
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("want an *Error, got %v", err)
			}
			if ce.Rule != tc.rule {
				t.Errorf("rule %q, want %q (%v)", ce.Rule, tc.rule, err)
			}
			if tc.row != "" && ce.Row != tc.row {
				t.Errorf("row %q, want %q", ce.Row, tc.row)
			}
			if tc.line != 0 && ce.Line != tc.line {
				t.Errorf("line %d, want %d (%v)", ce.Line, tc.line, err)
			}
			if ce.File == "" || ce.Error() == "" {
				t.Errorf("error names no file: %q", ce.Error())
			}
		})
	}
}

func TestPatchFileMissing(t *testing.T) {
	bund := fstest.MapFS{"bundles/b/bundle.yaml": {Data: []byte(base)}}
	home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}}
	_, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: bund, Patches: []File{{FS: bund, Path: "gone.yml"}}}, Options{})
	var ce *Error
	if !errors.As(err, &ce) || ce.Rule != RuleMissing || ce.File != "gone.yml" || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
	// a nil file system is a missing file
	_, _, err = Compose(Sources{Profile: "p", Home: home, Bundles: nil}, Options{})
	if !errors.As(err, &ce) || ce.Rule != RuleMissing {
		t.Fatalf("nil FS: %v", err)
	}
	// an include of a file that is not there
	bund["bundles/b/bundle.yaml"] = &fstest.MapFile{Data: []byte("- insert: [{id: i, plugin: core:include, roles: [api], config: {path: x.yaml}}]\n")}
	bund["bundles/b/x.yaml"] = &fstest.MapFile{Data: []byte("- {id: a}\n")}
	_, _, err = Compose(Sources{Profile: "p", Home: home, Bundles: bund}, Options{Server: true})
	if !errors.As(err, &ce) || ce.Rule != RuleInclude || ce.File != "bundles/b/x.yaml" {
		t.Fatalf("include with a patch: %v", err)
	}
	bund["bundles/b/x.yaml"] = &fstest.MapFile{Data: []byte("")}
	_, _, err = Compose(Sources{Profile: "p", Home: home, Bundles: bund}, Options{Server: true})
	if !errors.As(err, &ce) || ce.Rule != RuleEmpty || ce.File != "bundles/b/x.yaml" {
		t.Fatalf("empty include: %v", err)
	}
}

func TestProfileName(t *testing.T) {
	_, _, err := Compose(Sources{Profile: "../x"}, Options{})
	var ce *Error
	if !errors.As(err, &ce) || ce.Rule != RuleSyntaxID {
		t.Fatalf("got %v", err)
	}
}

type failFS struct{ fstest.MapFS }

func (failFS) Open(name string) (fs.File, error) { return nil, errors.New("boom") }

type badRead struct{}

func (badRead) Stat() (fs.FileInfo, error) { return nil, errors.New("x") }
func (badRead) Read([]byte) (int, error)   { return 0, errors.New("read boom") }
func (badRead) Close() error               { return nil }

type readFailFS struct{}

func (readFailFS) Open(string) (fs.File, error) { return badRead{}, nil }

func TestReadErrors(t *testing.T) {
	var ce *Error
	_, _, err := Compose(Sources{Profile: "p", Home: failFS{}}, Options{})
	if !errors.As(err, &ce) || ce.Rule != RuleRead {
		t.Fatalf("open error: %v", err)
	}
	_, _, err = Compose(Sources{Profile: "p", Home: readFailFS{}}, Options{})
	if !errors.As(err, &ce) || ce.Rule != RuleRead {
		t.Fatalf("read error: %v", err)
	}
	// an optional layer that cannot be read is an error, not a skip
	home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: []")}}
	both := splitFS{home: home, rest: failFS{}}
	_, _, err = Compose(Sources{Profile: "p", Home: both}, Options{})
	if !errors.As(err, &ce) || ce.Rule != RuleRead {
		t.Fatalf("optional read error: %v", err)
	}
	// a file over the bound
	big := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [" + strings.Repeat(" ", MaxFileBytes) + "]")}}
	_, _, err = Compose(Sources{Profile: "p", Home: big}, Options{})
	if !errors.As(err, &ce) || ce.Rule != RuleLimit {
		t.Fatalf("big file: %v", err)
	}
	// an include over the bound
	bund := fstest.MapFS{
		"bundles/b/bundle.yaml": {Data: []byte("- insert: [{id: i, plugin: core:include, roles: [api], config: {path: x.yaml}}]\n")},
		"bundles/b/x.yaml":      {Data: []byte("[" + strings.Repeat(" ", MaxFileBytes) + "]")},
	}
	h := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}}
	_, _, err = Compose(Sources{Profile: "p", Home: h, Bundles: bund}, Options{Server: true})
	if !errors.As(err, &ce) || ce.Rule != RuleLimit {
		t.Fatalf("big include: %v", err)
	}
}

// splitFS serves profile.yaml from home and fails everything else.
type splitFS struct {
	home fstest.MapFS
	rest fs.FS
}

func (s splitFS) Open(name string) (fs.File, error) {
	if name == "profiles/p/profile.yaml" {
		return s.home.Open(name)
	}
	return s.rest.Open(name)
}

func TestIncludeDepthAndRowLimits(t *testing.T) {
	bund := fstest.MapFS{"bundles/b/bundle.yaml": {Data: []byte("- insert: [{id: i0, plugin: core:include, roles: [api], config: {path: f1.yaml}}]\n")}}
	for i := 1; i <= MaxIncludeDepth+1; i++ {
		bund[fmt.Sprintf("bundles/b/f%d.yaml", i)] = &fstest.MapFile{Data: []byte(
			fmt.Sprintf("- insert: [{id: i%d, plugin: core:include, roles: [api], config: {path: f%d.yaml}}]\n", i, i+1))}
	}
	bund[fmt.Sprintf("bundles/b/f%d.yaml", MaxIncludeDepth+2)] = &fstest.MapFile{Data: []byte("[]")}
	home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}}
	_, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: bund}, Options{Server: true})
	var ce *Error
	if !errors.As(err, &ce) || ce.Rule != RuleInclude || !strings.Contains(ce.Msg, "deeper") {
		t.Fatalf("depth: %v", err)
	}

	var b strings.Builder
	b.WriteString("- insert:\n")
	for i := 0; i <= MaxRows; i++ {
		fmt.Fprintf(&b, "  - {id: r%d, plugin: o/a}\n", i)
	}
	bund = fstest.MapFS{"bundles/b/bundle.yaml": {Data: []byte(b.String())}}
	_, _, err = Compose(Sources{Profile: "p", Home: home, Bundles: bund}, Options{})
	if !errors.As(err, &ce) || ce.Rule != RuleLimit || !strings.Contains(ce.Msg, "rows") {
		t.Fatalf("rows: %v", err)
	}
}

// countFS counts the opens of each path.
type countFS struct {
	fstest.MapFS
	opens map[string]int
}

func (c countFS) Open(name string) (fs.File, error) {
	c.opens[name]++
	return c.MapFS.Open(name)
}

// Include rows that name one file again and again mount it again and again: three
// per file, four levels deep, mount the deepest file 3^4 times. Each mount counts
// against MaxComposeBytes, so near-full files stop the composition after about
// MaxComposeBytes/MaxFileBytes mounts instead of parsing all 121 of them.
func TestIncludeFanOutBound(t *testing.T) {
	pad := "#" + strings.Repeat("x", MaxFileBytes-1024) + "\n"
	fan := func(next string) string {
		var b strings.Builder
		b.WriteString(pad + "- insert:\n")
		for i := range 3 {
			fmt.Fprintf(&b, "  - {id: i%d, plugin: core:include, roles: [api], config: {path: %s}}\n", i, next)
		}
		return b.String()
	}
	bund := countFS{MapFS: fstest.MapFS{"bundles/b/bundle.yaml": {Data: []byte(fan("f1.yaml"))}}, opens: map[string]int{}}
	for i := 1; i < MaxIncludeDepth; i++ {
		bund.MapFS[fmt.Sprintf("bundles/b/f%d.yaml", i)] = &fstest.MapFile{Data: []byte(fan(fmt.Sprintf("f%d.yaml", i+1)))}
	}
	bund.MapFS[fmt.Sprintf("bundles/b/f%d.yaml", MaxIncludeDepth)] = &fstest.MapFile{Data: []byte(pad + "- insert: [{id: leaf, plugin: o/a, roles: [api]}]\n")}
	home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}}
	_, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: bund}, Options{Server: true})
	var ce *Error
	if !errors.As(err, &ce) || ce.Rule != RuleLimit || !strings.Contains(ce.Msg, "counted at each include") {
		t.Fatalf("want the composition bound, got %v", err)
	}
	opens := 0
	for _, n := range bund.opens {
		opens += n
	}
	if limit := MaxComposeBytes/(MaxFileBytes-1024) + 1; opens > limit {
		t.Errorf("%d files opened, want at most %d", opens, limit)
	}
}

func TestGroupNestingLimit(t *testing.T) {
	nest := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "[{id: g%d, plugin: core:group, config: ", i)
		}
		b.WriteString("[]")
		b.WriteString(strings.Repeat("}]", n))
		return "- insert: " + b.String() + "\n"
	}
	if _, _, err := (files{bundle: nest(5)}).compose(Options{}); err != nil {
		t.Fatal(err)
	}
	_, _, err := (files{bundle: nest(MaxDepth + 2)}).compose(Options{})
	var ce *Error
	if !errors.As(err, &ce) || ce.Rule != RuleLimit {
		t.Fatalf("got %v", err)
	}
}

func TestComposeRules(t *testing.T) {
	t.Run("required raised and kept", func(t *testing.T) {
		c, w, err := files{bundle: base, l2: "- {id: a, required: true}\n", l3: "- {id: a, required: true}\n"}.compose(Options{Server: true})
		if err != nil || len(w) != 0 {
			t.Fatal(err, w)
		}
		if !c.Rows[0].Required || c.Rows[0].Provenance.Fields[iRequired].Origin.Layer != 3 {
			t.Errorf("%+v", c.Rows[0])
		}
	})
	t.Run("config replaces whole", func(t *testing.T) {
		c, _, err := files{bundle: base, l2: "- {id: a, config: {z: 1}}\n"}.compose(Options{Server: true})
		if err != nil || show(c.Rows[0].Config) != "{z: int(1)}" {
			t.Fatal(err, show(c.Rows[0].Config))
		}
	})
	t.Run("omitted config untouched", func(t *testing.T) {
		c, _, err := files{bundle: base, l2: "- {id: a, enabled: false}\n"}.compose(Options{Server: true})
		if err != nil || show(c.Rows[0].Config) != "{k: int(1)}" || c.Rows[0].Enabled {
			t.Fatal(err)
		}
		if c.Rows[0].Provenance.Fields[iConfig].Origin.Layer != 1 || c.Rows[0].Provenance.Fields[iEnabled].Origin.Layer != 2 {
			t.Errorf("%+v", c.Rows[0].Provenance)
		}
	})
	t.Run("all patch fields", func(t *testing.T) {
		p := "- {id: a, enabled: false, required: true, inject: [x, y], intercept: {k: {m: 1}, a: 1}, isolate: [z], group: true}\n"
		c, _, err := files{bundle: base, patches: []string{p}}.compose(Options{Server: true})
		r := c.Rows[0]
		if err != nil || r.Enabled || !r.Required || !r.Group || len(r.Inject) != 2 || len(r.Isolate) != 1 || len(r.Intercept) != 2 || r.Intercept[0].Key != "a" {
			t.Fatalf("%v %+v", err, r)
		}
		for _, fo := range r.Provenance.Fields {
			if fo.Field != FieldID && fo.Field != FieldPlugin && fo.Field != FieldConfig && fo.Field != FieldRoles && fo.Origin.Layer != 4 {
				t.Errorf("%s from layer %d", fo.Field, fo.Origin.Layer)
			}
		}
	})
	t.Run("unknown id warns", func(t *testing.T) {
		_, w, err := files{bundle: base, l2: "- {id: nope, enabled: false}\n"}.compose(Options{Server: true})
		if err != nil || len(w) != 1 || w[0].Kind != WarnUnknownID || w[0].Row != "nope" || w[0].Line != 1 || !strings.Contains(w[0].String(), "unknown") {
			t.Fatal(err, w)
		}
	})
	t.Run("absent optional layers skipped, empty list is a layer", func(t *testing.T) {
		c, w, err := files{bundle: "[]\n", l2: "[]\n", l3: "[]\n", patches: []string{"[]"}}.compose(Options{})
		if err != nil || len(w) != 0 || len(c.Rows) != 0 {
			t.Fatal(err, w, c)
		}
	})
	t.Run("roles optional outside a server bundle", func(t *testing.T) {
		c, _, err := files{bundle: "- insert: [{id: a, plugin: o/a}]\n"}.compose(Options{})
		if err != nil || len(c.Rows) != 1 || len(c.Rows[0].Roles) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("patch addresses a group child", func(t *testing.T) {
		g := "- insert: [{id: g, plugin: core:group, roles: [api], config: [{id: c, plugin: o/c, roles: [api]}]}]\n"
		c, _, err := files{bundle: g, l2: "- {id: 'g:c', enabled: false}\n"}.compose(Options{Server: true})
		if err != nil || c.Rows[1].ID != "g:c" || c.Rows[1].Parent != "g" || c.Rows[1].Enabled {
			t.Fatal(err, c.Rows)
		}
	})
	t.Run("group without config", func(t *testing.T) {
		c, _, err := files{bundle: "- insert: [{id: g, plugin: core:group, roles: [api]}]\n"}.compose(Options{Server: true})
		if err != nil || len(c.Rows) != 1 || c.Rows[0].Config.Kind != KindList {
			t.Fatal(err)
		}
	})
	t.Run("row without config", func(t *testing.T) {
		c, _, err := files{bundle: "- insert: [{id: a, plugin: o/a, roles: [api]}]\n"}.compose(Options{Server: true})
		if err != nil || c.Rows[0].Config.Kind != KindMap || len(c.Rows[0].Config.Map) != 0 {
			t.Fatal(err)
		}
	})
	t.Run("input order of keys and sets does not matter", func(t *testing.T) {
		a := "- insert: [{id: a, plugin: o/a, roles: [worker, api], inject: [y, x], config: {b: 1, a: 2}}]\n"
		b := "- insert: [{inject: [x, y], config: {a: 2, b: 1}, roles: [api, worker], plugin: o/a, id: a}]\n"
		ca, _, err1 := files{bundle: a}.compose(Options{Server: true})
		cb, _, err2 := files{bundle: b}.compose(Options{Server: true})
		if err1 != nil || err2 != nil || ca.Digest != cb.Digest {
			t.Fatal(err1, err2, ca.Digest, cb.Digest)
		}
	})
}

func TestScalarTyping(t *testing.T) {
	tests := []struct {
		in, want string // want: show() of the value, or "error"
	}{
		{"1", "int(1)"}, {"-7", "int(-7)"}, {"+7", "int(7)"}, {"0", "int(0)"},
		{"1.5", "float(1.5)"}, {"1e3", "float(1000)"}, {"-0.0", "float(0)"}, {".5", "float(0.5)"},
		{"true", "true"}, {"False", "false"}, {"null", "null"}, {"~", "null"}, {"", "null"},
		{"hello", `"hello"`}, {"'yes'", `"yes"`}, {`"no"`, `"no"`}, {"'0755'", `"0755"`}, {"|\n  yes\n", `"yes\n"`},
		{"2001-12-14", `"2001-12-14"`}, {"2001-12-14T21:59:43Z", `"2001-12-14T21:59:43Z"`},
		{"0755", "error"}, {"0o17", "error"}, {"0x1F", "error"}, {"1_000", "error"}, {"00", "error"},
		{"9223372036854775807", "int(9223372036854775807)"},
		{"9223372036854775808", "error"}, {"-9223372036854775809", "error"},
		{"1e999", "error"}, {".inf", "error"}, {"-.inf", "error"}, {".nan", "error"}, {"1_0.5", "error"},
		{"yes", "error"}, {"No", "error"}, {"ON", "error"}, {"off", "error"}, {"y", "error"}, {"N", "error"},
		{"!!binary aGk=", "error"},
		{"'${env:AIOPS_REF_A}'", "${env:AIOPS_REF_A}"},
		{"'${file:/etc/x y}'", "${file:/etc/x y}"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			bundle := "- insert: [{id: a, plugin: o/a, roles: [api], config: {k: " + tc.in + "}}]\n"
			if strings.Contains(tc.in, "\n") {
				bundle = "- insert:\n  - id: a\n    plugin: o/a\n    roles: [api]\n    config:\n      k: " + strings.ReplaceAll(strings.TrimSuffix(tc.in, "\n"), "\n", "\n        ") + "\n"
			}
			c, _, err := files{bundle: bundle}.compose(Options{Server: true})
			if tc.want == "error" {
				var ce *Error
				if !errors.As(err, &ce) {
					t.Fatalf("want error, got %v", c.Rows)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := show(c.Rows[0].Config.Map[0].Value); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// ParseConfig reads one row config as a layer does and returns the tree the launcher
// hands the row; every rule of the layer parser holds.
func TestParseConfig(t *testing.T) {
	got, err := ParseConfig("cycle.yaml", []byte("n: 10\nr: 0.5\nname: \"yes\"\nl: [a, true]\nm: {k: ~}\nref: ${env:AIOPS_REF_X}\n"))
	want := map[string]any{"n": int64(10), "r": 0.5, "name": "yes", "l": []any{"a", true}, "m": map[string]any{"k": nil},
		"ref": launch.Ref{Scheme: "env", Name: "AIOPS_REF_X"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseConfig = %#v, %v", got, err)
	}
	for src, rule := range map[string]Rule{
		strings.Repeat("#", MaxFileBytes+1): RuleLimit,
		"# only a comment\n":                RuleEmpty,
		"a: [\n":                            RuleSyntax,
		"a: 1\n---\nb: 2\n":                 RuleDocument,
		"- a\n":                             RuleType,
		"n: 012\n":                          RuleType,
		"n: &v 1\nm: *v\n":                  RuleAlias,
	} {
		var e *Error
		if _, err := ParseConfig("cycle.yaml", []byte(src)); !errors.As(err, &e) || e.Rule != rule || e.File != "cycle.yaml" {
			t.Errorf("ParseConfig(%.20q) = %v, want rule %s", src, err, rule)
		}
	}
}

func TestParseRef(t *testing.T) {
	good := map[string]Ref{
		"${env:AIOPS_REF_X}":     {RefEnv, "AIOPS_REF_X"},
		"${env:_a1}":             {RefEnv, "_a1"},
		"${secret:svc-token.v2}": {RefSecret, "svc-token.v2"},
		"${flag:gateway-url}":    {RefFlag, "gateway-url"},
		"${file:/a/b c.pem}":     {RefFile, "/a/b c.pem"},
	}
	for s, want := range good {
		if got, ok := parseRef(s); !ok || got != want {
			t.Errorf("%q: %v %v", s, got, ok)
		}
	}
	for _, s := range []string{"", "${}", "${env}", "${env:}", "${env:1A}", "${env:a-b}", "${env:a b}", "${secret:-x}", "${secret:a b}",
		"${secret:" + strings.Repeat("a", 129) + "}", "${vault:x}", "${env:A}x", "x${env:A}", "${env:A}${env:B}", "${env:{A}}",
		"${file:a\nb}", "${file:a\x7fb}", "$env:A", "${env:A"} {
		if _, ok := parseRef(s); ok {
			t.Errorf("%q accepted", s)
		}
	}
	// the scheme of a failed parse is still returned for diagnostics
	if r, ok := parseRef("${env:1A}"); ok || r.Scheme != RefEnv {
		t.Errorf("%v %v", r, ok)
	}
}

func TestRefsPerRow(t *testing.T) {
	b := `- insert:
    - id: a
      plugin: o/a
      roles: [api]
      intercept: {k: {m: "flag:x"}}
      config: {z: "${env:Z}", a: {s: "${secret:s}"}, l: ["${file:/p}", 1]}
`
	c, _, err := files{bundle: b}.compose(Options{Server: true})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range c.Rows[0].Refs {
		got = append(got, u.Path+"="+string(u.Ref.Scheme))
	}
	want := "config.a.s=secret config.l[0]=file config.z=env"
	if strings.Join(got, " ") != want {
		t.Errorf("got %v", got)
	}
}

func TestKillSwitches(t *testing.T) {
	env := map[string]string{EnvAgentsPaused: "1"}
	k := ReadKillSwitches(func(n string) string { return env[n] })
	if !k.AgentsPaused || k.TelemetryDisabled {
		t.Errorf("%+v", k)
	}
	if k := ReadKillSwitches(func(string) string { return "" }); k != (KillSwitches{}) {
		t.Errorf("%+v", k)
	}
	if k := ReadKillSwitches(func(n string) string { return map[string]string{EnvTelemetryDisabled: "x"}[n] }); !k.TelemetryDisabled {
		t.Errorf("%+v", k)
	}
}

func TestFieldNames(t *testing.T) {
	n := FieldNames()
	if len(n) != numFields || n[0] != FieldID || n[numFields-1] != FieldGroup {
		t.Fatal(n)
	}
}

func TestErrorFormat(t *testing.T) {
	e := &Error{File: "f.yml", Line: 3, Row: "a", Rule: RuleEmpty, Msg: "m"}
	if got, want := e.Error(), `f.yml:3: row "a": empty-file: m`; got != want {
		t.Errorf("%q", got)
	}
	e2 := &Error{File: "f.yml", Rule: RuleLimit, Msg: "m", Err: fs.ErrNotExist}
	if e2.Error() != "f.yml: limit: m" || !errors.Is(e2, fs.ErrNotExist) {
		t.Errorf("%q", e2.Error())
	}
}

func TestYamlLine(t *testing.T) {
	for in, want := range map[string]int{"yaml: line 7: x": 7, "no line": 0, "line x": 0, "line 99999999999999": 0} {
		if got := yamlLine(errors.New(in)); got != want {
			t.Errorf("%q: %d, want %d", in, got, want)
		}
	}
}

// A kept bundle that is missing or does not parse inserts none of the artifact's rows,
// and the rest of the profile composes: the gate denies the artifact by check 5 or 8
// (ADR-047 decisions 6 and 18). A composition error would stop every profile that holds
// it.
func TestComposeLeavesOutAnInstalledBundleThatDoesNotRead(t *testing.T) {
	for name, files := range map[string]fstest.MapFS{
		"missing":   {},
		"malformed": {"bundles/acme/pack/bundle.yaml": {Data: []byte("- insert:\n    - {id: e1}\n")}},
	} {
		home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b, acme/pack]")}, "bundles/b/bundle.yaml": {Data: []byte(rows("id: x, plugin: o/x"))}}
		maps.Copy(home, files)
		c, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: home}, Options{})
		if err != nil || len(c.Rows) != 1 || c.Unread["acme/pack"] == "" {
			t.Errorf("%s: %v %+v", name, err, c)
		}
	}
	if _, _, err := Compose(Sources{Profile: "p", Home: fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}}}, Options{}); err == nil {
		t.Error("a missing first-party bundle composes")
	}
}

// Boot mounts the rows of an installed artifact's bundle under one core:group row it
// adds, named by the <name> segment of the plugin id, enabled, with every role a row
// states (ADR-047 decision 5). The bundle places no row outside it, so a patch item
// changes nothing (check 8 denies the artifact). A group whose id a row composed before
// it holds inserts none of the artifact's rows, and the rest composes (decision 6).
func TestComposeMountsAnInstalledArtifact(t *testing.T) {
	compose := func(base, pack string) Composition {
		t.Helper()
		home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b, acme/pack]")},
			"bundles/b/bundle.yaml": {Data: []byte(base)}, "bundles/acme/pack/bundle.yaml": {Data: []byte(pack)}}
		c, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: home}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	line := func(c Composition) string {
		var out []string
		for _, r := range c.Rows {
			out = append(out, fmt.Sprintf("%s %s parent=%s enabled=%v roles=%v origin=%s", r.ID, r.Plugin, r.Parent, r.Enabled, r.Roles, installedBundle(r)))
		}
		return strings.Join(out, "; ") + fmt.Sprintf("; clashes %v", c.Clashes)
	}
	pack := rows("id: e, plugin: aiops/echo, roles: [api], enabled: false", "id: f, plugin: aiops/echo, roles: [worker], enabled: false") +
		"- {id: x, enabled: false}\n"
	for _, tc := range []struct{ base, pack, want string }{
		{rows("id: x, plugin: o/x"), pack, "x o/x parent= enabled=true roles=[] origin=; " +
			"pack core:group parent= enabled=true roles=[api worker] origin=acme/pack; " +
			"pack:e aiops/echo parent=pack enabled=false roles=[api] origin=acme/pack; " +
			"pack:f aiops/echo parent=pack enabled=false roles=[worker] origin=acme/pack; clashes []"},
		{rows("id: x, plugin: o/x"), rows("id: e, plugin: aiops/echo, roles: [api], enabled: false", "id: f, plugin: aiops/echo, enabled: false"),
			"x o/x parent= enabled=true roles=[] origin=; pack core:group parent= enabled=true roles=[] origin=acme/pack; " +
				"pack:e aiops/echo parent=pack enabled=false roles=[api] origin=acme/pack; pack:f aiops/echo parent=pack enabled=false roles=[] origin=acme/pack; clashes []"},
		{rows("id: pack, plugin: o/x"), pack, "pack o/x parent= enabled=true roles=[] origin=; clashes [acme/pack]"},
	} {
		if got := line(compose(tc.base, tc.pack)); got != tc.want {
			t.Errorf("got  %s\nwant %s", got, tc.want)
		}
	}
}
