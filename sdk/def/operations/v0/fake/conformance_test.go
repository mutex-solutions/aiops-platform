// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the suite that the operations row passes (ADR-061 decision 18).
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) conformance.Subject {
		const registry = "registry.fake"
		images := map[string][]byte{}
		for name, manifest := range s.Images {
			images[registry+"/"+name+":dist"] = manifest
		}
		_, p := fake.NewWith(fake.Config{LockWait: s.LockWait, Held: s.Held, Interrupted: s.Interrupted, Shipped: s.Shipped, Rendered: s.Rendered,
			Images: images, Artifact: s.Artifact,
			Template: s.Template, Summary: s.Plugin.Summary, Failed: s.Plugin.Failed, Skipped: s.Plugin.Skipped,
			Verdict: s.Verdict, Result: s.Result, Socket: s.Socket, Built: s.Built, Running: s.Result != nil || s.Socket != ""})
		root := kerneltest.New(t)
		f := root.Plugin(p, nil)
		deftest.Active(t, f)
		return conformance.Subject{Root: root, Provider: f, Home: filepath.Join(t.TempDir(), "home"), Dir: t.TempDir(), Registry: registry}
	})
}

// The fake refuses a remove from a profile whose Rendered holds an error, as the row
// keeps a change only when the changed profile renders, and writes nothing. Its
// install, which renders nothing, puts the artifact there first, which the row does
// not, so the suite has no such case.
func TestRemoveFromAProfileThatDoesNotCompose(t *testing.T) {
	dev := launch.Rendered{Diagnostics: []launch.Diagnostic{{Check: "compose", Text: "acme/gone is no plugin"}}}
	_, p := fake.NewWith(fake.Config{Shipped: conformance.Shipped, Rendered: map[string]launch.Rendered{"dev": dev}, Artifact: conformance.Artifact})
	root := kerneltest.New(t)
	deftest.Active(t, root.Plugin(p, nil))
	_, _, ops := deftest.Consume(t, root, operations.Key)
	ctx, dir, home := t.Context(), t.TempDir(), filepath.Join(t.TempDir(), "home")
	c := operations.Caller{Env: func(n string) (string, bool) { return "pw", n == operations.KeyPasswordVar }}
	key, out := filepath.Join(dir, "author"), filepath.Join(dir, "out")
	if err := os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ops.GenerateKey(ctx, operations.KeyRequest{Caller: c, Prefix: key})
	if err == nil {
		_, err = ops.AddTrust(ctx, operations.TrustRequest{Caller: c, Home: home, KeyFile: key + ".pub", IDs: []string{"acme/*"}})
	}
	if err == nil {
		_, err = ops.Pack(ctx, operations.PackRequest{Caller: c, Dir: dir, Out: out})
	}
	if err == nil {
		_, err = ops.Sign(ctx, operations.SignRequest{Caller: c, Out: out, Key: key + ".key"})
	}
	if err == nil {
		_, err = ops.Install(ctx, operations.InstallRequest{Caller: c, Home: home, Profile: "dev", Spec: out})
	}
	if err != nil {
		t.Fatal(err)
	}
	before, _ := ops.Home(ctx, operations.HomeRequest{Home: home, Profile: "dev"})
	_, err = ops.Remove(ctx, operations.RemoveRequest{Caller: c, Home: home, Profile: "dev", ID: conformance.Artifact.ID})
	if after, _ := ops.Home(ctx, operations.HomeRequest{Home: home, Profile: "dev"}); !errors.Is(err, operations.ErrRefused) || !reflect.DeepEqual(after, before) {
		t.Errorf("Remove from a profile that does not compose: %v; want ErrRefused and nothing written", err)
	}
}

// The fake reports nothing from Inventory, since the definition takes no
// registration (ADR-060 decision 12).
func TestInventoryIsEmpty(t *testing.T) {
	f, _ := fake.New()
	if got := f.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory = %v", got)
	}
}
