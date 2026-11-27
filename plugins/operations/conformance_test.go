// SPDX-License-Identifier: Apache-2.0

package operations_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mutex-solutions/aiops-platform/plugins/operations"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0/conformance"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// The operations row passes the operations definition's suite, as its fake does (T2,
// 07-c4-plugin-sdk.md §8.2; ADR-061 decision 18), on a home in a temporary directory
// that each case's Setup prepares as the files a writer leaves, with the launch fake's
// toolbox answering as the Setup says, and a registry inside the test that holds the
// images of the Setup.
func TestConformance(t *testing.T) { conformance.Run(t, MountOperations) }

// MountOperations mounts the operations row on a root of its own for the suite of operations, as step 5 of plugin
// check does.
func MountOperations(t *testing.T, s conformance.Setup) conformance.Subject {
	home := filepath.Join(t.TempDir(), "home")
	if s.Result != nil || s.Socket != "" {
		home = serve(t, s.Result, s.Socket)
	}
	if s.Interrupted {
		interrupted(t, home)
	}
	if s.Held != "" {
		hold(t, home, s.Held)
	}
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	l.Box = box{s: s}
	kerneltest.Mount(root, lp)
	f := root.Plugin(operations.Plugin, map[string]any{"lockWait": s.LockWait.String()})
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	if st, err := f.State(t.Context()); err != nil || st != kernel.Active {
		t.Fatalf("the operations row is %v, %v", st, err)
	}
	reg := ocitest.New(t, ocitest.Options{})
	for name, manifest := range s.Images {
		image(reg, name, "dist", manifest)
	}
	return conformance.Subject{Root: root, Provider: f, Home: home, Dir: t.TempDir(), Registry: reg.Host}
}

// image pushes an image to the repository name under tag, with its distribution
// manifest as a referrer with no bundle, as the builder leaves it (ADR-044 decision 1).
func image(reg *ocitest.Registry, name, tag string, manifest []byte) ocitest.Descriptor {
	img := reg.Artifact(name, tag, "application/vnd.example.image.v1", nil, ocitest.Layer{MediaType: "application/vnd.example.layer.v1", Data: []byte(name)})
	reg.Artifact(name, "", def.DistributionType, &img, ocitest.Layer{MediaType: def.DistributionLayer, Data: manifest})
	return img
}

// box is the launcher's toolbox of a case: Render answers the Rendered of the Setup for
// the profile, Shipped its shipped files, ReadManifest its manifest, Release a release
// that serves echoers, and the gate passes every clause; the suites are not carried, as in an aiopsctl built before
// they land, and the module zip answers a module of the plugin directory, as that of
// aiopsctl does.
type box struct {
	launch.NoTools
	s conformance.Setup
}

// Build answers what the Setup says the builder built.
func (b box) Build(ctx context.Context, _ launch.BuildRequest) (launch.Built, error) {
	return b.s.Built, ctx.Err()
}

func (box) Module(ctx context.Context, q launch.ModuleRequest) (launch.Module, error) {
	return launch.Module{Path: "example.test/echo", Version: q.Version, Sum: "h1:Ryl9hfaUbtcrXqV18lzBbPaztAz29w8OPhu5dRdlIXM="}, ctx.Err()
}

func (b box) Render(ctx context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	return b.s.Rendered[q.Profile], ctx.Err()
}

func (b box) Shipped(ctx context.Context) (map[string][]byte, error) { return b.s.Shipped, ctx.Err() }

// Gate answers the verdict of the Setup at plugin install; at plugin check it passes
// every clause it evaluates, and denies check 10 where the attestation does not name the
// plugin of the Setup, under the id that its manifest names.
func (b box) Gate(ctx context.Context, q launch.GateRequest) (launch.Verdict, error) {
	if q.Entry == "plugin install" {
		return b.s.Verdict, ctx.Err()
	}
	v := launch.Verdict{Entry: q.Entry}
	for _, n := range []int{1, 2, 5, 7, 8, 10} {
		v.Clauses = append(v.Clauses, launch.Clause{Check: n, Outcome: launch.OutcomePass})
	}
	if !strings.Contains(string(q.Attestation), idOf(q.Manifest, b.s.Plugin.Summary.ID)+"@"+b.s.Plugin.Summary.Version) {
		v.Clauses[5].Outcome = launch.OutcomeDeny
	}
	return v, ctx.Err()
}

// ReadManifest answers the summary of the Setup under the id that plugin.yaml of the
// directory names, as boot's reader would, so that a case packs another owner's copy.
func (b box) ReadManifest(ctx context.Context, q launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	sum := b.s.Plugin.Summary
	f, _ := os.ReadFile(filepath.Join(q.Dir, "plugin.yaml"))
	sum.ID = idOf(f, sum.ID)
	return sum, nil, ctx.Err()
}

// idOf returns the id that the manifest names, or id where it names none.
func idOf(manifest []byte, id string) string {
	for _, l := range strings.Split(string(manifest), "\n") {
		if v, ok := strings.CutPrefix(l, "id: "); ok {
			return v
		}
	}
	return id
}

func (b box) Template(ctx context.Context, q launch.TemplateRequest) (map[string][]byte, error) {
	if q.Registry != "echoers" || q.Lang != "go" {
		return nil, fmt.Errorf("no template for %s in %s", q.Registry, q.Lang)
	}
	return b.s.Template, ctx.Err()
}

func (box) Generate(ctx context.Context, _ launch.GenerateRequest) ([]launch.Diagnostic, error) {
	return nil, ctx.Err()
}

func (box) Vet(ctx context.Context, _ string) ([]launch.Diagnostic, error) { return nil, ctx.Err() }

// Suite runs the plugin's own tests, which skip Skipped, or the suite of echoers, which
// fails Failed.
func (b box) Suite(ctx context.Context, q launch.SuiteRequest) (launch.SuiteReport, error) {
	if len(q.Definitions) == 0 {
		return launch.SuiteReport{Definitions: []launch.SuiteResult{{Skipped: b.s.Plugin.Skipped}}}, ctx.Err()
	}
	return launch.SuiteReport{Definitions: []launch.SuiteResult{{Name: "echoers", Revision: 1, Halves: []string{"obligations"},
		Failed: b.s.Plugin.Failed}}}, ctx.Err()
}

// Release answers a release that serves the key echoers under echoers, from which step
// 5 maps the key that the plugin of plugin check registers with to its definition.
func (box) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{Digest: "sha256:ab", Manifest: []byte("serves:\n  definitions:\n    echoers:\n      - version: \"0.1\"\n" +
		"        classes:\n          echoers: {provider: open, consumer: open}\n")}, nil
}

// serve makes a home in which a process runs dev and answers each submission on its
// socket with the result queued, then res (ADR-062 decisions 6 and 9), or as socket
// says (conformance.Setup), and returns it. The home is under the system's temporary
// directory, so its socket path stays under the 107 bytes of Linux.
func serve(t *testing.T, res *reload.WireResult, socket string) string {
	t.Helper()
	home, err := os.MkdirTemp("", "ops-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	run := filepath.Join(home, reload.RunDir)
	if err := os.Mkdir(run, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, reload.StatusFile), reload.Line(reload.ProcessStatus{Phase: "running", Profile: "dev"}), 0o600); err != nil {
		t.Fatal(err)
	}
	if socket == def.ReasonNoAnswer { // no socket answers
		return home
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(run, reload.SocketFile), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	reply := func(c net.Conn, r *reload.WireResult, e *reload.WireError) {
		_, _ = c.Write(reload.Line(reload.Reply{V: reload.WireVersion, Result: r, Error: e}))
	}
	go func() {
		for id := reload.ID(1); ; id++ {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadBytes('\n')
			q, _ := reload.DecodeRequest(line)
			switch queued := (&reload.WireResult{ID: id, Status: reload.Queued}); {
			case socket == def.SubmitRefused:
				reply(c, nil, reload.NewError("the queue is full", reload.CauseQueueFull))
			case socket == def.NoFinalResult && q.Op == reload.OpAwait:
				reply(c, nil, reload.NewError("no operation has that id", reload.CauseUnknown))
			case socket == def.NoFinalResult:
				reply(c, queued, nil)
			case socket == def.VerdictUnknown: // the id, and nothing more until the client goes
				reply(c, queued, nil)
				_, _ = io.Copy(io.Discard, c)
			default:
				r := *res
				r.Status = reload.Queued
				reply(c, &r, nil)
				reply(c, res, nil)
			}
			_ = c.Close()
		}
	}()
	return home
}

func mkhome(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
}

// interrupted writes what a trust add killed after its rename leaves (ADR-061
// decision 17): trust.yaml with one signer for left/*, which the home did not hold
// before, and the snapshot that lists trust.yaml as created.
func interrupted(t *testing.T, home string) {
	t.Helper()
	mkhome(t, home)
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	pub := strings.TrimSuffix(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "\n")
	trust := "signers:\n  - key: |\n      " + strings.ReplaceAll(pub, "\n", "\n      ") + "\n    ids: [left/*]\n"
	if err := os.WriteFile(filepath.Join(home, "trust.yaml"), []byte(trust), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "snapshot"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "snapshot", "paths.json"), []byte(`{"Replaced": null, "Created": ["trust.yaml"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// hold takes write.lock of home for the rest of the test, as another writer would,
// with its record naming command.
func hold(t *testing.T, home, command string) {
	t.Helper()
	mkhome(t, home)
	f, err := os.OpenFile(filepath.Join(home, "write.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("4242 " + command + "\n"); err != nil {
		t.Fatal(err)
	}
}
