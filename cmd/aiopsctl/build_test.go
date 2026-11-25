// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/authoring"
	"github.com/mutex-solutions/aiops-platform/internal/builder"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
	"github.com/mutex-solutions/aiops-platform/templates"
)

// testVersion is the version at which the case serves the modules of the repository: a
// pre-release that no tag carries, as the exit serves them (ADR-060 decision 40).
const testVersion = "v0.2.0-s2a.test"

// dist build on the local path, then dist sign and dist use of what it wrote (ADR-044;
// ADR-060 decision 22; ADR-020 decisions 12 and 29). A release is built from the
// repository's modules served at testVersion, its first phase, the manifest; a site's
// dist.yaml adds an echoer module, which the builder of the toolbox builds on that
// release, as the release's aiopsctl, which embeds it, does: the signed manifest holds
// the plugin and serves echoers, the server and the site's aiopsctl are written with
// their THIRD_PARTY_LICENSES/, NOTICE and SBOM, and the image layout carries the
// manifest as a referrer, which dist sign signs with the site's key and dist use
// records; the site's aiopsctl then boots on that record. This binary's own module
// table is not the release's, whose modules are served, so it runs only the toolbox on
// the release and the commands that do not read it. dist.yaml names the release
// artifact in a registry by digest, and one whose signed manifest is not the one the
// builder embeds is refused, as is a zip whose sum
// differs from dist.yaml's, and a module outside the release that
// declares an aiops/ id, links a fake package, registers a default-mux handler from
// init and is under a licence dist.yaml does not accept. It builds from nothing with the
// pinned toolchain module, read from the Go module cache, so -short skips it and the CI
// job builder runs it.
func TestDistBuildSignAndUse(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries from nothing with the pinned toolchain; the CI job builder runs it")
	}
	host := goEnv(t, "GOMODCACHE")
	if _, err := os.Stat(filepath.Join(host, "cache", "download", builder.Toolchain, "@v", builder.ToolchainVersion+".zip")); err != nil {
		t.Skipf("the module cache holds no %s@%s: go mod download it to run this case (the CI job builder does)", builder.Toolchain, builder.ToolchainVersion)
	}
	ctx, repo := context.Background(), abs(t, filepath.Join("..", ".."))
	src, cache, scratch := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("GOPROXY", "")
	if err := builder.Serve(repo, testVersion, src); err != nil {
		t.Fatal(err)
	}
	fill(t, repo, filepath.Join(host, "cache", "download"), src)
	echo := plugin(t, src, scratch)
	rogue := roguePlugin(t, src, echo, filepath.Join(host, "github.com", "google", "licenseclassifier", "v2@v2.0.0", "assets", "License", "GPL-2.0", "license.txt"))

	input, err := os.ReadFile(filepath.Join(repo, "dist", "dist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	rel, err := builder.Build(ctx, builder.Request{Input: input, Root: testVersion, ModCache: src, Cache: filepath.Join(cache, "aiopsctl"),
		Out: filepath.Join(t.TempDir(), "release"), Progress: &progress})
	if err != nil {
		t.Fatalf("the release, its first phase: %v\n%s", err, progress.String())
	}
	// The release build checks each module zip against the go.sum of the root module and
	// of the SDK modules it serves, the files their author resolved with, so every hash
	// of its pins is recorded; a zip of the module source that differs fails it (ADR-044
	// decision 8).
	if len(rel.Unchecked) > 0 {
		t.Errorf("the release build checked against nothing: %q", rel.Unchecked)
	}
	tampered := t.TempDir()
	linkTree(t, src, tampered)
	tamper(t, filepath.Join(tampered, "go.yaml.in", "yaml", "v3", "@v", "v3.0.6.zip"), "go.yaml.in/yaml/v3@v3.0.6/yaml.go")
	if _, err := builder.Build(ctx, builder.Request{Input: input, Root: testVersion, ModCache: tampered, Cache: filepath.Join(cache, "aiopsctl"),
		Out: filepath.Join(t.TempDir(), "release")}); err == nil || !strings.Contains(err.Error(), "go.yaml.in/yaml/v3@v3.0.6: checksum mismatch") {
		t.Errorf("a release build on a module zip that differs from the go.sum of the root module: %v", err)
	}

	// The release artifact that dist.yaml names, by digest, in a registry: the referrer
	// that holds the release's signed manifest (ADR-044 decision 1; ADR-060 decision 17).
	reg := ocitest.New(t, ocitest.Options{})
	releaseRef := releaseArtifact(reg, rel.Manifest, nil)
	site, home := t.TempDir(), filepath.Join(t.TempDir(), "home")
	env := map[string]string{"PWD": site, "AIOPS_KEY_PASSWORD": "", "AIOPS_HOME": home}
	var allow []string
	build := func(dir string, p modulePin) (launch.Built, string, error) {
		yaml := fmt.Sprintf("release: %s\nplugins:\n  - {module: %s, version: v0.1.0, sum: %s}\n", releaseRef, p.path, p.sum)
		if err := os.WriteFile(filepath.Join(dir, "dist.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		var progress bytes.Buffer
		tools := authoring.New()
		tools.Signed, tools.Progress = rel.Manifest, &progress
		b, err := tools.Build(ctx, launch.BuildRequest{DistYAML: filepath.Join(dir, "dist.yaml"), ModCache: src, Builder: "local", AllowBump: allow})
		return b, progress.String(), err
	}
	if _, _, err := build(t.TempDir(), modulePin{echo.path, h1Of("another zip")}); err == nil || !strings.Contains(err.Error(), "dist.yaml lists") {
		t.Errorf("a zip whose sum differs: %v", err)
	}
	// On the local path, a release whose signed manifest is not, by digest, the one this
	// aiopsctl embeds is refused, naming the release to use (ADR-044 decisions 1 and 14).
	releaseRef = releaseArtifact(reg, []byte("distVersion: 1\n# another release's manifest\n"), nil)
	if _, _, err := build(t.TempDir(), echo); err == nil || !strings.Contains(err.Error(), "name the release artifact of "+testVersion) {
		t.Errorf("a release whose manifest this aiopsctl does not embed: %v", err)
	}
	releaseRef = releaseArtifact(reg, rel.Manifest, nil)
	_, _, err = build(t.TempDir(), rogue)
	for _, want := range []string{"declares the id aiops/rogue", "a fake or sdk/testkit package", "init deny-list: a default-mux handler: net/http.HandleFunc",
		"example.com/acme/rogue is under GPL-2.0-only"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the rogue module: the refusal lacks %q: %v", want, err)
		}
	}
	// A raise that --allow-bump admits passes only when every check passes on the raised
	// build list, the suites of the first-party plugins among them, so that the
	// first-party rows run their suites against what the binary links; without the flag
	// the raise fails (ADR-044 decision 10).
	bump := bumpPlugin(t, src, echo, "golang.org/x/mod", "v0.42.0", "v0.42.1")
	if _, _, err := build(t.TempDir(), bump); err == nil || !strings.Contains(err.Error(), "--allow-bump golang.org/x/mod admits the raise") {
		t.Errorf("a raise without --allow-bump: %v", err)
	}
	allow = []string{"golang.org/x/mod"}
	_, printed, err := build(t.TempDir(), bump)
	allow = nil
	for _, want := range []string{"bump: golang.org/x/mod, pinned by the release at v0.42.0, is v0.42.1 in this build, required by ", "example.com/acme/bump@v0.1.0",
		"suites: go test example.com/acme/bump/...", "suites: go test github.com/mutex-solutions/aiops-platform/plugins/",
		"aiops-platform/plugins/clock ", "aiops-platform/plugins/operations ", "on the raised build list (ADR-044 decision 10)"} {
		if err != nil || !strings.Contains(printed, want) {
			t.Errorf("a raise with --allow-bump: %v, the progress lacks %q:\n%s", err, want, printed)
		}
	}

	built, printed, err := build(site, echo)
	out := filepath.Join(site, "build")
	if err != nil || built.Image != filepath.Join(out, "image") || built.Aiopsctl != filepath.Join(out, "aiopsctl", "aiopsctl") {
		t.Fatalf("dist build: %+v, %v\n%s", built, err, printed)
	}
	manifest, err := os.ReadFile(filepath.Join(out, "distribution.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := boot.ReadDistribution("distribution.yaml", manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Plugins["acme/echo"]; !ok || len(d.Serves.Definitions["echoers"]) == 0 {
		t.Errorf("the signed manifest lacks acme/echo or echoers: %v", d.Serves.Definitions)
	}
	// One build of the same inputs writes the same bytes, SBOMs and image layout included,
	// so the image that dist sign signs can be built again to its digest (ADR-044
	// decision 3).
	again := t.TempDir()
	if _, printed, err := build(again, echo); err != nil {
		t.Fatalf("dist build again: %v\n%s", err, printed)
	}
	a, b := tree(t, out), tree(t, filepath.Join(again, "build"))
	for name, sum := range a {
		if b[name] != sum {
			t.Errorf("%s differs between two builds of the same inputs", name)
		}
	}
	if len(a) != len(b) {
		t.Errorf("two builds of the same inputs wrote %d and %d files", len(a), len(b))
	}
	for _, f := range []string{"aiops/aiops", "aiops/NOTICE", "aiops/aiops.spdx.json", "aiops/THIRD_PARTY_LICENSES/golang.org/toolchain/LICENSE",
		"aiops/THIRD_PARTY_LICENSES/golang.org/toolchain/src/runtime/memmove_amd64.s.NOTICE", "aiops/THIRD_PARTY_LICENSES/example.com/acme/echo/LICENSE",
		"aiopsctl/aiopsctl", "aiopsctl/THIRD_PARTY_LICENSES/github.com/google/licenseclassifier/v2/LICENSE", "aiopsctl/aiopsctl.spdx.json", "image/index.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
			t.Errorf("dist build wrote no %s: %v", f, err)
		}
	}

	for _, args := range [][]string{{"key", "generate", "site"}, {"trust", "add", "site.pub", "--distribution"},
		{"dist", "sign", filepath.Join(out, "image"), "--key", "site.key"}, {"dist", "use", filepath.Join(out, "image")}} {
		if r := do(t, os.DirFS("/"), env, args...); r.code != 0 {
			t.Fatalf("%q: exit %d\n%s", args, r.code, r.errOut)
		}
	}
	current, err := os.ReadFile(filepath.Join(home, "verified", "current"))
	if err != nil || strings.TrimSpace(string(current)) != d.Digest {
		t.Errorf("verified/current %q, the manifest %s: %v", current, d.Digest, err)
	}
	cmd := exec.Command(filepath.Join(out, "aiopsctl", "aiopsctl"), "profile", "verify")
	cmd.Dir, cmd.Env = site, []string{"AIOPS_HOME=" + home, "HOME=" + site, "PATH=" + os.Getenv("PATH")}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the site's aiopsctl on the record dist use wrote: %v\n%s", err, b)
	}
}

func goEnv(t *testing.T, name string) string {
	t.Helper()
	b, err := exec.Command("go", "env", name).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func abs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// fill copies into the module source src, from the download cache of the Go module
// cache, what a site that builds on the release fills its module cache with (ADR-044
// decision 9): the .info and .mod of every module version of the repository's module
// graph, since go list -m all of a build reads them for whichever version that build
// selects; the zip of every module of the build list; and the toolchain module, which
// the case found there. It fetches the others into the module cache first, from the
// module proxy when the cache is cold, and fails, naming them, when it still lacks any.
func fill(t *testing.T, repo, download, src string) {
	t.Helper()
	repoGo := func(args ...string) []string {
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Env = repo, append(os.Environ(), "GOFLAGS=-mod=readonly")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var mvs []string
		for _, f := range strings.Fields(string(out)) {
			if p, _, ok := strings.Cut(f, "@"); ok && strings.Contains(p, ".") && !strings.HasPrefix(p, "github.com/mutex-solutions/aiops-platform") {
				mvs = append(mvs, f)
			}
		}
		slices.Sort(mvs)
		return slices.Compact(mvs)
	}
	graph, list := repoGo("mod", "graph"), repoGo("list", "-m", "-f", "{{.Path}}@{{.Version}}", "all")
	// go mod download writes the three files of each module of the build list, and go
	// list -m the .info and .mod of each module version of the graph; both run outside
	// the module, so that the repository's go.sum is never written.
	for _, args := range [][]string{append([]string{"mod", "download"}, list...), append([]string{"list", "-m"}, graph...)} {
		cmd := exec.Command("go", args...)
		cmd.Dir = t.TempDir()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("go %s fails (%v): the case needs the modules of the build list in the module cache or a module proxy that serves them; the CI jobs builder and builder-image have one\n%s", args[0]+" "+args[1], err, out)
		}
	}
	type file struct{ mv, ext string }
	want := map[file]bool{}
	for _, ext := range []string{".info", ".mod", ".zip"} {
		want[file{builder.Toolchain + "@" + builder.ToolchainVersion, ext}] = true
	}
	for _, mv := range graph {
		want[file{mv, ".info"}], want[file{mv, ".mod"}] = true, true
	}
	for _, mv := range list {
		want[file{mv, ".zip"}] = true
	}
	var missing []string
	for f := range want {
		path, version, _ := strings.Cut(f.mv, "@")
		esc, err := module.EscapePath(path)
		from, to := filepath.Join(download, esc, "@v", version+f.ext), filepath.Join(src, esc, "@v", version+f.ext)
		if _, serr := os.Stat(from); serr != nil {
			missing = append(missing, f.mv+f.ext)
			continue
		}
		if err := errors.Join(err, os.MkdirAll(filepath.Dir(to), 0o755), link(from, to)); err != nil {
			t.Fatal(err)
		}
	}
	if slices.Sort(missing); len(missing) > 0 {
		t.Fatalf("the module cache lacks %d files of the module source: %s", len(missing), strings.Join(missing, ", "))
	}
}

// releaseArtifact stores in reg the release artifact of the signed manifest, the
// distribution referrer with the manifest as its layer and notes among its annotations,
// and returns its reference by digest.
func releaseArtifact(reg *ocitest.Registry, manifest []byte, notes map[string]string) string {
	const name = "aiops/release"
	layer := reg.Blob(name, "application/vnd.aiops.distribution.manifest.v1+yaml", manifest)
	m, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ocitest.ManifestType, "artifactType": "application/vnd.aiops.distribution.v1",
		"config": reg.Blob(name, ocitest.EmptyType, []byte("{}")), "layers": []ocitest.Descriptor{layer}, "annotations": notes})
	return reg.Host + "/" + name + "@" + reg.Manifest(name, "", ocitest.ManifestType, m).Digest
}

// linkTree links every file under from into to, at the same path.
func linkTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, e os.DirEntry, err error) error {
		rel, _ := filepath.Rel(from, p)
		switch {
		case err != nil:
			return err
		case e.IsDir():
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		return link(p, filepath.Join(to, rel))
	})
	if err != nil {
		t.Fatal(err)
	}
}

// tamper writes the module zip file again, as a new file, with a line added to its
// entry name.
func tamper(t *testing.T, file, name string) {
	t.Helper()
	z, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, f := range z.File {
		r, err := f.Open()
		var data []byte
		if err == nil {
			data, err = io.ReadAll(r)
		}
		var w io.Writer
		if err == nil {
			w, err = zw.Create(f.Name)
		}
		if f.Name == name {
			data = append(data, "\n// A line the release's go.sum does not record.\n"...)
		}
		if err == nil {
			_, err = w.Write(data)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(zw.Close(), os.Remove(file), os.WriteFile(file, b.Bytes(), 0o644)); err != nil {
		t.Fatal(err)
	}
}

func link(from, to string) error {
	if os.Link(from, to) == nil {
		return nil
	}
	r, err := os.Open(from)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return errors.Join(err, w.Close())
}

type modulePin struct{ path, sum string }

// plugin writes the module example.com/acme/echo at v0.1.0 into the module source src:
// the echoers template of plugin new for acme/echo, its stub written as an author
// fills it in, its go.sum resolved through src and its generated half written, as plugin
// check --write writes it. It works in scratch with a module cache of its own.
func plugin(t *testing.T, src, scratch string) modulePin {
	t.Helper()
	data, err := templates.NewData("acme/echo", testVersion, false)
	if err != nil {
		t.Fatal(err)
	}
	files, err := templates.Render("echoers", data)
	if err != nil {
		t.Fatal(err)
	}
	files["internal/echo/echo.go"] = []byte(`// Package echo is the echoer of acme/echo.
package echo

import (
	"context"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Echoer answers messages for the activation of the plugin that registered it.
type Echoer struct{ c *kernel.Context }

// New returns the echoer of the activation c.
func New(c *kernel.Context) Echoer { return Echoer{c: c} }

// Echo returns "echo: " and msg while the activation is live.
func (e Echoer) Echo(ctx context.Context, msg string) (string, error) {
	leave, ok := e.c.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "echo: " + msg, nil
}
`)
	dir := filepath.Join(scratch, "echo")
	written := map[string]string{}
	for p, b := range files {
		written[p] = string(b)
	}
	if err := writeFiles(dir, written); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"GOPROXY": "file://" + filepath.ToSlash(src), "GOSUMDB": "off", "GOFLAGS": "-mod=mod -modcacherw", "GOWORK": "off",
		"GOMODCACHE": filepath.Join(scratch, "mod"), "GONOSUMDB": "", "GOPRIVATE": "", "GOTOOLCHAIN": "local"} {
		t.Setenv(k, v)
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy of the plugin: %v\n%s", err, b)
	}
	if err := plugingen.Write(dir); err != nil {
		t.Fatal(err)
	}
	return zipModule(t, src, data.Module, dir)
}

// bumpPlugin writes into src the module path at the version raised, a copy of its zip at
// version with another version, and example.com/acme/bump at v0.1.0: the echoer under
// the id acme/bump, whose go.mod requires path at raised.
func bumpPlugin(t *testing.T, src string, echo modulePin, path, version, raised string) modulePin {
	t.Helper()
	esc, _ := module.EscapePath(path)
	at := filepath.Join(src, esc, "@v")
	z, err := zip.OpenReader(filepath.Join(at, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, f := range z.File {
		r, err := f.Open()
		var w io.Writer
		if err == nil {
			w, err = zw.Create(strings.Replace(f.Name, "@"+version+"/", "@"+raised+"/", 1))
		}
		if err == nil {
			_, err = io.Copy(w, r)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	gomod, err := os.ReadFile(filepath.Join(at, version+".mod"))
	info, _ := json.Marshal(map[string]string{"Version": raised})
	if err = errors.Join(err, zw.Close(), os.WriteFile(filepath.Join(at, raised+".zip"), b.Bytes(), 0o644), os.WriteFile(filepath.Join(at, raised+".mod"), gomod, 0o644),
		os.WriteFile(filepath.Join(at, raised+".info"), info, 0o644)); err != nil {
		t.Fatal(err)
	}
	eesc, _ := module.EscapePath(echo.path)
	dir := t.TempDir()
	if err := modzip.Unzip(dir, module.Version{Path: echo.path, Version: "v0.1.0"}, filepath.Join(src, eesc, "@v", "v0.1.0.zip")); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		files[rel] = strings.ReplaceAll(strings.ReplaceAll(string(b), "example.com/acme/echo", "example.com/acme/bump"), "acme/echo", "acme/bump")
		return errors.Join(err, os.Chmod(p, 0o644))
	})
	files["go.mod"] += "\nrequire " + path + " " + raised + "\n"
	if err == nil {
		err = writeFiles(dir, files)
	}
	if err != nil {
		t.Fatal(err)
	}
	return zipModule(t, src, "example.com/acme/bump", dir)
}

// roguePlugin writes example.com/acme/rogue at v0.1.0 into src: the echoer under the
// id aiops/rogue, which links a fake package, registers a handler on the default mux
// from init and carries GPL-2.0, whose text it takes from gpl.
func roguePlugin(t *testing.T, src string, echo modulePin, gpl string) modulePin {
	t.Helper()
	esc, _ := module.EscapePath(echo.path)
	dir := t.TempDir()
	if err := modzip.Unzip(dir, module.Version{Path: echo.path, Version: "v0.1.0"}, filepath.Join(src, esc, "@v", "v0.1.0.zip")); err != nil {
		t.Fatal(err)
	}
	licence, err := os.ReadFile(gpl)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"LICENSE": string(licence), "rogue.go": "package echo\n\nimport (\n\t\"net/http\"\n\n\t_ \"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/fake\"\n)\n\n" +
		"func init() { http.HandleFunc(\"/rogue\", func(http.ResponseWriter, *http.Request) {}) }\n"}
	err = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		if _, set := files[rel]; !set && err == nil {
			s := strings.ReplaceAll(string(b), "example.com/acme/echo", "example.com/acme/rogue")
			files[rel] = strings.NewReplacer("acme/echo", "aiops/rogue", "publisher: {id: acme}", "publisher: {id: aiops}").Replace(s)
		}
		return errors.Join(err, os.Chmod(p, 0o644))
	})
	if err == nil {
		err = writeFiles(dir, files)
	}
	if err != nil {
		t.Fatal(err)
	}
	return zipModule(t, src, "example.com/acme/rogue", dir)
}

// zipModule writes the module path at v0.1.0 from dir into src and returns its sum.
func zipModule(t *testing.T, src, path, dir string) modulePin {
	t.Helper()
	esc, _ := module.EscapePath(path)
	at := filepath.Join(src, esc, "@v")
	if err := os.MkdirAll(at, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(at, "v0.1.0.zip"))
	if err != nil {
		t.Fatal(err)
	}
	err = errors.Join(modzip.CreateFromDir(f, module.Version{Path: path, Version: "v0.1.0"}, dir), f.Close())
	gomod, rerr := os.ReadFile(filepath.Join(dir, "go.mod"))
	info, _ := json.Marshal(map[string]string{"Version": "v0.1.0"})
	if err = errors.Join(err, rerr, os.WriteFile(filepath.Join(at, "v0.1.0.mod"), gomod, 0o644), os.WriteFile(filepath.Join(at, "v0.1.0.info"), info, 0o644)); err != nil {
		t.Fatal(err)
	}
	sum, err := dirhash.HashZip(filepath.Join(at, "v0.1.0.zip"), dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	return modulePin{path, sum}
}

func h1Of(s string) string {
	sum, _ := dirhash.Hash1([]string{"f"}, func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(s)), nil })
	return sum
}
