// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// handPack writes the layout of an artifact of acme/connector-x with layers as given,
// the statement that pack records naming its digest, and signs it with a new key that
// trust.yaml of home lists for acme/* and in-process code: a layout that plugin pack
// would not write.
func handPack(t *testing.T, l Library, home string, layers ...blob) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "artifact")
	d, raw, err := manifestOf(artifactType, nil, nil, layers)
	if err == nil {
		err = writeLayout(t.Context(), out, d, raw, layers, "0.1.0", def.Statement("acme/connector-x", "0.1.0", d.Digest.Encoded(), nil, nil, nil))
	}
	if err != nil {
		t.Fatal(err)
	}
	prefix := generate(t, l, t.TempDir(), "author")
	if _, err := l.AddTrust(t.Context(), def.TrustRequest{Caller: caller(), Home: home, KeyFile: prefix + ".pub", IDs: []string{"acme/*"}, Runtimes: []string{"inproc"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Sign(t.Context(), def.SignRequest{Caller: caller(), Out: out, Key: prefix + ".key"}); err != nil {
		t.Fatal(err)
	}
	return out
}

// Content is a pack's (ADR-060 decision 19): its digest enters the generation digest
// through the lock entry, of which in-process code has none, so boot would deny such
// content at every boot. Pack refuses in-process code with a content/ directory, and
// install refuses an in-process artifact with a content layer and writes nothing.
func TestInProcessCodeCarriesNoContent(t *testing.T) {
	sum := packSummary()
	sum.Runtimes = []string{"inproc"}
	l := Library{Box: installBox{&packBox{sum: sum, mod: launch.Module{Path: "example.test/echo", Sum: echoSum}}}, LockWait: time.Second}
	manifest := strings.Replace(packManifest, "runtimes: []", "runtimes: [inproc]", 1)
	dir := pluginDir(t, map[string]string{"plugin.yaml": manifest, "bundle.yaml": "[]\n", "go.mod": "module example.test/echo\n", "echo.go": "package echo\n",
		"content/runbooks/restart.md": "# Restart\n"})
	if _, err := l.Pack(t.Context(), def.PackRequest{Caller: caller(), Dir: dir, Out: filepath.Join(t.TempDir(), "a")}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "ADR-060 decision 19") {
		t.Errorf("pack of in-process code with content/: %v; want ErrRefused naming ADR-060 decision 19", err)
	}
	home := filepath.Join(t.TempDir(), "home")
	canonical, _ := canonicalTar(map[string][]byte{"runbooks/restart.md": []byte("# Restart\n")})
	spec := handPack(t, l, home, blob{layerManifest, []byte(manifest), title("plugin.yaml")}, blob{layerBundle, []byte("[]\n"), title("bundle.yaml")},
		blob{layerModule, []byte(`{"path":"example.test/echo","version":"v0.1.0","sum":"` + echoSum + `"}`), title("module.json")},
		contentLayer(canonical, "runbooks/restart.md"), blob{layerSBOM, []byte("{}"), title("sbom.spdx.json")})
	before := tree(t, Home{Dir: home})
	if _, err := l.Install(t.Context(), def.InstallRequest{Caller: caller(), Home: home, Profile: "dev", Spec: spec}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "ADR-060 decision 19") {
		t.Errorf("install of in-process code with a content layer: %v; want ErrRefused naming ADR-060 decision 19", err)
	}
	if got := tree(t, Home{Dir: home}); !equal(got, before) {
		t.Errorf("the refused install wrote the home")
	}
}

// Install hands the gate the module that module.json of in-process code records, the
// one it writes into the record, so that checks 5 and 10 judge at install what boot
// judges at every boot (ADR-047 decision 17; ADR-020 decision 12).
func TestInstallGatesTheModuleOfInProcessCode(t *testing.T) {
	sum := packSummary()
	sum.Runtimes = []string{"inproc"}
	box := &packBox{sum: sum}
	l := Library{Box: installBox{box}, LockWait: time.Second}
	home := filepath.Join(t.TempDir(), "home")
	spec := handPack(t, l, home, blob{layerManifest, []byte(strings.Replace(packManifest, "runtimes: []", "runtimes: [inproc]", 1)), title("plugin.yaml")},
		blob{layerBundle, []byte("[]\n"), title("bundle.yaml")},
		blob{layerModule, []byte(`{"path":"example.test/echo","version":"v0.1.0","sum":"` + echoSum + `"}`), title("module.json")},
		blob{layerSBOM, []byte("{}"), title("sbom.spdx.json")})
	if _, err := l.Install(t.Context(), def.InstallRequest{Caller: caller(), Home: home, Profile: "dev", Spec: spec}); err != nil {
		t.Fatal(err)
	}
	want := &launch.Module{Path: "example.test/echo", Version: "v0.1.0", Sum: echoSum}
	if len(box.gates) != 1 || !reflect.DeepEqual(box.gates[0].Module, want) {
		t.Errorf("the gate requests %+v; want one with the module %+v", box.gates, want)
	}
}

// contentLayer is a content layer of the tar b whose annotation lists paths, none for
// nil.
func contentLayer(b []byte, paths ...string) blob {
	ann := title("content.tar")
	if paths != nil {
		j, _ := json.Marshal(paths)
		ann[contentPaths] = string(j)
	}
	return blob{layerContent, b, ann}
}

// Install refuses, before it writes anything, what boot's check of the kept files
// would deny at every boot (ADR-047 decisions 1 to 3 and 18): a content layer that is
// not in the canonical form, whose entries written back in it give other bytes; one
// that holds paths its annotation aiops.plugin.content.paths does not list, or has no
// such annotation, so that plugin inspect shows what install keeps; and an image
// manifest that holds a layer type twice. A layer in that form installs.
func TestInstallRefusesAContentLayerBootDenies(t *testing.T) {
	l := Library{Box: installBox{&packBox{sum: packSummary()}}, LockWait: time.Second}
	canonical, _ := canonicalTar(map[string][]byte{"runbooks/restart.md": []byte("# Restart\n")})
	var stamped bytes.Buffer
	tw := tar.NewWriter(&stamped)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "runbooks/restart.md", Mode: 0o755, Size: 10, ModTime: time.Unix(1700000000, 0)})
	_, _ = tw.Write([]byte("# Restart\n"))
	_ = tw.Close()
	hidden, _ := canonicalTar(map[string][]byte{"runbooks/restart.md": []byte("# Restart\n"), "hidden/run.sh": []byte("#!/bin/sh\n")})
	man, bun := blob{layerManifest, []byte(packManifest), title("plugin.yaml")}, blob{layerBundle, []byte(pack["bundle.yaml"]), title("bundle.yaml")}
	sbom := blob{layerSBOM, []byte("{}"), title("sbom.spdx.json")}
	for name, layers := range map[string][]blob{
		"a tar with a time and a mode of its own":  {man, bun, contentLayer(stamped.Bytes(), "runbooks/restart.md"), sbom},
		"a path that the annotation does not list": {man, bun, contentLayer(hidden, "runbooks/restart.md"), sbom},
		"no annotation of the paths":               {man, bun, contentLayer(canonical), sbom},
		"a layer type twice":                       {man, bun, contentLayer(canonical, "runbooks/restart.md"), sbom, {layerSBOM, []byte("[]"), nil}},
	} {
		home := filepath.Join(t.TempDir(), "home")
		spec := handPack(t, l, home, layers...)
		before := tree(t, Home{Dir: home})
		if _, err := l.Install(t.Context(), def.InstallRequest{Caller: caller(), Home: home, Profile: "dev", Spec: spec}); !errors.Is(err, def.ErrRefused) {
			t.Errorf("%s: %v; want ErrRefused", name, err)
		}
		if got := tree(t, Home{Dir: home}); !equal(got, before) {
			t.Errorf("%s: the refused install wrote the home", name)
		}
	}
	home := filepath.Join(t.TempDir(), "home")
	spec := handPack(t, l, home, man, bun, contentLayer(canonical, "runbooks/restart.md"), sbom)
	if _, err := l.Install(t.Context(), def.InstallRequest{Caller: caller(), Home: home, Profile: "dev", Spec: spec}); err != nil {
		t.Errorf("a canonical content layer that its annotation lists: %v", err)
	}
}

// serverBox is installBox that records whether each render composes in server mode.
type serverBox struct {
	installBox
	server []bool
}

func (b *serverBox) Render(_ context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	b.server = append(b.server, q.Server)
	return launch.Rendered{}, nil
}

// Install renders the target profile as the server boots it, in server mode, so that a
// pack row that states no roles is refused where the installer sees it, and not at the
// next start (08:156; ADR-047 decision 17).
func TestInstallRendersAServerProfile(t *testing.T) {
	box := &serverBox{installBox: installBox{&packBox{sum: packSummary()}}}
	l := Library{Box: box, LockWait: time.Second}
	if _, err := installed(t, l, filepath.Join(t.TempDir(), "home"), pack, "acme"); err != nil {
		t.Fatal(err)
	}
	if len(box.server) == 0 || slices.Contains(box.server, false) {
		t.Errorf("the renders of an install into dev in server mode: %v; want each true", box.server)
	}
}

// rowsBox is installBox whose Render answers rd.
type rowsBox struct {
	installBox
	rd launch.Rendered
}

func (b rowsBox) Render(context.Context, launch.RenderRequest) (launch.Rendered, error) {
	return b.rd, nil
}

// Disable refuses the rows that carry its own submission, reload-socket and reload,
// writes nothing and submits nothing to the process that runs the profile, so the
// command exits 1 with the home as it was (ADR-062 decisions 12 and 18).
func TestDisableOfTheRowsOfTheSubmission(t *testing.T) {
	rows := []launch.Row{{ID: "reload", Plugin: "aiops/reload", Enabled: true}, {ID: "reload-socket", Plugin: "aiops/reload-socket", Enabled: true}}
	l := Library{Box: rowsBox{installBox{&packBox{}}, launch.Rendered{Composition: launch.Composition{Rows: rows}}}, LockWait: time.Second}
	h := Home{Dir: filepath.Join(t.TempDir(), "home")}
	put(t, h, "profiles/dev/profile.yaml", "bundles: [aiops-core]\n")
	put(t, h, "run/status.json", `{"phase": "running", "profile": "dev"}`) // a process runs dev, with no socket to answer
	before := tree(t, h)
	for _, r := range rows {
		ch, err := l.Disable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "dev", Row: r.ID, ConnectWait: time.Millisecond})
		if !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "ADR-062 decision 12") || ch.Outcome != "" {
			t.Errorf("disable %s: %+v, %v; want ErrRefused naming ADR-062 decision 12 and no submission", r.ID, ch, err)
		}
	}
	if got := tree(t, h); !equal(got, before) {
		t.Errorf("a refused disable wrote the home: %v", got)
	}
}

// aiopsctl never listens (ADR-062 decision 2): enable refuses reload-socket in the
// profile cli, whose row could not take effect there, and writes nothing.
func TestEnableOfTheSocketInTheProfileCli(t *testing.T) {
	rows := []launch.Row{{ID: "reload-socket", Plugin: "aiops/reload-socket"}}
	l := Library{Box: rowsBox{installBox{&packBox{}}, launch.Rendered{Composition: launch.Composition{Rows: rows}}}, LockWait: time.Second}
	h := Home{Dir: filepath.Join(t.TempDir(), "home")}
	put(t, h, "profiles/cli/profile.yaml", "bundles: [aiops-core, aiops-cli]\n")
	before := tree(t, h)
	if _, err := l.Enable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "cli", Row: "reload-socket"}); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), "ADR-062 decision 2") {
		t.Errorf("enable reload-socket in cli: %v; want ErrRefused naming ADR-062 decision 2", err)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Errorf("a refused enable wrote the home: %v", got)
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

// Remove deletes bundles/<id>/ and content/<id>/ only once its write is kept and the
// snapshot dropped, so a deletion that fails leaves the bundle list and plugins.lock
// without the id and says so: it never leaves a lock entry without its files (ADR-047
// decision 10; ADR-062 decision 11).
func TestRemoveDeletesTheKeptFilesAfterTheWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root deletes from a directory of mode 0500")
	}
	l := Library{Box: installBox{&packBox{sum: packSummary()}}, LockWait: time.Second}
	home := filepath.Join(t.TempDir(), "home")
	if _, err := installed(t, l, home, pack, "acme"); err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(home, "content", "acme", "connector-x")
	if err := os.Chmod(content, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(content, 0o700) })
	ch, err := l.Remove(t.Context(), def.RemoveRequest{Home: home, Profile: "dev", ID: "acme/connector-x"})
	files := tree(t, Home{Dir: home})
	if err != nil || files["profiles/dev/profile.yaml"] != "bundles: [aiops-core]\n" || strings.Contains(files["profiles/dev/plugins.lock"], "acme/connector-x") {
		t.Errorf("remove: %v; profile.yaml %q, plugins.lock %q; want both kept without the id",
			err, files["profiles/dev/profile.yaml"], files["profiles/dev/plugins.lock"])
	}
	if !slices.ContainsFunc(ch.Notes, func(n string) bool { return strings.Contains(n, content) }) {
		t.Errorf("notes %q; want one that names the directory that stays", ch.Notes)
	}
}

// Remove drops the enabled field of every item of the profile layer under the
// artifact's group, and an item left with its id alone, so that a later install leaves
// those rows disabled; it keeps every other field and names each item it kept (ADR-047
// decision 10; ADR-062 decision 11).
func TestRemoveDropsTheEnabledOfItsRows(t *testing.T) {
	l := Library{Box: installBox{&packBox{sum: packSummary()}}, LockWait: time.Second}
	home := filepath.Join(t.TempDir(), "home")
	if _, err := installed(t, l, home, pack, "acme"); err != nil {
		t.Fatal(err)
	}
	layer := filepath.Join(home, "profiles", "dev", "aiops.patch.yml")
	put(t, Home{Dir: home}, "profiles/dev/aiops.patch.yml", "- {id: 'connector-x:logs', enabled: true}\n"+
		"- {id: 'connector-x:audit', enabled: true, config: {level: 2}}\n- {id: metrics, enabled: false}\n")
	ch, err := l.Remove(t.Context(), def.RemoveRequest{Home: home, Profile: "dev", ID: "acme/connector-x"})
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := readYAML(layer, &got); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"id": "connector-x:audit", "config": map[string]any{"level": 2}}, {"id": "metrics", "enabled": false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the profile layer holds %v, want %v", got, want)
	}
	notes := strings.Join(ch.Notes, "\n")
	if !strings.Contains(notes, "connector-x:audit") || strings.Contains(notes, "connector-x:logs") {
		t.Errorf("notes %q; want the kept item connector-x:audit named, and not connector-x:logs", ch.Notes)
	}
}

// enableBox renders a profile as composing the row echo, and as not composing once the
// profile layer it reads enables echo; it records each home it read that layer in.
type enableBox struct {
	installBox
	refusedIn []string
}

func (b *enableBox) Render(_ context.Context, q launch.RenderRequest) (launch.Rendered, error) {
	layer, _ := os.ReadFile(filepath.Join(q.Home, "profiles", q.Profile, "aiops.patch.yml"))
	if strings.Contains(string(layer), "enabled: true") {
		b.refusedIn = append(b.refusedIn, q.Home)
		return launch.Rendered{Diagnostics: []launch.Diagnostic{{Check: "compose", Text: "echo needs a key that no row provides"}}}, nil
	}
	return launch.Rendered{Composition: launch.Composition{Rows: []launch.Row{{ID: "echo", Plugin: "acme/echo"}}}}, nil
}

// Enable and Disable render the changed profile before they rename anything, so a
// change that boot would refuse is never in the home, not even until a restore, and
// writes nothing; one that boot admits is kept (ADR-061 decision 17). The view they
// render is gone when they return.
func TestEnableWritesNothingThatBootRefuses(t *testing.T) {
	box := &enableBox{installBox: installBox{&packBox{sum: packSummary()}}}
	l := Library{Box: box, LockWait: time.Second}
	h := Home{Dir: filepath.Join(t.TempDir(), "home")}
	put(t, h, "profiles/dev/profile.yaml", "bundles: [aiops-core]\n")
	before := tree(t, h)
	if _, err := l.Enable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "dev", Row: "echo"}); !errors.Is(err, def.ErrRefused) || !reflect.DeepEqual(tree(t, h), before) {
		t.Errorf("an enable that boot refuses: %v; want ErrRefused and nothing written", err)
	}
	if len(box.refusedIn) == 0 || slices.Contains(box.refusedIn, h.Dir) {
		t.Errorf("the changed profile was rendered in %q: want a view outside the home %s, never the home", box.refusedIn, h.Dir)
	}
	for _, dir := range box.refusedIn {
		if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the view %s is left behind: %v", dir, err)
		}
	}
	if _, err := l.Disable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "dev", Row: "echo"}); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, h)["profiles/dev/aiops.patch.yml"]; got != "" {
		t.Errorf("the profile layer after a disable of a row that the bundle layers disable: %q; want nothing written", got)
	}
}

// Enable and disable write only a departure from the bundle layers into the profile
// layer (08:375; ADR-061 decision 17): a value the bundle layers give the row already
// writes nothing, and one that returns to it drops the item of the row, so a later
// change of the bundle's default reaches the profile.
func TestEnableWritesOnlyADeparture(t *testing.T) {
	rows := []launch.Row{{ID: "clock", Plugin: "aiops/clock", Enabled: true}}
	l := Library{Box: rowsBox{installBox{&packBox{}}, launch.Rendered{Composition: launch.Composition{Rows: rows}}}, LockWait: time.Second}
	h := Home{Dir: filepath.Join(t.TempDir(), "home")}
	put(t, h, "profiles/dev/profile.yaml", "bundles: [aiops-core]\n")
	before := tree(t, h)
	if ch, err := l.Enable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "dev", Row: "clock"}); err != nil || len(ch.Written) != 0 || !equal(tree(t, h), before) {
		t.Errorf("enable of a row that the bundle layers enable: %+v, %v; want nothing written", ch, err)
	}
	put(t, h, "profiles/dev/aiops.patch.yml", "- {id: clock, enabled: false}\n- {id: other, enabled: false}\n")
	if _, err := l.Enable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "dev", Row: "clock"}); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, h)["profiles/dev/aiops.patch.yml"]; strings.Contains(got, "clock") || !strings.Contains(got, "other") {
		t.Errorf("the profile layer after an enable back to the bundle's value: %q; want the item of clock dropped and other kept", got)
	}
}

// With no process on the profile, an enable whose changed profile still renders the row
// disabled, as the site layer overrides it, says so, and not that the change applies
// when the process starts; it exits 0 (ADR-062 decision 9).
func TestEnableOverriddenWithNoProcessSaysSo(t *testing.T) {
	rows := []launch.Row{{ID: "echo", Plugin: "acme/echo"}} // disabled in every render: the site layer disables it
	l := Library{Box: rowsBox{installBox{&packBox{}}, launch.Rendered{Composition: launch.Composition{Rows: rows}}}, LockWait: time.Second}
	h := Home{Dir: filepath.Join(t.TempDir(), "home")}
	put(t, h, "profiles/dev/profile.yaml", "bundles: [aiops-core]\n")
	ch, err := l.Enable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "dev", Row: "echo"})
	notes := strings.Join(ch.Notes, "\n")
	if err != nil || ch.Outcome != def.NotRunning || !strings.Contains(notes, "overrides") || strings.Contains(notes, "applies when it starts") {
		t.Errorf("an overridden enable with no process: %+v, %v; want not-running, a note that a later layer overrides it, and none that it applies", ch, err)
	}
}

// A refused enable, disable or install writes nothing, so it does not say that it copied
// the shipped profile; one that writes says so.
func TestARefusalCopiesNothing(t *testing.T) {
	l := Library{Box: rowsBox{installBox{&packBox{}}, launch.Rendered{}}, LockWait: time.Second}
	h := Home{Dir: filepath.Join(t.TempDir(), "home")}
	ch, err := l.Disable(t.Context(), def.RowRequest{Home: h.Dir, Profile: "dev", Row: "nosuchrow"})
	if !errors.Is(err, def.ErrRefused) || slices.ContainsFunc(ch.Notes, func(n string) bool { return strings.Contains(n, "copied") }) {
		t.Errorf("a refused disable: %+v, %v; want ErrRefused and no note of a copy", ch, err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "profiles")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the refused disable wrote profiles/: %v", err)
	}
}

// One digest of a plugin id per site (ADR-047 decisions 7 and 16): an install of an id
// that another profile names at another digest, by its lock entry or, as in-process
// code does, by the kept files alone, refuses, names that digest and those profiles,
// and writes nothing; at the same digest it appends the bundle list and reuses the kept
// files. Remove deletes them once no profile names the id, and an install removes a
// leftover that no profile names, and says so (decision 10).
func TestInstallKeepsOneDigestPerID(t *testing.T) {
	l := Library{Box: installBox{&packBox{sum: packSummary()}}, LockWait: time.Second}
	home := filepath.Join(t.TempDir(), "home")
	h := Home{Dir: home}
	v1 := signedPack(t, l, home, pack, "acme")
	other := maps.Clone(pack)
	other["content/labels.yaml"] = "service: y\n"
	v2 := signedPack(t, l, home, other, "acme")
	install := func(spec, p string) (def.Change, error) {
		return l.Install(t.Context(), def.InstallRequest{Caller: caller(), Home: home, Profile: p, Spec: spec})
	}
	if _, err := install(v1, "dev"); err != nil {
		t.Fatal(err)
	}
	put(t, h, "profiles/two/profile.yaml", "bundles: [aiops-core]\n")
	put(t, h, "profiles/three/profile.yaml", "bundles: [aiops-core, acme/connector-x]\n")
	before := tree(t, h)
	digest := strings.TrimSuffix(strings.SplitN(strings.SplitN(before["profiles/dev/plugins.lock"], "digest: ", 2)[1], ",", 2)[0], "}")
	if _, err := install(v2, "two"); !errors.Is(err, def.ErrRefused) || !strings.Contains(err.Error(), digest) ||
		!strings.Contains(err.Error(), "dev") || !strings.Contains(err.Error(), "three") {
		t.Errorf("install of another digest: %v; want ErrRefused naming %s, dev and three", err, digest)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Fatalf("the refused install wrote the home: profile.yaml of two %q", got["profiles/two/profile.yaml"])
	}
	ch, err := install(v1, "two")
	if got := tree(t, h)["profiles/two/profile.yaml"]; err != nil || got != "bundles: [aiops-core, acme/connector-x]\n" {
		t.Fatalf("install of the same digest into two: %v, %q", err, got)
	}
	for _, w := range ch.Written {
		if strings.HasPrefix(w, filepath.Join(home, "bundles")) || strings.HasPrefix(w, filepath.Join(home, "content")) {
			t.Errorf("install at the same digest wrote %s again", w)
		}
	}
	kept := filepath.Join(home, "bundles", "acme", "connector-x", "artifact.json")
	for _, p := range []string{"dev", "two", "three"} {
		if _, err := l.Remove(t.Context(), def.RemoveRequest{Home: home, Profile: p, ID: "acme/connector-x"}); err != nil {
			t.Fatalf("remove from %s: %v", p, err)
		}
		if _, err := os.Stat(kept); (p == "three") != errors.Is(err, fs.ErrNotExist) {
			t.Errorf("after remove from %s, the kept files: %v", p, err)
		}
	}
	put(t, h, "bundles/acme/connector-x/stale", "x\n")
	put(t, h, "content/acme/connector-x/stale.md", "x\n")
	ch, err = install(v2, "dev")
	if err != nil || !slices.ContainsFunc(ch.Notes, func(n string) bool { return strings.Contains(n, "leftover") }) {
		t.Errorf("install over a leftover: %q, %v; want a note that names the leftover", ch.Notes, err)
	}
	if files := tree(t, h); files["bundles/acme/connector-x/stale"] != "" || files["content/acme/connector-x/stale.md"] != "" {
		t.Errorf("the leftover stays")
	}
}

// A plugins.lock that does not read stops no install into another profile, which names
// it and that it went unchecked for another digest of the id; and profile sanitize sets
// aside the one of its profile, as it sets aside a layer file, so that plugin remove
// can then take the artifact (ADR-047 decisions 7, 8 and 10; ADR-061 decision 41).
func TestAnUnreadableLock(t *testing.T) {
	l := Library{Box: installBox{&packBox{sum: packSummary()}}, LockWait: time.Second}
	home := filepath.Join(t.TempDir(), "home")
	h := Home{Dir: home}
	put(t, h, "profiles/prod/profile.yaml", "bundles: [aiops-core]\n")
	put(t, h, "profiles/prod/plugins.lock", "lockVersion: 1\nartifacts: [\n")
	ch, err := installed(t, l, home, pack, "acme")
	if err != nil || !slices.ContainsFunc(ch.Notes, func(n string) bool { return strings.Contains(n, "profiles/prod/plugins.lock") }) {
		t.Fatalf("an install into dev beside a prod lock that does not read: %+v, %v; want it kept, naming that lock", ch, err)
	}
	put(t, h, "profiles/dev/plugins.lock", "lockVersion: 1\nartifacts: [\n")
	ch, err = l.SanitizeProfile(t.Context(), def.ProfileRequest{Home: home, Profile: "dev"})
	if err != nil || !slices.ContainsFunc(ch.Notes, func(n string) bool { return strings.Contains(n, "set aside as plugins.lock.bak-") }) {
		t.Errorf("sanitize of dev with a lock that does not read: %+v, %v; want the lock set aside", ch, err)
	}
	if _, err := l.Remove(t.Context(), def.RemoveRequest{Home: home, Profile: "dev", ID: "acme/connector-x"}); err != nil {
		t.Errorf("remove after the sanitize: %v", err)
	}
}
