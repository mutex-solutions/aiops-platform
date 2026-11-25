// SPDX-License-Identifier: Apache-2.0

// Package builder is the builder of ADR-044: aiopsctl dist build on the local path,
// and the signed distribution manifest of every binary built in the repository. It is
// a root-module package compiled into aiopsctl only, through the toolbox; the closure
// rule of internal/vet keeps it, and what it links, out of every server binary (ADR-060
// decision 22; ADR-061 decision 21; ADR-044, Consequences).
//
// A build generates, in a scratch directory, a main module that requires each module
// of the build at its version, with no replace line, its go.sum written from recorded
// hashes, and the module table of the distribution; it runs the pinned toolchain
// module with an environment of its own (ADR-044 decision 2); it refuses what ADR-044
// and ADR-060 decision 22 refuse; and it writes the signed distribution manifest, the
// server binary and its image, the site's aiopsctl, and beside each binary its
// THIRD_PARTY_LICENSES/, NOTICE and SBOM.
package builder

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing/fstest"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	"github.com/mutex-solutions/aiops-platform/internal/vet"
)

// Request is one build. A site build names its dist.yaml and the release's signed
// manifest, and links in one phase. A release build names the release input in the
// form of dist/dist.yaml and the version of the root module, and runs in two phases
// (ADR-020 decision 13): the first writes the manifest and stops; the second, given it
// back as Signed, links the release binaries with it. A build of plugin smoke names, for
// dist.yaml, the plugin directory and the release artifact by digest.
type Request struct {
	Signed    *Signed           // a release build's second phase
	DistYAML  string            // the site's dist.yaml
	Verify    bool              // plugin verify of in-process code: step 5 on the plugins of dist.yaml, as smoke runs it
	Smoke     string            // plugin smoke: the plugin directory, a module of the site
	Against   string            // plugin smoke: the release artifact, as dist.yaml names it
	Release   []byte            // the release's signed manifest: the one this aiopsctl embeds
	Input     []byte            // a release build: the release input
	Root      string            // a release build: the version of the root module
	ModCache  string            // the module source, in the download layout of the module proxy protocol
	Mirrors   map[string]string // --mirror
	AllowBump []string          // --allow-bump
	Proxy     string            // the caller's GOPROXY
	Out       string            // the output directory, which does not exist or is empty
	Cache     string            // the builder's own cache, which holds the toolchain
	Progress  io.Writer
}

// Signed is the second phase of a release build (ADR-020 decision 13): the manifest
// that the first phase wrote, the bundle that the release pipeline signed it into with
// cosign sign-blob, the release public key, PEM, and the definition of the builder
// image, dist/builder/image.yaml. The release server and the release aiopsctl, for
// linux/amd64, embed the manifest, the bundle and the key, with the manifest as their
// built-in record; the layout holds the builder image too, and the release artifact.
// The bundle is verified by the release pipeline before it publishes, with cosign
// verify-blob, and by dist use of the release image; boot verifies none (08:404).
type Signed struct{ Manifest, Bundle, Key, Definition []byte }

// Result is what a build wrote: the signed manifest, and for a site build the paths of
// the server binary, its OCI image layout and the site's aiopsctl; the hashes that no
// source records, each checked against nothing, and the versions raised by
// --allow-bump.
type Result struct {
	Manifest               []byte
	Aiops, Image, Aiopsctl string
	Unchecked, Bumps       []string
}

// sdkModules are the modules the release pipeline builds, which --allow-bump never
// names (ADR-044 decision 20).
var sdkModules = []string{rootModule, rootModule + "/sdk", rootModule + "/sdk/kernel", rootModule + "/sdk/testkit"}

// scratchModule is the path of the scratch main module: under the root module path, so
// that it may import the launcher packages under internal/ (cmd/aiops, cmd/aiopsctl).
const scratchModule = rootModule + "/site"

type build struct {
	q        Request
	release  *boot.Distribution
	site     Site
	input    boot.DistManifest
	g        Go
	goroot   string
	keyID    string // the release key's id, for a release's second phase
	scratch  string
	recorded map[string]string // go.sum line key ("<path> <version>[/go.mod]") to hash
	served   map[string]string // a release build: the go.sum lines of the modules it serves
	base     []tablePlugin     // a site build: the release's module table
	plugins  []tablePlugin     // what the build builds: the site's plugins, or the release's table
	progress io.Writer
}

// tablePlugin is a plugin of the module table: package, accessor and manifest.
type tablePlugin struct {
	pkg, accessor string
	manifest      []byte
}

// Build runs one build on the local path.
func Build(ctx context.Context, q Request) (Result, error) {
	b := &build{q: q, recorded: map[string]string{}, progress: cmp.Or[io.Writer](q.Progress, io.Discard)}
	scratch, err := os.MkdirTemp("", "aiopsctl-build-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(scratch)
	b.scratch = scratch
	if err := b.inputs(ctx); err != nil {
		return Result{}, err
	}
	if b.goroot, err = Tree(q.ModCache, q.Cache, b.toolchainSum()); err != nil {
		return Result{}, err
	}
	b.g = Go{Bin: filepath.Join(b.goroot, "bin", "go"), Dir: scratch, Env: b.env(b.goroot)}
	if err := b.module(ctx); err != nil {
		return Result{}, err
	}
	unchecked, err := b.resolve(ctx)
	if err != nil {
		return Result{}, err
	}
	pkgs, err := b.graph(ctx, "./cmd/aiops")
	if err != nil {
		return Result{}, err
	}
	list, err := b.buildList(ctx)
	if err != nil {
		return Result{}, err
	}
	pins, bumps, err := b.versions(ctx, list, unchecked)
	res := Result{Unchecked: unchecked, Bumps: bumps}
	for _, u := range unchecked {
		fmt.Fprintf(b.progress, "unchecked: %s, which no source records, as the module source serves it (ADR-044 decision 8)\n", u)
	}
	for _, line := range bumps {
		fmt.Fprintln(b.progress, "bump: "+line)
	}
	l, lerr := ScanLicences(pkgs)
	findings, ferr := ScanInits(ctx, scratch, b.g.Env, "./cmd/aiops")
	for _, f := range findings {
		ferr = errors.Join(ferr, fmt.Errorf("init deny-list: %s (ADR-044 decision 11)", f))
	}
	m := Manifest{Graph: pkgs, Licences: l.Modules, Pins: pins, Release: b.input, Site: b.site.Bindings, Accept: b.site.AcceptLicenses}
	var merr, aerr error
	if q.Smoke != "" || q.Verify {
		m.Attested, aerr = b.attest(ctx)
	}
	res.Manifest, merr = m.Write()
	if err = errors.Join(err, lerr, ferr, aerr, CheckLicences(l, b.site.AcceptLicenses), merr, fakes(pkgs)); err != nil {
		return res, err
	}
	switch {
	case b.release == nil && q.Signed == nil: // the first phase of a release stops at the manifest
		return res, errors.Join(b.gate(res.Manifest), b.source(ctx, false))
	case b.release == nil && !bytes.Equal(res.Manifest, q.Signed.Manifest):
		return res, errors.New("the second phase writes another manifest than the first phase wrote: the inputs changed between the phases (ADR-020 decision 13)")
	case b.release != nil:
		if err := errors.Join(b.gate(res.Manifest), b.source(ctx, len(bumps) > 0)); err != nil {
			return res, err
		}
	}
	return b.link(ctx, res, pkgs, l)
}

// inputs reads the request: the site's dist.yaml with the release's signed manifest,
// whose release it holds to the one this aiopsctl embeds, or the release input; and
// refuses what the local path cannot take.
func (b *build) inputs(ctx context.Context) error {
	q := b.q
	switch {
	case q.ModCache == "":
		return errors.New("dist build needs --modcache: every module source is a directory until L14 clears, and no default proxy is asked (ADR-044 decision 7)")
	case strings.Contains(strings.ReplaceAll(q.Proxy, "file://", ""), "://"): // in any element of the list
		return fmt.Errorf("GOPROXY names %s: until L14 clears, every module source of the builder is a file:// directory and dist build refuses a proxy URL (ADR-044 decision 7; ADR-060 decision 21)", q.Proxy)
	case !filepath.IsAbs(q.Out) || !filepath.IsAbs(q.ModCache) || !filepath.IsAbs(q.Cache):
		return errors.New("the output directory, the module cache and the builder's cache are absolute paths")
	}
	if entries, err := os.ReadDir(q.Out); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists and is not empty", q.Out)
	}
	if q.DistYAML == "" && q.Smoke == "" {
		var err error
		switch b.input, err = boot.ParseDist("the release input", q.Input); {
		case err != nil || !semver.IsValid(q.Root):
			return errors.Join(err, fmt.Errorf("a release build names the version of the root module, not %q", q.Root))
		case len(b.input.Modules) == 0:
			return errors.New("the release input lists no module: a release names its module table (ADR-060 decision 20)")
		}
		var errs []error
		for _, m := range sdkModules {
			errs = append(errs, b.goSum(module.Version{Path: m, Version: q.Root}))
		}
		b.served, b.recorded = b.recorded, map[string]string{} // checked as go writes them (resolve)
		if s := q.Signed; s != nil {
			var bundle struct{ MediaType string }
			var kerr, berr error
			b.keyID, kerr = keyID(s.Key)
			_, derr := readDefinition(s.Definition)
			_, merr := boot.ReadDistribution("the manifest of the first phase", s.Manifest)
			if json.Unmarshal(s.Bundle, &bundle) != nil || bundle.MediaType != bundleType {
				berr = fmt.Errorf("the bundle of the manifest is not a Sigstore bundle of media type %s (ADR-020 decision 2)", bundleType)
			}
			errs = append(errs, kerr, derr, merr, berr)
		}
		return errors.Join(errs...)
	}
	var err error
	if q.Smoke != "" {
		b.site, err = b.smoke()
	} else {
		b.site, err = readSite(q.DistYAML)
	}
	if err != nil {
		return err
	}
	d, err := boot.ReadDistribution("the release's signed manifest", q.Release)
	if err != nil {
		return err
	}
	b.release, b.input = &d, d.Release
	if v := b.releaseVersion(rootModule); v == "" {
		return errors.New("the release's signed manifest names the root module at no version: it was built in the repository, not from served modules, and a site builds only on a release (ADR-060 decision 21)")
	}
	for _, p := range d.Pins {
		if p.Path == Toolchain {
			continue
		}
		if p.Sum != "" {
			b.recorded[p.Path+" "+p.Version] = p.Sum
		}
		if p.GoMod != "" {
			b.recorded[p.Path+" "+p.Version+"/go.mod"] = p.GoMod
		}
	}
	return b.sameRelease(ctx)
}

// releaseVersion returns the version at which the release's signed manifest records
// the module path, "" for none.
func (b *build) releaseVersion(path string) string {
	for _, m := range b.release.Modules {
		if m.Path == path && m.Replace == nil {
			return m.Version
		}
	}
	return ""
}

// env is the Go environment the builder sets on both paths (ADR-044 decision 2; ADR-060
// decision 21): none of the caller's.
func (b *build) env(goroot string) []string {
	s, proxy := b.scratch, "file://"+filepath.ToSlash(b.q.ModCache)
	if b.q.Smoke != "" {
		proxy = "file://" + filepath.ToSlash(filepath.Join(s, "source")) + "," + proxy // the source of smoke first (ADR-044 decision 7)
	}
	return []string{"PATH=" + filepath.Join(goroot, "bin"), "HOME=" + filepath.Join(s, "home"), "GOROOT=" + goroot,
		"GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly -modcacherw", "GOSUMDB=off", "GOPROXY=" + proxy,
		"GOENV=off", "GOWORK=off", "CGO_ENABLED=0", "GOTELEMETRY=off",
		"GOCACHE=" + filepath.Join(s, "cache"), "GOMODCACHE=" + filepath.Join(s, "mod"), "TMPDIR=" + filepath.Join(s, "tmp")}
}

// module writes the scratch main module: go.mod, the go.sum lines that the sources
// record, and the module table with the two main packages.
func (b *build) module(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, d := range []string{"home", "tmp", "site/manifests", "cmd/aiops", "cmd/aiopsctl/suites"} {
		if err := os.MkdirAll(filepath.Join(b.scratch, d), 0o755); err != nil {
			return err
		}
	}
	if err := b.table(); err != nil {
		return err
	}
	f := &modfile.File{}
	_ = f.AddModuleStmt(scratchModule)
	_ = f.AddGoStmt("1.26.0")
	if b.release == nil {
		_ = f.AddRequire(rootModule, b.q.Root)
	} else {
		for _, p := range b.release.Pins {
			if p.Sum != "" && p.Path != Toolchain {
				_ = f.AddRequire(p.Path, p.Version)
			}
		}
	}
	for _, p := range b.site.Plugins {
		if err := b.plugin(p); err != nil {
			return err
		}
		_ = f.AddRequire(p.Path, p.Version)
	}
	f.Cleanup()
	gomod, err := f.Format()
	if err != nil {
		return err
	}
	var sum bytes.Buffer
	for _, k := range sortedKeys(b.recorded) {
		fmt.Fprintf(&sum, "%s %s\n", k, b.recorded[k])
	}
	var key, bundle []byte
	if s := b.q.Signed; s != nil {
		key, bundle = s.Key, s.Bundle
	}
	table := slices.Concat(b.base, b.plugins)
	files := map[string][]byte{"go.mod": gomod, "go.sum": sum.Bytes(), "site/distribution.yaml": nil, "LICENSE": apache(),
		"site/release.pub": key, "site/distribution.bundle": bundle,
		"site/site.go": siteGo(table, b.keyID), "cmd/aiops/main.go": []byte(aiopsMain), "cmd/aiopsctl/main.go": []byte(aiopsctlMain),
		"cmd/aiopsctl/suites/README": suitesReadme}
	for i, p := range table {
		files[fmt.Sprintf("site/manifests/%d.yaml", i)] = p.manifest
	}
	if b.release == nil { // a release's aiopsctl embeds the suite binaries (ADR-060 decision 46)
		suites, err := b.suites()
		if err != nil {
			return err
		}
		maps.Copy(files, suites)
	}
	if b.q.Smoke != "" || b.q.Verify { // step 5 of plugin check on the site's plugins
		tests, err := b.smokeTests()
		if err != nil {
			return err
		}
		maps.Copy(files, tests)
	}
	for name, data := range files {
		p := filepath.Join(b.scratch, filepath.FromSlash(name))
		if err := errors.Join(os.MkdirAll(filepath.Dir(p), 0o755), os.WriteFile(p, data, 0o644)); err != nil {
			return err
		}
	}
	return nil
}

// goSum records the lines of the go.sum inside the module zip of m in the module
// source, if it holds one: the file its author resolved with (ADR-044 decision 8).
func (b *build) goSum(m module.Version) error {
	z, err := zip.OpenReader(b.zip(m))
	if err != nil {
		return fmt.Errorf("module %s %s: %w", m.Path, m.Version, err)
	}
	defer z.Close()
	i := slices.IndexFunc(z.File, func(f *zip.File) bool { return f.Name == m.String()+"/go.sum" })
	if i < 0 {
		return nil
	}
	data, err := readZip(z.File[i])
	for _, l := range strings.Split(string(data), "\n") {
		if fs := strings.Fields(l); len(fs) == 3 && err == nil {
			err = b.record(fs[0]+" "+fs[1], fs[2], "the go.sum of "+m.Path)
		}
	}
	return err
}

// plugin reads the module zip of a plugin of dist.yaml from the module source, checked
// against the sum dist.yaml lists: it records the sum and the lines of the zip's go.sum,
// and adds each package of the module that holds a plugin.yaml to the module table. Two
// sources that record different hashes for one version fail the build (ADR-044
// decision 8).
func (b *build) plugin(p module.Version) error {
	file := b.zip(p)
	got, err := dirhash.HashZip(file, dirhash.Hash1)
	if err != nil {
		return fmt.Errorf("plugin module %s %s: %w", p.Path, p.Version, err)
	}
	if want := b.site.Sums[p.Path]; got != want {
		return fmt.Errorf("the module source serves %s %s with the sum %s, and dist.yaml lists %s (ADR-044 decision 8)", p.Path, p.Version, got, want)
	}
	if err := errors.Join(b.record(p.Path+" "+p.Version, got, "dist.yaml"), b.goSum(p)); err != nil {
		return err
	}
	ps, err := tablePackages(file, p.Path, p.Version)
	b.plugins = append(b.plugins, ps...)
	return err
}

// tablePackages returns each package of the module zip file of path at version that holds a
// plugin.yaml outside testdata/, with its accessor and its manifest.
func tablePackages(file, path, version string) ([]tablePlugin, error) {
	z, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	prefix := path + "@" + version + "/"
	dirs := map[string][]*zip.File{}
	for _, f := range z.File {
		rel := strings.TrimPrefix(f.Name, prefix)
		dirs[filepath.ToSlash(filepath.Dir(rel))] = append(dirs[filepath.ToSlash(filepath.Dir(rel))], f)
	}
	var out []tablePlugin
	for _, dir := range sortedKeys(dirs) {
		var manifest []byte
		accessor := ""
		for _, f := range dirs[dir] { // a plugin.yaml and the Go files, the rest unread
			name := filepath.Base(f.Name)
			if name != "plugin.yaml" && (!strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go")) {
				continue
			}
			switch data, err := readZip(f); {
			case err != nil:
				return nil, err
			case name == "plugin.yaml":
				manifest = data
			default:
				if f, err := parser.ParseFile(token.NewFileSet(), "", data, parser.SkipObjectResolution); err == nil {
					accessor = plugingen.Accessor(accessor, f)
				}
			}
		}
		if manifest == nil || strings.Contains("/"+dir+"/", "/testdata/") {
			continue
		}
		if accessor == "" {
			return nil, fmt.Errorf("%s/%s holds a plugin.yaml and declares neither var Plugin nor func New()", path, dir)
		}
		pkg := path
		if dir != "." {
			pkg += "/" + dir
		}
		out = append(out, tablePlugin{pkg: pkg, accessor: accessor, manifest: manifest})
	}
	return out, nil
}

// table reads the release's module table from the zip of the root module (ADR-060
// decision 20): what a release builds, the packages its input lists, or the start of a
// site's, those its release records. ponytail: contrib modules (S6a) add their zips.
func (b *build) table() error {
	pkgs, version, out := b.input.Modules, b.q.Root, &b.plugins
	if b.release != nil {
		pkgs, version, out = nil, b.releaseVersion(rootModule), &b.base
		for _, p := range b.release.Plugins {
			pkgs = append(pkgs, p.Package)
		}
	}
	all, err := tablePackages(b.zip(module.Version{Path: rootModule, Version: version}), rootModule, version)
	*out = slices.DeleteFunc(all, func(p tablePlugin) bool { return !slices.Contains(pkgs, p.pkg) })
	for _, pkg := range pkgs {
		if !slices.ContainsFunc(*out, func(p tablePlugin) bool { return p.pkg == pkg }) && err == nil {
			err = fmt.Errorf("the module table lists %s, which declares no plugin in %s %s", pkg, rootModule, version)
		}
	}
	return err
}

// zip returns the file of the module zip of m in the module source, in that of plugin
// smoke first.
func (b *build) zip(m module.Version) string {
	esc, _ := module.EscapePath(m.Path)
	if p := filepath.Join(b.scratch, "source", esc, "@v", m.Version+".zip"); b.q.Smoke != "" && fileExists(p) {
		return p
	}
	return filepath.Join(b.q.ModCache, esc, "@v", m.Version+".zip")
}

// smoke writes the module of the plugin directory of plugin smoke as a module zip at the
// zero pseudo-version, which no tag carries, into a module source of the scratch
// directory that the build reads before the module cache, prints its sum, and returns
// the site of the throwaway distribution: the release that Against names and that
// module, at that sum, so the directory enters the build as every module does (ADR-044
// decisions 1, 7 and 8).
func (b *build) smoke() (Site, error) {
	if !byDigest.MatchString(b.q.Against) {
		return Site{}, fmt.Errorf("plugin smoke --against names the release artifact by digest, not %q (08:401)", b.q.Against)
	}
	gomod, err := os.ReadFile(filepath.Join(b.q.Smoke, "go.mod"))
	path := modfile.ModulePath(gomod)
	if err == nil && path == "" {
		err = errors.New("go.mod names no module")
	}
	if err != nil {
		return Site{}, fmt.Errorf("plugin smoke builds the module of %s: %w", b.q.Smoke, err)
	}
	_, major, _ := module.SplitPathVersion(path)
	v := module.Version{Path: path, Version: module.ZeroPseudoVersion(strings.TrimLeft(major, "/."))}
	esc, _ := module.EscapePath(path)
	at := filepath.Join(b.scratch, "source", esc, "@v")
	info, _ := json.Marshal(map[string]string{"Version": v.Version})
	err = errors.Join(os.MkdirAll(at, 0o755), os.WriteFile(filepath.Join(at, v.Version+".mod"), gomod, 0o644), os.WriteFile(filepath.Join(at, v.Version+".info"), info, 0o644))
	var f *os.File
	if err == nil {
		f, err = os.Create(filepath.Join(at, v.Version+".zip"))
	}
	if err == nil {
		err = errors.Join(modzip.CreateFromDir(f, v, b.q.Smoke), f.Close())
	}
	var sum, modSum string
	if err == nil {
		sum, err = dirhash.HashZip(f.Name(), dirhash.Hash1)
	}
	if err == nil {
		modSum, err = dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(gomod)), nil })
	}
	if err = errors.Join(err, b.record(path+" "+v.Version+"/go.mod", modSum, "plugin smoke")); err != nil {
		return Site{}, err
	}
	fmt.Fprintf(b.progress, "smoke: %s at %s, sum %s, the module zip of %s\n", path, v.Version, sum, b.q.Smoke)
	return Site{Release: b.q.Against, Plugins: []module.Version{v}, Sums: map[string]string{path: sum}}, nil
}

func readZip(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, boot.MaxDistBytes))
}

// record adds one go.sum line that a source records, and fails when another source
// records another hash for it.
func (b *build) record(key, hash, source string) error {
	if have, ok := b.recorded[key]; ok && have != hash {
		return fmt.Errorf("%s records %s %s, and another source records %s (ADR-044 decision 8)", source, key, hash, have)
	}
	b.recorded[key] = hash
	return nil
}

// resolve has go fetch every module the build needs from the module source, the
// compiled ones' zips and the graph's go.mod files, checking each against the go.sum
// lines the sources record; a hash that no source records is the module source's, and
// is returned to be printed and recorded as checked against nothing (ADR-044 decision
// 8), but in a release build that of a module it serves, its own source. A release
// build writes no line before go runs and checks each that go writes against the go.sum
// of the modules it serves, before anything compiles, so its pins are the lines of its
// own build. go completes the scratch go.mod with the requirements of the modules that give
// packages, whose versions the strict-version check then holds to the release.
func (b *build) resolve(ctx context.Context) ([]string, error) {
	args := []string{"list", "-mod=mod", "-deps", "-test", "-tags=grpcnotrace", "./..."}
	for _, p := range b.site.Plugins {
		args = append(args, p.Path+"/...")
	}
	if _, err := b.g.Run(ctx, args...); err != nil {
		return nil, err
	}
	sum, err := os.ReadFile(filepath.Join(b.scratch, "go.sum"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(string(sum), "\n") {
		if fs := strings.Fields(l); len(fs) == 3 {
			have, served := b.served[fs[0]+" "+fs[1]]
			if served && have != fs[2] {
				return nil, fmt.Errorf("%s@%s: checksum mismatch: the module source serves %s, and the go.sum of a module the release serves records %s (ADR-044 decision 8)", fs[0], fs[1], fs[2], have)
			}
			own := b.release == nil && slices.Contains(sdkModules, fs[0]) && strings.TrimSuffix(fs[1], "/go.mod") == b.q.Root
			if _, ok := b.recorded[fs[0]+" "+fs[1]]; !ok && !served && !own {
				out = append(out, l)
			}
			b.recorded[fs[0]+" "+fs[1]] = fs[2]
		}
	}
	return out, nil
}

// buildList returns the selected version of each module of the build list.
func (b *build) buildList(ctx context.Context) (map[string]string, error) {
	out, err := b.g.Run(ctx, "list", "-m", "-f", "{{.Path}} {{.Version}}", "all")
	if err != nil {
		return nil, err
	}
	list := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if p, v, ok := strings.Cut(l, " "); ok {
			list[p] = v
		}
	}
	return list, nil
}

// versions is the strict-version check (ADR-044 decisions 10 and 20): every module the
// release pins keeps its version unless --allow-bump names it, the raise keeps the
// major, and the module is none the release pipeline builds; a flag that names an
// unchanged module fails. It returns the pins of the build: each go.sum line, with the
// release's version beside a raised one and the unchecked ones marked.
func (b *build) versions(ctx context.Context, list map[string]string, unchecked []string) ([]boot.DistPin, []string, error) {
	pins := map[string]*boot.DistPin{}
	for _, k := range sortedKeys(b.recorded) {
		path, version, _ := strings.Cut(k, " ")
		v, isMod := strings.CutSuffix(version, "/go.mod")
		p := pins[path+" "+v]
		if p == nil {
			p = &boot.DistPin{Path: path, Version: v}
			pins[path+" "+v] = p
		}
		if isMod {
			p.GoMod = b.recorded[k]
		} else {
			p.Sum = b.recorded[k]
		}
		p.Unchecked = p.Unchecked || slices.Contains(unchecked, k+" "+b.recorded[k])
	}
	var bumps []string
	var err error
	if b.release != nil {
		graph, gerr := b.g.Run(ctx, "mod", "graph")
		if gerr != nil {
			return nil, nil, gerr
		}
		var admitted map[string]string
		bumps, admitted, err = strict(b.release.Pins, list, b.q.AllowBump, string(graph))
		for path, release := range admitted {
			if p := pins[path+" "+list[path]]; p != nil {
				p.Release = release
			}
		}
	}
	var out []boot.DistPin
	for _, k := range sortedKeys(pins) {
		out = append(out, *pins[k])
	}
	return append(out, boot.DistPin{Path: Toolchain, Version: ToolchainVersion, Sum: b.toolchainSum()}), bumps, err
}

// strict compares the selected version of each module of list with the pins of the
// release: each module the release compiles, which its pin with a zip sum names, keeps
// its version, and each module the release pipeline builds is at the release version,
// the root module's, whatever the pins hold of it. A pin of a go.mod alone names no
// selected version: the module graph that go list -m all loads holds versions whose
// go.mod no line of the release's go.sum records. A raise passes only when allow names
// it, it keeps the major, and the module is none the release pipeline builds; a module
// of allow that list does not raise fails. It returns a line per admitted raise, naming
// the modules whose go.mod requires it in graph, the output of go mod graph, and the
// release's version of each.
func strict(release []boot.DistPin, list map[string]string, allow []string, graph string) ([]string, map[string]string, error) {
	var errs []error
	var bumps []string
	raised, admitted, pinned := map[string]bool{}, map[string]string{}, map[string]boot.DistPin{}
	for _, rp := range release {
		if rp.Sum != "" {
			pinned[rp.Path] = rp
		}
	}
	for _, m := range sdkModules {
		pinned[m] = boot.DistPin{Path: m, Version: pinned[rootModule].Version}
	}
	for _, path := range sortedKeys(pinned) {
		rp, got := pinned[path], list[path]
		if rp.Path == Toolchain || got == rp.Version || got == "" {
			continue
		}
		raised[rp.Path] = true
		why := fmt.Sprintf("%s, pinned by the release at %s, is %s in this build, required by %s", rp.Path, rp.Version, got, requirers(graph, rp.Path+"@"+got))
		switch {
		case slices.Contains(sdkModules, rp.Path):
			errs = append(errs, fmt.Errorf("%s: the release pipeline builds this module, which --allow-bump never names; require the SDK version of the release (ADR-044 decision 20)", why))
		case semver.Major(got) != semver.Major(rp.Version) || strings.HasSuffix(got, "+incompatible") != strings.HasSuffix(rp.Version, "+incompatible"):
			errs = append(errs, fmt.Errorf("%s: a raise that changes the major fails with or without --allow-bump (ADR-044 decision 10)", why))
		case !slices.Contains(allow, rp.Path):
			errs = append(errs, fmt.Errorf("%s: --allow-bump %s admits the raise (ADR-044 decision 10)", why, rp.Path))
		default:
			bumps, admitted[rp.Path] = append(bumps, why), rp.Version
		}
	}
	for _, m := range allow {
		if !raised[m] {
			errs = append(errs, fmt.Errorf("--allow-bump %s names a module whose version this build does not change; a stale flag would admit a later raise (ADR-044 decision 10)", m))
		}
	}
	return bumps, admitted, errors.Join(errs...)
}

// graph is Graph of main in the scratch module, the standard library at the sum of the
// toolchain module, which the SBOM records.
func (b *build) graph(ctx context.Context, main string, flags ...string) ([]Package, error) {
	pkgs, err := Graph(ctx, b.g, main, flags...)
	for i := range pkgs {
		if pkgs[i].Standard {
			pkgs[i].Module.Sum = b.toolchainSum()
		}
	}
	return pkgs, err
}

// toolchainSum is the sum of the toolchain module that the release records, or for a
// release build the pin of this builder.
func (b *build) toolchainSum() string {
	if b.release != nil {
		return pinOf(b.release.Pins, Toolchain, ToolchainVersion).Sum
	}
	return ToolchainSum
}

// requirers returns the modules whose go.mod requires target, from go mod graph.
func requirers(graph, target string) string {
	var out []string
	for _, l := range strings.Split(graph, "\n") {
		if from, to, ok := strings.Cut(l, " "); ok && to == target {
			out = append(out, from)
		}
	}
	return strings.Join(out, ", ")
}

// fakes refuses a fake or sdk/testkit package in the package graph of the server
// (07:47; ADR-060 decisions 22 and 58), and a package under testdata/, which go builds
// once a package imports it and which neither the module table nor plugin vet reads.
func fakes(pkgs []Package) error {
	var errs []error
	for _, p := range pkgs {
		switch elems := strings.Split(p.ImportPath, "/"); {
		case slices.Contains(elems, "fake") || strings.HasPrefix(p.ImportPath, rootModule+"/sdk/testkit"):
			errs = append(errs, fmt.Errorf("the server links %s, a fake or sdk/testkit package (07:47; ADR-060 decision 58)", p.ImportPath))
		case slices.Contains(elems, "testdata"):
			errs = append(errs, fmt.Errorf("the server links %s, a package under testdata/ (ADR-060 decision 22)", p.ImportPath))
		}
	}
	return errors.Join(errs...)
}

// gate runs the clauses of dist build on the manifests of what the build builds, an
// enabled row of each plugin (ADR-060 decisions 17 and 22): it prints each clause it does
// not evaluate with the reason, and a denial fails the build and names its check.
func (b *build) gate(manifest []byte) error {
	d, err := boot.ReadDistribution("the signed manifest of the build", manifest)
	if err != nil {
		return err
	}
	var rows []string
	for _, p := range b.plugins {
		switch man, err := boot.ReadManifest(p.pkg+"/plugin.yaml", p.manifest, boot.ManifestLenient); {
		case err != nil:
			return err
		case man.ID != boot.LoaderID: // boot mounts the loader's row
			rows = append(rows, fmt.Sprintf("{id: %s, plugin: %s}", strings.ReplaceAll(man.ID, "/", "."), man.ID))
		}
	}
	mods, fsys := d.Table(), fstest.MapFS{"profiles/build/profile.yaml": {Data: []byte("bundles: [build]\n")},
		"bundles/build/bundle.yaml": {Data: []byte("- insert: [" + strings.Join(rows, ", ") + "]\n")}}
	c, _, err := boot.Compose(boot.Sources{Profile: "build", Home: fsys, Bundles: fsys}, boot.Options{Modules: mods})
	if err != nil {
		return err
	}
	g := boot.GateWith(boot.WithLoader(c), boot.GateInput{Entry: boot.EntryBuild, Mods: mods, Bindings: d.Bindings(), Dist: &d, Artifacts: map[string]boot.Artifact{}})
	for _, l := range g.NotEvaluated() {
		fmt.Fprintln(b.progress, "gate: "+l)
	}
	return g.Denied()
}

// moduleDir returns the directory of the package pkg of a site plugin in the scratch
// module cache.
func (b *build) moduleDir(pkg string) string {
	for _, p := range b.site.Plugins {
		if pkg == p.Path || strings.HasPrefix(pkg, p.Path+"/") {
			esc, _ := module.EscapePath(p.Path)
			return filepath.Join(b.scratch, "mod", esc+"@"+p.Version, strings.TrimPrefix(strings.TrimPrefix(pkg, p.Path), "/"))
		}
	}
	return ""
}

// source runs on the source of what the build builds what the build runs there (ADR-060
// decision 22), without -race, since the builder has cgo off: on each site plugin's
// package, the import boundary and plugin vet of internal/vet, and the tests of each
// site module, the suites among them; on each package of a release's table, the import
// boundary of the repository, and its suites and generated tests. With a raise that
// --allow-bump admitted, a site build runs those of each plugin of the release's table
// on the raised build list, so that the first-party rows run their suites against what
// the binary links (ADR-044 decision 10). A go.mod and go.sum of their own take the lines
// of their test dependencies, which the binary does not link.
func (b *build) source(ctx context.Context, bumped bool) error {
	var errs []error
	first, why := b.base, "on the raised build list (ADR-044 decision 10)"
	if b.release == nil {
		first, why = b.plugins, "the release's own (ADR-060 decision 22)"
		var dirs []string
		for _, p := range b.plugins {
			fmt.Fprintf(b.progress, "boundary: the import boundary of %s\n", p.pkg)
			dirs = append(dirs, strings.TrimPrefix(p.pkg, rootModule+"/"))
		}
		errs = append(errs, vet.Boundary(filepath.Join(b.scratch, "mod", rootModule+"@"+b.q.Root), dirs)) // its path needs no escape
	} else {
		for _, p := range b.plugins {
			fs, err := vet.Module(b.moduleDir(p.pkg), vet.Std{Go: b.g.Bin, Env: b.g.Env, Dir: b.g.Dir})
			errs = append(errs, err)
			for _, f := range fs {
				errs = append(errs, fmt.Errorf("import boundary of %s: %s", p.pkg, f))
			}
		}
		if !bumped {
			first = nil
		}
	}
	for _, p := range b.site.Plugins {
		fmt.Fprintf(b.progress, "suites: go test %s/...\n", p.Path)
		if _, err := b.g.Run(ctx, "test", "-count=1", "-tags=grpcnotrace", p.Path+"/..."); err != nil {
			errs = append(errs, fmt.Errorf("the suites of %s: %w", p.Path, err))
		}
	}
	var pkgs []string
	for _, p := range first {
		pkgs = append(pkgs, p.pkg)
	}
	if len(pkgs) > 0 {
		mod, err := os.ReadFile(filepath.Join(b.scratch, "go.mod"))
		sum, serr := os.ReadFile(filepath.Join(b.scratch, "go.sum"))
		err = errors.Join(err, serr, os.WriteFile(filepath.Join(b.scratch, "suites.mod"), mod, 0o644), os.WriteFile(filepath.Join(b.scratch, "suites.sum"), sum, 0o644))
		fmt.Fprintf(b.progress, "suites: go test %s, %s\n", strings.Join(pkgs, " "), why)
		if err == nil {
			_, err = b.g.Run(ctx, slices.Concat([]string{"test", "-count=1", "-tags=grpcnotrace", "-modfile=suites.mod", "-mod=mod", "-run", "^(TestConformance|TestCycle|TestTwoTenants)$"}, pkgs)...)
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// link writes the signed manifest into the module table, links the server for
// linux/amd64 and aiopsctl, the site's for the host and the release's for linux/amd64,
// with -trimpath, writes beside each its THIRD_PARTY_LICENSES/, NOTICE and SBOM, the zip
// of each MPL-2.0 module, and the image layout (ADR-044 decision 6; ADR-020 decisions 13
// and 29).
func (b *build) link(ctx context.Context, res Result, pkgs []Package, l Licences) (Result, error) {
	if err := os.WriteFile(filepath.Join(b.scratch, "site", "distribution.yaml"), res.Manifest, 0o644); err != nil {
		return res, err
	}
	out := b.q.Out
	res.Aiops, res.Aiopsctl, res.Image = filepath.Join(out, "aiops", "aiops"), filepath.Join(out, "aiopsctl", "aiopsctl"), filepath.Join(out, "image")
	g, ctlGo := b.g, b.g
	g.Env = append(slices.Clone(g.Env), "GOOS=linux", "GOARCH=amd64")
	if b.release == nil {
		ctlGo = g // the release aiopsctl, which the builder image holds
	}
	if _, err := g.Run(ctx, "build", "-trimpath", "-tags=grpcnotrace", "-o", res.Aiops, "./cmd/aiops"); err != nil {
		return res, err
	}
	var suites [][]Package // the graphs of the suite binaries that the release aiopsctl embeds
	if b.release == nil {
		var err error
		if suites, err = b.compileSuites(ctx, ctlGo); err != nil {
			return res, err
		}
	}
	if _, err := ctlGo.Run(ctx, "build", "-trimpath", "-tags=grpcnotrace", "-o", res.Aiopsctl, "./cmd/aiopsctl"); err != nil {
		return res, err
	}
	created := time.Unix(0, 0).UTC() // the epoch, as every manifest of the image records, so one build writes one digest
	mpl, err := Notices(filepath.Join(out, "aiops"), "aiops", pkgs, l, created)
	if err != nil {
		return res, err
	}
	ctl, err := b.graph(ctx, "./cmd/aiopsctl")
	ctl = union(append([][]Package{ctl}, suites...)...) // each test binary it embeds is linked on its own (ADR-044 decision 5)
	var cl Licences
	if err == nil {
		cl, err = ScanLicences(ctl)
		err = errors.Join(err, CheckLicences(cl, b.site.AcceptLicenses))
		if err == nil {
			_, err = Notices(filepath.Join(out, "aiopsctl"), "aiopsctl", ctl, cl, created)
		}
	}
	if err != nil {
		return res, err
	}
	for _, m := range mpl {
		esc, _ := module.EscapePath(m.Path)
		data, err := os.ReadFile(b.zip(module.Version{Path: m.Path, Version: m.Version}))
		if err == nil {
			err = errors.Join(os.MkdirAll(filepath.Join(out, "mpl-sources"), 0o755),
				os.WriteFile(filepath.Join(out, "mpl-sources", strings.ReplaceAll(esc, "/", "_")+"@"+m.Version+".zip"), data, 0o644))
		}
		if err != nil {
			return res, err
		}
		fmt.Fprintf(b.progress, "MPL-2.0: %s %s is linked; whoever distributes the image keeps its source, mpl-sources/ beside it (ADR-044 decision 6)\n", m.Path, m.Version)
	}
	if err := os.WriteFile(filepath.Join(out, "distribution.yaml"), res.Manifest, 0o644); err != nil {
		return res, err
	}
	return res, b.layout(ctx, res, ctl, cl, created)
}
