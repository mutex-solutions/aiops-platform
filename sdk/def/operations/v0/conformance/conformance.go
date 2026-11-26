// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the operations definition: what the operations
// row and the fake must both do (07-c4-plugin-sdk.md §8.2; ADR-061 decision 18). Its
// cases set up and observe a home only through the methods of operations.Operations
// and their results, Home above all, and read no file of the home; the state a case
// starts from is a Setup, which each subject's Mount makes. The obligations half holds
// a provider to key generate, the forms and refusals of trust add, dist sign and dist
// use on the images of a registry, the profile commands on the launcher's toolbox, the
// outcomes they answer, plugin pack, sign, push, inspect and verify of the pack of the
// suite and their refusals, and plugin install, enable, disable and remove with the
// outcome of their submission and what they refuse, and dist build through the
// toolbox. The guard half holds it to the write lock and the
// snapshot: a write after an interrupted one restores first, a second writer fails
// after its bound naming the holder, concurrent writers lose nothing; and to what it
// refuses once released, with no side door. Each case has an id, the first word of its
// name. The package imports the standard library, sdk/kernel and definitions only.
package conformance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Setup is the state a case starts from, which Mount makes for its subject.
type Setup struct {
	// LockWait is the bound a second writer waits for the write lock of the home.
	LockWait time.Duration
	// Held, when set, is the command recorded by a holder of the write lock of the
	// home, which holds it for the whole case.
	Held string
	// Interrupted makes the home what a trust add that was killed after its rename
	// left: a home with no trust.yaml before it, a trust.yaml with one signer for
	// left/* after it, and the snapshot of that write.
	Interrupted bool
	// Shipped and Rendered are what the launcher's toolbox answers, the toolbox of the
	// row and the config of the fake: the shipped files by path, and the result of
	// Render for each profile. use sets Shipped of the suite, and Rendered of the suite
	// for each profile that a case's Rendered does not name.
	Shipped  map[string][]byte
	Rendered map[string]launch.Rendered
	// Images are the images of the registry of the case, by repository: each is tagged
	// dist and carries its distribution manifest as a referrer with no bundle, as the
	// builder leaves it beside the image. use sets Images of the suite.
	Images map[string][]byte
	// Artifact is what the fake packs of Pack. use sets the suite's.
	Artifact operations.Inspected

	// Template is what the toolbox's Template answers for the Go template of echoers,
	// by path; it has no other template. use sets the suite's.
	Template map[string][]byte
	// Plugin is what the toolbox finds in the plugin of plugin check or plugin pack:
	// ReadManifest answers its summary, PackSummary unless a case sets one, Suite
	// fails the obligations of echoers in Failed, and the plugin's own tests skip the
	// tests in Skipped; Generate, Vet and Gate find nothing; and Release answers a
	// release whose serves hold the key echoers under echoers, which step 5 reads.
	Plugin Plugin
	// Verdict is what the toolbox's Gate answers at plugin install, and Result the final
	// result with which a process that runs the profile answers a submission over the
	// socket of ADR-062; with no Result no process runs.
	Verdict launch.Verdict
	Result  *reload.WireResult
	// Socket, when set, is how a process that runs the profile answers a submission in
	// place of Result (ADR-062 decision 11): operations.SubmitRefused, an error line
	// before an id; NoFinalResult, the id of each submission and then the end of the
	// connection, with an await of it answered unknown; VerdictUnknown, the id and then
	// nothing within Wait; ReasonNoAnswer, no connection within ConnectWait.
	Socket string
	// Built is what the toolbox's builder answers for dist build. use sets the suite's.
	Built launch.Built
}

// Built is what the builder of the suite builds, and the rows that plugin smoke boots:
// one ACTIVE, one that waits for a key and one FAILED.
var Built = launch.Built{Image: "/site/build/image", Manifest: []byte("distVersion: 1\n"), Aiopsctl: "/site/build/aiopsctl/aiopsctl",
	Rows: []launch.RowWait{{ID: "clock", State: kernel.Active}, {ID: "echo:echo", State: kernel.Pending, WaitsFor: []string{"echoers"}},
		{ID: "echo:broken", State: kernel.Failed}}}

// Pack is the plugin directory of the cases of the artifact, by path: a pack whose
// bundle inserts one row, disabled, with content and a fixture (ADR-047 decision 1).
var Pack = map[string][]byte{
	"plugin.yaml": []byte("manifestVersion: \"1.0\"\nid: acme/connector-x\nversion: 0.1.0\ntitle: Acme connector for X\nlicense: Apache-2.0\n" +
		"publisher: {id: acme}\nstability: experimental\nscope: [process]\nruntimes: []\nrequires: {definitions: {}}\n"),
	"bundle.yaml":                 []byte("- insert: [{id: logs, plugin: aiops/echoer, roles: [api], enabled: false, config: {endpoint: 'http://x.example.test'}}]\n"),
	"content/runbooks/restart.md": []byte("# Restart\n"),
	"content/labels.yaml":         []byte("service: x\n"),
	"fixtures/echo.json":          []byte("{}\n"),
}

// PackSummary is the summary of the manifest of Pack.
var PackSummary = launch.ManifestSummary{ID: "acme/connector-x", Version: "0.1.0", Definitions: map[string][]string{}}

// Artifact is what inspect reads of the artifact of Pack, its digest aside.
var Artifact = operations.Inspected{ID: "acme/connector-x", Version: "0.1.0",
	Rows:    []operations.Row{{ID: "logs", Plugin: "aiops/echoer", Config: map[string]any{"endpoint": "http://x.example.test"}}},
	Content: []string{"labels.yaml", "runbooks/restart.md"}}

// Images are the images of the suite, each with its distribution manifest.
var Images = map[string][]byte{"site": []byte("distVersion: 1\n"), "foreign": []byte("distVersion: 2\n"), "unsigned": []byte("distVersion: 3\n")}

// Plugin is what plugin check finds in a Go plugin that contributes to echoers.
type Plugin struct {
	Summary         launch.ManifestSummary
	Failed, Skipped []string
}

// Template is the Go template of echoers of the suite.
var Template = map[string][]byte{"plugin.go": []byte("package echo\n"), "internal/echo/echo.go": []byte("package echo\n")}

// Echo is the plugin of the suite: acme/echo, in process, contributing to echoers.
var Echo = launch.ManifestSummary{ID: "acme/echo", Version: "0.1.0", Runtimes: []string{"inproc"},
	Registers: []string{"echoers"}, Definitions: map[string][]string{"echoers": {"0.1"}}}

// Shipped are the shipped profiles of the suite, cli, dev and ops, which renders no row.
var Shipped = map[string][]byte{"profiles/cli/profile.yaml": []byte("bundles: [aiops-core, aiops-cli]\n"),
	"profiles/dev/profile.yaml": []byte("bundles: [aiops-core]\n"), "profiles/ops/profile.yaml": []byte("bundles: [aiops-core]\n")}

// Rendered is what the toolbox of the suite renders: cli with one departure from the
// release bindings and one dump, and dev with a required row, the one provider of a
// bound key, the row of the pack and a warning, which refuses nothing.
var Rendered = map[string]launch.Rendered{"cli": {Departures: []string{"the site adds consumers of appExit: acme/x"},
	Dumps: map[string]string{"--dump": "the dump of cli\n"}, Distribution: "sha256:ab"},
	"dev": {Composition: launch.Composition{Rows: []launch.Row{{ID: "loader", Plugin: "aiops/loader", Required: true},
		{ID: "store", Plugin: "acme/store"}, {ID: Row, Plugin: "aiops/echoer", Origin: "acme/connector-x"}}},
		Keys:        []launch.KeyProviders{{Name: "toy.policy", Provider: "bound", Consumer: "open", Rows: []string{"store"}}},
		Diagnostics: []launch.Diagnostic{{Check: "compose", File: "profiles/dev/aiops.patch.yml", Line: 1, Text: "a departure of no effect", Warning: true}}}}

// Row is the row of the pack once installed, under the group of its name.
const Row = "connector-x:logs"

// Passed is a final result that shows plugin enable of Row.
var Passed = &reload.WireResult{ID: 7, Source: "aiopsctl plugin enable", Status: reload.Passed, Generation: 2, Digest: "ab",
	Report: reload.WireReport{Pass: true, Rows: []reload.WireRow{{ID: Row, Outcome: "pass", State: "ACTIVE"}}}}

// Subject is a mounted provider of operations.Key.
type Subject struct {
	Root     *kernel.Root
	Provider *kernel.Fiber // ACTIVE; the suite releases the provider by disposing it
	Home     string        // the home of the case, absolute; it exists only where Setup asks for a state
	Dir      string        // an existing directory outside the home, for key files
	Registry string        // the host of a registry that holds the images of Setup
}

// Mount mounts a provider of operations.Key on a root of its own, its home in the
// state of s.
type Mount func(t *testing.T, s Setup) Subject

// Run runs the suite, each case on a provider of its own.
func Run(t *testing.T, mount Mount) {
	rec := deftest.Ran(t, "operations", "obligations", "guard")
	rec.Run(t, "obligations", func(t *testing.T) {
		rec.Run(t, "operations.O1 key generate writes a pair once, with the password of the environment", func(t *testing.T) {
			u := use(t, mount, Setup{})
			prefix := filepath.Join(u.s.Dir, "author")
			w, err := u.ops.GenerateKey(t.Context(), operations.KeyRequest{Caller: u.caller(), Prefix: prefix})
			if err != nil || !slices.Equal(w.Paths, []string{prefix + ".key", prefix + ".pub"}) {
				t.Fatalf("GenerateKey = %v, %v; want %s.key and %s.pub", w, err, prefix, prefix)
			}
			if _, err := u.ops.GenerateKey(t.Context(), operations.KeyRequest{Caller: u.caller(), Prefix: prefix}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("a second pair on one prefix: %v, want ErrRefused", err)
			}
			_, err = u.ops.GenerateKey(t.Context(), operations.KeyRequest{Caller: operations.Caller{}, Prefix: prefix + "2"})
			if !errors.Is(err, operations.ErrUsage) || !strings.Contains(err.Error(), operations.KeyPasswordVar) {
				t.Errorf("no password in the environment: %v, want ErrUsage naming %s", err, operations.KeyPasswordVar)
			}
			if _, err := u.ops.GenerateKey(t.Context(), operations.KeyRequest{Caller: u.caller(), Prefix: "relative"}); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("a relative prefix: %v, want ErrUsage", err)
			}
		})

		rec.Run(t, "operations.O2 trust add appends a signer with sorted sets", func(t *testing.T) {
			u := use(t, mount, Setup{})
			a, b := u.key("a"), u.key("b")
			ch := u.trust(operations.TrustRequest{KeyFile: a, IDs: []string{"acme/x", "acme/*", "acme/x"}, Runtimes: []string{"remote", "inproc"}, Signed: []string{"identities"}})
			if ch.Outcome != operations.NotRunning || !slices.Contains(ch.Written, filepath.Join(u.s.Home, "trust.yaml")) {
				t.Errorf("trust add = %+v; want trust.yaml written and the outcome not-running", ch)
			}
			u.trust(operations.TrustRequest{KeyFile: b, IDs: []string{"beta/*"}})
			h := u.home()
			if len(h.Signers) != 2 || len(h.Signers[0].KeyID) != 44 || h.Signers[0].KeyID == h.Signers[1].KeyID {
				t.Fatalf("signers %+v; want two, each with its key id", h.Signers)
			}
			want := []operations.Signer{{KeyID: h.Signers[0].KeyID, IDs: []string{"acme/*", "acme/x"}, Runtimes: []string{"inproc", "remote"}, Signed: []string{"identities"}},
				{KeyID: h.Signers[1].KeyID, IDs: []string{"beta/*"}}}
			if !reflect.DeepEqual(norm(h.Signers), norm(want)) || !slices.Equal(h.Distributions, []string{operations.Release}) {
				t.Errorf("home %+v; want signers %+v and the default root alone", h, want)
			}
		})

		rec.Run(t, "operations.O3 trust add of a distribution keeps the default root", func(t *testing.T) {
			u := use(t, mount, Setup{})
			a, b := u.key("a"), u.key("b")
			u.trust(operations.TrustRequest{KeyFile: a, Distribution: true})
			u.trust(operations.TrustRequest{KeyFile: b, Distribution: true})
			u.trust(operations.TrustRequest{KeyFile: a, IDs: []string{"acme/*"}})
			h := u.home()
			if len(h.Distributions) != 3 || h.Distributions[0] != operations.Release || h.Distributions[1] != h.Signers[0].KeyID || h.Distributions[2] == h.Distributions[1] {
				t.Fatalf("distributions %v with signer %+v; want release, then the key of a, then that of b", h.Distributions, h.Signers)
			}
			u.refused(operations.TrustRequest{KeyFile: a, Distribution: true}, "a distribution key listed twice")
		})

		rec.Run(t, "operations.O4 each refused form of trust add writes nothing", func(t *testing.T) {
			u := use(t, mount, Setup{})
			a := u.key("a")
			u.trust(operations.TrustRequest{KeyFile: a, IDs: []string{"acme/*"}})
			for _, c := range []struct {
				why string
				r   operations.TrustRequest
			}{
				{"a bare wildcard", operations.TrustRequest{KeyFile: a, IDs: []string{"*"}}},
				{"a wildcard inside a name", operations.TrustRequest{KeyFile: a, IDs: []string{"acme/ech*"}}},
				{"an owner alone", operations.TrustRequest{KeyFile: a, IDs: []string{"acme"}}},
				{"an id that is not kebab-case", operations.TrustRequest{KeyFile: a, IDs: []string{"Acme/x"}}},
				{"the owner aiops", operations.TrustRequest{KeyFile: a, IDs: []string{"aiops/x"}}},
				{"the owner contrib", operations.TrustRequest{KeyFile: a, IDs: []string{"contrib/*"}}},
				{"the runtime wasm", operations.TrustRequest{KeyFile: a, IDs: []string{"acme/*"}, Runtimes: []string{"wasm"}}},
				{"an unknown runtime", operations.TrustRequest{KeyFile: a, IDs: []string{"acme/*"}, Runtimes: []string{"jvm"}}},
				{"a signed entry that is no key or hook", operations.TrustRequest{KeyFile: a, IDs: []string{"acme/*"}, Signed: []string{"no such key!"}}},
				{"neither ids nor a distribution", operations.TrustRequest{KeyFile: a}},
				{"ids and a distribution", operations.TrustRequest{KeyFile: a, IDs: []string{"acme/*"}, Distribution: true}},
				{"runtimes on a distribution", operations.TrustRequest{KeyFile: a, Runtimes: []string{"inproc"}, Distribution: true}},
				{"a private key file", operations.TrustRequest{KeyFile: strings.TrimSuffix(a, ".pub") + ".key", IDs: []string{"acme/*"}}},
				{"a key file that does not exist", operations.TrustRequest{KeyFile: filepath.Join(u.s.Dir, "none.pub"), IDs: []string{"acme/*"}}},
			} {
				u.refused(c.r, c.why)
			}
		})

		rec.Run(t, "operations.O5 no home is a usage error, and a missing home reads empty", func(t *testing.T) {
			u := use(t, mount, Setup{})
			if h := u.home(); len(h.Signers) != 0 || !slices.Equal(h.Distributions, []string{operations.Release}) || len(h.Records) != 0 ||
				h.Current != "" || h.Snapshot {
				t.Errorf("a home that does not exist reads %+v; want it empty, with the default root", h)
			}
			a := u.key("a")
			calls := map[string]error{}
			_, calls["Home"] = u.ops.Home(t.Context(), operations.HomeRequest{Caller: u.caller()})
			_, calls["AddTrust"] = u.ops.AddTrust(t.Context(), operations.TrustRequest{Caller: u.caller(), KeyFile: a, IDs: []string{"acme/*"}})
			_, calls["UseDist"] = u.ops.UseDist(t.Context(), operations.UseRequest{Caller: u.caller(), Image: u.image("site")})
			_, calls["InitProfile"] = u.ops.InitProfile(t.Context(), operations.ProfileRequest{Caller: u.caller(), Profile: "cli"})
			_, calls["SanitizeProfile"] = u.ops.SanitizeProfile(t.Context(), operations.ProfileRequest{Caller: u.caller()})
			for name, err := range calls {
				if !errors.Is(err, operations.ErrUsage) || !strings.Contains(err.Error(), operations.HomeVar) {
					t.Errorf("%s with no home: %v, want ErrUsage naming %s", name, err, operations.HomeVar)
				}
			}
		})

		rec.Run(t, "operations.O6 dist sign and dist use record a distribution and its image", func(t *testing.T) {
			u := use(t, mount, Setup{})
			pub := u.key("site")
			u.trust(operations.TrustRequest{KeyFile: pub, Distribution: true})
			img := u.image("site")
			w, err := u.ops.SignDist(t.Context(), operations.SignDistRequest{Caller: u.caller(), Image: img, Key: private(pub)})
			if err != nil || len(w.Paths) == 0 || slices.ContainsFunc(w.Paths, func(p string) bool { return !strings.HasPrefix(p, u.s.Registry+"/site@sha256:") }) {
				t.Fatalf("SignDist = %v, %v; want the referrers it pushed, by digest", w, err)
			}
			ch, err := u.ops.UseDist(t.Context(), operations.UseRequest{Caller: u.caller(), Home: u.s.Home, Image: img})
			if err != nil || ch.Outcome != operations.RestartRequired || ch.Reason != operations.ReasonImage {
				t.Fatalf("UseDist = %+v, %v; want restart-required for the reason image", ch, err)
			}
			sum := sha256.Sum256(Images["site"])
			d := "sha256:" + hex.EncodeToString(sum[:])
			h := u.home()
			want := []operations.Record{{Digest: d, Kind: "distribution", Signer: h.Distributions[1]}}
			if !reflect.DeepEqual(h.Records, want) || h.Current != d || !strings.HasPrefix(h.Image, u.s.Registry+"/site@sha256:") {
				t.Errorf("records %+v, current %q and image %q; want %+v, %s and the image by digest", h.Records, h.Current, h.Image, want, d)
			}
		})

		rec.Run(t, "operations.O7 dist use refuses what no trusted key verifies, and writes nothing", func(t *testing.T) {
			u := use(t, mount, Setup{})
			site, other := u.key("site"), u.key("other")
			u.trust(operations.TrustRequest{KeyFile: site, Distribution: true})
			before := u.home()
			u.signed("foreign", other)
			for why, img := range map[string]string{"an unsigned image": u.image("unsigned"), "an image signed by an untrusted key": u.image("foreign"),
				"an image the registry does not hold": u.s.Registry + "/none:dist"} {
				if _, err := u.ops.UseDist(t.Context(), operations.UseRequest{Caller: u.caller(), Home: u.s.Home, Image: img}); !errors.Is(err, operations.ErrRefused) {
					t.Errorf("%s: %v, want ErrRefused", why, err)
				}
			}
			if after := u.home(); !reflect.DeepEqual(after, before) {
				t.Errorf("a refused dist use changed the home: %+v, was %+v", after, before)
			}
			img := u.image("site")
			wrong := operations.Caller{Env: env(map[string]string{operations.KeyPasswordVar: "not the password"})}
			if _, err := u.ops.SignDist(t.Context(), operations.SignDistRequest{Caller: wrong, Image: img, Key: private(site)}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("dist sign with a wrong password: %v, want ErrRefused", err)
			}
			if _, err := u.ops.SignDist(t.Context(), operations.SignDistRequest{Image: img, Key: private(site)}); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("dist sign with no password in the environment: %v, want ErrUsage", err)
			}
			if _, err := u.ops.SignDist(t.Context(), operations.SignDistRequest{Caller: u.caller(), Image: u.s.Registry + "/none:dist", Key: private(site)}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("dist sign of an image the registry does not hold: %v, want ErrRefused", err)
			}
		})

		rec.Run(t, "operations.O8 profile init copies a shipped profile once", func(t *testing.T) {
			u := use(t, mount, Setup{})
			ch := u.profile(u.ops.InitProfile, "cli")
			p := filepath.Join(u.s.Home, "profiles", "cli", "profile.yaml")
			if !slices.Equal(ch.Written, []string{p}) || ch.Outcome != operations.NotRunning || !hasNote(ch, "copied") {
				t.Errorf("profile init cli = %+v; want %s written, a note that says so, and not-running", ch, p)
			}
			if h := u.home(); !slices.Equal(h.Profiles, []string{"cli"}) {
				t.Errorf("profiles %v; want cli", h.Profiles)
			}
			if ch := u.profile(u.ops.InitProfile, "cli"); len(ch.Written) != 0 || ch.Outcome != "" || !hasNote(ch, "kept") {
				t.Errorf("a second profile init cli = %+v; want nothing written or submitted and a note that the profile is kept", ch)
			}
			before := u.home()
			for p, want := range map[string]error{"nope": operations.ErrRefused, "": operations.ErrUsage, "..": operations.ErrUsage, "a/b": operations.ErrUsage} {
				if _, err := u.ops.InitProfile(t.Context(), operations.ProfileRequest{Caller: u.caller(), Home: u.s.Home, Profile: p}); !errors.Is(err, want) {
					t.Errorf("profile init %q: %v, want %v", p, err, want)
				}
			}
			if after := u.home(); !reflect.DeepEqual(after, before) {
				t.Errorf("a refused profile init changed the home: %+v, was %+v", after, before)
			}
		})

		rec.Run(t, "operations.O9 render and verify answer what the toolbox renders", func(t *testing.T) {
			u := use(t, mount, Setup{})
			q := launch.RenderRequest{Home: u.s.Home, Profile: "cli", Dumps: []string{"--dump"}}
			r, err := u.ops.Render(t.Context(), operations.RenderRequest{Caller: u.caller(), RenderRequest: q})
			if err != nil || !reflect.DeepEqual(r, Rendered["cli"]) {
				t.Errorf("Render = %+v, %v; want %+v", r, err, Rendered["cli"])
			}
			q.Home = ""
			if _, err := u.ops.Render(t.Context(), operations.RenderRequest{Caller: u.caller(), RenderRequest: q}); err != nil {
				t.Errorf("Render with no home, which composes the shipped profiles alone: %v", err)
			}
			v, err := u.ops.VerifyProfile(t.Context(), operations.RenderRequest{Caller: u.caller(), RenderRequest: launch.RenderRequest{Home: u.s.Home}})
			if err != nil || !slices.Equal(v.Departures, Rendered["cli"].Departures) {
				t.Errorf("VerifyProfile = %+v, %v; want the departures of cli", v, err)
			}
			q.Home = "relative"
			if _, err := u.ops.Render(t.Context(), operations.RenderRequest{Caller: u.caller(), RenderRequest: q}); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("Render of a relative home: %v, want ErrUsage", err)
			}
		})

		rec.Run(t, "operations.O10 profile sanitize of a profile that composes changes nothing", func(t *testing.T) {
			u := use(t, mount, Setup{})
			u.profile(u.ops.InitProfile, "cli")
			before := u.home()
			if ch := u.profile(u.ops.SanitizeProfile, ""); len(ch.Written) != 0 || ch.Outcome != "" || len(ch.Notes) == 0 {
				t.Errorf("profile sanitize = %+v; want nothing written or submitted, and a note that says so", ch)
			}
			if after := u.home(); !reflect.DeepEqual(after, before) {
				t.Errorf("profile sanitize changed the home: %+v, was %+v", after, before)
			}
		})

		rec.Run(t, "operations.O11 pack, sign and push an artifact that inspect and verify read", func(t *testing.T) {
			u := use(t, mount, Setup{})
			dir := u.plugin()
			out := filepath.Join(dir, "artifact")
			if w, err := u.ops.Pack(t.Context(), operations.PackRequest{Caller: u.caller(), Dir: dir, Out: out}); err != nil || !slices.Equal(w.Paths, []string{out}) {
				t.Fatalf("Pack = %v, %v; want the layout %s", w, err, out)
			}
			in := u.inspect(out)
			pub := u.key("author")
			u.trust(operations.TrustRequest{KeyFile: pub, IDs: []string{"acme/*"}})
			if _, err := u.verify(out); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("verify of an unsigned artifact: %v, want ErrRefused", err)
			}
			if w, err := u.ops.Sign(t.Context(), operations.SignRequest{Caller: u.caller(), Out: out, Key: private(pub)}); err != nil || !slices.Equal(w.Paths, []string{out}) {
				t.Fatalf("Sign = %v, %v; want the layout %s", w, err, out)
			}
			u.verified(out, in.Digest)
			repo := u.s.Registry + "/connector-x"
			p, err := u.ops.Push(t.Context(), operations.PushRequest{Caller: u.caller(), Out: out, Registry: repo})
			if err != nil || p.Spec != "oci://"+repo+"@"+in.Digest {
				t.Fatalf("Push = %+v, %v; want the spec by the digest %s", p, err, in.Digest)
			}
			for _, spec := range []string{p.Spec, "oci://" + repo + ":0.1.0"} {
				if got := u.inspect(spec); !reflect.DeepEqual(got, in) {
					t.Errorf("inspect %s = %+v; want %+v, as of the layout", spec, got, in)
				}
				u.verified(spec, in.Digest)
			}
		})

		rec.Run(t, "operations.O12 pack, sign, inspect and verify refuse", func(t *testing.T) {
			u := use(t, mount, Setup{})
			dir := u.plugin()
			out := filepath.Join(dir, "artifact")
			if _, err := u.ops.Pack(t.Context(), operations.PackRequest{Caller: u.caller(), Dir: dir, Out: out}); err != nil {
				t.Fatal(err)
			}
			if _, err := u.ops.Pack(t.Context(), operations.PackRequest{Caller: u.caller(), Dir: dir, Out: out}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("pack into a layout that is not empty: %v, want ErrRefused", err)
			}
			if _, err := u.ops.Pack(t.Context(), operations.PackRequest{Caller: u.caller(), Dir: dir, Out: "relative"}); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("pack into a relative layout: %v, want ErrUsage", err)
			}
			if err := os.Remove(filepath.Join(dir, "bundle.yaml")); err != nil {
				t.Fatal(err)
			}
			if _, err := u.ops.Pack(t.Context(), operations.PackRequest{Caller: u.caller(), Dir: dir, Out: filepath.Join(dir, "other")}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("pack of a directory with no bundle.yaml: %v, want ErrRefused", err)
			}
			if _, err := u.ops.Inspect(t.Context(), operations.SpecRequest{Caller: u.caller(), Spec: filepath.Join(dir, "none")}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("inspect of a layout that does not exist: %v, want ErrRefused", err)
			}
			author, other := u.key("author"), u.key("other")
			wrong := operations.Caller{Env: env(map[string]string{operations.KeyPasswordVar: "not the password"})}
			if _, err := u.ops.Sign(t.Context(), operations.SignRequest{Caller: wrong, Out: out, Key: private(author)}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("sign with a wrong password: %v, want ErrRefused", err)
			}
			u.trust(operations.TrustRequest{KeyFile: author, IDs: []string{"acme/*"}})
			u.trust(operations.TrustRequest{KeyFile: other, IDs: []string{"beta/*"}})
			if _, err := u.ops.Sign(t.Context(), operations.SignRequest{Caller: u.caller(), Out: out, Key: private(other)}); err != nil {
				t.Fatal(err)
			}
			if _, err := u.verify(out); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("verify of an artifact signed by a key trust.yaml lists for other ids: %v, want ErrRefused", err)
			}
		})

		rec.Run(t, "operations.O13 plugin new writes the template into a new directory, once", func(t *testing.T) {
			u := use(t, mount, Setup{})
			dir := filepath.Join(u.s.Dir, "echo")
			q := operations.NewRequest{Caller: u.caller(), Registry: "echoers", ID: "acme/echo", Lang: "go", Dir: dir}
			w, err := u.ops.New(t.Context(), q)
			want := []string{filepath.Join(dir, "internal/echo/echo.go"), filepath.Join(dir, "plugin.go")}
			if err != nil || !slices.Equal(w.Paths, want) {
				t.Fatalf("New = %v, %v; want %v", w.Paths, err, want)
			}
			if _, err := u.ops.New(t.Context(), q); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("plugin new into a directory that holds a plugin: %v, want ErrRefused", err)
			}
			for why, r := range map[string]operations.NewRequest{
				"a registry with no template": {Registry: "tools", ID: "acme/x", Lang: "go", Dir: filepath.Join(u.s.Dir, "x")},
				"a language with no template": {Registry: "echoers", ID: "acme/y", Lang: "python", Dir: filepath.Join(u.s.Dir, "y")},
			} {
				r.Caller = u.caller()
				if w, err := u.ops.New(t.Context(), r); !errors.Is(err, operations.ErrRefused) || len(w.Paths) != 0 {
					t.Errorf("%s: %v, %v; want ErrRefused and nothing written", why, w, err)
				}
			}
			if _, err := u.ops.New(t.Context(), operations.NewRequest{Caller: u.caller(), Registry: "echoers", ID: "acme/z", Lang: "go", Dir: "z"}); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("a relative directory: %v, want ErrUsage", err)
			}
		})

		rec.Run(t, "operations.O14 plugin check reports eight steps and the attestation it would sign", func(t *testing.T) {
			u := use(t, mount, Setup{Plugin: Plugin{Summary: Echo, Skipped: []string{"TestTwoTenants"}}})
			r := u.check()
			want := []string{operations.StepPass, operations.StepPass, operations.StepPass, operations.StepPass,
				operations.StepPass, operations.StepNotApplicable, operations.StepPass, operations.StepNotApplicable}
			if got := outcomes(r); !slices.Equal(got, want) || !r.Passed() {
				t.Fatalf("steps %v, want %v", got, want)
			}
			var st struct {
				Type    string `json:"_type"`
				Subject []struct{ Name string }
				Pred    struct {
					Definitions map[string]struct {
						Version string
						Suite   int
						Halves  []string
					}
					NotRun []string
				} `json:"predicate"`
			}
			if err := json.Unmarshal(r.Attestation, &st); err != nil {
				t.Fatalf("the attestation %s: %v", r.Attestation, err)
			}
			echo := st.Pred.Definitions["echoers"]
			if st.Type != "https://in-toto.io/Statement/v1" || len(st.Subject) != 1 || st.Subject[0].Name != "acme/echo@0.1.0" ||
				echo.Version != "0.1" || echo.Suite != 1 || !slices.Equal(echo.Halves, []string{"obligations"}) ||
				!slices.Equal(st.Pred.NotRun, []string{"TestTwoTenants"}) {
				t.Errorf("the attestation %s; want acme/echo@0.1.0 with echoers 0.1, suite 1, the obligations, and TestTwoTenants not run", r.Attestation)
			}
		})

		rec.Run(t, "operations.O15 a failed step stops plugin check with one line per obligation", func(t *testing.T) {
			u := use(t, mount, Setup{Plugin: Plugin{Summary: Echo, Failed: []string{"echoers/reply", "echoers/stable"}}})
			r := u.check()
			want := []string{operations.StepPass, operations.StepPass, operations.StepPass, operations.StepPass,
				operations.StepFail, operations.StepSkipped, operations.StepSkipped, operations.StepSkipped}
			if got := outcomes(r); !slices.Equal(got, want) || r.Passed() || r.Attestation != nil {
				t.Fatalf("steps %v and attestation %s; want %v and none", got, r.Attestation, want)
			}
			var ids []string
			for _, d := range r.Steps[4].Diagnostics {
				ids = append(ids, d.Check)
			}
			if !slices.Equal(ids, []string{"echoers/reply", "echoers/stable"}) {
				t.Errorf("step 5 says %+v; want one line per failed obligation, by its id", r.Steps[4].Diagnostics)
			}
			if _, err := u.ops.Check(t.Context(), operations.CheckRequest{Caller: u.caller(), Dir: "plugin"}); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("a relative directory: %v, want ErrUsage", err)
			}
		})

		rec.Run(t, "operations.O16 plugin install keeps the artifact, locks a pack and submits", func(t *testing.T) {
			u := use(t, mount, Setup{})
			spec := u.signedPack()
			in := u.inspect(spec)
			q := operations.InstallRequest{Caller: u.caller(), Home: u.s.Home, Profile: "dev", Spec: spec}
			ch, err := u.ops.Install(t.Context(), q)
			if err != nil || ch.Outcome != operations.NotRunning || !hasNote(ch, "copied") {
				t.Fatalf("Install = %+v, %v; want not-running, with the shipped dev copied first", ch, err)
			}
			h := u.homeOf("dev")
			lock := slices.IndexFunc(h.Locked, func(l operations.Locked) bool { return l.ID == in.ID && l.Digest == in.Digest })
			if !slices.Equal(h.Bundles, []string{"aiops-core", in.ID}) || len(h.Locked) != 1 || lock < 0 ||
				!slices.ContainsFunc(h.Records, func(r operations.Record) bool { return r.Digest == in.Digest && r.ID == in.ID }) {
				t.Errorf("after install: %+v; want the bundle last, one lock entry and the record of %s", h, in.Digest)
			}
			if _, err := u.ops.Install(t.Context(), q); !errors.Is(err, operations.ErrRefused) || !reflect.DeepEqual(u.homeOf("dev"), h) {
				t.Errorf("a second install of %s: %v; want ErrRefused and nothing written", in.ID, err)
			}
		})

		rec.Run(t, "operations.O17 plugin install writes nothing on a denial, rebuild-required or a pack into cli", func(t *testing.T) {
			deny := launch.Verdict{Clauses: []launch.Clause{{Check: 2, Outcome: launch.OutcomeDeny, Reason: "requires echoers 0.2"}}}
			for why, c := range map[string]struct {
				s       Setup
				profile string
				want    error
			}{
				"a denial":         {Setup{Verdict: deny}, "dev", operations.ErrRefused},
				"rebuild-required": {Setup{Verdict: launch.Verdict{RebuildRequired: true}}, "dev", operations.ErrRebuildRequired},
				"a pack into cli":  {Setup{}, "cli", operations.ErrRefused},
			} {
				u := use(t, mount, c.s)
				spec := u.signedPack()
				before := u.homeOf(c.profile)
				ch, err := u.ops.Install(t.Context(), operations.InstallRequest{Caller: u.caller(), Home: u.s.Home, Profile: c.profile, Spec: spec})
				if !errors.Is(err, c.want) || !reflect.DeepEqual(u.homeOf(c.profile), before) || why == "a denial" && len(ch.Verdict.Clauses) != 1 {
					t.Errorf("%s: %+v, %v; want %v, the verdict, and nothing written", why, ch, err, c.want)
				}
				// The lines a site adds to the plugins of its dist.yaml (08:397, :401).
				if l := ch.DistLines; why == "rebuild-required" && (len(l) != 2 || l[0] != "plugins:" || !strings.HasPrefix(l[1], "  - {module: ") || !strings.Contains(l[1], ", sum: ")) {
					t.Errorf("rebuild-required: the dist.yaml lines %q; want plugins: and one {module, version, sum} of its module", l)
				}
			}
		})

		rec.Run(t, "operations.O18 plugin enable and disable write a departure from the bundle layers and submit, and disable refuses what the manager cannot", func(t *testing.T) {
			u := use(t, mount, Setup{Result: Passed})
			ch, err := u.ops.Enable(t.Context(), u.row(Row))
			if err != nil || ch.Result == nil || ch.Result.Status != reload.Passed || ch.Result.Generation != 2 || !u.homeOf("dev").Enabled[Row] {
				t.Fatalf("Enable = %+v, %v; want the passed result and the departure", ch, err)
			}
			for _, row := range []string{"loader", "store"} { // a required row; the one provider of a bound key
				before := u.homeOf("dev")
				if _, err := u.ops.Disable(t.Context(), u.row(row)); !errors.Is(err, operations.ErrRefused) || !reflect.DeepEqual(u.homeOf("dev"), before) {
					t.Errorf("disable %s: %v; want ErrRefused and nothing written", row, err)
				}
			}
			// The bundle layers leave the row disabled, so disable drops the departure, and
			// writes none of its own (08:375). The result still holds the row ACTIVE: a
			// pass that does not show the change keeps the files and fails.
			ch, err = u.ops.Disable(t.Context(), u.row(Row))
			if _, ok := u.homeOf("dev").Enabled[Row]; !errors.Is(err, operations.ErrSubmission) || ch.Result == nil || ok {
				t.Errorf("disable with the row still ACTIVE: %+v, %v; want ErrSubmission with the departure dropped", ch, err)
			}
		})

		rec.Run(t, "operations.O19 a result that does not pass restores the files, and plugin remove detaches", func(t *testing.T) {
			u := use(t, mount, Setup{Result: &reload.WireResult{ID: 8, Status: reload.RolledBack, Generation: 3}})
			before := u.home()
			if ch, err := u.ops.Enable(t.Context(), u.row(Row)); !errors.Is(err, operations.ErrSubmission) || ch.Result == nil || !reflect.DeepEqual(u.home(), before) {
				t.Errorf("enable rolled back: %+v, %v; want ErrSubmission and the home restored", ch, err)
			}
			u = use(t, mount, Setup{})
			if err := u.install(u.signedPack(), "dev"); err != nil {
				t.Fatal(err)
			}
			q := operations.RemoveRequest{Caller: u.caller(), Home: u.s.Home, Profile: "dev", ID: "acme/connector-x"}
			if ch, err := u.ops.Remove(t.Context(), q); err != nil || ch.Outcome != operations.NotRunning {
				t.Errorf("Remove = %+v, %v; want not-running", ch, err)
			}
			if h := u.homeOf("dev"); !slices.Equal(h.Bundles, []string{"aiops-core"}) || len(h.Locked) != 0 {
				t.Errorf("after remove: %+v; want the bundle and the lock entry gone", h)
			}
			if _, err := u.ops.Remove(t.Context(), q); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("remove of what is not installed: %v, want ErrRefused", err)
			}
		})
		rec.Run(t, "operations.O20 dist build answers what the toolbox builds and refuses a relative path", func(t *testing.T) {
			u := use(t, mount, Setup{})
			q := operations.BuildRequest{Caller: u.caller(), BuildRequest: launch.BuildRequest{DistYAML: filepath.Join(u.s.Dir, "dist.yaml"),
				ModCache: filepath.Join(u.s.Dir, "modcache"), Builder: "local"}}
			if got, err := u.ops.Build(t.Context(), q); err != nil || !reflect.DeepEqual(got, Built) {
				t.Errorf("Build = %+v, %v; want %+v", got, err, Built)
			}
			q.DistYAML = "dist.yaml"
			if _, err := u.ops.Build(t.Context(), q); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("a relative dist.yaml: %v, want ErrUsage", err)
			}
			if h := u.home(); h.Current != "" || len(h.Records) != 0 {
				t.Errorf("dist build wrote the home: %+v", h)
			}
		})
		rec.Run(t, "operations.O21 plugin remove takes an installed artifact only, and drops the enabled departures of its rows", func(t *testing.T) {
			u := use(t, mount, Setup{})
			if err := u.install(u.signedPack(), "dev"); err != nil {
				t.Fatal(err)
			}
			if _, err := u.ops.Enable(t.Context(), u.row(Row)); err != nil {
				t.Fatal(err)
			}
			before := u.homeOf("dev")
			if err := u.remove("dev", "aiops-core"); !errors.Is(err, operations.ErrRefused) || !reflect.DeepEqual(u.homeOf("dev"), before) {
				t.Errorf("remove of aiops-core, a bundle that is no installed artifact: %v; want ErrRefused and nothing written", err)
			}
			if err := u.remove("dev", "acme/connector-x"); err != nil {
				t.Fatal(err)
			}
			if en, ok := u.homeOf("dev").Enabled[Row]; ok {
				t.Errorf("after remove the departure {id: %s, enabled: %v} stays; want it dropped, so a later install leaves the row disabled", Row, en)
			}
		})
		rec.Run(t, "operations.O22 a site holds one digest of a plugin id", func(t *testing.T) {
			u := use(t, mount, Setup{})
			v1 := u.signedPack()
			if err := os.WriteFile(filepath.Join(filepath.Dir(v1), "content", "labels.yaml"), []byte("service: y\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			v2 := filepath.Join(u.s.Dir, "v2")
			u.packed(filepath.Dir(v1), v2, filepath.Join(u.s.Dir, "author.key"))
			d1, d2 := u.inspect(v1).Digest, u.inspect(v2).Digest
			if err := u.install(v1, "dev"); err != nil || d1 == d2 {
				t.Fatalf("install of %s: %v; and %s, want another digest for other content", d1, err, d2)
			}
			before := u.home()
			if err := u.install(v2, "ops"); !errors.Is(err, operations.ErrRefused) || !strings.Contains(err.Error(), d1) || !strings.Contains(err.Error(), "dev") ||
				!reflect.DeepEqual(u.home(), before) {
				t.Errorf("install of %s while dev holds %s: %v; want ErrRefused naming dev and %s, and nothing written", d2, d1, err, d1)
			}
			if err := u.install(v1, "ops"); err != nil {
				t.Errorf("install into ops of the digest that dev holds: %v", err)
			}
			for _, p := range []string{"dev", "ops"} {
				if err := u.remove(p, "acme/connector-x"); err != nil {
					t.Fatalf("remove from %s: %v", p, err)
				}
			}
			if err := u.install(v2, "dev"); err != nil {
				t.Errorf("install of %s once no profile names acme/connector-x: %v", d2, err)
			}
		})
		rec.Run(t, "operations.O23 plugin smoke answers the rows the toolbox booted and fails one that is neither ACTIVE nor waiting", func(t *testing.T) {
			u := use(t, mount, Setup{})
			q := operations.BuildRequest{Caller: u.caller(), BuildRequest: launch.BuildRequest{Plugin: filepath.Join(u.s.Dir, "plugin"),
				Against: "oci://registry.test/aiops/release@sha256:" + strings.Repeat("ab", 32), ModCache: filepath.Join(u.s.Dir, "modcache"), Builder: "local"}}
			got, err := u.ops.Smoke(t.Context(), q)
			if !reflect.DeepEqual(got, Built) || !errors.Is(err, operations.ErrRefused) || !strings.Contains(err.Error(), "echo:broken") || strings.Contains(err.Error(), "echo:echo ") {
				t.Errorf("Smoke = %+v, %v; want %+v and a refusal naming echo:broken alone", got, err, Built)
			}
			q.Plugin = "plugin"
			if _, err := u.ops.Smoke(t.Context(), q); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("a relative plugin directory: %v, want ErrUsage", err)
			}
			q.Plugin, q.Against = filepath.Join(u.s.Dir, "plugin"), "oci://registry.test/aiops/release:latest"
			if _, err := u.ops.Smoke(t.Context(), q); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("a release named by a tag, not by digest (08:401): %v, want ErrUsage", err)
			}
			if h := u.home(); h.Current != "" || len(h.Records) != 0 {
				t.Errorf("plugin smoke wrote the home: %+v", h)
			}
		})
		rec.Run(t, "operations.O24 plugin enable and disable write nothing while the profile does not compose", func(t *testing.T) {
			dev := Rendered["dev"]
			dev.Diagnostics = append(slices.Clone(dev.Diagnostics), launch.Diagnostic{Check: "compose", File: "profiles/dev/aiops.patch.yml", Line: 2, Text: "acme/gone is no plugin"})
			u := use(t, mount, Setup{Rendered: map[string]launch.Rendered{"dev": dev}})
			for name, m := range map[string]func(context.Context, operations.RowRequest) (operations.Change, error){"enable": u.ops.Enable, "disable": u.ops.Disable} {
				before := u.homeOf("dev")
				if _, err := m(t.Context(), u.row(Row)); !errors.Is(err, operations.ErrRefused) || !reflect.DeepEqual(u.homeOf("dev"), before) {
					t.Errorf("%s %s of a profile that does not compose: %v; want ErrRefused and nothing written", name, Row, err)
				}
			}
		})
		rec.Run(t, "operations.O25 plugin remove refuses to take away every enabled provider of a bound or a required key", func(t *testing.T) {
			bound, required := Rendered["dev"], Rendered["dev"] // the rows of the pack, none the last provider of a key alone
			bound.Composition.Rows = append(slices.Clone(bound.Composition.Rows), launch.Row{ID: "connector-x:metrics", Plugin: "aiops/echoer", Origin: "acme/connector-x"})
			bound.Keys = append(slices.Clone(bound.Keys), launch.KeyProviders{Name: "toy.sink", Provider: "bound", Consumer: "open", Rows: []string{Row, "connector-x:metrics"}})
			required.Keys = append(slices.Clone(required.Keys), launch.KeyProviders{Name: "toy.logs", Provider: "open", Consumer: "open", Rows: []string{Row}})
			required.Required.Keys = []string{"toy.logs"}
			for key, dev := range map[string]launch.Rendered{"toy.sink": bound, "toy.logs": required} {
				u := use(t, mount, Setup{Rendered: map[string]launch.Rendered{"dev": dev}})
				if err := u.install(u.signedPack(), "dev"); err != nil {
					t.Fatal(err)
				}
				before := u.homeOf("dev")
				if err := u.remove("dev", "acme/connector-x"); !errors.Is(err, operations.ErrRefused) || !strings.Contains(fmt.Sprint(err), key) || !reflect.DeepEqual(u.homeOf("dev"), before) {
					t.Errorf("remove of the last providers of %s: %v; want ErrRefused naming the key, and nothing written", key, err)
				}
			}
		})
		rec.Run(t, "operations.O26 a submission with no final result keeps or restores the files, and a write the wire does not carry is not submitted", func(t *testing.T) {
			for _, c := range []struct{ socket, says string }{{operations.SubmitRefused, "refused"}, {operations.NoFinalResult, "no-final-result"},
				{operations.VerdictUnknown, "verdict-unknown"}, {operations.ReasonNoAnswer, "restart-required"}} {
				u := use(t, mount, Setup{Socket: c.socket})
				before, q := u.homeOf("dev"), u.row(Row)
				q.ConnectWait, q.Wait = time.Second, time.Second
				ch, err := u.ops.Enable(t.Context(), q)
				refused, after := c.socket == operations.SubmitRefused, u.homeOf("dev")
				switch {
				case !errors.Is(err, operations.ErrSubmission) || !strings.Contains(err.Error(), ": "+c.says+":"):
					t.Errorf("%s: %+v, %v; want ErrSubmission saying %s", c.socket, ch, err, c.says)
				case refused && (ch.Error == nil || !reflect.DeepEqual(after, before)), !refused && !after.Enabled[Row]:
					t.Errorf("%s: %+v, the departure of %s %v; want the error line and the files restored, or the files kept", c.socket, ch, Row, after.Enabled[Row])
				case c.socket == operations.NoFinalResult && len(ch.IDs) != 2, c.socket == operations.VerdictUnknown && len(ch.IDs) != 1,
					c.socket == operations.ReasonNoAnswer && (ch.Outcome != operations.RestartRequired || ch.Reason != operations.ReasonNoAnswer):
					t.Errorf("%s: %+v; want the id of each submission, or restart-required for no-answer", c.socket, ch)
				}
			}
			u := use(t, mount, Setup{Result: Passed})
			if ch := u.trust(operations.TrustRequest{KeyFile: u.key("a"), IDs: []string{"acme/*"}}); ch.Outcome != operations.RestartRequired ||
				ch.Reason != operations.ReasonNotSubmitted || len(u.home().Signers) != 1 {
				t.Errorf("trust add while a process runs dev: %+v; want restart-required, not-submitted, and the signer kept", ch)
			}
		})
	})

	rec.Run(t, "guard", func(t *testing.T) {
		rec.Run(t, "operations.G1 a write after an interrupted one restores first", func(t *testing.T) {
			u := use(t, mount, Setup{Interrupted: true})
			if h := u.home(); !h.Snapshot || len(h.Signers) != 1 || !slices.Equal(h.Signers[0].IDs, []string{"left/*"}) {
				t.Fatalf("the interrupted home reads %+v; want its snapshot and the signer it left", h)
			}
			ch := u.trust(operations.TrustRequest{KeyFile: u.key("a"), IDs: []string{"acme/*"}})
			if !slices.ContainsFunc(ch.Notes, func(n string) bool { return strings.Contains(n, "restored") }) {
				t.Errorf("notes %q; want one that says the snapshot was restored", ch.Notes)
			}
			if h := u.home(); h.Snapshot || len(h.Signers) != 1 || !slices.Equal(h.Signers[0].IDs, []string{"acme/*"}) {
				t.Errorf("home %+v; want the interrupted write undone, then the signer of acme/*", h)
			}
		})

		rec.Run(t, "operations.G1b profile init after an interrupted write restores first", func(t *testing.T) {
			u := use(t, mount, Setup{Interrupted: true})
			ch := u.profile(u.ops.InitProfile, "cli")
			if !hasNote(ch, "restored") {
				t.Errorf("notes %q; want one that says the snapshot was restored", ch.Notes)
			}
			if h := u.home(); h.Snapshot || len(h.Signers) != 0 || !slices.Equal(h.Profiles, []string{"cli"}) {
				t.Errorf("home %+v; want the interrupted write undone, then the profile cli", h)
			}
		})

		rec.Run(t, "operations.G2 a second writer fails after its bound, naming the holder", func(t *testing.T) {
			const holder = "a holder of the suite"
			u := use(t, mount, Setup{Held: holder, LockWait: 50 * time.Millisecond})
			before := u.home() // a read takes no lock
			_, err := u.ops.AddTrust(t.Context(), operations.TrustRequest{Caller: u.caller(), Home: u.s.Home, KeyFile: u.key("a"), IDs: []string{"acme/*"}})
			if !errors.Is(err, operations.ErrLocked) || !strings.Contains(err.Error(), holder) {
				t.Errorf("a write while the lock is held: %v, want ErrLocked naming %q", err, holder)
			}
			if after := u.home(); !reflect.DeepEqual(after, before) {
				t.Errorf("a writer that did not get the lock changed the home: %+v", after)
			}
		})

		rec.Run(t, "operations.G3 concurrent writers lose nothing", func(t *testing.T) {
			u := use(t, mount, Setup{LockWait: time.Minute})
			keys := []string{u.key("a"), u.key("b"), u.key("c"), u.key("d")}
			var wg sync.WaitGroup
			errs := make([]error, len(keys))
			for i, k := range keys {
				wg.Go(func() {
					_, errs[i] = u.ops.AddTrust(t.Context(), operations.TrustRequest{Caller: u.caller(), Home: u.s.Home, KeyFile: k, IDs: []string{"owner" + string(rune('a'+i)) + "/*"}})
				})
			}
			wg.Wait()
			if err := errors.Join(errs...); err != nil {
				t.Fatal(err)
			}
			if h := u.home(); len(h.Signers) != len(keys) {
				t.Errorf("signers %+v; want one per writer", h.Signers)
			}
		})

		rec.Run(t, "operations.G4 after release every method fails with ErrInactive", func(t *testing.T) {
			u := use(t, mount, Setup{})
			a := u.key("a")
			deftest.Dispose(t, u.s.Root, u.s.Provider)
			ctx, c := t.Context(), u.caller()
			calls := map[string]error{}
			_, calls["Home"] = u.ops.Home(ctx, operations.HomeRequest{Caller: c, Home: u.s.Home})
			_, calls["AddTrust"] = u.ops.AddTrust(ctx, operations.TrustRequest{Caller: c, Home: u.s.Home, KeyFile: a, IDs: []string{"acme/*"}})
			_, calls["GenerateKey"] = u.ops.GenerateKey(ctx, operations.KeyRequest{Caller: c, Prefix: filepath.Join(u.s.Dir, "late")})
			_, calls["SignDist"] = u.ops.SignDist(ctx, operations.SignDistRequest{Caller: c, Image: u.image("site"), Key: private(a)})
			_, calls["UseDist"] = u.ops.UseDist(ctx, operations.UseRequest{Caller: c, Home: u.s.Home, Image: u.image("site")})
			q := operations.RenderRequest{Caller: c, RenderRequest: launch.RenderRequest{Home: u.s.Home, Profile: "cli"}}
			_, calls["Render"] = u.ops.Render(ctx, q)
			_, calls["VerifyProfile"] = u.ops.VerifyProfile(ctx, q)
			_, calls["InitProfile"] = u.ops.InitProfile(ctx, operations.ProfileRequest{Caller: c, Home: u.s.Home, Profile: "cli"})
			_, calls["SanitizeProfile"] = u.ops.SanitizeProfile(ctx, operations.ProfileRequest{Caller: c, Home: u.s.Home})
			out := filepath.Join(u.s.Dir, "artifact")
			_, calls["Pack"] = u.ops.Pack(ctx, operations.PackRequest{Caller: c, Dir: u.s.Dir, Out: out})
			_, calls["Sign"] = u.ops.Sign(ctx, operations.SignRequest{Caller: c, Out: out, Key: private(a)})
			_, calls["Push"] = u.ops.Push(ctx, operations.PushRequest{Caller: c, Out: out, Registry: u.s.Registry + "/x"})
			_, calls["Inspect"] = u.ops.Inspect(ctx, operations.SpecRequest{Caller: c, Spec: out})
			_, calls["Verify"] = u.ops.Verify(ctx, operations.SpecRequest{Caller: c, Spec: out, Home: u.s.Home})
			_, calls["New"] = u.ops.New(ctx, operations.NewRequest{Caller: c, Registry: "echoers", ID: "acme/echo", Lang: "go", Dir: filepath.Join(u.s.Dir, "late")})
			_, calls["Check"] = u.ops.Check(ctx, operations.CheckRequest{Caller: c, Dir: u.s.Dir})
			_, calls["Gate"] = u.ops.Gate(ctx, operations.GateRequest{Caller: c, Spec: out, Against: out})
			_, calls["Install"] = u.ops.Install(ctx, operations.InstallRequest{Caller: c, Home: u.s.Home, Profile: "dev", Spec: out})
			_, calls["Enable"] = u.ops.Enable(ctx, u.row(Row))
			_, calls["Disable"] = u.ops.Disable(ctx, u.row(Row))
			_, calls["Remove"] = u.ops.Remove(ctx, operations.RemoveRequest{Caller: c, Home: u.s.Home, Profile: "dev", ID: "acme/x"})
			_, calls["Build"] = u.ops.Build(ctx, operations.BuildRequest{Caller: c, BuildRequest: launch.BuildRequest{DistYAML: filepath.Join(u.s.Dir, "dist.yaml"), Builder: "local"}})
			_, calls["Smoke"] = u.ops.Smoke(ctx, operations.BuildRequest{Caller: c, BuildRequest: launch.BuildRequest{Plugin: u.s.Dir,
				Against: "oci://registry.test/aiops/release@sha256:" + strings.Repeat("ab", 32), ModCache: u.s.Dir, Builder: "local"}})
			for name, err := range calls {
				if !errors.Is(err, kernel.ErrInactive) {
					t.Errorf("%s after release: %v, want ErrInactive", name, err)
				}
			}
		})

		rec.Run(t, "operations.G5 no side door", func(t *testing.T) {
			u := use(t, mount, Setup{})
			if doors := deftest.SideDoors[operations.Operations](u.ops); len(doors) > 0 {
				t.Errorf("the provided value has methods outside operations.Operations: %v", doors)
			}
		})
	})
}

// user is a consumer of operations.Key on a mounted provider.
type user struct {
	t   *testing.T
	s   Subject
	ops operations.Operations
}

func use(t *testing.T, mount Mount, s Setup) *user {
	t.Helper()
	if s.LockWait == 0 {
		s.LockWait = time.Minute
	}
	rendered := maps.Clone(Rendered)
	maps.Copy(rendered, s.Rendered)
	s.Shipped, s.Rendered, s.Images, s.Artifact, s.Template, s.Built = Shipped, rendered, Images, Artifact, Template, Built
	if s.Plugin.Summary.ID == "" {
		s.Plugin.Summary = PackSummary
	}
	u := &user{t: t, s: mount(t, s)}
	_, _, u.ops = deftest.Consume(t, u.s.Root, operations.Key)
	return u
}

const password = "the suite's password"

func env(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) { v, ok := m[name]; return v, ok }
}

func (u *user) caller() operations.Caller {
	return operations.Caller{Env: env(map[string]string{operations.KeyPasswordVar: password})}
}

// key generates the pair Dir/name and returns its public key file.
func (u *user) key(name string) string {
	u.t.Helper()
	w, err := u.ops.GenerateKey(u.t.Context(), operations.KeyRequest{Caller: u.caller(), Prefix: filepath.Join(u.s.Dir, name)})
	if err != nil {
		u.t.Fatal(err)
	}
	return w.Paths[1]
}

func private(pub string) string { return strings.TrimSuffix(pub, ".pub") + ".key" }

// image returns the reference of the image of the suite in the repository name.
func (u *user) image(name string) string { return u.s.Registry + "/" + name + ":dist" }

// signed signs the image name with the private key of pub.
func (u *user) signed(name, pub string) {
	u.t.Helper()
	if _, err := u.ops.SignDist(u.t.Context(), operations.SignDistRequest{Caller: u.caller(), Image: u.image(name), Key: private(pub)}); err != nil {
		u.t.Fatal(err)
	}
}

// plugin writes the files of Pack under Dir/plugin and returns that directory.
func (u *user) plugin() string {
	u.t.Helper()
	dir := filepath.Join(u.s.Dir, "plugin")
	for name, data := range Pack {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			u.t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			u.t.Fatal(err)
		}
	}
	return dir
}

// inspect inspects spec and holds what it reads to Artifact, with a digest and no
// signer.
func (u *user) inspect(spec string) operations.Inspected {
	u.t.Helper()
	in, err := u.ops.Inspect(u.t.Context(), operations.SpecRequest{Caller: u.caller(), Spec: spec})
	if err != nil {
		u.t.Fatalf("inspect %s: %v", spec, err)
	}
	got := in
	got.Digest, got.Runtimes = "", nil
	if !strings.HasPrefix(in.Digest, "sha256:") || len(in.Runtimes) > 0 || !reflect.DeepEqual(got, Artifact) {
		u.t.Errorf("inspect %s = %+v; want %+v with a digest, no runtime and no signer", spec, in, Artifact)
	}
	return in
}

func (u *user) verify(spec string) (operations.Verified, error) {
	return u.ops.Verify(u.t.Context(), operations.SpecRequest{Caller: u.caller(), Spec: spec, Home: u.s.Home})
}

// verified holds a verify of spec to the digest, the signer that trust.yaml lists
// first, a signature and an attestation that pass, and a verdict that is every step's.
func (u *user) verified(spec, digest string) {
	u.t.Helper()
	v, err := u.verify(spec)
	if err != nil {
		u.t.Fatalf("verify %s: %v", spec, err)
	}
	passed := len(v.Steps) == 3
	for i, s := range v.Steps {
		passed = passed && s.Outcome == "pass"
		if want := []string{"signature", "attestation", "suites"}[min(i, 2)]; s.Name != want || i < 2 && s.Outcome != "pass" {
			u.t.Errorf("verify %s: step %d is %+v; want %s, and the first two passing", spec, i, s, want)
		}
	}
	if v.Digest != digest || v.Signer != u.home().Signers[0].KeyID || v.Passed != passed {
		u.t.Errorf("verify %s = %+v; want the digest %s, the signer of acme/* and the verdict of its steps", spec, v, digest)
	}
}

// signedPack packs Pack, signs it with the key of an author whom trust.yaml lists for
// acme/*, and returns its layout.
func (u *user) signedPack() string {
	u.t.Helper()
	out := filepath.Join(u.plugin(), "artifact")
	pub := u.key("author")
	u.trust(operations.TrustRequest{KeyFile: pub, IDs: []string{"acme/*"}})
	u.packed(filepath.Dir(out), out, private(pub))
	return out
}

// packed packs the plugin directory dir into the layout out and signs it with the
// private key file key.
func (u *user) packed(dir, out, key string) {
	u.t.Helper()
	if _, err := u.ops.Pack(u.t.Context(), operations.PackRequest{Caller: u.caller(), Dir: dir, Out: out}); err != nil {
		u.t.Fatal(err)
	}
	if _, err := u.ops.Sign(u.t.Context(), operations.SignRequest{Caller: u.caller(), Out: out, Key: key}); err != nil {
		u.t.Fatal(err)
	}
}

// install installs spec into the profile p, and remove removes the plugin id from it.
func (u *user) install(spec, p string) error {
	_, err := u.ops.Install(u.t.Context(), operations.InstallRequest{Caller: u.caller(), Home: u.s.Home, Profile: p, Spec: spec})
	return err
}

func (u *user) remove(p, id string) error {
	_, err := u.ops.Remove(u.t.Context(), operations.RemoveRequest{Caller: u.caller(), Home: u.s.Home, Profile: p, ID: id})
	return err
}

// row is a request for the row id of dev.
func (u *user) row(id string) operations.RowRequest {
	return operations.RowRequest{Caller: u.caller(), Home: u.s.Home, Profile: "dev", Row: id}
}

// homeOf is Home for the profile p.
func (u *user) homeOf(p string) operations.Home {
	u.t.Helper()
	h, err := u.ops.Home(u.t.Context(), operations.HomeRequest{Caller: u.caller(), Home: u.s.Home, Profile: p})
	if err != nil {
		u.t.Fatal(err)
	}
	return h
}

func (u *user) home() operations.Home { u.t.Helper(); return u.homeOf("") }

func (u *user) trust(r operations.TrustRequest) operations.Change {
	u.t.Helper()
	r.Caller, r.Home = u.caller(), u.s.Home
	ch, err := u.ops.AddTrust(u.t.Context(), r)
	if err != nil {
		u.t.Fatalf("trust add %+v: %v", r, err)
	}
	return ch
}

// refused holds a trust add to ErrRefused and to a home that it leaves as it was.
func (u *user) refused(r operations.TrustRequest, why string) {
	u.t.Helper()
	before := u.home()
	r.Caller, r.Home = u.caller(), u.s.Home
	if _, err := u.ops.AddTrust(u.t.Context(), r); !errors.Is(err, operations.ErrRefused) {
		u.t.Errorf("%s: %v, want ErrRefused", why, err)
	}
	if after := u.home(); !reflect.DeepEqual(after, before) {
		u.t.Errorf("%s changed the home: %+v, was %+v", why, after, before)
	}
}

// profile runs m, InitProfile or SanitizeProfile, on the profile p of the home.
func (u *user) profile(m func(context.Context, operations.ProfileRequest) (operations.Change, error), p string) operations.Change {
	u.t.Helper()
	ch, err := m(u.t.Context(), operations.ProfileRequest{Caller: u.caller(), Home: u.s.Home, Profile: p})
	if err != nil {
		u.t.Fatalf("profile %q: %v", p, err)
	}
	return ch
}

// check writes the plugin of plugin check, Dir/plugin with its plugin.yaml and its
// bundle.yaml, and runs plugin check on it.
func (u *user) check() operations.CheckReport {
	u.t.Helper()
	dir := filepath.Join(u.s.Dir, "plugin")
	if err := os.Mkdir(dir, 0o755); err != nil {
		u.t.Fatal(err)
	}
	// In-process code, packed by step 7 from the root of its module.
	for name, body := range map[string]string{"plugin.yaml": "id: acme/echo\n", "bundle.yaml": "- insert: []\n", "go.mod": "module example.test/echo\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			u.t.Fatal(err)
		}
	}
	r, err := u.ops.Check(u.t.Context(), operations.CheckRequest{Caller: u.caller(), Dir: dir})
	if err != nil {
		u.t.Fatalf("Check: %v", err)
	}
	for i, s := range r.Steps {
		if s.N != i+1 || s.Name != operations.Steps[i] {
			u.t.Errorf("step %d is %d %q; want %d %q", i+1, s.N, s.Name, i+1, operations.Steps[i])
		}
	}
	return r
}

func outcomes(r operations.CheckReport) []string {
	var out []string
	for _, s := range r.Steps {
		out = append(out, s.Outcome)
	}
	return out
}

func hasNote(ch operations.Change, word string) bool {
	return slices.ContainsFunc(ch.Notes, func(n string) bool { return strings.Contains(n, word) })
}

// norm reads an absent set and an empty one alike.
func norm(ss []operations.Signer) []operations.Signer {
	out := slices.Clone(ss)
	for i := range out {
		for _, p := range []*[]string{&out[i].IDs, &out[i].Runtimes, &out[i].Signed} {
			if len(*p) == 0 {
				*p = nil
			}
		}
	}
	return out
}
