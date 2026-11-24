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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
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

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/vet"
)

// Request is one build. A site build names its dist.yaml and the release's signed
// manifest, and links in one phase. A release build names the release input in the
// form of dist/dist.yaml and the version of the root module, and runs in two phases
// (ADR-020 decision 13): the first writes the manifest and stops; the second, given it
// back as Signed, links the release binaries with it.
type Request struct {
	Signed    *Signed           // a release build's second phase
	DistYAML  string            // the site's dist.yaml
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
	Created   time.Time // the creation time of the SBOMs
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
	plugins  []tablePlugin
	progress io.Writer
}

// tablePlugin is a plugin of a site module that the module table gains.
type tablePlugin struct {
	pkg, accessor string
	manifest      []byte
}

// Build runs one build on the local path.
func Build(ctx context.Context, q Request) (Result, error) {
	b := &build{q: q, recorded: map[string]string{}, progress: q.Progress}
	if b.progress == nil {
		b.progress = io.Discard
	}
	if err := b.inputs(); err != nil {
		return Result{}, err
	}
	scratch, err := os.MkdirTemp("", "aiopsctl-build-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(scratch)
	b.scratch = scratch
	b.goroot, err = Tree(q.ModCache, q.Cache, b.toolchainSum())
	if err != nil {
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
	err = errors.Join(err, lerr, ferr, CheckLicences(l, b.site.AcceptLicenses), b.owners(), fakes(pkgs))
	if err != nil {
		return res, err
	}
	m := Manifest{Graph: pkgs, Licences: l.Modules, Pins: pins, Release: b.input, Site: b.site.Bindings, Accept: b.site.AcceptLicenses}
	if res.Manifest, err = m.Write(); err != nil {
		return res, err
	}
	switch {
	case b.release == nil && q.Signed == nil:
		return res, nil // the first phase of a release stops at the manifest
	case b.release == nil && !bytes.Equal(res.Manifest, q.Signed.Manifest):
		return res, errors.New("the second phase writes another manifest than the first phase wrote: the inputs changed between the phases (ADR-020 decision 13)")
	case b.release != nil:
		if err := errors.Join(b.gate(res.Manifest), b.source(ctx)); err != nil {
			return res, err
		}
	}
	return b.link(ctx, res, pkgs, l)
}

// inputs reads the request: the site's dist.yaml with the release's signed manifest,
// or the release input; and refuses what the local path cannot take.
func (b *build) inputs() error {
	q := b.q
	switch {
	case q.ModCache == "":
		return errors.New("dist build needs --modcache: every module source is a directory until L14 clears, and no default proxy is asked (ADR-044 decision 7)")
	case strings.Contains(q.Proxy, "://") && !strings.HasPrefix(q.Proxy, "file://"):
		return fmt.Errorf("GOPROXY names %s: until L14 clears, every module source of the builder is a file:// directory and dist build refuses a proxy URL (ADR-044 decision 7; ADR-060 decision 21)", q.Proxy)
	case len(q.Mirrors) > 0:
		return errors.New("--mirror rewrites what the image path pulls, and the local path pulls nothing: it reads the release's manifest from this aiopsctl")
	case !filepath.IsAbs(q.Out) || !filepath.IsAbs(q.ModCache) || !filepath.IsAbs(q.Cache):
		return errors.New("the output directory, the module cache and the builder's cache are absolute paths")
	}
	if entries, err := os.ReadDir(q.Out); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists and is not empty", q.Out)
	}
	if q.DistYAML == "" {
		var err error
		b.input, err = boot.ParseDist("the release input", q.Input)
		if err != nil || !semver.IsValid(q.Root) {
			return errors.Join(err, fmt.Errorf("a release build names the version of the root module, not %q", q.Root))
		}
		if s := q.Signed; s != nil {
			var bundle struct {
				MediaType string `json:"mediaType"`
			}
			var kerr, berr error
			b.keyID, kerr = keyID(s.Key)
			_, derr := readDefinition(s.Definition)
			_, merr := boot.ReadDistribution("the manifest of the first phase", s.Manifest)
			if json.Unmarshal(s.Bundle, &bundle) != nil || bundle.MediaType != bundleType {
				berr = fmt.Errorf("the bundle of the manifest is not a Sigstore bundle of media type %s (ADR-020 decision 2)", bundleType)
			}
			return errors.Join(kerr, derr, merr, berr)
		}
		return nil
	}
	raw, err := os.ReadFile(q.DistYAML)
	if err == nil {
		b.site, err = ReadSite(raw)
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
	return nil
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
	s := b.scratch
	return []string{"PATH=" + filepath.Join(goroot, "bin"), "HOME=" + filepath.Join(s, "home"), "GOROOT=" + goroot,
		"GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly -modcacherw", "GOSUMDB=off", "GOPROXY=file://" + filepath.ToSlash(b.q.ModCache),
		"GOENV=off", "GOWORK=off", "CGO_ENABLED=0", "GOTELEMETRY=off",
		"GOCACHE=" + filepath.Join(s, "cache"), "GOMODCACHE=" + filepath.Join(s, "mod"), "TMPDIR=" + filepath.Join(s, "tmp")}
}

// module writes the scratch main module: go.mod, the go.sum lines that the sources
// record, and the module table with the two main packages.
func (b *build) module(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, d := range []string{"home", "tmp", "site/manifests", "cmd/aiops", "cmd/aiopsctl"} {
		if err := os.MkdirAll(filepath.Join(b.scratch, d), 0o755); err != nil {
			return err
		}
	}
	f := &modfile.File{}
	_ = f.AddModuleStmt(scratchModule)
	_ = f.AddGoStmt("1.26.0")
	if b.release == nil {
		_ = f.AddRequire(rootModule, b.q.Root)
	}
	if b.release != nil {
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
	files := map[string][]byte{"go.mod": gomod, "go.sum": sum.Bytes(), "site/distribution.yaml": nil, "LICENSE": apache(),
		"site/release.pub": key, "site/distribution.bundle": bundle,
		"site/site.go": siteGo(b.plugins, b.keyID), "cmd/aiops/main.go": []byte(aiopsMain), "cmd/aiopsctl/main.go": []byte(aiopsctlMain)}
	for i, p := range b.plugins {
		files[fmt.Sprintf("site/manifests/%d.yaml", i)] = p.manifest
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(b.scratch, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// plugin reads the module zip of a plugin of dist.yaml from the module source, checked
// against the sum dist.yaml lists: it records the sum and the lines of the zip's go.sum,
// the file its author resolved with, and adds each package of the module that holds a
// plugin.yaml to the module table. Two sources that record different hashes for one
// version fail the build (ADR-044 decision 8).
func (b *build) plugin(p module.Version) error {
	esc, err := module.EscapePath(p.Path)
	if err != nil {
		return err
	}
	file := filepath.Join(b.q.ModCache, esc, "@v", p.Version+".zip")
	got, err := dirhash.HashZip(file, dirhash.Hash1)
	if err != nil {
		return fmt.Errorf("plugin module %s %s: %w", p.Path, p.Version, err)
	}
	if want := b.site.Sums[p.Path]; got != want {
		return fmt.Errorf("the module source serves %s %s with the sum %s, and dist.yaml lists %s (ADR-044 decision 8)", p.Path, p.Version, got, want)
	}
	if err := b.record(p.Path+" "+p.Version, got, "dist.yaml"); err != nil {
		return err
	}
	z, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer z.Close()
	prefix := p.Path + "@" + p.Version + "/"
	dirs := map[string][]*zip.File{}
	for _, f := range z.File {
		rel := strings.TrimPrefix(f.Name, prefix)
		dirs[filepath.ToSlash(filepath.Dir(rel))] = append(dirs[filepath.ToSlash(filepath.Dir(rel))], f)
		if rel != "go.sum" {
			continue
		}
		data, err := readZip(f)
		if err != nil {
			return err
		}
		for _, l := range strings.Split(string(data), "\n") {
			if fs := strings.Fields(l); len(fs) == 3 {
				if err := b.record(fs[0]+" "+fs[1], fs[2], "the go.sum of "+p.Path); err != nil {
					return err
				}
			}
		}
	}
	for _, dir := range sortedKeys(dirs) {
		var manifest []byte
		accessor := ""
		for _, f := range dirs[dir] {
			name := filepath.Base(f.Name)
			switch data, err := readZip(f); {
			case err != nil:
				return err
			case name == "plugin.yaml":
				manifest = data
			case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"):
				accessor = accessorOf(accessor, data)
			}
		}
		if manifest == nil || strings.Contains("/"+dir+"/", "/testdata/") {
			continue
		}
		if accessor == "" {
			return fmt.Errorf("%s/%s holds a plugin.yaml and declares neither var Plugin nor func New()", p.Path, dir)
		}
		pkg := p.Path
		if dir != "." {
			pkg += "/" + dir
		}
		b.plugins = append(b.plugins, tablePlugin{pkg: pkg, accessor: accessor, manifest: manifest})
	}
	return nil
}

func readZip(f *zip.File) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, boot.MaxDistBytes))
}

// accessorOf returns have, or how the Go file src gives its plugin, as plugingen
// reads a plugin: Plugin for a package-level var Plugin, New() for a func New.
func accessorOf(have string, src []byte) string {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if have != "" || err != nil {
		return have
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "New" {
				return "New()"
			}
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				if v, ok := sp.(*ast.ValueSpec); ok && d.Tok == token.VAR && slices.ContainsFunc(v.Names, func(n *ast.Ident) bool { return n.Name == "Plugin" }) {
					return "Plugin"
				}
			}
		}
	}
	return ""
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
// 8). go completes the scratch go.mod with the requirements of the modules that give
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
			if _, ok := b.recorded[fs[0]+" "+fs[1]]; !ok && b.release != nil {
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
// release: a raise passes only when allow names it, it keeps the major, and the module
// is none the release pipeline builds; a module of allow that list does not raise
// fails. It returns a line per admitted raise, naming the modules whose go.mod requires
// it in graph, the output of go mod graph, and the release's version of each.
func strict(release []boot.DistPin, list map[string]string, allow []string, graph string) ([]string, map[string]string, error) {
	var errs []error
	var bumps []string
	raised, admitted := map[string]bool{}, map[string]string{}
	for _, rp := range release {
		got := list[rp.Path]
		if rp.Sum == "" || rp.Path == Toolchain || got == rp.Version || got == "" {
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
func (b *build) graph(ctx context.Context, main string) ([]Package, error) {
	pkgs, err := Graph(ctx, b.g, main)
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

// owners refuses a plugin of a module outside the release whose id is under aiops/
// or contrib/, the two owners reserved to the release pipeline (08:402).
func (b *build) owners() error {
	var errs []error
	for _, p := range b.plugins {
		man, err := boot.ReadManifest(p.pkg+"/plugin.yaml", p.manifest, boot.ManifestLenient)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if owner, _, _ := strings.Cut(man.ID, "/"); owner == "aiops" || owner == "contrib" {
			errs = append(errs, fmt.Errorf("%s declares the id %s: the owner %s is reserved to the artifacts the release pipeline builds, and the module is outside the release (08:402)", p.pkg, man.ID, owner))
		}
	}
	return errors.Join(errs...)
}

// fakes refuses a fake or sdk/testkit package in the package graph of the server
// (07:47; ADR-060 decisions 22 and 58).
func fakes(pkgs []Package) error {
	var errs []error
	for _, p := range pkgs {
		if slices.Contains(strings.Split(p.ImportPath, "/"), "fake") || strings.HasPrefix(p.ImportPath, rootModule+"/sdk/testkit") {
			errs = append(errs, fmt.Errorf("the server links %s, a fake or sdk/testkit package (07:47; ADR-060 decision 58)", p.ImportPath))
		}
	}
	return errors.Join(errs...)
}

// gate runs the clauses of the entry point dist build on the module table of the
// build, with each site plugin's bundle.yaml in a profile of its own (ADR-060
// decision 17): a denial fails the build and names its check.
func (b *build) gate(manifest []byte) error {
	d, err := boot.ReadDistribution("the signed manifest of the build", manifest)
	if err != nil {
		return err
	}
	mods := d.Table()
	files := map[string][]byte{}
	var ids []string
	for _, p := range b.plugins {
		man, err := boot.ReadManifest(p.pkg+"/plugin.yaml", p.manifest, boot.ManifestLenient)
		if err != nil {
			return err
		}
		dir := filepath.Join(b.moduleDir(p.pkg), "bundle.yaml")
		if data, err := os.ReadFile(dir); err == nil {
			files["bundles/"+man.ID+"/bundle.yaml"] = data
			ids = append(ids, man.ID)
		}
	}
	files["profiles/build/profile.yaml"] = []byte("bundles: []\n")
	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: data}
	}
	c, _, err := boot.Compose(boot.Sources{Profile: "build", Home: fsys, Bundles: fsys, Append: ids}, boot.Options{Modules: mods})
	if err != nil {
		return err
	}
	g := boot.GateWith(boot.WithLoader(c), boot.GateInput{Entry: boot.EntryBuild, Mods: mods, Bindings: d.Bindings(), Dist: &d, Artifacts: map[string]boot.Artifact{}})
	var errs []error
	for _, s := range g.Slots {
		for _, f := range s.Fails {
			errs = append(errs, fmt.Errorf("the gate at dist build denies row %s by check %d (%s): %s", f.Row, s.N, f.Rule, f.Reason))
		}
	}
	for _, f := range g.Unresolved {
		errs = append(errs, fmt.Errorf("the gate at dist build denies row %s: %s", f.Row, f.Reason))
	}
	return errors.Join(errs...)
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

// source runs on the site plugins' source what the build runs there (ADR-060 decision
// 22): the import boundary and plugin vet of internal/vet on each plugin package, and
// the tests of each plugin module, the suites among them, without -race, since the
// builder has cgo off.
func (b *build) source(ctx context.Context) error {
	var errs []error
	for _, p := range b.plugins {
		fs, err := vet.Module(b.moduleDir(p.pkg))
		errs = append(errs, err)
		for _, f := range fs {
			errs = append(errs, fmt.Errorf("import boundary of %s: %s", p.pkg, f))
		}
	}
	for _, p := range b.site.Plugins {
		fmt.Fprintf(b.progress, "suites: go test %s/...\n", p.Path)
		if _, err := b.g.Run(ctx, "test", "-count=1", "-tags=grpcnotrace", p.Path+"/..."); err != nil {
			errs = append(errs, fmt.Errorf("the suites of %s: %w", p.Path, err))
		}
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
	if _, err := ctlGo.Run(ctx, "build", "-trimpath", "-tags=grpcnotrace", "-o", res.Aiopsctl, "./cmd/aiopsctl"); err != nil {
		return res, err
	}
	created := b.q.Created
	if created.IsZero() {
		created = time.Now()
	}
	mpl, err := Notices(filepath.Join(out, "aiops"), "aiops", pkgs, l, created)
	if err != nil {
		return res, err
	}
	ctl, err := b.graph(ctx, "./cmd/aiopsctl")
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
		data, err := os.ReadFile(filepath.Join(b.q.ModCache, esc, "@v", m.Version+".zip"))
		if err == nil {
			err = os.MkdirAll(filepath.Join(out, "mpl-sources"), 0o755)
		}
		if err == nil {
			err = os.WriteFile(filepath.Join(out, "mpl-sources", strings.ReplaceAll(esc, "/", "_")+"@"+m.Version+".zip"), data, 0o644)
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
