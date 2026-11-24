// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"path/filepath"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
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
			Verdict: s.Verdict, Result: s.Result, Built: s.Built})
		root := kerneltest.New(t)
		f := root.Plugin(p, nil)
		deftest.Active(t, f)
		return conformance.Subject{Root: root, Provider: f, Home: filepath.Join(t.TempDir(), "home"), Dir: t.TempDir(), Registry: registry}
	})
}

// The fake reports nothing from Inventory, since the definition takes no
// registration (ADR-060 decision 12).
func TestInventoryIsEmpty(t *testing.T) {
	f, _ := fake.New()
	if got := f.Inventory(); len(got) != 0 {
		t.Fatalf("Inventory = %v", got)
	}
}
