// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/sumdb/dirhash"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// The site's dist.yaml has four fields (08:401): release by digest, the plugin modules,
// bindings with sealedHooks and required (ADR-044 decision 22), and acceptLicenses;
// anything else, a release by tag, a malformed module, a module twice and a binding
// that boot's reader refuses are refused.
func TestReadSite(t *testing.T) {
	const release = "release: registry.example/aiops/release@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + "\n"
	sum := h1("echo")
	s, err := ReadSite([]byte(release + "plugins:\n  - {module: example.test/echo, version: v0.1.0, sum: " + sum + "}\n" +
		"bindings:\n  consumers: {operations: [acme/echo]}\n  sealedHooks: {internal/status: [acme/echo]}\n  required: {keys: [echoers], rows: [echo]}\n" +
		"acceptLicenses: [MPL-2.0]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Plugins) != 1 || s.Sums["example.test/echo"] != sum || len(s.Bindings.Consumers) != 1 || s.Bindings.SealedHooks[0].Hook != "internal/status" ||
		!slices.Equal(s.Bindings.RequiredKeys, []string{"echoers"}) || !slices.Equal(s.Bindings.RequiredRows, []string{"echo"}) || s.AcceptLicenses[0] != "MPL-2.0" {
		t.Errorf("site %+v", s)
	}
	for _, bad := range []string{
		"release: registry.example/aiops/release:1.0\n",
		release + "providers: {x: [acme/x]}\n",
		release + "plugins:\n  - {module: example.test/echo, version: latest, sum: " + sum + "}\n",
		release + "plugins:\n  - {module: example.test/echo, version: v0.1.0, sum: " + sum + "}\n  - {module: example.test/echo, version: v0.2.0, sum: " + sum + "}\n",
		release + "bindings:\n  consumers: {operations: [not an id]}\n",
		release + "acceptLicenses: [MIT AND GPL-2.0-only]\n",
	} {
		if _, err := ReadSite([]byte(bad)); err == nil {
			t.Errorf("ReadSite took %q", bad)
		}
	}
}

// Strict versions (ADR-044 decisions 10 and 20): a pinned module raised by the build
// fails unless --allow-bump names it, and passes with it, printed with the module that
// requires it; the SDK modules and the root module fail with or without the flag, at
// the release version whatever the pins hold of them; a module pinned by its go.mod
// alone is pinned; a raise from v0 to v1, or across +incompatible, fails with it; a
// flag that names an unchanged module fails.
func TestStrictVersions(t *testing.T) {
	release := []boot.DistPin{{Path: "golang.org/x/sync", Version: "v0.24.0", Sum: h1("sync")}, {Path: rootModule + "/sdk", Version: "v0.3.0", Sum: h1("sdk")},
		{Path: rootModule, Version: "v0.3.0", Sum: h1("root")},
		{Path: "example.test/zero", Version: "v0.9.0", Sum: h1("zero")}, {Path: "example.test/same", Version: "v1.0.0", Sum: h1("same")},
		{Path: "example.test/graph", Version: "v1.0.0", GoMod: h1("graph")}, {Path: Toolchain, Version: ToolchainVersion, Sum: ToolchainSum}}
	graph := "example.test/echo@v0.1.0 golang.org/x/sync@v0.25.0\nexample.test/echo@v0.1.0 example.test/sdk@v0.4.0\n"
	list := map[string]string{"golang.org/x/sync": "v0.25.0", "example.test/same": "v1.0.0", "example.test/graph": "v1.0.0", rootModule + "/sdk/testkit": "v0.3.0"}
	if _, _, err := strict(release, list, nil, graph); err == nil || !strings.Contains(err.Error(), "--allow-bump golang.org/x/sync admits the raise") {
		t.Errorf("a raise without the flag: %v", err)
	}
	bumps, admitted, err := strict(release, list, []string{"golang.org/x/sync"}, graph)
	if err != nil || len(bumps) != 1 || !strings.Contains(bumps[0], "pinned by the release at v0.24.0, is v0.25.0 in this build, required by example.test/echo@v0.1.0") ||
		admitted["golang.org/x/sync"] != "v0.24.0" {
		t.Errorf("a raise with the flag: %v, %v, %v", bumps, admitted, err)
	}
	for _, c := range []struct {
		list  map[string]string
		allow []string
		want  string
	}{
		{map[string]string{rootModule + "/sdk": "v0.4.0"}, []string{rootModule + "/sdk"}, "which --allow-bump never names"},
		// An SDK module that neither release binary links has no zip sum among the pins,
		// and the release version holds it all the same (ADR-044 decision 20).
		{map[string]string{rootModule + "/sdk/testkit": "v0.4.0"}, nil, rootModule + "/sdk/testkit, pinned by the release at v0.3.0, is v0.4.0 in this build"},
		{map[string]string{rootModule + "/sdk/testkit": "v0.4.0"}, []string{rootModule + "/sdk/testkit"}, "which --allow-bump never names"},
		{map[string]string{rootModule: "v0.4.0"}, nil, "which --allow-bump never names"},
		// A module that the release pins by its go.mod alone is pinned too: a raise of it
		// is a bump (ADR-044 decision 10).
		{map[string]string{"example.test/graph": "v1.1.0"}, nil, "--allow-bump example.test/graph admits the raise"},
		{map[string]string{"example.test/zero": "v1.0.0"}, []string{"example.test/zero"}, "changes the major"},
		{map[string]string{}, []string{"example.test/same"}, "--allow-bump example.test/same names a module whose version this build does not change"},
	} {
		if _, _, err := strict(release, c.list, c.allow, graph); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v with %v: %v, want %q", c.list, c.allow, err, c.want)
		}
	}
}

// The local path runs the pinned toolchain module from the module source, checked
// against the sum the signed manifest records, never the host's (ADR-044 decision 16):
// a module whose sum differs is refused, and one that matches is extracted under the
// builder's cache with its commands executable, once.
func TestTree(t *testing.T) {
	if runtime.GOOS+"-"+runtime.GOARCH != "linux-amd64" {
		if _, err := Tree(t.TempDir(), t.TempDir(), ToolchainSum); err == nil || !strings.Contains(err.Error(), "no toolchain module for") {
			t.Errorf("another host: %v", err)
		}
		return
	}
	modcache, sum := toolchainZip(t)
	cache := t.TempDir()
	if _, err := Tree(modcache, cache, h1("another tree")); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("another sum: %v", err)
	}
	goroot, err := Tree(modcache, cache, sum)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Go{Bin: filepath.Join(goroot, "bin", "go")}.Run(context.Background())
	if err != nil || string(out) != "go\n" {
		t.Errorf("the extracted go: %q, %v", out, err)
	}
	if again, err := Tree(modcache, cache, sum); err != nil || again != goroot {
		t.Errorf("a second extraction: %v", err)
	}
}

// toolchainZip writes into a module source of its own a toolchain module of three files
// at the pinned version, and returns the source and the sum of the zip.
func toolchainZip(t *testing.T) (string, string) {
	t.Helper()
	modcache := t.TempDir()
	dir := filepath.Join(modcache, "golang.org", "toolchain", "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, ToolchainVersion+".zip")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range map[string]string{"bin/go": "#!/bin/sh\necho go\n", "LICENSE": "a licence\n", "pkg/tool/linux_amd64/compile": "#!/bin/sh\n"} {
		w, _ := zw.Create(Toolchain + "@" + ToolchainVersion + "/" + name)
		_, _ = w.Write([]byte(data))
	}
	if err := zw.Close(); err != nil || f.Close() != nil {
		t.Fatal(err)
	}
	sum, err := dirhash.HashZip(file, dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	return modcache, sum
}
