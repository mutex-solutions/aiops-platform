package boot

import (
	"errors"
	"testing"
	"testing/fstest"
)

// FuzzCompose feeds arbitrary bytes as a --patch file over a small valid
// profile. It must never panic, and must either compose or return an *Error.
func FuzzCompose(f *testing.F) {
	for _, s := range []string{
		"", "[]", "- {id: a, enabled: false}", "- insert: [{id: z, plugin: o/z, roles: [api]}]",
		"- id: a\n  config: {k: '${env:X}'}", "- &x {id: a}\n- *x", "---\n[]\n---\n[]",
		"- {id: a, config: {k: 0755}}", "- {id: req, required: false}", "[[[[[[[", "\x00\xff",
		"- insert: [{id: g, plugin: core:group, roles: [api], config: [{id: c, plugin: o/c, roles: [api]}]}]",
		"- insert: [{id: i, plugin: core:include, roles: [api], config: {path: ../x}}]",
	} {
		f.Add([]byte(s))
	}
	home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}}
	f.Fuzz(func(t *testing.T, data []byte) {
		bund := fstest.MapFS{
			"bundles/b/bundle.yaml": {Data: []byte(base)},
			"patch.yml":             {Data: data},
		}
		c, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: bund, Patches: []File{{FS: bund, Path: "patch.yml"}}}, Options{Server: true})
		if err != nil {
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("untyped error %T: %v", err, err)
			}
			return
		}
		if len(c.Rows) < 2 {
			t.Fatalf("composition lost rows: %d", len(c.Rows))
		}
	})
}
