// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/builder"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/test/modsrc"
)

// The test release (ADR-060 decisions 28 and 38): the builder takes test/release/dist.yaml,
// a release input in the form of dist/dist.yaml, and builds it from the repository's
// modules served at testVersion in two phases with a throwaway release key, as it builds
// the release (ADR-060 decisions 20 and 22; ADR-020 decision 13). The first phase runs
// the gate of dist build on a row of each plugin of the module table, printing each
// clause it does not evaluate, and the source pass, the import boundary and the suites
// and generated tests of each plugin, as on every release; its manifest's module table
// is the input's, the release table plus the dispatcher, the instantiable echoer, the
// caller and teststate. The second writes the server and the aiopsctl of that
// distribution, and that aiopsctl boots on its built-in record with no trust.yaml and
// runs the case of the connector template of the template job on its pack. It
// builds from nothing with the pinned toolchain module, so -short skips it and the CI job
// builder runs it.
func TestTestRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the test release in two phases with the pinned toolchain; the CI job builder runs it")
	}
	host := goEnv(t, "GOMODCACHE")
	if _, err := os.Stat(filepath.Join(host, "cache", "download", builder.Toolchain, "@v", builder.ToolchainVersion+".zip")); err != nil {
		t.Skipf("the module cache holds no %s@%s: go mod download it to run this case (the CI job builder does)", builder.Toolchain, builder.ToolchainVersion)
	}
	ctx, repo := context.Background(), abs(t, filepath.Join("..", ".."))
	src, cache := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("GOPROXY", "")
	if err := builder.Serve(repo, testVersion, src); err != nil {
		t.Fatal(err)
	}
	modsrc.Fill(t, repo, src)
	input, err := os.ReadFile(filepath.Join(repo, "test", "release", "dist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	in, err := boot.ParseDist("test/release/dist.yaml", input)
	if err != nil {
		t.Fatal(err)
	}
	def, err := os.ReadFile(filepath.Join(repo, "dist", "builder", "image.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	var progress bytes.Buffer
	q := builder.Request{Input: input, Root: testVersion, ModCache: src, Cache: filepath.Join(cache, "aiopsctl"), Out: filepath.Join(t.TempDir(), "first"), Progress: &progress}
	first, err := builder.Build(ctx, q)
	if err != nil {
		t.Fatalf("the first phase: %v\n%s", err, progress.String())
	}
	for _, want := range []string{"gate: check 4 Stability: not evaluated at dist build: no profile is composed\n",
		"gate: check 10 Attestation: not evaluated at dist build: ", "gate: check 7 Key and hook classes: not evaluated at dist build: signed: ",
		"boundary: the import boundary of github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate",
		"suites: go test github.com/mutex-solutions/aiops-platform/examples/caller "} {
		if !strings.Contains(progress.String(), want) {
			t.Errorf("the first phase printed no %q:\n%s", want, progress.String())
		}
	}
	d, err := boot.ReadDistribution("the test release manifest", first.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var pkgs []string
	for _, p := range d.Plugins {
		pkgs = append(pkgs, p.Package)
	}
	if slices.Sort(pkgs); !slices.Equal(pkgs, in.Modules) {
		t.Errorf("the module table holds %v, the input lists %v", pkgs, in.Modules)
	}
	for _, id := range []string{"aiops/example-dispatch", "aiops/example-echoer", "aiops/example-caller", "aiops-test/test-state", "aiops/clock", "aiops/cli"} {
		if _, ok := d.Plugins[id]; !ok {
			t.Errorf("the module table lacks %s: %v", id, slices.Sorted(maps.Keys(d.Plugins)))
		}
	}
	if m, err := boot.ReadManifest("echoer", d.Plugins["aiops/example-echoer"].Manifest, boot.ManifestLenient); err != nil || !m.Instantiable {
		t.Errorf("the echoer of the test release is not instantiable: %v", err)
	}

	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	sum := sha256.Sum256(der)
	bundle := []byte(`{"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json", "note": "the bundle of the test release manifest of this case"}`)
	q.Out = filepath.Join(t.TempDir(), "release")
	q.Signed = &builder.Signed{Manifest: first.Manifest, Bundle: bundle, Key: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), Definition: def}
	if _, err := builder.Build(ctx, q); err != nil {
		t.Fatalf("the second phase: %v\n%s", err, progress.String())
	}
	cmd := exec.Command(filepath.Join(q.Out, "aiopsctl", "aiopsctl"), "profile", "verify")
	cmd.Env = []string{"AIOPS_HOME=" + t.TempDir(), "HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
	b, err := cmd.CombinedOutput()
	want := fmt.Sprintf("the default release root, key %s, is trusted; the built-in record of %s is in use", base64.StdEncoding.EncodeToString(sum[:]), d.Digest)
	if err != nil || !strings.Contains(string(b), want) {
		t.Errorf("the aiopsctl of the test release with no trust.yaml: %v, want %q\n%s", err, want, b)
	}
	if _, err := os.Stat(filepath.Join(q.Out, "aiops", "aiops")); err != nil {
		t.Errorf("the second phase wrote no server: %v", err)
	}

	// The case of the connector template of the template job, which this job runs since it
	// builds the release (ADR-060 decision 24, note of 2026-11-26): plugin new connector of
	// the release's aiopsctl writes a pack of the release's instantiable echoer, and
	// plugin check fails each obligation of echoers once, on the suite binary that the
	// release embeds.
	work := t.TempDir()
	ctl := func(dir string, args ...string) (int, string) {
		cmd := exec.Command(filepath.Join(q.Out, "aiopsctl", "aiopsctl"), args...)
		cmd.Dir, cmd.Env = dir, []string{"AIOPS_HOME=" + t.TempDir(), "HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
		b, err := cmd.CombinedOutput()
		if err != nil && cmd.ProcessState == nil {
			t.Fatal(err)
		}
		return cmd.ProcessState.ExitCode(), string(b)
	}
	if code, out := ctl(work, "plugin", "new", "connector", "acme/job-connector"); code != 0 {
		t.Fatalf("plugin new connector of the test release: exit %d\n%s", code, out)
	}
	code, out := ctl(filepath.Join(work, "job-connector"), "plugin", "check")
	var failed []string
	for _, l := range strings.Split(out, "\n") {
		if id, ok := strings.CutSuffix(strings.TrimSpace(l), ": the obligation failed"); ok {
			failed = append(failed, id)
		}
	}
	if code != 1 || !strings.Contains(out, "step 5 conformance: fail\n") || !slices.Equal(failed, echoers.Obligations) {
		t.Errorf("plugin check of the connector pack: exit %d, the failed obligations %v; want step 5 failing each of %v once\n%s", code, failed, echoers.Obligations, out)
	}
}
