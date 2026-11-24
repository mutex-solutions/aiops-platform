// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the operations definition: what the operations
// row and the fake must both do (07-c4-plugin-sdk.md §8.2; ADR-061 decision 18). Its
// cases set up and observe a home only through the methods of operations.Operations
// and their results, Home above all, and read no file of the home; the state a case
// starts from is a Setup, which each subject's Mount makes. The obligations half holds
// a provider to key generate, the forms and refusals of trust add, dist sign and dist
// use, the profile commands on the launcher's toolbox, and the outcomes they answer.
// The guard half holds it to the write lock and the
// snapshot: a write after an interrupted one restores first, a second writer fails
// after its bound naming the holder, concurrent writers lose nothing; and to what it
// refuses once released, with no side door. Each case has an id, the first word of its
// name. The package imports the standard library, sdk/kernel and definitions only.
package conformance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	// Render for each profile. use sets Shipped and Rendered of the suite.
	Shipped  map[string][]byte
	Rendered map[string]launch.Rendered
}

// Shipped is the one shipped profile of the suite, cli.
var Shipped = map[string][]byte{"profiles/cli/profile.yaml": []byte("bundles: [aiops-core, aiops-cli]\n")}

// Rendered is what the toolbox of the suite renders: cli with one departure from the
// release bindings and one dump, and no diagnostic.
var Rendered = map[string]launch.Rendered{"cli": {Departures: []string{"the site adds consumers of appExit: acme/x"},
	Dumps: map[string]string{"--dump": "the dump of cli\n"}, Distribution: "sha256:ab"}}

// Subject is a mounted provider of operations.Key.
type Subject struct {
	Root     *kernel.Root
	Provider *kernel.Fiber // ACTIVE; the suite releases the provider by disposing it
	Home     string        // the home of the case, absolute; it exists only where Setup asks for a state
	Dir      string        // an existing directory outside the home, for key files and images
}

// Mount mounts a provider of operations.Key on a root of its own, its home in the
// state of s.
type Mount func(t *testing.T, s Setup) Subject

// Run runs the suite, each case on a provider of its own.
func Run(t *testing.T, mount Mount) {
	t.Run("obligations", func(t *testing.T) {
		t.Run("operations.O1 key generate writes a pair once, with the password of the environment", func(t *testing.T) {
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

		t.Run("operations.O2 trust add appends a signer with sorted sets", func(t *testing.T) {
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

		t.Run("operations.O3 trust add of a distribution keeps the default root", func(t *testing.T) {
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

		t.Run("operations.O4 each refused form of trust add writes nothing", func(t *testing.T) {
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

		t.Run("operations.O5 no home is a usage error, and a missing home reads empty", func(t *testing.T) {
			u := use(t, mount, Setup{})
			if h := u.home(); len(h.Signers) != 0 || !slices.Equal(h.Distributions, []string{operations.Release}) || len(h.Records) != 0 ||
				h.Current != "" || h.Snapshot {
				t.Errorf("a home that does not exist reads %+v; want it empty, with the default root", h)
			}
			a := u.key("a")
			calls := map[string]error{}
			_, calls["Home"] = u.ops.Home(t.Context(), operations.HomeRequest{Caller: u.caller()})
			_, calls["AddTrust"] = u.ops.AddTrust(t.Context(), operations.TrustRequest{Caller: u.caller(), KeyFile: a, IDs: []string{"acme/*"}})
			_, calls["UseDist"] = u.ops.UseDist(t.Context(), operations.UseRequest{Caller: u.caller(), Image: u.s.Dir})
			_, calls["InitProfile"] = u.ops.InitProfile(t.Context(), operations.ProfileRequest{Caller: u.caller(), Profile: "cli"})
			_, calls["SanitizeProfile"] = u.ops.SanitizeProfile(t.Context(), operations.ProfileRequest{Caller: u.caller()})
			for name, err := range calls {
				if !errors.Is(err, operations.ErrUsage) || !strings.Contains(err.Error(), operations.HomeVar) {
					t.Errorf("%s with no home: %v, want ErrUsage naming %s", name, err, operations.HomeVar)
				}
			}
		})

		t.Run("operations.O6 dist sign and dist use record a distribution", func(t *testing.T) {
			u := use(t, mount, Setup{})
			pub := u.key("site")
			u.trust(operations.TrustRequest{KeyFile: pub, Distribution: true})
			manifest := []byte("distVersion: 1\n")
			img := u.image("img", manifest)
			w, err := u.ops.SignDist(t.Context(), operations.SignDistRequest{Caller: u.caller(), Image: img, Key: private(pub)})
			if err != nil || len(w.Paths) != 1 || filepath.Dir(w.Paths[0]) != img {
				t.Fatalf("SignDist = %v, %v; want the bundle written in the image", w, err)
			}
			ch, err := u.ops.UseDist(t.Context(), operations.UseRequest{Caller: u.caller(), Home: u.s.Home, Image: img})
			if err != nil || ch.Outcome != operations.RestartRequired || ch.Reason != operations.ReasonImage {
				t.Fatalf("UseDist = %+v, %v; want restart-required for the reason image", ch, err)
			}
			sum := sha256.Sum256(manifest)
			d := "sha256:" + hex.EncodeToString(sum[:])
			h := u.home()
			want := []operations.Record{{Digest: d, Kind: "distribution", Signer: h.Distributions[1]}}
			if !reflect.DeepEqual(h.Records, want) || h.Current != d {
				t.Errorf("records %+v and current %q; want %+v and %s", h.Records, h.Current, want, d)
			}
		})

		t.Run("operations.O7 dist use refuses what no trusted key verifies, and writes nothing", func(t *testing.T) {
			u := use(t, mount, Setup{})
			site, other := u.key("site"), u.key("other")
			u.trust(operations.TrustRequest{KeyFile: site, Distribution: true})
			before := u.home()
			unsigned := u.image("unsigned", []byte("distVersion: 1\n"))
			foreign := u.signed("foreign", []byte("distVersion: 2\n"), other)
			changed := u.signed("changed", []byte("distVersion: 3\n"), site)
			if err := os.WriteFile(filepath.Join(changed, "manifest"), []byte("distVersion: 4\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			for why, img := range map[string]string{"an unsigned image": unsigned, "an image signed by an untrusted key": foreign,
				"a manifest changed after it was signed": changed} {
				if _, err := u.ops.UseDist(t.Context(), operations.UseRequest{Caller: u.caller(), Home: u.s.Home, Image: img}); !errors.Is(err, operations.ErrRefused) {
					t.Errorf("%s: %v, want ErrRefused", why, err)
				}
			}
			if after := u.home(); !reflect.DeepEqual(after, before) {
				t.Errorf("a refused dist use changed the home: %+v, was %+v", after, before)
			}
			img := u.image("img", []byte("distVersion: 5\n"))
			wrong := operations.Caller{Env: env(map[string]string{operations.KeyPasswordVar: "not the password"})}
			if _, err := u.ops.SignDist(t.Context(), operations.SignDistRequest{Caller: wrong, Image: img, Key: private(site)}); !errors.Is(err, operations.ErrRefused) {
				t.Errorf("dist sign with a wrong password: %v, want ErrRefused", err)
			}
			if _, err := u.ops.SignDist(t.Context(), operations.SignDistRequest{Image: img, Key: private(site)}); !errors.Is(err, operations.ErrUsage) {
				t.Errorf("dist sign with no password in the environment: %v, want ErrUsage", err)
			}
		})

		t.Run("operations.O8 profile init copies a shipped profile once", func(t *testing.T) {
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

		t.Run("operations.O9 render and verify answer what the toolbox renders", func(t *testing.T) {
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

		t.Run("operations.O10 profile sanitize of a profile that composes changes nothing", func(t *testing.T) {
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
	})

	t.Run("guard", func(t *testing.T) {
		t.Run("operations.G1 a write after an interrupted one restores first", func(t *testing.T) {
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

		t.Run("operations.G1b profile init after an interrupted write restores first", func(t *testing.T) {
			u := use(t, mount, Setup{Interrupted: true})
			ch := u.profile(u.ops.InitProfile, "cli")
			if !hasNote(ch, "restored") {
				t.Errorf("notes %q; want one that says the snapshot was restored", ch.Notes)
			}
			if h := u.home(); h.Snapshot || len(h.Signers) != 0 || !slices.Equal(h.Profiles, []string{"cli"}) {
				t.Errorf("home %+v; want the interrupted write undone, then the profile cli", h)
			}
		})

		t.Run("operations.G2 a second writer fails after its bound, naming the holder", func(t *testing.T) {
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

		t.Run("operations.G3 concurrent writers lose nothing", func(t *testing.T) {
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

		t.Run("operations.G4 after release every method fails with ErrInactive", func(t *testing.T) {
			u := use(t, mount, Setup{})
			a := u.key("a")
			deftest.Dispose(t, u.s.Root, u.s.Provider)
			ctx, c := t.Context(), u.caller()
			calls := map[string]error{}
			_, calls["Home"] = u.ops.Home(ctx, operations.HomeRequest{Caller: c, Home: u.s.Home})
			_, calls["AddTrust"] = u.ops.AddTrust(ctx, operations.TrustRequest{Caller: c, Home: u.s.Home, KeyFile: a, IDs: []string{"acme/*"}})
			_, calls["GenerateKey"] = u.ops.GenerateKey(ctx, operations.KeyRequest{Caller: c, Prefix: filepath.Join(u.s.Dir, "late")})
			_, calls["SignDist"] = u.ops.SignDist(ctx, operations.SignDistRequest{Caller: c, Image: u.s.Dir, Key: private(a)})
			_, calls["UseDist"] = u.ops.UseDist(ctx, operations.UseRequest{Caller: c, Home: u.s.Home, Image: u.s.Dir})
			q := operations.RenderRequest{Caller: c, RenderRequest: launch.RenderRequest{Home: u.s.Home, Profile: "cli"}}
			_, calls["Render"] = u.ops.Render(ctx, q)
			_, calls["VerifyProfile"] = u.ops.VerifyProfile(ctx, q)
			_, calls["InitProfile"] = u.ops.InitProfile(ctx, operations.ProfileRequest{Caller: c, Home: u.s.Home, Profile: "cli"})
			_, calls["SanitizeProfile"] = u.ops.SanitizeProfile(ctx, operations.ProfileRequest{Caller: c, Home: u.s.Home})
			for name, err := range calls {
				if !errors.Is(err, kernel.ErrInactive) {
					t.Errorf("%s after release: %v, want ErrInactive", name, err)
				}
			}
		})

		t.Run("operations.G5 no side door", func(t *testing.T) {
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
	s.Shipped, s.Rendered = Shipped, Rendered
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

// image writes the local form of an image, Dir/name with its manifest.
func (u *user) image(name string, manifest []byte) string {
	u.t.Helper()
	dir := filepath.Join(u.s.Dir, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		u.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest"), manifest, 0o644); err != nil {
		u.t.Fatal(err)
	}
	return dir
}

// signed writes an image and signs it with the private key of pub.
func (u *user) signed(name string, manifest []byte, pub string) string {
	u.t.Helper()
	img := u.image(name, manifest)
	if _, err := u.ops.SignDist(u.t.Context(), operations.SignDistRequest{Caller: u.caller(), Image: img, Key: private(pub)}); err != nil {
		u.t.Fatal(err)
	}
	return img
}

func (u *user) home() operations.Home {
	u.t.Helper()
	h, err := u.ops.Home(u.t.Context(), operations.HomeRequest{Caller: u.caller(), Home: u.s.Home})
	if err != nil {
		u.t.Fatal(err)
	}
	return h
}

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
