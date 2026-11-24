// SPDX-License-Identifier: Apache-2.0

package operations_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
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
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/ocitest"
)

// The operations row passes the operations definition's suite, as its fake does (T2,
// 07-c4-plugin-sdk.md §8.2; ADR-061 decision 18), on a home in a temporary directory
// that each case's Setup prepares as the files a writer leaves, with the launch fake's
// toolbox answering as the Setup says, and a registry inside the test that holds the
// images of the Setup.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) conformance.Subject {
		home := filepath.Join(t.TempDir(), "home")
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
	})
}

// image pushes an image to the repository name under tag, with its distribution
// manifest as a referrer with no bundle, as the builder leaves it (ADR-044 decision 1).
func image(reg *ocitest.Registry, name, tag string, manifest []byte) ocitest.Descriptor {
	img := reg.Artifact(name, tag, "application/vnd.example.image.v1", nil, ocitest.Layer{MediaType: "application/vnd.example.layer.v1", Data: []byte(name)})
	reg.Artifact(name, "", def.DistributionType, &img, ocitest.Layer{MediaType: def.DistributionLayer, Data: manifest})
	return img
}

// box is the launcher's toolbox of a case: Render answers the Rendered of the Setup for
// the profile, Shipped its shipped files, ReadManifest its manifest, and the gate
// passes every clause; the suites are not carried, as in an aiopsctl built before
// they land.
type box struct {
	launch.NoTools
	s conformance.Setup
}

func (b box) Render(ctx context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	return b.s.Rendered[q.Profile], ctx.Err()
}

func (b box) Shipped(ctx context.Context) (map[string][]byte, error) { return b.s.Shipped, ctx.Err() }

func (box) Gate(ctx context.Context, q launch.GateRequest) (launch.Verdict, error) {
	return launch.Verdict{Entry: q.Entry}, ctx.Err()
}

func (b box) ReadManifest(ctx context.Context, _ launch.ManifestRequest) (launch.ManifestSummary, []launch.Diagnostic, error) {
	return b.s.Manifest, nil, ctx.Err()
}

func (box) Release(context.Context, string) (launch.Release, error) {
	return launch.Release{}, launch.ErrNotCarried
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
