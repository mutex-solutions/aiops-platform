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

// signedPack packs files, signs the artifact with a new author's key that trust.yaml of
// home lists for owner/*, and returns its layout.
func signedPack(t *testing.T, l Library, home string, files map[string]string, owner string) string {
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
	return out
}

// installed packs and signs files as signedPack does and installs the artifact into dev.
func installed(t *testing.T, l Library, home string, files map[string]string, owner string) (def.Change, error) {
	t.Helper()
	return l.Install(t.Context(), def.InstallRequest{Caller: caller(), Home: home, Profile: "dev", Spec: signedPack(t, l, home, files, owner)})
}

// removeBox is installBox whose Render answers before for a profile whose profile.yaml
// names acme/connector-x, and after for one that does not, as the profile that its
// removal changed.
type removeBox struct {
	installBox
	before, after launch.Rendered
}

func (b *removeBox) Render(_ context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	f, _ := os.ReadFile(filepath.Join(q.Home, "profiles", q.Profile, "profile.yaml"))
	if strings.Contains(string(f), "acme/connector-x") {
		return b.before, nil
	}
	return b.after, nil
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

// Remove takes the plugin id of an installed artifact only, one with its kept files or
// its lock entry: aiops-cli of the profile cli, the bundle that inserts reload-socket,
// another first-party bundle, an id with no kept files and an id that is no plugin id
// are refused, and nothing is written (ADR-047 decision 10; ADR-062 decision 12). It
// refuses too, before it writes, a removal that takes away the last enabled providers
// of a bound or a required key, and keeps nothing of one whose changed profile does not
// compose (ADR-061 decision 17).
func TestRemoveTakesAnInstalledArtifactOnly(t *testing.T) {
	box := &removeBox{installBox: installBox{&packBox{sum: packSummary()}}}
	l := Library{Box: box, LockWait: time.Second}
	home := filepath.Join(t.TempDir(), "home")
	if _, err := installed(t, l, home, pack, "acme"); err != nil {
		t.Fatal(err)
	}
	h := Home{Dir: home}
	put(t, h, "profiles/cli/profile.yaml", "bundles: [aiops-core, aiops-cli, toys]\n")
	put(t, h, "profiles/dev/profile.yaml", "bundles: [aiops-core, socket, acme/connector-x, acme/ghost, 'acme/../connector-x']\n")
	before := tree(t, h)
	remove := func(p, id string) error {
		_, err := l.Remove(t.Context(), def.RemoveRequest{Home: home, Profile: p, ID: id})
		return err
	}
	for _, c := range [][2]string{{"cli", "aiops-cli"}, {"dev", "socket"}, {"dev", "aiops-core"}, {"dev", "acme/ghost"}, {"dev", "acme/../connector-x"}} {
		if err := remove(c[0], c[1]); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "no installed artifact") {
			t.Errorf("remove %s from %s: %v; want ErrRefused naming no installed artifact", c[1], c[0], err)
		}
	}
	if got := tree(t, h); !equal(got, before) {
		t.Fatalf("a refused remove wrote the home: %v", got)
	}
	rows := []launch.Row{{ID: "connector-x", Plugin: "core:group", Enabled: true, Origin: "acme/connector-x"},
		{ID: "connector-x:a", Plugin: "acme/connector-x", Enabled: true, Origin: "acme/connector-x"},
		{ID: "connector-x:b", Plugin: "acme/connector-x", Enabled: true, Origin: "acme/connector-x"}}
	keys := []launch.KeyProviders{{Name: "x.events", Provider: "bound", Consumer: "open", Rows: []string{"connector-x:a", "connector-x:b"}}}
	box.before = launch.Rendered{Composition: launch.Composition{Rows: rows}, Keys: keys}
	if err := remove("dev", "acme/connector-x"); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "x.events") {
		t.Errorf("remove of the last providers of a bound key: %v; want ErrRefused naming the key", err)
	}
	keys[0].Provider = "open"
	box.before.Required.Keys = []string{"x.events"}
	if err := remove("dev", "acme/connector-x"); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "x.events") {
		t.Errorf("remove of the last providers of a required key: %v; want ErrRefused naming the key", err)
	}
	box.before = launch.Rendered{}
	box.after = launch.Rendered{Diagnostics: []launch.Diagnostic{{Text: "the required key x.events has no provider"}}}
	if err := remove("dev", "acme/connector-x"); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "has no provider") {
		t.Errorf("remove whose changed profile does not compose: %v; want ErrRefused with the diagnostic", err)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Errorf("a refused remove kept a change: %v", got)
	}
}
