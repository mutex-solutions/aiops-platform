// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The boot of plugin smoke, with the server that cmd/aiops builds in the repository
// (ADR-060 decision 58, reading 1; ADR-062 decision 27): the test-only profile is the
// aiops-core bundle that the server ships (ADR-060 decision 27), so the home holds no
// copy of it, and the plugin's bundle, its rows enabled and reload-socket disabled by a
// --patch file, and Boot answers each row that the server mounted, from
// run/status.json, once it runs. A plugin row that does not mount, as one of a plugin
// the distribution does not link, fails it by its id.
func TestBoot(t *testing.T) {
	aiops := filepath.Join(t.TempDir(), "aiops")
	if out, err := exec.Command("go", "build", "-o", aiops, "../../cmd/aiops").CombinedOutput(); err != nil {
		t.Fatalf("go build of cmd/aiops: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile("../../dist/distribution.yaml") // the signed manifest that cmd/aiops embeds
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	home := filepath.Join(t.TempDir(), "home")
	rows, err := Boot(t.Context(), aiops, manifest, dir, home, &progress)
	ids := slices.Collect(func(yield func(string) bool) {
		for _, r := range rows {
			if r.State != kernel.Active || !yield(r.ID) {
				return
			}
		}
	})
	if err != nil || !slices.Equal(ids, []string{"clock", "config-refs", "loader", "readiness", "reload", "restart-policy"}) {
		t.Errorf("Boot of aiops-core = %+v, %v; want each row but reload-socket ACTIVE\n%s", rows, err, progress.String())
	}
	if _, err := os.Stat(filepath.Join(home, "bundles", "aiops-core")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the home of smoke holds bundles/aiops-core, which the server ships: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte("- insert:\n    - {id: ghost, plugin: acme/ghost, roles: [api], enabled: false}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Boot(t.Context(), aiops, manifest, dir, filepath.Join(t.TempDir(), "home"), &progress); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Errorf("Boot with a row of a plugin the distribution does not link: %v", err)
	}
}

// plugin verify of in-process code runs step 5 of plugin check on the local path, and the
// builder image's aiopsctl takes no request of it, so on the image path it builds nothing
// and answers that the step is not carried, never a pass of the module's own tests
// (ADR-060 decision 46, note of 2026-11-25).
func TestVerifyOnTheImagePath(t *testing.T) {
	_, err := Tools{}.Build(t.Context(), launch.BuildRequest{Module: "example.test/echo", Version: "v0.1.0", Sum: "h1:" + strings.Repeat("A", 43) + "=",
		Against: "oci://registry.test/aiops/release@sha256:" + strings.Repeat("ab", 32), ModCache: "modcache", Builder: "image"})
	if !errors.Is(err, launch.ErrNotCarried) || !strings.Contains(err.Error(), "--builder local") {
		t.Errorf("plugin verify of in-process code on the image path: %v; want that it is not carried, naming --builder local", err)
	}
}

// ParseRows reads the rows that plugin smoke prints, which the image path reads from
// the builder image's aiopsctl.
func TestParseRows(t *testing.T) {
	got := ParseRows("smoke: progress\nrow clock: ACTIVE\nrow echo:echo: PENDING and waits for echoers, stampers\nrow ghost: -\n")
	want := []launch.RowWait{{ID: "clock", State: kernel.Active}, {ID: "echo:echo", State: kernel.Pending, WaitsFor: []string{"echoers", "stampers"}}, {ID: "ghost"}}
	if len(got) != len(want) {
		t.Fatalf("ParseRows = %+v", got)
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].State != want[i].State || !slices.Equal(got[i].WaitsFor, want[i].WaitsFor) {
			t.Errorf("row %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}
