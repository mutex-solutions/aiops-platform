// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"os"
	"path/filepath"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// Module reads the path of go.mod, the version asked and the h1 sum of the module zip
// of the directory, as the Go toolchain writes it in go.sum, and the rest of the build
// list, each module with its path, its version with its replacement and its sum, as go
// list -m -json all reads it, a module that only a requirement requires among them
// (ADR-047 decision 1). The module cache alone serves golang.org/x/sync, which the
// repository requires at that version. A directory with no go.mod is refused.
func TestModule(t *testing.T) {
	t.Setenv("GOPROXY", "off")
	root := t.TempDir()
	for name, data := range map[string]string{
		"b/go.mod": "module example.test/b\n\ngo 1.26\n\nrequire example.test/c v1.0.0\n",
		"c/go.mod": "module example.test/c\n\ngo 1.26\n",
		"echo/go.mod": "module example.test/echo\n\ngo 1.26.0\n\nrequire (\n\texample.test/b v1.0.0\n\tgolang.org/x/sync v0.24.0\n)\n\n" +
			"replace example.test/b => ../b\n\nreplace example.test/c => ../c\n",
		"echo/go.sum": "golang.org/x/sync v0.24.0 h1:iqiDrGoDgyuvAKrhLQthUEzU/R378N78gXbBjxed7oA=\ngolang.org/x/sync v0.24.0/go.mod h1:sUUOizhqBxiL6pEWpqNLUiaJn1ShEbZ6BBqskPbjZm0=\n",
		"echo/plugin.yaml": "manifestVersion: \"1.0\"\nid: acme/connector-x\nversion: 0.1.0\ntitle: Acme connector for X\nlicense: Apache-2.0\npublisher: {id: acme}\n" +
			"stability: experimental\nscope: [process]\nruntimes: [inproc]\nrequires: {definitions: {echoers: [\"0.1\"]}}\n",
		"echo/bundle.yaml": "[]\n",
		"echo/echo.go":     "package echo\n",
		"echo/fixtures/x":  "x\n",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Tools{}.Module(t.Context(), launch.ModuleRequest{Dir: filepath.Join(root, "echo"), Version: "v0.1.0"})
	if err != nil || m.Path != "example.test/echo" || m.Version != "v0.1.0" || m.Sum != "h1:Ryl9hfaUbtcrXqV18lzBbPaztAz29w8OPhu5dRdlIXM=" {
		t.Fatalf("Module = %+v, %v", m, err)
	}
	want := []launch.Module{{Path: "example.test/b", Version: "v1.0.0 => ../b"}, {Path: "example.test/c", Version: "v1.0.0 => ../c"},
		{Path: "golang.org/x/sync", Version: "v0.24.0", Sum: "h1:iqiDrGoDgyuvAKrhLQthUEzU/R378N78gXbBjxed7oA="}}
	if len(m.Deps) != len(want) {
		t.Fatalf("the build list %+v; want %+v", m.Deps, want)
	}
	for i := range want {
		if m.Deps[i].Path != want[i].Path || m.Deps[i].Version != want[i].Version || m.Deps[i].Sum != want[i].Sum || m.Deps[i].Deps != nil {
			t.Errorf("the build list %+v; want %+v", m.Deps, want)
		}
	}
	if _, err := (Tools{}).Module(t.Context(), launch.ModuleRequest{Dir: root, Version: "v0.1.0"}); err == nil {
		t.Error("Module of a directory with no go.mod = nil error")
	}
	// A plugin whose package sits below its module root, as each plugin of this repository
	// does, packs with the module that holds it (ADR-060 decision 32, standing rule 2).
	if err := os.Mkdir(filepath.Join(root, "echo", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if n, err := (Tools{}).Module(t.Context(), launch.ModuleRequest{Dir: filepath.Join(root, "echo", "sub"), Version: "v0.1.0"}); err != nil || n.Path != m.Path || n.Sum != m.Sum {
		t.Errorf("Module of a package below its module root = %+v, %v; want the module %s at %s", n, err, m.Path, m.Sum)
	}
}
