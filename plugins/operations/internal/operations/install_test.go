// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// installBox is packBox with the shipped profile dev, bundles [aiops-core], and a
// render with no diagnostic.
type installBox struct{ *packBox }

func (installBox) Shipped(context.Context) (map[string][]byte, error) {
	return map[string][]byte{"profiles/dev/profile.yaml": []byte("bundles: [aiops-core]\n")}, nil
}

func (installBox) Render(context.Context, launch.RenderRequest) (launch.Rendered, error) {
	return launch.Rendered{}, nil
}

// installed packs files, signs the artifact with an author's key that trust.yaml of
// home lists for owner/*, and installs it into dev.
func installed(t *testing.T, l Library, home string, files map[string]string, owner string) (def.Change, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "artifact")
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: pluginDir(t, files), Out: out}); err != nil {
		t.Fatal(err)
	}
	prefix := generate(t, l, t.TempDir(), "author")
	if _, err := l.AddTrust(t.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: prefix + ".pub", IDs: []string{owner + "/*"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Sign(t.Context(), def.SignRequest{Caller: caller(), Out: out, Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	return l.Install(t.Context(), def.InstallRequest{Caller: caller(), Home: home, Profile: "dev", Spec: out})
}

// What install keeps of a pack, in the files of ADR-047 decisions 5, 8 and 18: the
// manifest, plugin.yaml and the bundle under bundles/<id>/, the content tree under
// content/<id>/, the record with both bundles under verified/, a plugins.lock entry
// with the content digest, and the id last in the copied bundle list; and an artifact
// of the same name under another owner is refused and writes nothing (ADR-047
// decision 17).
func TestInstallKeepsAPack(t *testing.T) {
	box := &packBox{sum: packSummary()}
	l := Library{Box: installBox{box}, LockWait: time.Second}
	home := filepath.Join(t.TempDir(), "home")
	ch, err := installed(t, l, home, pack, "acme")
	if err != nil || ch.Outcome != def.NotRunning {
		t.Fatalf("Install = %+v, %v", ch, err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(rel)))
		if err != nil {
			t.Error(err)
		}
		return string(b)
	}
	if got := read("profiles/dev/profile.yaml"); got != "bundles: [aiops-core, acme/connector-x]\n" {
		t.Errorf("profile.yaml = %q", got)
	}
	lock := read("profiles/dev/plugins.lock")
	if !strings.HasPrefix(lock, "lockVersion: 1\nartifacts:\n  - {id: acme/connector-x, digest: sha256:") || !strings.Contains(lock, ", content: sha256:") {
		t.Errorf("plugins.lock = %q", lock)
	}
	digest := strings.TrimSuffix(strings.SplitN(strings.SplitN(lock, "digest: ", 2)[1], ",", 2)[0], "}")
	rec := "verified/" + strings.Replace(digest, ":", "-", 1)
	if read("content/acme/connector-x/runbooks/restart.md") != "# Restart\n" || read("bundles/acme/connector-x/bundle.yaml") != pack["bundle.yaml"] ||
		!strings.Contains(read(rec+"/record.yaml"), "kind: artifact\ndigest: "+digest+"\n") || read(rec+"/signature.sigstore.json") == "" {
		t.Errorf("the kept files or the record of %s differ", digest)
	}
	other := map[string]string{}
	for k, v := range pack {
		other[k] = strings.Replace(v, "acme/connector-x", "beta/connector-x", 1)
	}
	before := lock
	box.sum.ID = "beta/connector-x" // as boot's reader reads the plugin.yaml of other
	if _, err := installed(t, l, home, other, "beta"); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "never compose together") || read("profiles/dev/plugins.lock") != before {
		t.Errorf("an artifact of the name connector-x under beta: %v; want ErrRefused and nothing written", err)
	}
	if _, err := os.Stat(filepath.Join(home, "bundles", "beta")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the refused install kept files: %v", err)
	}
}
