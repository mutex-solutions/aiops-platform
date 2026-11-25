// SPDX-License-Identifier: Apache-2.0

// Package authoring is the S2a exit test (ADR-060 decisions 30 and 31), which make
// s2a-exit and the CI jobs s2a-exit and s2a-exit-local run.
//
// It first builds what a release pipeline would build, with throwaway keys: the module
// proxy of the exit, a directory that serves the root and SDK modules of this repository
// at one pre-release version that no tag carries, with their served go.mod copies, and
// the modules of their build list (ADR-060 decisions 21 and 40); the test release of
// test/release/dist.yaml, built in two phases by the builder from those modules, its
// manifest signed with a throwaway release key (ADR-060 decisions 28 and 38; ADR-020
// decision 13); and the registry of the exit, the pinned distribution image run inside
// the job, which receives the release image, the builder image and the release
// artifact (ADR-047 decision 4). Nothing is published outside the test, and every key,
// container and image it makes is removed with it (ADR-057 decision 5).
//
// It then drives three environments, each a directory of its own outside the
// repository (machine): the author's repository, with a Go toolchain and the aiopsctl
// of the test release; the pack author's machine, with that aiopsctl alone; and the
// clean machine, with that aiopsctl and a container runtime, whose builds run on the
// builder image, or with AIOPS_EXIT_BUILDER=local on the pinned toolchain module that
// the module proxy serves (the second job). Every step A1 to A3, P1, C1 to C6, D1 and
// D2 passes only as decision 30 states it, and the kernel of steps C3 to C6 runs in the
// container form of ADR-047 decision 15. The cases of decision 31 that the exit reaches
// run beside them: plugin smoke of A2's plugin, plugin verify of A3's artifact, of P1's
// pack and of a copy of the pack whose fixture changed, and a site build that departs
// from the release bindings and accepts a licence outside the allow-list. So do the
// built-binary cases of ADR-061 and ADR-062 that need these environments: the help of
// the built aiopsctl, the release aiopsctl on a home after dist use, in-process code
// installed into the profile cli, and the refusals of plugin disable of a row of
// aiops-cli in cli, of reload-socket and of reload.
//
// It needs the pinned toolchain module in the module cache and a container runtime, so
// -short skips it, and make s2a-exit fails when it skips.
package authoring

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/internal/authoring"
	"github.com/mutex-solutions/aiops-platform/internal/builder"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/templates"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/modsrc"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// version is the version at which the exit serves the modules of the repository: a
// pre-release that no tag of the repository carries (ADR-060 decision 40).
const version = "v0.2.0-s2a.exit"

// The media types of an artifact and of its referrers (ADR-047 decision 1; ADR-020
// decisions 2 and 24).
const (
	layerManifest  = "application/vnd.aiops.plugin.manifest.v1+yaml"
	layerBundle    = "application/vnd.aiops.plugin.bundle.v1+yaml"
	layerModule    = "application/vnd.aiops.plugin.module.v1+json"
	layerFixtures  = "application/vnd.aiops.plugin.fixtures.v1.tar"
	layerSBOM      = "application/spdx+json"
	statementType  = "application/vnd.in-toto+json"
	bundleType     = "application/vnd.dev.sigstore.bundle.v0.3+json"
	predicateKey   = "dev.sigstore.bundle.predicateType"
	distribution   = "application/vnd.aiops.distribution.v1"
	builderPinned  = "the builder image"
	echoModule     = "example.com/acme/echo"
	packID         = "globex/connector-echo"
	attestationURI = operations.AttestationType
)

// exit is what the release pipeline of the exit built.
type exit struct {
	t                *testing.T
	root, repo       string
	builder          string // the path of the clean machine's builds: image or local
	proxy            string // the module proxy, a directory served through file://
	reg              string // the registry: its host and port on the loopback interface
	manifest         []byte // the test release's signed manifest
	signed           string // the file that the release publishes it in, for plugin gate --against
	release          string // the release artifact, by digest
	aiopsctl         string // the aiopsctl of the test release
	builderImage     string // the builder image, by digest
	shipped          fs.FS  // the bundles and profiles that the release ships
	gpl              string // the text of GPL-2.0, from the licence classifier's assets in the module cache
	images           []string
	pushed, packSpec string
}

func TestS2aExit(t *testing.T) {
	if testing.Short() {
		t.Skip("the S2a exit builds the test release and drives three environments; make s2a-exit runs it")
	}
	b := cmp(os.Getenv("AIOPS_EXIT_BUILDER"), "image")
	if b != "image" && b != "local" {
		t.Fatalf("AIOPS_EXIT_BUILDER is %q: image, the s2a-exit job, or local, the s2a-exit-local job", b)
	}
	if err := exec.Command("docker", "version").Run(); err != nil {
		t.Skipf("no container runtime answers docker version (%v): the clean machine has one and the registry runs in one; the CI jobs s2a-exit and s2a-exit-local have one", err)
	}
	gomodcache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(strings.TrimSpace(string(gomodcache)), "cache", "download", builder.Toolchain, "@v", builder.ToolchainVersion+".zip")); err != nil {
		t.Skipf("the module cache holds no %s@%s: go mod download it to run the exit (make s2a-exit does)", builder.Toolchain, builder.ToolchainVersion)
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	x := &exit{t: t, root: t.TempDir(), repo: repo, builder: b, shipped: dist.Shipped(),
		gpl: filepath.Join(strings.TrimSpace(string(gomodcache)), "github.com", "google", "licenseclassifier", "v2@v2.0.0", "assets", "License", "GPL-2.0", "license.txt")}
	t.Cleanup(func() {
		for _, img := range x.images {
			_ = exec.Command("docker", "image", "rm", img).Run()
		}
	})
	x.reg = startRegistry(t)
	x.buildRelease()

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBin, _ = filepath.EvalSymlinks(goBin)
	dockerBin, err := exec.LookPath("docker")
	if err != nil {
		t.Fatal(err)
	}
	author := newMachine(t, x.root, "author", x.proxy, map[string]string{"go": goBin, "gofmt": filepath.Join(filepath.Dir(goBin), "gofmt"),
		"aiopsctl": x.aiopsctl}, true)
	packer := newMachine(t, x.root, "packer", x.proxy, map[string]string{"aiopsctl": x.aiopsctl}, false, "DOCKER_HOST=unix:///nonexistent")
	home := filepath.Join(x.root, "clean", "site", "home")
	clean := newMachine(t, x.root, "clean", x.proxy, map[string]string{"aiopsctl": x.aiopsctl, "docker": dockerBin}, false, "AIOPS_HOME="+home)
	if err := os.MkdirAll(filepath.Dir(home), 0o755); err != nil {
		t.Fatal(err)
	}

	a := x.author(author)
	x.pack(packer)
	x.cleanMachine(clean, author, packer, a, home)
}

// buildRelease builds what the release pipeline of the exit builds: the module proxy,
// the test release in two phases with a throwaway release key, and the release image,
// builder image and release artifact in the registry.
func (x *exit) buildRelease() {
	t := x.t
	x.proxy = filepath.Join(x.root, "proxy")
	if err := builder.Serve(x.repo, version, x.proxy); err != nil {
		t.Fatal(err)
	}
	modsrc.Fill(t, x.repo, x.proxy)
	x.primeProxy()
	input, err := os.ReadFile(filepath.Join(x.repo, "test", "release", "dist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	def, err := os.ReadFile(filepath.Join(x.repo, "dist", "builder", "image.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(x.root, "pipeline-cache")
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("GOPROXY", "")
	var progress bytes.Buffer
	q := builder.Request{Input: input, Root: version, ModCache: x.proxy, Cache: filepath.Join(cache, "aiopsctl"), Out: filepath.Join(x.root, "release-first"), Progress: &progress}
	first, err := builder.Build(context.Background(), q)
	if err != nil {
		t.Fatalf("the test release, its first phase: %v\n%s", err, progress.String())
	}
	bundle, pub := trusted.Sign(t, first.Manifest)
	q.Out, q.Signed = filepath.Join(x.root, "release"), &builder.Signed{Manifest: first.Manifest, Bundle: bundle, Key: pub, Definition: def}
	if _, err := builder.Build(context.Background(), q); err != nil {
		t.Fatalf("the test release, its second phase: %v\n%s", err, progress.String())
	}
	x.manifest, x.aiopsctl = first.Manifest, filepath.Join(q.Out, "aiopsctl", "aiopsctl")
	x.signed = filepath.Join(x.root, "release.yaml")
	write(t, x.signed, string(first.Manifest))
	layout := filepath.Join(q.Out, "image")
	idx := pushLayout(t, layout, x.reg+"/aiops/release")
	for _, d := range idx {
		m := manifestOf(t, layout, d)
		switch {
		case m.ArtifactType == distribution && m.Subject != nil:
			x.release = x.reg + "/aiops/release@" + d.Digest.String()
		case d.Annotations[ocispec.AnnotationRefName] == "builder":
			x.builderImage = x.reg + "/aiops/release@" + d.Digest.String()
		}
	}
	if x.release == "" || x.builderImage == "" {
		t.Fatalf("the release layout holds no release artifact or no builder image: %+v", idx)
	}
	x.images = append(x.images, x.builderImage)
}

// primeProxy makes the module proxy serve what the module of a plugin of each Go
// template resolves, the modules that the tests of its dependencies import among them,
// which the build list of the release need not hold (go mod tidy reads those tests):
// the release pipeline renders each template at the served SDK version, writes its
// generated half and tests as plugin check --write does, and runs go mod tidy through
// the module proxy and then the host's module cache and module proxy, which checks
// every module of another path against the checksum database; it adds to the module
// proxy what that fetched.
func (x *exit) primeProxy() {
	x.t.Run("proxy", func(t *testing.T) {
		gopath := t.TempDir()
		t.Cleanup(func() { writable(gopath) })
		env := func(name string) string {
			out, err := exec.Command("go", "env", name).Output()
			if err != nil {
				t.Fatal(err)
			}
			return strings.TrimSpace(string(out))
		}
		t.Setenv("GOPROXY", "file://"+x.proxy+",file://"+filepath.Join(env("GOMODCACHE"), "cache", "download")+","+env("GOPROXY"))
		t.Setenv("GONOSUMDB", "github.com/mutex-solutions/aiops-platform")
		t.Setenv("GOMODCACHE", filepath.Join(gopath, "mod"))
		t.Setenv("GOFLAGS", "-mod=mod")
		t.Setenv("GOTOOLCHAIN", "local")
		t.Setenv("GOWORK", "off")
		for _, reg := range templates.Registries {
			data, err := templates.NewData("acme/proxy-"+reg, version, false)
			var files map[string][]byte
			if err == nil {
				files, err = templates.Render(reg, data)
			}
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(gopath, reg)
			for name, b := range files {
				write(t, filepath.Join(dir, filepath.FromSlash(name)), string(b))
			}
			if ds, err := authoring.New().Generate(context.Background(), launch.GenerateRequest{Dir: dir, Write: true}); err != nil || len(ds) > 0 {
				t.Fatalf("plugin check --write of the template of %s: %+v, %v", reg, ds, err)
			}
			cmd := exec.Command("go", "mod", "tidy")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go mod tidy of the template of %s: %v\n%s", reg, err, out)
			}
		}
		modsrc.Merge(t, filepath.Join(gopath, "mod", "cache", "download"), x.proxy)
	})
}

// authored is what the author's repository made.
type authored struct {
	dir      string // the plugin's directory
	key      string // the author's public key file
	spec     string // the artifact of A3 in the registry
	digest   string
	sum      string // the module's h1 sum
	unserved string // the copy of D1
	copySum  string // the sum of the copy's module
	copySrc  string // the module source that serves the copy's module
	foreign  string // the copy of D2
	watchSum string // the sum of the module of the departing plugin acme/watch
}

// author runs steps A1 to A3, and makes the copies of D1 and D2 and runs plugin smoke
// on the plugin of A2 (ADR-060 decision 31).
func (x *exit) author(m *machine) authored {
	t := x.t
	var a authored
	a.dir = filepath.Join(m.work, "echo")
	t.Run("A1", func(t *testing.T) {
		m.t = t
		r := m.must("", "aiopsctl", "plugin", "new", "echoers", "acme/echo", "--lang", "go")
		if !strings.Contains(r.errOut, "wrote "+filepath.Join(a.dir, "plugin.go")) {
			t.Errorf("plugin new names no plugin.go it wrote:\n%s", r.errOut)
		}
		gomod := read(t, filepath.Join(a.dir, "go.mod"))
		if strings.Contains(gomod, "replace") || !strings.Contains(gomod, "github.com/mutex-solutions/aiops-platform/sdk "+version) {
			t.Errorf("the skeleton's go.mod holds a replace line or no require of the SDK at %s:\n%s", version, gomod)
		}
		m.run(a.dir, "aiopsctl", "plugin", "check", "--write")
		if !strings.Contains(read(t, filepath.Join(a.dir, "plugin.yaml")), "# --- generated by plugingen") {
			t.Errorf("plugin check --write wrote no generated half:\n%s", read(t, filepath.Join(a.dir, "plugin.yaml")))
		}
		m.must(a.dir, "go", "mod", "tidy")
		r = m.run(a.dir, "aiopsctl", "plugin", "check")
		var failed []string
		for _, l := range strings.Split(r.errOut, "\n") {
			if id, ok := strings.CutSuffix(strings.TrimSpace(l), ": the obligation failed"); ok {
				failed = append(failed, id)
			}
		}
		if r.code != 1 || !strings.Contains(r.errOut, "step 5 conformance: fail\n") || !slices.Equal(failed, echoers.Obligations) {
			t.Errorf("plugin check of the skeleton: exit %d, the failed obligations %v; want step 5 failing each of %v once", r.code, failed, echoers.Obligations)
		}
	})
	t.Run("A2", func(t *testing.T) {
		m.t = t
		write(t, filepath.Join(a.dir, "internal", "echo", "echo.go"), read(t, filepath.Join("testdata", "echo.go")))
		r := m.must(a.dir, "aiopsctl", "plugin", "check", "--json")
		var rep operations.CheckReport
		if err := json.Unmarshal([]byte(r.out), &rep); err != nil {
			t.Fatalf("plugin check --json: %v\n%s", err, r.out)
		}
		write(t, filepath.Join(m.root, "check.json"), r.out) // the report is kept
		want := []string{"pass", "pass", "pass", "pass", "pass", "not applicable", "pass", "not applicable"}
		var got []string
		for _, s := range rep.Steps {
			got = append(got, s.Outcome)
		}
		if !slices.Equal(got, want) || len(rep.Attestation) == 0 {
			t.Errorf("plugin check of the filled-in echoer: steps %v, want %v, with an attestation", got, want)
		}
		if len(rep.Steps) == 8 && !slices.ContainsFunc(rep.Steps[3].Diagnostics, func(d launch.Diagnostic) bool {
			return d.Warning && strings.Contains(d.Text, "TestTwoTenants skipped and did not run")
		}) {
			t.Errorf("step 4 reports no case of question Q7 that did not run: %+v", rep.Steps[3])
		}
	})
	t.Run("A3", func(t *testing.T) {
		m.t = t
		m.must("", "aiopsctl", "key", "generate", "author")
		a.key = filepath.Join(m.work, "author.pub")
		out, unsigned := filepath.Join(m.work, "artifact"), filepath.Join(m.work, "unsigned")
		m.must(a.dir, "aiopsctl", "plugin", "pack", "--out", out)
		copyDir(t, out, unsigned)
		m.must(a.dir, "aiopsctl", "plugin", "sign", "--key", filepath.Join(m.work, "author.key"), "--out", out)
		art, layers := artifactOf(t, out)
		a.digest = art.Digest.String()
		man := string(layers[layerManifest])
		if !strings.Contains(man, "requires:\n  pluginApi: ") || !strings.Contains(man, "  definitions:\n    echoers: [\"0.1\"]") {
			t.Errorf("the artifact's manifest carries no generated requires of echoers 0.1:\n%s", man)
		}
		if rows := bundleRows(t, layers[layerBundle]); len(rows) != 1 || rows[0]["enabled"] != false {
			t.Errorf("the artifact's bundle holds %v; want one disabled row", rows)
		}
		var mod struct{ Path, Version, Sum string }
		if err := json.Unmarshal(layers[layerModule], &mod); err != nil || mod.Path != echoModule || mod.Version != "v0.1.0" {
			t.Errorf("module.json %s: %v", layers[layerModule], err)
		}
		// The author publishes the module, at the version its artifact names, through the
		// exit's module proxy (ADR-060 decision 40).
		if a.sum = modsrc.Zip(t, x.proxy, echoModule, "v0.1.0", a.dir); a.sum != mod.Sum {
			t.Errorf("module.json names the sum %s, and the module zip of the author's directory is %s", mod.Sum, a.sum)
		}
		st := attestationOf(t, out, art)
		if want := map[string]any{"version": "0.1", "suite": 1.0, "halves": []any{"obligations"}}; !jsonEqual(st.Predicate.Definitions["echoers"], want) ||
			st.Subject != art.Digest.Encoded() || st.Name != "acme/echo@0.1.0" || !slices.Contains(st.Predicate.NotRun, "TestTwoTenants") {
			t.Errorf("the attestation %+v; want acme/echo@0.1.0 at %s, echoers at 0.1 with suite revision 1 and the obligations half, and TestTwoTenants not run",
				st, art.Digest.Encoded())
		}
		a.spec = strings.TrimSpace(m.must(a.dir, "aiopsctl", "plugin", "push", x.reg+"/acme/echo", "--out", out).out)
		if a.spec != "oci://"+x.reg+"/acme/echo@"+a.digest {
			t.Errorf("plugin push printed %q; want the spec by digest of %s", a.spec, a.digest)
		}

		// D1's copy: the echoer whose requires names echoers 0.2, which the release does
		// not serve, made without plugin check: its module and its artifact, attested by
		// the author so that only the version differs, signed by plugin sign.
		cp := filepath.Join(m.work, "copy")
		copyDir(t, a.dir, cp)
		write(t, filepath.Join(cp, "plugin.yaml"), strings.Replace(read(t, filepath.Join(cp, "plugin.yaml")), `echoers: ["0.1"]`, `echoers: ["0.2"]`, 1))
		a.copySrc = filepath.Join(m.work, "copy-source")
		modsrc.LinkTree(t, x.proxy, a.copySrc)
		esc := filepath.Join(a.copySrc, "example.com", "acme", "echo", "@v")
		for _, ext := range []string{".zip", ".mod", ".info"} {
			if err := os.Remove(filepath.Join(esc, "v0.1.0"+ext)); err != nil {
				t.Fatal(err)
			}
		}
		a.copySum = modsrc.Zip(t, a.copySrc, echoModule, "v0.1.0", cp)
		d1 := filepath.Join(m.work, "unserved")
		rewrite(t, unsigned, d1, map[string]func([]byte) []byte{
			layerManifest: func([]byte) []byte { return []byte(read(t, filepath.Join(cp, "plugin.yaml"))) },
			layerModule:   func(b []byte) []byte { return bytes.Replace(b, []byte(a.sum), []byte(a.copySum), 1) },
			layerSBOM:     func(b []byte) []byte { return bytes.Replace(b, []byte(a.sum), []byte(a.copySum), 1) },
		}, func(defs map[string]any) { defs["echoers"].(map[string]any)["version"] = "0.2" })
		m.must(a.dir, "aiopsctl", "plugin", "sign", "--key", filepath.Join(m.work, "author.key"), "--out", d1)
		a.unserved = strings.TrimSpace(m.must(a.dir, "aiopsctl", "plugin", "push", x.reg+"/acme/echo-unserved", "--out", d1).out)

		// D2's copy: the artifact of A3 and its signature, with the attestation, signed by
		// the author, of another artifact: one whose SBOM differs, so that its statement
		// differs from A3's in the subject digest alone.
		other := filepath.Join(m.work, "other")
		rewrite(t, unsigned, other, map[string]func([]byte) []byte{layerSBOM: func(b []byte) []byte { return append(b, '\n') }}, nil)
		m.must(a.dir, "aiopsctl", "plugin", "sign", "--key", filepath.Join(m.work, "author.key"), "--out", other)
		d2 := filepath.Join(m.work, "foreign")
		copyDir(t, out, d2)
		swapAttestation(t, d2, other)
		a.foreign = strings.TrimSpace(m.must(a.dir, "aiopsctl", "plugin", "push", x.reg+"/acme/echo-foreign", "--out", d2).out)
		if a.foreign != "oci://"+x.reg+"/acme/echo-foreign@"+a.digest {
			t.Errorf("the copy of D2 is %s; want the artifact of A3, %s", a.foreign, a.digest)
		}
	})
	// plugin smoke on the plugin of A2 against the test release (ADR-060 decisions 31 and
	// 58, reading 1): the test-only profile boots with every row of aiops-core ACTIVE
	// but reload-socket, which the patch of smoke disables, and the echo row waiting for
	// echoers, which no row of the profile provides.
	t.Run("smoke", func(t *testing.T) {
		m.t = t
		r := m.must(a.dir, "aiopsctl", "plugin", "smoke", ".", "--against", x.release, "--modcache", x.proxy, "--builder", x.builder)
		for _, want := range []string{"row clock: ACTIVE\n", "row config-refs: ACTIVE\n", "row loader: ACTIVE\n", "row readiness: ACTIVE\n",
			"row reload: ACTIVE\n", "row restart-policy: ACTIVE\n", "row echo: PENDING and waits for echoers\n"} {
			if !strings.Contains(r.out, want) {
				t.Errorf("plugin smoke --builder %s printed no %q", x.builder, want)
			}
		}
	})
	// The module of a site's plugin that departs from the release (ADR-060 decision 31):
	// an echoer that also wants reload, a key that the release binds to its own
	// consumers, under GPL-2.0-only, a licence outside the allow-list, so that a site
	// lists it in bindings and acceptLicenses. Its generated half is written as plugin
	// check --write writes it, and its module is published through the module proxy. It
	// wants the key rather than needs it, so that its tests mount it with the echoers
	// fake alone, as the template's tests do.
	t.Run("departing", func(t *testing.T) {
		m.t = t
		m.must("", "aiopsctl", "plugin", "new", "echoers", "acme/watch", "--lang", "go")
		dir := filepath.Join(m.work, "watch")
		write(t, filepath.Join(dir, "internal", "echo", "echo.go"), read(t, filepath.Join("testdata", "echo.go")))
		write(t, filepath.Join(dir, "plugin.go"), strings.NewReplacer(
			"Inject:    []kernel.Dep{kernel.Need(echoers.Key)},", "Inject:    []kernel.Dep{kernel.Need(echoers.Key), kernel.Want(reload.Key)},",
			"\t\"github.com/mutex-solutions/aiops-platform/sdk/kernel\"", "\treload \"github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0\"\n\t\"github.com/mutex-solutions/aiops-platform/sdk/kernel\"",
		).Replace(read(t, filepath.Join(dir, "plugin.go"))))
		write(t, filepath.Join(dir, "plugin.yaml"), strings.Replace(read(t, filepath.Join(dir, "plugin.yaml")), "license: Apache-2.0", "license: GPL-2.0-only", 1))
		write(t, filepath.Join(dir, "LICENSE"), read(t, x.gpl))
		m.run(dir, "aiopsctl", "plugin", "check", "--write")
		m.must(dir, "go", "mod", "tidy")
		if man := read(t, filepath.Join(dir, "plugin.yaml")); !strings.Contains(man, "{key: reload, major: 0, as: want}") {
			t.Fatalf("the generated half of acme/watch uses no reload:\n%s", man)
		}
		a.watchSum = modsrc.Zip(t, x.proxy, "example.com/acme/watch", "v0.1.0", dir)
	})
	m.t = t
	return a
}

// pack runs step P1 on the pack author's machine, and the help of the cli row on the
// built aiopsctl (ADR-061 decision 31, T3).
func (x *exit) pack(m *machine) {
	t := x.t
	t.Run("help", func(t *testing.T) {
		m.t = t
		r := m.must("", "aiopsctl", "help")
		for _, c := range []string{"dist build", "dist sign", "dist use", "key generate", "plugin new", "plugin check", "plugin smoke", "plugin pack",
			"plugin sign", "plugin push", "plugin inspect", "plugin verify", "plugin gate", "plugin install", "plugin enable", "plugin disable",
			"plugin remove", "profile init", "profile render", "profile sanitize", "profile verify", "trust add"} {
			if !strings.Contains(r.out, c) {
				t.Errorf("aiopsctl help lists no %s", c)
			}
		}
	})
	t.Run("P1", func(t *testing.T) {
		m.t = t
		m.must("", "aiopsctl", "plugin", "new", "connector", packID)
		dir := filepath.Join(m.work, "connector-echo")
		if b := read(t, filepath.Join(dir, "bundle.yaml")); !strings.Contains(b, `{id: example-echoer, plugin: aiops/example-echoer, roles: [api, worker], enabled: false, config: {"name":"loud","prefix":"LOUD "}}`) {
			t.Errorf("plugin new connector wrote no disabled row of the instantiable echoer with its first example:\n%s", b)
		}
		r := m.run(dir, "aiopsctl", "plugin", "check")
		var failed []string
		for _, l := range strings.Split(r.errOut, "\n") {
			if id, ok := strings.CutSuffix(strings.TrimSpace(l), ": the obligation failed"); ok {
				failed = append(failed, id)
			}
		}
		if r.code != 1 || !slices.Equal(failed, echoers.Obligations) {
			t.Errorf("plugin check of the fresh pack: exit %d, the failed obligations %v; want each of %v once", r.code, failed, echoers.Obligations)
		}
		copyDir(t, filepath.Join("testdata", "pack"), dir)
		m.must(dir, "aiopsctl", "plugin", "check")
		m.must("", "aiopsctl", "key", "generate", "packer")
		out := filepath.Join(m.work, "artifact")
		m.must(dir, "aiopsctl", "plugin", "pack", "--out", out)
		m.must(dir, "aiopsctl", "plugin", "sign", "--key", filepath.Join(m.work, "packer.key"), "--out", out)
		x.packSpec = strings.TrimSpace(m.must(dir, "aiopsctl", "plugin", "push", x.reg+"/"+packID, "--out", out).out)
		if !strings.HasPrefix(x.packSpec, "oci://"+x.reg+"/"+packID+"@sha256:") {
			t.Errorf("plugin push printed %q", x.packSpec)
		}
	})
	m.t = t
}

// cleanMachine runs steps C1 to C6, D1 and D2 on the clean machine, with the cases of
// decision 31 that need it.
func (x *exit) cleanMachine(m *machine, author, packer *machine, a authored, home string) {
	t := x.t
	site := filepath.Join(m.root, "site")
	run := filepath.Join(home, "run")
	siteCtl := filepath.Join(site, "build", "aiopsctl", "aiopsctl")
	ok := t.Run("C1", func(t *testing.T) {
		m.t = t
		copyDir(t, filepath.Join(author.work), filepath.Join(m.work, "from-author"))
		m.must("", "aiopsctl", "key", "generate", "site")
		m.must("", "aiopsctl", "trust", "add", filepath.Join(m.work, "from-author", "author.pub"), "--ids", "acme/*", "--runtimes", "inproc")
		m.must("", "aiopsctl", "trust", "add", filepath.Join(packer.work, "packer.pub"), "--ids", "globex/*")
		m.must("", "aiopsctl", "trust", "add", "site.pub", "--distribution")
		tr := read(t, filepath.Join(home, "trust.yaml"))
		pemLines := strings.Split(read(t, filepath.Join(m.work, "site.pub")), "\n")
		for _, want := range []string{"ids: [acme/*]\n    runtimes: [inproc]\n", "ids: [globex/*]\n", "distributions:\n  - key: release\n", pemLines[1]} {
			if !strings.Contains(tr, want) {
				t.Errorf("trust.yaml lacks %q:\n%s", want, tr)
			}
		}
		if st, err := os.Stat(run); err != nil || st.Mode().Perm() != 0o700 {
			t.Errorf("the home's run/ is %v, %v; aiopsctl creates it with mode 0700 (ADR-047 decision 14)", st, err)
		}
		r := m.must("", "aiopsctl", "plugin", "inspect", a.spec)
		if !strings.Contains(r.out, "id: acme/echo\n") || !strings.Contains(r.out, "row echo: plugin acme/echo, config ") || !strings.Contains(r.out, "runtimes: [inproc]\n") {
			t.Errorf("plugin inspect of the echoer printed no row of acme/echo:\n%s", r.out)
		}
		r = m.must("", "aiopsctl", "plugin", "inspect", x.packSpec)
		if !strings.Contains(r.out, `row example-echoer: plugin aiops/example-echoer, config {"name":"loud","prefix":"LOUD "}`) || !strings.Contains(r.out, "content: README.md\n") {
			t.Errorf("plugin inspect of the pack printed no row, config and content path:\n%s", r.out)
		}
	})
	if !ok {
		return
	}
	// D1 and D2 at plugin gate --against and at plugin install, before dist use, where
	// install evaluates every clause of decision 17 but the module clause of check 5, and
	// D1 at dist build: each denial names one check, and no site file changes.
	denies := func(t *testing.T, check int, dir string, args ...string) {
		t.Helper()
		before := tree(t, homeKeeps(home), home, dir)
		r := m.run(dir, args...)
		got := deniedChecks(r.out + r.errOut)
		if r.code != 1 || !slices.Equal(got, []int{check}) {
			t.Errorf("%s: exit %d, denied by checks %v; want exit 1 and check %d alone", strings.Join(args, " "), r.code, got, check)
		}
		if c := changed(before, tree(t, homeKeeps(home), home, dir)); len(c) > 0 {
			t.Errorf("%s changed %v", strings.Join(args, " "), c)
		}
	}
	t.Run("D1", func(t *testing.T) {
		m.t = t
		denies(t, 2, m.work, "aiopsctl", "plugin", "gate", a.unserved, "--against", x.signed)
		denies(t, 2, m.work, "aiopsctl", "plugin", "install", a.unserved, "--profile", "dev")
		dir := filepath.Join(m.root, "unserved")
		write(t, filepath.Join(dir, "dist.yaml"), fmt.Sprintf("release: %s\nplugins:\n  - {module: %s, version: v0.1.0, sum: %s}\n", x.release, echoModule, a.copySum))
		denies(t, 2, dir, "aiopsctl", "dist", "build", "dist.yaml", "--modcache", a.copySrc, "--builder", x.builder)
	})
	t.Run("D2", func(t *testing.T) {
		m.t = t
		denies(t, 10, m.work, "aiopsctl", "plugin", "gate", a.foreign, "--against", x.signed)
		denies(t, 10, m.work, "aiopsctl", "plugin", "install", a.foreign, "--profile", "dev")
	})
	// plugin verify on the artifact of A3 and on the pack of P1, and on a copy of the
	// pack whose fixture changed after signing (ADR-060 decision 31). Verify of
	// in-process code builds with the local toolchain until the builder image takes a
	// request of verify (ADR-060 decision 46, second note of 2026-11-25).
	t.Run("verify", func(t *testing.T) {
		m.t = t
		r := m.must("", "aiopsctl", "plugin", "verify", a.spec, "--against", x.release, "--modcache", x.proxy, "--builder", "local")
		if !strings.Contains(r.out, "step suites: pass: ") {
			t.Errorf("plugin verify of the echoer passed no step suites:\n%s", r.out)
		}
		r = m.must("", "aiopsctl", "plugin", "verify", x.packSpec)
		if !strings.Contains(r.out, "step suites: pass: ") {
			t.Errorf("plugin verify of the pack passed no step suites:\n%s", r.out)
		}
		changedPack := filepath.Join(m.work, "changed")
		copyDir(t, filepath.Join(packer.work, "artifact"), changedPack)
		art, _ := artifactOf(t, changedPack)
		var fixtures ocispec.Descriptor
		for _, l := range manifestOf(t, changedPack, art).Layers {
			if l.MediaType == layerFixtures {
				fixtures = l
			}
		}
		file := filepath.Join(changedPack, "blobs", "sha256", fixtures.Digest.Encoded())
		b := []byte(read(t, file))
		b[len(b)/2] ^= 1
		write(t, file, string(b))
		r = m.run("", "aiopsctl", "plugin", "verify", changedPack)
		if r.code != 1 || !strings.Contains(r.out+r.errOut, fixtures.Digest.String()) {
			t.Errorf("plugin verify of a pack whose fixture changed after signing: exit %d; want exit 1 naming the fixtures layer %s", r.code, fixtures.Digest)
		}
	})
	// C2: install of in-process code answers rebuild-required with the dist.yaml lines
	// and changes no file; dist build passes every check of decision 22 on the path of
	// the job, and builds the same bytes on the other path (ADR-044 decision 3); dist
	// sign and dist use record the site as the signer; install, run again with the
	// site's aiopsctl, appends the bundle with the row disabled.
	ok = t.Run("C2", func(t *testing.T) {
		m.t = t
		before := tree(t, homeKeeps(home), home, m.work)
		r := m.run("", "aiopsctl", "plugin", "install", a.spec, "--profile", "dev")
		lines := "plugins:\n  - {module: " + echoModule + ", version: v0.1.0, sum: " + a.sum + "}\n"
		if r.code != 1 || r.out != lines || !strings.Contains(r.errOut, "rebuild-required") || len(deniedChecks(r.out+r.errOut)) > 0 {
			t.Errorf("plugin install of in-process code: exit %d, stdout %q; want exit 1, rebuild-required and the lines %q", r.code, r.out, lines)
		}
		if c := changed(before, tree(t, homeKeeps(home), home, m.work)); len(c) > 0 {
			t.Errorf("plugin install answering rebuild-required changed %v", c)
		}
		write(t, filepath.Join(site, "dist.yaml"), "release: "+x.release+"\n"+r.out)
		r = m.must(site, "aiopsctl", "dist", "build", "dist.yaml", "--modcache", x.proxy, "--builder", x.builder)
		for _, f := range []string{"aiops/aiops", "aiops/NOTICE", "aiops/aiops.spdx.json", "aiops/THIRD_PARTY_LICENSES/golang.org/toolchain/LICENSE",
			"aiops/THIRD_PARTY_LICENSES/" + echoModule + "/LICENSE", "aiopsctl/aiopsctl", "image/index.json", "distribution.yaml"} {
			if _, err := os.Stat(filepath.Join(site, "build", filepath.FromSlash(f))); err != nil {
				t.Errorf("dist build wrote no %s: %v", f, err)
			}
		}
		names := imageFiles(t, filepath.Join(site, "build", "image"))
		for _, f := range []string{"aiops", "NOTICE", "aiops.spdx.json", "THIRD_PARTY_LICENSES/" + echoModule + "/LICENSE"} {
			if !names[f] {
				t.Errorf("the image that dist build wrote holds no %s", f)
			}
		}
		d, err := boot.ReadDistribution("distribution.yaml", []byte(read(t, filepath.Join(site, "build", "distribution.yaml"))))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := d.Plugins["acme/echo"]; !ok || !slices.ContainsFunc(d.Serves.Definitions["echoers"], func(s boot.ServedDefinition) bool { return s.Version == "0.1" }) {
			t.Errorf("the signed manifest holds no acme/echo or serves no echoers 0.1: %v", d.Serves.Definitions)
		}
		// The path comparison of ADR-044 decision 3: the same inputs on the other path
		// write the same files, the licence reports, the SBOMs and the binaries among them.
		other := map[string]string{"image": "local", "local": "image"}[x.builder]
		again := filepath.Join(m.root, "again")
		write(t, filepath.Join(again, "dist.yaml"), read(t, filepath.Join(site, "dist.yaml")))
		m.must(again, "aiopsctl", "dist", "build", "dist.yaml", "--modcache", x.proxy, "--builder", other)
		l, o := contents(t, filepath.Join(site, "build")), contents(t, filepath.Join(again, "build"))
		for p, sum := range l {
			if o[p] != sum {
				t.Errorf("%s differs between the %s path and the %s path", p, x.builder, other)
			}
		}
		if len(l) != len(o) {
			t.Errorf("the %s path wrote %d files and the %s path %d", x.builder, len(l), other, len(o))
		}
		m.must(site, "aiopsctl", "dist", "sign", filepath.Join(site, "build", "image"), "--key", filepath.Join(m.work, "site.key"))
		r = m.must(site, "aiopsctl", "dist", "use", filepath.Join(site, "build", "image"))
		if !strings.Contains(r.errOut, "restart-required: image") {
			t.Errorf("dist use answered no restart-required for the image:\n%s", r.errOut)
		}
		if cur := strings.TrimSpace(read(t, filepath.Join(home, "verified", "current"))); cur != d.Digest {
			t.Errorf("verified/current is %q; dist use recorded no %s", cur, d.Digest)
		}
		rec := read(t, filepath.Join(home, "verified", "sha256-"+strings.TrimPrefix(d.Digest, "sha256:"), "record.yaml"))
		if !strings.Contains(rec, keyID(t, filepath.Join(m.work, "site.pub"))) {
			t.Errorf("the record of dist use names no site key as the signer:\n%s", rec)
		}
		// The rows of the toy dispatcher, the toy caller and teststate of the test release
		// enter dev through the profile layer of the home (ADR-060 decision 38).
		write(t, filepath.Join(home, "profiles", "dev", "aiops.patch.yml"), "- insert:\n"+
			"    - {id: state, plugin: aiops-test/test-state, roles: [api]}\n"+
			"    - {id: dispatch, plugin: aiops/example-dispatch, roles: [api, worker]}\n"+
			"    - {id: caller, plugin: aiops/example-caller, roles: [api, worker]}\n")
		r = m.must("", siteCtl, "plugin", "install", a.spec, "--profile", "dev")
		if !strings.Contains(r.errOut, "copied the shipped profile dev to profiles/dev/profile.yaml") {
			t.Errorf("install into a home with no dev did not say it copied the shipped one:\n%s", r.errOut)
		}
		dev, err := fs.ReadFile(x.shipped, "profiles/dev/profile.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := read(t, filepath.Join(home, "profiles", "dev", "profile.yaml")), strings.Replace(string(dev), "bundles: [aiops-core]\n", "bundles: [aiops-core, acme/echo]\n", 1); got != want {
			t.Errorf("profiles/dev/profile.yaml of the home is %q; want %q", got, want)
		}
		if b := read(t, filepath.Join(home, "bundles", "acme", "echo", "bundle.yaml")); !strings.Contains(b, "enabled: false") {
			t.Errorf("the bundle install kept holds no disabled row:\n%s", b)
		}
		x.afterDistUse(t, m, home, a)
	})
	if !ok {
		return
	}
	// A site whose dist.yaml adds a consumer of reload, a key that the release binds, and
	// accepts GPL-2.0-only, outside the allow-list, for the departing plugin acme/watch
	// (ADR-060 decision 31): dist build passes, the signed manifest records both, and the
	// site's aiopsctl, on a home that trusts the site key and uses that build, prints both
	// at its boot and profile verify reports the departure.
	t.Run("departures", func(t *testing.T) {
		m.t = t
		dir := filepath.Join(m.root, "departing")
		write(t, filepath.Join(dir, "dist.yaml"), read(t, filepath.Join(site, "dist.yaml"))+
			"  - {module: example.com/acme/watch, version: v0.1.0, sum: "+a.watchSum+"}\n"+
			"bindings:\n  consumers:\n    reload: [acme/watch]\nacceptLicenses: [GPL-2.0-only]\n")
		m.must(dir, "aiopsctl", "dist", "build", "dist.yaml", "--modcache", x.proxy, "--builder", x.builder)
		d, err := boot.ReadDistribution("distribution.yaml", []byte(read(t, filepath.Join(dir, "build", "distribution.yaml"))))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(d.AcceptLicenses, "GPL-2.0-only") || !slices.ContainsFunc(d.Site.Consumers, func(b boot.Binding) bool { return b.Key == "reload" && slices.Contains(b.Plugins, "acme/watch") }) {
			t.Errorf("the signed manifest records acceptLicenses %v and the site's consumers %+v", d.AcceptLicenses, d.Site.Consumers)
		}
		h := m.with("AIOPS_HOME=" + filepath.Join(m.root, "departing-home"))
		h.must("", "aiopsctl", "trust", "add", filepath.Join(m.work, "site.pub"), "--distribution")
		h.must(dir, "aiopsctl", "dist", "sign", filepath.Join(dir, "build", "image"), "--key", filepath.Join(m.work, "site.key"))
		h.must(dir, "aiopsctl", "dist", "use", filepath.Join(dir, "build", "image"))
		r := h.must("", filepath.Join(dir, "build", "aiopsctl", "aiopsctl"), "profile", "verify")
		for _, want := range []string{"the site adds consumers of reload: acme/watch", "licence: the distribution accepts GPL-2.0-only"} {
			if !strings.Contains(r.out+r.errOut, want) {
				t.Errorf("profile verify of the departing site printed no %q", want)
			}
		}
	})
	// The kernel in the container form of ADR-047 decision 15: the image that image.yaml
	// names, run by the host with the home bound read-only at its path, run/ bound
	// read-write over it, the user of the home and AIOPS_HOME.
	kernel := x.startKernel(t, home)
	state := func(t *testing.T, ok func(teststate.State) bool) teststate.State {
		t.Helper()
		var s teststate.State
		for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			b, err := os.ReadFile(filepath.Join(run, teststate.File))
			if err == nil && json.Unmarshal(b, &s) == nil && ok(s) {
				return s
			}
		}
		t.Fatalf("the state of the kernel never met the step: %+v\n%s", s, kernel.logs(t))
		return s
	}
	state(t, func(s teststate.State) bool { return s.Ready && rowState(s, "reload-socket") == "ACTIVE" })
	if st, err := os.Stat(filepath.Join(run, "status.json")); err != nil || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		t.Errorf("run/status.json, written by the kernel in its container, is %v, %v; want it owned by the home's owner (ADR-047 decision 15)", st, err)
	}
	t.Run("C3", func(t *testing.T) {
		m.t = t
		r := m.must("", siteCtl, "plugin", "install", x.packSpec, "--profile", "dev")
		if !strings.HasPrefix(r.out, "result passed, generation ") {
			t.Errorf("install of the pack into the running process answered %q", r.out)
		}
		_, digest, _ := strings.Cut(x.packSpec, "@")
		if b := read(t, filepath.Join(home, "profiles", "dev", "plugins.lock")); !strings.Contains(b, "{id: "+packID+", digest: "+digest+", content: sha256:") {
			t.Errorf("plugins.lock of dev holds no %s at %s with its content:\n%s", packID, digest, b)
		}
		if got := read(t, filepath.Join(home, "content", "globex", "connector-echo", "README.md")); got != read(t, filepath.Join("testdata", "pack", "content", "README.md")) {
			t.Errorf("the content install unpacked is %q", got)
		}
		if got := read(t, filepath.Join(home, "profiles", "dev", "profile.yaml")); !strings.Contains(got, "bundles: [aiops-core, acme/echo, "+packID+"]\n") {
			t.Errorf("profiles/dev/profile.yaml is %q; want the pack's bundle appended", got)
		}
		if b := read(t, filepath.Join(home, "bundles", "globex", "connector-echo", "bundle.yaml")); strings.Contains(b, "enabled: true") || !strings.Contains(b, "enabled: false") {
			t.Errorf("the pack's bundle holds a row that is not disabled:\n%s", b)
		}
		if id := kernel.running(t); id != kernel.id {
			t.Errorf("the kernel container is %s, not %s: an install with no rebuild restarts nothing", id, kernel.id)
		}
	})
	t.Run("C4", func(t *testing.T) {
		m.t = t
		for _, row := range []string{"echo:echo", "connector-echo:example-echoer"} {
			r := m.must("", siteCtl, "plugin", "enable", row, "--profile", "dev")
			var gen uint64
			var dg string
			if _, err := fmt.Sscanf(r.out, "result passed, generation %d, digest %s\n", &gen, &dg); err != nil {
				t.Fatalf("plugin enable %s printed %q, not the final result passed: %v", row, r.out, err)
			}
			s := state(t, func(s teststate.State) bool {
				return s.Reload != nil && s.Reload.Generation >= gen && rowState(s, row) == "ACTIVE"
			})
			if s.Reload.Generation != gen || s.Reload.Status != "passed" || s.Reload.Digest != dg {
				t.Errorf("plugin enable %s names generation %d and digest %s; test-state reports %+v", row, gen, dg, s.Reload)
			}
		}
		s := state(t, func(s teststate.State) bool { return true })
		for _, r := range s.Rows {
			if r.State != "ACTIVE" {
				t.Errorf("the row %s of the composition is %s %s, not ACTIVE", r.ID, r.Mount, r.State)
			}
		}
	})
	t.Run("C5", func(t *testing.T) {
		m.t = t
		msg := []byte("hello from the exit")
		tmp := filepath.Join(run, ".call.json")
		write(t, tmp, string(msg))
		if err := os.Rename(tmp, filepath.Join(run, "call.json")); err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("%x", sha256.Sum256(msg))
		var o struct {
			Request string
			Echoers map[string]struct{ Reply, Error string }
		}
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline) && o.Request != want; time.Sleep(25 * time.Millisecond) {
			if b, err := os.ReadFile(filepath.Join(run, "call.result.json")); err == nil {
				_ = json.Unmarshal(b, &o)
			}
		}
		got := map[string]string{}
		for name, r := range o.Echoers {
			got[name] = r.Reply + r.Error
		}
		if w := map[string]string{"echo": "echo: " + string(msg), "loud": "LOUD " + string(msg)}; o.Request != want || !maps.Equal(got, w) {
			t.Errorf("the caller's round %s answered %v; want %v", o.Request, got, w)
		}
	})
	// C6: the composition holds no policy row and no tenant row: its rows are those of
	// aiops-core, of the site's profile layer and of the two artifacts, none of a policy
	// definition, and each plugin's manifest in the signed manifest of the site has the
	// scope process alone.
	t.Run("C6", func(t *testing.T) {
		m.t = t
		s := state(t, func(s teststate.State) bool { return true })
		d, err := boot.ReadDistribution("distribution.yaml", []byte(read(t, filepath.Join(site, "build", "distribution.yaml"))))
		if err != nil {
			t.Fatal(err)
		}
		var plugins []string
		for _, r := range s.Rows {
			if r.Plugin == "core:group" {
				continue
			}
			plugins = append(plugins, r.Plugin)
			if man, err := boot.ReadManifest(r.Plugin, d.Plugins[r.Plugin].Manifest, boot.ManifestLenient); err != nil || !slices.Equal(man.Scope, []string{"process"}) {
				t.Errorf("the row %s of %s has the scope %v, %v; want process alone", r.ID, r.Plugin, man.Scope, err)
			}
		}
		slices.Sort(plugins)
		want := []string{"acme/echo", "aiops-test/test-state", "aiops/clock", "aiops/config-refs", "aiops/example-caller", "aiops/example-dispatch",
			"aiops/example-echoer", "aiops/readiness", "aiops/reload", "aiops/reload-socket", "aiops/restart-policy"}
		if !slices.Equal(plugins, want) {
			t.Errorf("the composition's rows are of %v; want %v", plugins, want)
		}
	})
	// What the manager cannot disable (ADR-061 decision 17; ADR-062 decision 12): a row
	// of aiops-cli in the profile cli, reload-socket and reload; each writes nothing and
	// exits 1.
	t.Run("cannot-disable", func(t *testing.T) {
		m.t = t
		for _, args := range [][]string{{"command-plugin", "--profile", "cli"}, {"reload-socket", "--profile", "dev"}, {"reload", "--profile", "dev"}} {
			before := tree(t, homeKeeps(home), home)
			r := m.run("", append([]string{siteCtl, "plugin", "disable"}, args...)...)
			if r.code != 1 {
				t.Errorf("plugin disable %s: exit %d; want 1", strings.Join(args, " "), r.code)
			}
			if c := changed(before, tree(t, homeKeeps(home), home)); len(c) > 0 {
				t.Errorf("plugin disable %s changed %v", strings.Join(args, " "), c)
			}
		}
	})
	// The site's aiopsctl disables and removes, on the running process, what it enabled
	// and installed (ADR-061 decisions 17 and 26; ADR-062 decision 11): disable of the
	// pack's row and remove of the pack each answer passed, and remove takes the pack's
	// bundle and lock entry out of dev and deletes the files that install kept.
	t.Run("disable-remove", func(t *testing.T) {
		m.t = t
		row := "connector-echo:example-echoer"
		if r := m.must("", siteCtl, "plugin", "disable", row, "--profile", "dev"); !strings.HasPrefix(r.out, "result passed, generation ") {
			t.Errorf("plugin disable %s answered %q", row, r.out)
		}
		state(t, func(s teststate.State) bool { return rowState(s, row) == "" })
		if r := m.must("", siteCtl, "plugin", "remove", packID, "--profile", "dev"); !strings.HasPrefix(r.out, "result passed, generation ") {
			t.Errorf("plugin remove %s answered %q", packID, r.out)
		}
		if p := read(t, filepath.Join(home, "profiles", "dev", "profile.yaml")) + read(t, filepath.Join(home, "profiles", "dev", "plugins.lock")); strings.Contains(p, packID) {
			t.Errorf("dev still names %s after remove:\n%s", packID, p)
		}
		for _, d := range []string{"bundles", "content"} {
			if _, err := os.Stat(filepath.Join(home, d, "globex", "connector-echo")); err == nil {
				t.Errorf("remove left %s/%s", d, packID)
			}
		}
		state(t, func(s teststate.State) bool {
			return !slices.ContainsFunc(s.Rows, func(r teststate.Row) bool { return strings.HasPrefix(r.ID, "connector-echo") })
		})
	})
}

// afterDistUse runs, on a home after dist use of the site's image, the release aiopsctl
// and the site's (ADR-061 decisions 26 and 40): the release aiopsctl refuses profile
// render, profile verify, plugin install, enable, disable and remove, and still runs
// the other commands; the site's aiopsctl runs them, and installs in-process code into
// the profile cli with its rows disabled.
func (x *exit) afterDistUse(t *testing.T, m *machine, home string, a authored) {
	siteCtl := filepath.Join(m.root, "site", "build", "aiopsctl", "aiopsctl")
	for _, args := range [][]string{{"profile", "render", "--profile", "dev"}, {"profile", "verify"}, {"plugin", "install", x.packSpec, "--profile", "dev"},
		{"plugin", "enable", "echo:echo", "--profile", "dev"}, {"plugin", "disable", "echo:echo", "--profile", "dev"}, {"plugin", "remove", "acme/echo", "--profile", "dev"}} {
		before := tree(t, homeKeeps(home), home)
		r := m.run("", append([]string{"aiopsctl"}, args...)...)
		if r.code != 1 || !strings.Contains(r.errOut, "which dist use made current") || !strings.Contains(r.errOut, "run the aiopsctl that dist build wrote") {
			t.Errorf("the release aiopsctl, %s, on a home after dist use: exit %d; want 1 and a refusal that names the site's aiopsctl", strings.Join(args, " "), r.code)
		}
		if c := changed(before, tree(t, homeKeeps(home), home)); len(c) > 0 {
			t.Errorf("the release aiopsctl, %s, changed %v", strings.Join(args, " "), c)
		}
	}
	m.must("", "aiopsctl", "plugin", "inspect", a.spec)
	m.must("", siteCtl, "profile", "verify")
	m.must("", siteCtl, "profile", "render", "--profile", "cli")
	r := m.must("", siteCtl, "plugin", "install", a.spec, "--profile", "cli")
	if got := read(t, filepath.Join(home, "profiles", "cli", "profile.yaml")); !strings.Contains(got, "acme/echo]") {
		t.Errorf("install of in-process code into cli: %q\n%s", got, r.errOut)
	}
}

// kernelContainer is the kernel of the clean machine.
type kernelContainer struct{ id, image string }

func (k kernelContainer) logs(t *testing.T) string {
	out, _ := exec.Command("docker", "logs", k.id).CombinedOutput()
	return string(out)
}

// running returns the id of the running container, "" when it does not run.
func (k kernelContainer) running(t *testing.T) string {
	out, err := exec.Command("docker", "inspect", "-f", "{{if .State.Running}}{{.Id}}{{end}}", k.id).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// startKernel starts the image that image.yaml of home names in the container form
// of ADR-047 decision 15: the host pushes the layout that dist build wrote to the
// registry of the exit, and the container runtime pulls it by its digest.
func (x *exit) startKernel(t *testing.T, home string) kernelContainer {
	t.Helper()
	img, ok := strings.CutPrefix(strings.TrimSpace(read(t, filepath.Join(home, "image.yaml"))), "image: ")
	layout, digest, _ := strings.Cut(img, "@")
	if !ok || !filepath.IsAbs(layout) {
		t.Fatalf("image.yaml names %q, not the layout that dist build wrote", img)
	}
	pushLayout(t, layout, x.reg+"/site/aiops")
	ref := x.reg + "/site/aiops@" + digest
	x.images = append(x.images, ref)
	name := fmt.Sprintf("aiops-exit-%d", time.Now().UnixNano())
	run := filepath.Join(home, "run")
	id := docker(t, "run", "-d", "--name", name, "--network", "none", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", home+":"+home+":ro", "-v", run+":"+run, "-e", "AIOPS_HOME="+home, ref, "--profile", "dev")
	k := kernelContainer{id: id, image: ref}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the kernel container:\n%s", k.logs(t))
		}
		_ = exec.Command("docker", "rm", "-f", id).Run()
	})
	return k
}

func rowState(s teststate.State, id string) string {
	for _, r := range s.Rows {
		if r.ID == id {
			return r.State
		}
	}
	return ""
}

// deniedChecks returns the checks that the denials of out name: a clause whose outcome
// is deny, as plugin gate and the gate of dist build print it, and the refusal that
// aiopsctl prints as its error, but no clause that is not evaluated.
func deniedChecks(out string) []int {
	var got []int
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "not evaluated") || !strings.Contains(l, ": deny") && !strings.HasPrefix(strings.TrimSpace(l), "aiopsctl: ") {
			continue
		}
		for _, mm := range checkRE.FindAllStringSubmatch(l, -1) {
			var n int
			fmt.Sscan(mm[1], &n)
			got = append(got, n)
		}
	}
	slices.Sort(got)
	return slices.Compact(got)
}

var checkRE = regexp.MustCompile(`\bcheck (\d+)\b`)

// artifactOf returns the artifact of the layout dir, its descriptor and its layers by
// media type.
func artifactOf(t *testing.T, dir string) (ocispec.Descriptor, map[string][]byte) {
	t.Helper()
	for _, d := range readIndex(t, dir) {
		m := manifestOf(t, dir, d)
		if m.Subject == nil && m.ArtifactType == "application/vnd.aiops.plugin.v1" {
			layers := map[string][]byte{}
			for _, l := range m.Layers {
				layers[l.MediaType] = blob(t, dir, l)
			}
			return d, layers
		}
	}
	t.Fatalf("the layout %s holds no artifact", dir)
	return ocispec.Descriptor{}, nil
}

// statement is what the exit reads of an attestation.
type statement struct {
	Name, Subject string
	Predicate     struct {
		Definitions map[string]any
		NotRun      []string
	}
}

// attestationOf returns the attestation of art in the layout dir, from the payload of
// the bundle that plugin sign wrote.
func attestationOf(t *testing.T, dir string, art ocispec.Descriptor) statement {
	t.Helper()
	for _, d := range readIndex(t, dir) {
		m := manifestOf(t, dir, d)
		if m.Subject == nil || m.Subject.Digest != art.Digest || m.Annotations[predicateKey] != attestationURI {
			continue
		}
		var b struct{ DsseEnvelope struct{ Payload string } }
		if err := json.Unmarshal(blob(t, dir, m.Layers[0]), &b); err != nil {
			t.Fatal(err)
		}
		payload, err := base64.StdEncoding.DecodeString(b.DsseEnvelope.Payload)
		var raw struct {
			Subject []struct {
				Name   string
				Digest map[string]string
			}
			Predicate struct {
				Definitions map[string]any
				NotRun      []string
			}
		}
		if err == nil {
			err = json.Unmarshal(payload, &raw)
		}
		if err != nil || len(raw.Subject) != 1 {
			t.Fatalf("the attestation of %s: %v\n%s", dir, err, payload)
		}
		return statement{Name: raw.Subject[0].Name, Subject: raw.Subject[0].Digest["sha256"], Predicate: raw.Predicate}
	}
	t.Fatalf("the layout %s holds no signed attestation of %s", dir, art.Digest)
	return statement{}
}

// rewrite copies the unsigned layout from, which plugin pack wrote, into to, with the
// layers of its artifact that edit changes, by media type, and the statement that pack
// recorded naming the new artifact, its definitions changed by defs.
func rewrite(t *testing.T, from, to string, edit map[string]func([]byte) []byte, defs func(map[string]any)) {
	t.Helper()
	copyDir(t, from, to)
	art, _ := artifactOf(t, to)
	m := manifestOf(t, to, art)
	for i, l := range m.Layers {
		if f := edit[l.MediaType]; f != nil {
			d := put(t, to, l.MediaType, f(blob(t, to, l)))
			d.Annotations = l.Annotations
			m.Layers[i] = d
		}
	}
	newArt := putManifest(t, to, m, art.Annotations)
	var idx []ocispec.Descriptor
	for _, d := range readIndex(t, to) {
		sm := manifestOf(t, to, d)
		switch {
		case d.Digest == art.Digest:
			idx = append(idx, newArt)
		case sm.Subject != nil && sm.Subject.Digest == art.Digest && sm.ArtifactType == statementType:
			var st map[string]any
			if err := json.Unmarshal(blob(t, to, sm.Layers[0]), &st); err != nil {
				t.Fatal(err)
			}
			st["subject"].([]any)[0].(map[string]any)["digest"] = map[string]any{"sha256": newArt.Digest.Encoded()}
			if defs != nil {
				defs(st["predicate"].(map[string]any)["definitions"].(map[string]any))
			}
			b, _ := json.Marshal(st)
			sm.Layers[0] = put(t, to, statementType, b)
			sm.Subject = &ocispec.Descriptor{MediaType: newArt.MediaType, Digest: newArt.Digest, Size: newArt.Size, ArtifactType: newArt.ArtifactType}
			idx = append(idx, putManifest(t, to, sm, d.Annotations))
		default:
			idx = append(idx, d)
		}
	}
	writeIndex(t, to, idx)
}

// swapAttestation replaces, in the signed layout dir, the attestation of its artifact
// with the signed attestation of the artifact of the signed layout other, which names
// that other artifact.
func swapAttestation(t *testing.T, dir, other string) {
	t.Helper()
	art, _ := artifactOf(t, dir)
	oart, _ := artifactOf(t, other)
	var att ocispec.Manifest
	for _, d := range readIndex(t, other) {
		if m := manifestOf(t, other, d); m.Subject != nil && m.Subject.Digest == oart.Digest && m.Annotations[predicateKey] == attestationURI {
			att = m
		}
	}
	if att.Subject == nil {
		t.Fatalf("the layout %s holds no signed attestation", other)
	}
	b := put(t, dir, bundleType, blob(t, other, att.Layers[0]))
	att.Layers[0].Digest, att.Layers[0].Size = b.Digest, b.Size
	att.Subject = &ocispec.Descriptor{MediaType: art.MediaType, Digest: art.Digest, Size: art.Size, ArtifactType: art.ArtifactType}
	var idx []ocispec.Descriptor
	for _, d := range readIndex(t, dir) {
		if m := manifestOf(t, dir, d); m.Subject != nil && m.Annotations[predicateKey] == attestationURI {
			continue
		}
		idx = append(idx, d)
	}
	writeIndex(t, dir, append(idx, putManifest(t, dir, att, nil)))
}

// imageFiles returns the names of the files of the one layer of the image tagged
// latest in the layout dir.
func imageFiles(t *testing.T, dir string) map[string]bool {
	t.Helper()
	for _, d := range readIndex(t, dir) {
		if d.Annotations[ocispec.AnnotationRefName] != "latest" {
			continue
		}
		m := manifestOf(t, dir, d)
		if len(m.Layers) != 1 {
			t.Fatalf("the image %s has %d layers: one, on no base layer", d.Digest, len(m.Layers))
		}
		return tarNames(t, blob(t, dir, m.Layers[0]))
	}
	t.Fatalf("the layout %s holds no image tagged latest", dir)
	return nil
}

// bundleRows returns the rows that the insert items of a bundle insert, read by boot's
// reader of a config, under a key that makes the list a mapping.
func bundleRows(t *testing.T, src []byte) []map[string]any {
	t.Helper()
	tree, err := boot.ParseConfig("bundle.yaml", []byte("items:\n  "+strings.ReplaceAll(string(src), "\n", "\n  ")))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	items, _ := tree.(map[string]any)["items"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		ins, _ := m["insert"].([]any)
		for _, r := range ins {
			row, _ := r.(map[string]any)
			rows = append(rows, row)
		}
	}
	return rows
}

// tarNames returns the names of the entries of the tar b, directories without their
// slash.
func tarNames(t *testing.T, b []byte) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for r := tar.NewReader(bytes.NewReader(b)); ; {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names[strings.TrimSuffix(h.Name, "/")] = true
	}
}

func keyID(t *testing.T, pubFile string) string {
	t.Helper()
	b, _ := pem.Decode([]byte(read(t, pubFile)))
	if b == nil {
		t.Fatalf("%s holds no PEM block", pubFile)
	}
	if _, err := x509.ParsePKIXPublicKey(b.Bytes); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b.Bytes)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, p, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
