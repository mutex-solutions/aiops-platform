package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// The module tie of ADR-020 decision 12, in each form of build information: a builder
// build, whose modules have a version and a sum, and a build inside the repository,
// whose main module and replaced modules have none (ADR-020 decision 28).
func TestTieModules(t *testing.T) {
	dep := func(path, version, sum string, rep *debug.Module) *debug.Module {
		return &debug.Module{Path: path, Version: version, Sum: sum, Replace: rep}
	}
	builder := &debug.BuildInfo{Main: debug.Module{Path: "gen.test"}, Deps: []*debug.Module{
		dep("m.test", "v1.0.0", sumA, nil), dep("m.test/sub", "v1.1.0", sumA, nil), dep("r.test", "v0.0.0", "", &debug.Module{Path: "./r"})}}
	for _, tc := range []struct {
		name    string
		bi      *debug.BuildInfo
		pkg     string
		modules []DistModule
		err     string
		bare    bool
	}{
		{"a builder build", builder, "m.test/p", []DistModule{{GoModule: GoModule{"m.test", "v1.0.0", sumA}}}, "", false},
		{"the longest prefix", builder, "m.test/sub/p", []DistModule{{GoModule: GoModule{"m.test/sub", "v1.1.0", sumA}}}, "", false},
		{"another version", builder, "m.test/p", []DistModule{{GoModule: GoModule{"m.test", "v1.0.1", sumA}}}, "the binary runs m.test v1.0.0", false},
		{"another sum", builder, "m.test/p", []DistModule{{GoModule: GoModule{"m.test", "v1.0.0", "h1:x"}}}, "lists m.test v1.0.0 h1:x", false},
		{"the main module of a build inside the repository", testBuildInfo(), testModule + "/p", []DistModule{{GoModule: GoModule{Path: testModule}}}, "", true},
		{"a replaced module", builder, "r.test/p", []DistModule{{GoModule: GoModule{Path: "r.test"}, Replace: &GoModule{Path: "./r"}}}, "", true},
		{"a replaced module listed without its replacement", builder, "r.test/p", []DistModule{{GoModule: GoModule{Path: "r.test"}}}, "=> ./r", false},
		{"another replacement", builder, "r.test/p", []DistModule{{GoModule: GoModule{Path: "r.test"}, Replace: &GoModule{Path: "./s"}}}, "lists r.test => ./s", false},
		{"a module absent from the manifest", builder, "m.test/p", nil, "absent from the trusted distribution manifest", false},
		{"a package in no module", builder, "x.test/p", nil, "in no module", false},
		{"no build information", nil, "m.test/p", nil, "in no module", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Distribution{Plugins: map[string]DistPlugin{"o/p": {Package: tc.pkg}}, Modules: tc.modules}
			tie, lines := tieModules(d, tc.bi)
			if err := tie["o/p"].Err; tc.err == "" && err != nil || tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("err %v, want %q", err, tc.err)
			}
			if (len(lines) == 1) != tc.bare || tc.bare && !strings.Contains(lines[0], "decision 28") {
				t.Fatalf("lines %q", lines)
			}
		})
	}
}

// homeFor returns a home that trusts the signed manifest of a toy table, and the manifest.
func homeFor(t *testing.T) (fstest.MapFS, Distribution) {
	t.Helper()
	signed := signedFor(ModuleTable{"o/p": {ID: "o/p"}}, DistManifest{})
	d, err := ReadDistribution("distribution.yaml", signed)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := trustedHome(signed)
	return home, d
}

// What boot reads of the home at every boot and every live generation: whether the
// embedded manifest is trusted, by the current record or by the built-in record of a
// release binary, and what it prints (ADR-020 decisions 12 and 13).
func TestBootInputTrust(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(home fstest.MapFS, d *Distribution)
		rel   *Release
		err   string
		first string
	}{
		{"the current record", nil, nil, "", "no release key and no built-in record"},
		{"another current distribution", func(h fstest.MapFS, _ *Distribution) {
			h["verified/current"] = &fstest.MapFile{Data: []byte("sha256:" + hexC + "\n")}
		}, nil, "run aiopsctl dist use", ""},
		{"no current distribution", func(h fstest.MapFS, _ *Distribution) { delete(h, "verified/current") }, nil, `current distribution ""`, ""},
		{"a current record that is missing", func(h fstest.MapFS, d *Distribution) {
			for name := range h {
				if strings.HasSuffix(name, "record.yaml") {
					delete(h, name)
				}
			}
		}, nil, "file-missing", ""},
		{"a signer that distributions does not list", func(h fstest.MapFS, _ *Distribution) {
			h["trust.yaml"] = &fstest.MapFile{Data: []byte("distributions: []\n")}
		}, nil, "which distributions of trust.yaml does not list", ""},
		{"a trust.yaml that does not read", func(h fstest.MapFS, _ *Distribution) {
			h["trust.yaml"] = &fstest.MapFile{Data: []byte("indexes: []\n")}
		}, nil, "S6a", ""},
		{"a current file that does not read", func(h fstest.MapFS, _ *Distribution) {
			h["verified/current"] = &fstest.MapFile{Data: []byte("x\n")}
		}, nil, "one line", ""},
		{"a release binary on its built-in record", func(h fstest.MapFS, _ *Distribution) { delete(h, "trust.yaml") },
			&Release{KeyID: "K"}, "", "release root, key K, is trusted; the built-in record of sha256:"},
		{"a release binary whose root the site removed, on the current record", nil,
			&Release{KeyID: "K"}, "", "is removed in trust.yaml; the built-in record of sha256:"},
		// ADR-020 decision 30, reading 5: the built-in record is a record of its own.
		{"a release binary on its built-in record beside a current file that does not read", func(h fstest.MapFS, _ *Distribution) {
			delete(h, "trust.yaml")
			h["verified/current"] = &fstest.MapFile{Data: []byte("x\n")}
		}, &Release{KeyID: "K"}, "", "release root, key K, is trusted; the built-in record of sha256:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, d := homeFor(t)
			if tc.edit != nil {
				tc.edit(home, &d)
			}
			in, lines := bootInput(home, "p", Composition{}, nil, d, tc.rel, nil)
			if tc.err == "" && in.Trusted != nil || tc.err != "" && (in.Trusted == nil || !strings.Contains(in.Trusted.Error(), tc.err) ||
				!strings.Contains(in.Trusted.Error(), "to recover")) {
				t.Fatalf("trusted %v, want %q", in.Trusted, tc.err)
			}
			if !strings.Contains(lines[0], tc.first) || in.BuiltIn != strings.Contains(tc.first, "is trusted") || in.Entry != EntryBoot {
				t.Fatalf("lines %q, built-in %v", lines, in.BuiltIn)
			}
			if tc.rel != nil && strings.Contains(lines[0], "removed") != strings.Contains(lines[0], "not in use") {
				t.Fatalf("line %q", lines[0])
			}
			if warned := slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "warning: verified/current") }); warned != strings.Contains(tc.name, "beside a current file") {
				t.Fatalf("lines %q", lines)
			}
		})
	}
}

// Boot prints allowUnsigned, the accepted licences and every departure from the release
// bindings at every boot, and warns on each enabled row whose plugin declares a broader
// credential (08-profiles-roadmap-adrs.md:391, :403, :404).
func TestBootInputPrints(t *testing.T) {
	td := newTestDist()
	td.add(manifestText("o/p", "integrates: [{system: ex, versions: \">=1\", tested: [\"1.0\"], credential: broader}]"), "")
	td.accept = "[GPL-3.0-only]"
	td.site = "{providers: {k: [o/p]}, consumers: {c: [o/p]}, sealedHooks: {h: [o/p]}, required: {keys: [k], rows: [r]}}"
	d, err := ReadDistribution("distribution.yaml", td.text())
	if err != nil {
		t.Fatal(err)
	}
	home := fstest.MapFS{"trust.yaml": {Data: []byte("allowUnsigned: [acme/x]\n")}}
	// A pack states its credential class in integrates, and an in-process artifact is
	// warned on once (08-profiles-roadmap-adrs.md:391).
	installHome(home, packManifestText("acme/pack")+"integrates: [{system: ex, versions: \">=1\", tested: [\"1.0\"], credential: broader}]\n", "[]\n", "")
	installHome(home, td.plugins["o/p"][0], "[]\n", "")
	c := Composition{Rows: []ComposedRow{{Row: Row{ID: "a", Plugin: "o/p", Enabled: true}}, {Row: Row{ID: "b", Plugin: "o/p"}},
		instRow("pack:e1", "o/plain", "acme/pack"), instRow("p:x", "o/p", "o/p")}}
	_, lines := bootInput(home, "p", c, nil, d, nil, nil)
	broader := func(row, id string) string {
		return fmt.Sprintf("warning: row %q: plugin %s declares credential: broader for ex (08-profiles-roadmap-adrs.md:391)", row, id)
	}
	want := []string{"trust: allowUnsigned lists acme/x", "licence: the distribution accepts GPL-3.0-only",
		"bindings: the site adds providers of k: o/p", "bindings: the site adds consumers of c: o/p",
		"bindings: the site adds listeners of the sealed hook h: o/p", "bindings: the site raises the required set by k, r",
		broader("a", "o/p"), broader("pack:e1", "acme/pack"), broader("p:x", "o/p")}
	if !slices.Equal(lines[1:], want) {
		t.Fatalf("lines\n%s", strings.Join(lines, "\n"))
	}
}

// installHome writes into home what install keeps and records of the artifact of
// manifest with bundle: bundles/<id>/ with artifact.json, plugin.yaml and bundle.yaml,
// and the record of its digest, signed by keyP256ID with the statement attestation.
// A pack, which in-process code is not, gets the lock entry of the profile p. It
// returns the digest.
func installHome(home fstest.MapFS, manifest, bundle, attestation string) string {
	m, err := ReadManifest("t", []byte(manifest), ManifestLenient)
	if err != nil {
		panic(err)
	}
	layer := func(typ, data string) map[string]any {
		return map[string]any{"mediaType": typ, "digest": digestOf([]byte(data))}
	}
	art, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": ociManifest, "artifactType": artifactType,
		"config": map[string]any{"mediaType": ociEmpty}, "layers": []any{layer(layerManifest, manifest), layer(layerBundle, bundle)}})
	d, dir := digestOf(art), "bundles/"+m.ID+"/"
	rec := fmt.Sprintf("kind: artifact\ndigest: %s\nsigner: %s\nid: %s\nversion: %s\nruntimes: [%s]\n", d, keyP256ID, m.ID, m.Version, strings.Join(m.Runtimes, ", "))
	if slices.Contains(m.Runtimes, "inproc") {
		rec += "module: {path: " + testModule + "}\n"
	} else {
		home["profiles/p/plugins.lock"] = &fstest.MapFile{Data: []byte("lockVersion: 1\nartifacts:\n  - {id: " + m.ID + ", digest: " + d + "}\n")}
	}
	rdir := "verified/" + strings.Replace(d, ":", "-", 1) + "/"
	for name, data := range map[string]string{dir + "artifact.json": string(art), dir + "plugin.yaml": manifest, dir + "bundle.yaml": bundle,
		rdir + "record.yaml": rec, rdir + "attestation.json": attestation} {
		home[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return d
}

// instRow is a row that the bundle of the installed artifact id inserted.
func instRow(row, plugin, id string) ComposedRow {
	return ComposedRow{Row: Row{ID: row, Plugin: plugin, Enabled: true},
		Provenance: Provenance{Fields: []FieldOrigin{{Field: FieldID, Origin: Origin{Layer: 1, Source: "bundle:" + id}}}}}
}

// Boot reads each installed artifact of the composition: by the digest of its lock
// entry, or for in-process code, which has none, by the artifact.json that install
// kept (ADR-047 decision 18), then the kept files and the record of the digest. The
// locks section of the generation digest holds each entry and its content.
func TestBootInputArtifacts(t *testing.T) {
	home, digest := keptHome(t, nil)
	rec := "verified/" + strings.Replace(digest, ":", "-", 1) + "/record.yaml"
	home[rec] = &fstest.MapFile{Data: []byte("kind: artifact\ndigest: " + digest + "\nsigner: " + keyP256ID + "\nid: acme/connector-x\nversion: 0.1.0\nruntimes: []\n")}
	kept, err := ReadKept(home, "acme/connector-x", digest)
	if err != nil {
		t.Fatal(err)
	}
	lock := "lockVersion: 1\nartifacts:\n  - {id: acme/connector-x, digest: " + digest + ", content: " + kept.Content + "}\n"
	c := Composition{Rows: []ComposedRow{instRow("a", "aiops/echo", "acme/connector-x"), instRow("b", "aiops/echo", "acme/connector-x")}}
	_, d := homeFor(t)
	for _, tc := range []struct {
		name, lock string
		edit       func(h fstest.MapFS)
		err        string
		locks      int
	}{
		{"locked", lock, nil, "", 2},
		{"in-process code, with no lock entry", "", func(h fstest.MapFS) {
			h[rec] = &fstest.MapFile{Data: []byte("kind: artifact\ndigest: " + digest + "\nsigner: " + keyP256ID + "\nid: acme/connector-x\nversion: 0.1.0\nruntimes: [inproc]\nmodule: {path: " + testModule + "}\n")}
		}, "", 0},
		// Only in-process code has no lock entry (ADR-047 decisions 8 and 18).
		{"a pack with no lock entry", "", nil, "profiles/p/plugins.lock: kept: names no digest for acme/connector-x", 0},
		{"a pack with no lock entry in a lock", "lockVersion: 1\nartifacts: []\n", nil, "names no digest", 0},
		{"no artifact.json and no lock entry", "", func(h fstest.MapFS) { delete(h, keptDir+"artifact.json") }, "artifact.json", 0},
		{"kept files that differ", lock, func(h fstest.MapFS) { h[keptDir+"bundle.yaml"] = &fstest.MapFile{Data: []byte("[]\n")} }, "kept", 2},
		{"no record", lock, func(h fstest.MapFS) { delete(h, rec) }, "file-missing", 2},
		{"a lock that does not read", "lockVersion: 2\n", nil, "plugins.lock", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := cloneFS(home)
			if tc.lock != "" {
				h["profiles/p/plugins.lock"] = &fstest.MapFile{Data: []byte(tc.lock)}
			}
			if tc.edit != nil {
				tc.edit(h)
			}
			in, _ := bootInput(h, "p", c, nil, d, nil, nil)
			a := in.Artifacts["acme/connector-x"]
			if tc.err == "" && (a.Err != nil || a.Digest != digest || a.Record.Signer != keyP256ID || a.Kept.Content != kept.Content) ||
				tc.err != "" && (a.Err == nil || !strings.Contains(a.Err.Error(), tc.err)) || len(in.Artifacts) != 1 {
				t.Fatalf("artifact %+v, want %q", a, tc.err)
			}
			if got := lockDigests(h, "p"); len(got) != tc.locks || tc.locks > 0 && got[1].Name != "content/acme/connector-x" {
				t.Fatalf("locks %+v", got)
			}
		})
	}
}

func cloneFS(m fstest.MapFS) fstest.MapFS {
	out := fstest.MapFS{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Boot refuses a binary whose embedded manifest it cannot read, and denies every row,
// so stops on the required ones, when the manifest is not trusted; the refusal names
// the recovery (ADR-020 decisions 12 and 13). A release binary boots on its built-in
// record while the default root is trusted, and prints so.
func TestRunTrustsTheEmbeddedManifest(t *testing.T) {
	t.Run("a manifest that does not read", func(t *testing.T) {
		f := newFixture(t, happyBundle)
		f.sign()
		f.opt.Signed = []byte("distVersion: 2\n")
		if code := Run(t.Context(), f.opt); code != ExitRefused || !strings.Contains(f.errb.String(), "distribution: ") {
			t.Fatalf("exit %d: %s", code, f.errb.String())
		}
	})
	t.Run("an untrusted manifest", func(t *testing.T) {
		f := newFixture(t, happyBundle)
		f.sign()
		delete(f.files, "verified/current")
		if code := Run(t.Context(), f.opt); code != ExitRefused || !strings.Contains(f.errb.String(), "gate check 5 (Trust), rule module") ||
			!strings.Contains(f.errb.String(), "to recover") {
			t.Fatalf("exit %d: %s", code, f.errb.String())
		}
	})
	t.Run("a release binary", func(t *testing.T) {
		f := newFixture(t, happyBundle)
		f.sign()
		delete(f.files, "trust.yaml")
		delete(f.files, "verified/current")
		f.opt.Release = &Release{KeyID: "K"}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan int, 1)
		go func() { done <- Run(ctx, f.opt) }()
		f.waitReady(t, done)
		cancel()
		if code := <-done; code != ExitOK || !strings.Contains(f.errb.String(), "the default release root, key K, is trusted") {
			t.Fatalf("exit %d: %s", code, f.errb.String())
		}
	})
}

// profile.yaml may set minStability, one of the three stabilities.
func TestProfileMinStability(t *testing.T) {
	for y, want := range map[string]string{"bundles: [b]\nminStability: preview": "preview", "bundles: [b]": "",
		"bundles: [b]\nminStability: beta": "error", "bundles: [b]\nminStability: [ga]": "error"} {
		home := fstest.MapFS{"profiles/p/profile.yaml": {Data: []byte(y)}, "bundles/b/bundle.yaml": {Data: []byte("[]")}}
		c, _, err := Compose(Sources{Profile: "p", Home: home, Bundles: home}, Options{})
		if want == "error" && err == nil || want != "error" && (err != nil || c.MinStability != want) {
			t.Errorf("%q: %v %q", y, err, c.MinStability)
		}
	}
}
