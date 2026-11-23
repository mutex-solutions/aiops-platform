package boot

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// comp composes a bundle (layer 1) and patch files (layer 4) held in memory.
func comp(t *testing.T, bundle string, patches ...string) Composition {
	t.Helper()
	c, err := tryComp(bundle, patches...)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	return c
}

func tryComp(bundle string, patches ...string) (Composition, error) {
	home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte("bundles: [b]")}}
	bund := fstest.MapFS{"bundles/b/bundle.yaml": {Data: []byte(bundle)}}
	var files []File
	for i, p := range patches {
		name := fmt.Sprintf("patch%d.yml", i+1)
		bund[name] = &fstest.MapFile{Data: []byte(p)}
		files = append(files, File{FS: bund, Path: name})
	}
	c, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: bund, Patches: files}, Options{})
	return c, err
}

// Gate and Admit run the gate, and admit, given no signed distribution manifest: on a
// module table and bindings alone, which is what the layer and class rules read.
func Gate(c Composition, mods ModuleTable, dist DistManifest) GateResult {
	return GateWith(c, GateInput{Entry: EntryRender, Mods: mods, Bindings: dist})
}

func Admit(c Composition, mods ModuleTable, dist DistManifest, roles []Role) Admission {
	return AdmitWith(c, GateInput{Entry: EntryRender, Mods: mods, Bindings: dist}, roles)
}

func mustMods(t *testing.T, y string) ModuleTable {
	t.Helper()
	m, err := ParseModules("modules.yaml", []byte(y))
	if err != nil {
		t.Fatalf("modules: %v", err)
	}
	return m
}

func mustDist(t *testing.T, y string) DistManifest {
	t.Helper()
	if strings.TrimSpace(y) == "" {
		y = "{}"
	}
	d, err := ParseDist("dist.yaml", []byte(y))
	if err != nil {
		t.Fatalf("dist: %v", err)
	}
	return d
}

// rows builds a bundle of inserts from one-line row specs.
func rows(specs ...string) string {
	var b strings.Builder
	b.WriteString("- insert:\n")
	for _, s := range specs {
		b.WriteString("    - {" + s + "}\n")
	}
	return b.String()
}

// gateLines summarises the failures of a gate result, one line per failure.
func gateLines(g GateResult) []string {
	var out []string
	for _, f := range g.Unresolved {
		out = append(out, fmt.Sprintf("0 %s %s stop=%v", f.Row, f.Rule, f.Stop))
	}
	for _, s := range g.Slots {
		for _, f := range s.Fails {
			out = append(out, fmt.Sprintf("%d %s %s stop=%v", s.N, f.Row, f.Rule, f.Stop))
		}
	}
	return out
}

func equalLines(a, b []string) bool { return equalStrings(a, b) }
