// SPDX-License-Identifier: Apache-2.0

// Package fake is the in-memory engine of the operations definition (ADR-061
// decision 18): it holds each home in memory, keeps the key pairs it generates and the
// signatures it makes, and writes no file. It reads a file only where a request names
// one that it did not make: a public key file of trust add, and the manifest of an
// image. It takes from its config what it cannot compute without the libraries of the
// operations row: a holder of every write lock, an interrupted write in every home,
// whether a process runs the profile, and what the launcher's toolbox ships and
// renders. On that model it keeps the rules that do not concern files: the forms and
// refusals of trust add, the default root, the refusals of dist use, the copy of a
// shipped profile, the restore of an interrupted write before the next one, and a lock
// with its wait. It imports the standard library, sdk/kernel and its
// definition only.
package fake

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Config is what the fake takes.
type Config struct {
	// LockWait is how long a second writer waits for the lock of a home; zero is two
	// minutes, the row's default.
	LockWait time.Duration
	// Held, when set, is the command of a holder of the lock of every home, which
	// never releases it.
	Held string
	// Interrupted gives every home what a trust add killed after its rename left: one
	// signer for left/* that the home did not hold before, and the snapshot of that
	// write.
	Interrupted bool
	// Running says that a process runs the profile of every home, so a trust add
	// answers restart-required with the reason not-submitted.
	Running bool
	// Shipped and Rendered stand for the launcher's toolbox: the shipped files by path,
	// and what Render answers for each profile.
	Shipped  map[string][]byte
	Rendered map[string]launch.Rendered
	// Images stand for a registry: the distribution manifest of each image, by its
	// reference.
	Images map[string][]byte
}

// Fake is the in-memory engine.
type Fake struct {
	cfg    Config
	mu     sync.Mutex
	homes  map[string]*home
	keys   map[string]key       // the key files it generated, by path
	images map[string]signature // the images it signed, by directory
	calls  []string
}

type home struct {
	state    state
	snapshot *state        // the state an interrupted write left to restore
	lock     chan struct{} // one slot: full while a writer holds the lock
}

type state struct {
	profiles []string // the profiles copied into the home, sorted
	signers  []operations.Signer
	dists    []string // nil when the field is absent
	records  []operations.Record
	current  string
}

type key struct {
	id, password string
	private      bool
}

type signature struct{ digest, id string }

// New returns the fake with no config and the plugin that provides it under
// operations.Key. It takes no argument, so the generated cycle test of a consumer
// mounts it (ADR-060 decision 12).
func New() (*Fake, kernel.Plugin) { return NewWith(Config{}) }

// NewWith returns the fake with cfg and the plugin that provides it under
// operations.Key. Each activation provides the methods of operations.Operations
// only; Inventory and Calls are the test's (no side door, 07-c4-plugin-sdk.md §4.3).
func NewWith(cfg Config) (*Fake, kernel.Plugin) {
	if cfg.LockWait == 0 {
		cfg.LockWait = 2 * time.Minute
	}
	f := &Fake{cfg: cfg, homes: map[string]*home{}, keys: map[string]key{}, images: map[string]signature{}}
	return f, kernel.Define(kernel.Plugin{
		ID:       "aiops/operations-fake",
		Provides: []kernel.AnyKey{operations.Key},
		Apply:    func(c *kernel.Context, _ any) error { return operations.Key.Provide(c, served{f, c}) },
	})
}

// Inventory lists nothing: the definition takes no registration (ADR-060 decision 12).
func (f *Fake) Inventory() []string { return nil }

// Calls returns the methods called, in order.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// served is what an activation provides: each call is refused with kernel.ErrInactive
// once the activation is released, as the row refuses it.
type served struct {
	f   *Fake
	act *kernel.Context
}

func (s served) enter(name string) (func(), error) {
	leave, ok := s.act.Enter()
	if !ok {
		return nil, kernel.ErrInactive
	}
	s.f.mu.Lock()
	s.f.calls = append(s.f.calls, name)
	s.f.mu.Unlock()
	return leave, nil
}

func usage(format string, a ...any) error {
	return fmt.Errorf("%w: %s", operations.ErrUsage, fmt.Sprintf(format, a...))
}

func refused(format string, a ...any) error {
	return fmt.Errorf("%w: %s", operations.ErrRefused, fmt.Sprintf(format, a...))
}

func homeOf(dir string) error {
	if dir == "" {
		return usage("no home: %s is unset or empty", operations.HomeVar)
	}
	if !filepath.IsAbs(dir) {
		return usage("the home %s is not an absolute path", dir)
	}
	return nil
}

func password(c operations.Caller) (string, error) {
	if c.Env != nil {
		if v, ok := c.Env(operations.KeyPasswordVar); ok {
			return v, nil
		}
	}
	return "", usage("%s is unset", operations.KeyPasswordVar)
}

// home returns the home at dir, made on first use in the state the config gives.
// f.mu is held.
func (f *Fake) home(dir string) *home {
	h := f.homes[dir]
	if h == nil {
		h = &home{lock: make(chan struct{}, 1)}
		if f.cfg.Held != "" {
			h.lock <- struct{}{}
		}
		if f.cfg.Interrupted {
			h.state.signers = []operations.Signer{{KeyID: newID(), IDs: []string{"left/*"}}}
			h.snapshot = &state{}
		}
		f.homes[dir] = h
	}
	return h
}

// write runs change on the state of the home at dir under its lock, after it restores
// an interrupted write; a change that fails leaves the state as it was.
func (f *Fake) write(ctx context.Context, dir string, change func(*state) error) ([]string, error) {
	f.mu.Lock()
	h := f.home(dir)
	f.mu.Unlock()
	t := time.NewTimer(f.cfg.LockWait)
	defer t.Stop()
	select {
	case h.lock <- struct{}{}:
	case <-t.C:
		return nil, fmt.Errorf("%w, after %v, by %s", operations.ErrLocked, f.cfg.LockWait, f.cfg.Held)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-h.lock }()
	f.mu.Lock()
	defer f.mu.Unlock()
	var notes []string
	if h.snapshot != nil {
		h.state, h.snapshot = *h.snapshot, nil
		notes = append(notes, "restored the snapshot that an interrupted write left")
	}
	next := h.state
	next.signers, next.dists, next.records = slices.Clone(next.signers), slices.Clone(next.dists), slices.Clone(next.records)
	next.profiles = slices.Clone(next.profiles)
	if err := change(&next); err != nil {
		return notes, err
	}
	h.state = next
	return notes, nil
}

func (s served) Home(_ context.Context, r operations.HomeRequest) (operations.Home, error) {
	leave, err := s.enter("Home")
	if err != nil {
		return operations.Home{}, err
	}
	defer leave()
	if err := homeOf(r.Home); err != nil {
		return operations.Home{}, err
	}
	f := s.f
	f.mu.Lock()
	defer f.mu.Unlock()
	h := f.home(r.Home)
	out := operations.Home{Profiles: slices.Clone(h.state.profiles), Signers: slices.Clone(h.state.signers), Distributions: slices.Clone(h.state.dists),
		Records: slices.Clone(h.state.records), Current: h.state.current, Snapshot: h.snapshot != nil}
	if out.Distributions == nil {
		out.Distributions = []string{operations.Release}
	}
	return out, nil
}

func (s served) AddTrust(ctx context.Context, r operations.TrustRequest) (operations.Change, error) {
	leave, err := s.enter("AddTrust")
	if err != nil {
		return operations.Change{}, err
	}
	defer leave()
	if err := homeOf(r.Home); err != nil {
		return operations.Change{}, err
	}
	switch {
	case r.Distribution && len(r.IDs)+len(r.Runtimes)+len(r.Signed) > 0:
		return operations.Change{}, refused("a distribution root takes no ids, runtimes or signed entries")
	case !r.Distribution && len(r.IDs) == 0:
		return operations.Change{}, refused("trust add takes id patterns or a distribution root")
	}
	for _, p := range r.IDs {
		owner, name, ok := strings.Cut(p, "/")
		if !ok || !kebab(owner) || name != "*" && !kebab(name) || owner == "aiops" || owner == "contrib" {
			return operations.Change{}, refused("%q is not an id pattern", p)
		}
	}
	for _, rt := range r.Runtimes {
		if rt != "inproc" && rt != "remote" {
			return operations.Change{}, refused("%q is not a runtime", rt)
		}
	}
	for _, sg := range r.Signed {
		if !okHook(sg) {
			return operations.Change{}, refused("%q names no signed key or hook", sg)
		}
	}
	id, err := s.f.publicKey(r.KeyFile)
	if err != nil {
		return operations.Change{}, err
	}
	set := func(v []string) []string { return slices.Compact(slices.Sorted(slices.Values(v))) }
	notes, err := s.f.write(ctx, r.Home, func(st *state) error {
		if !r.Distribution {
			st.signers = append(st.signers, operations.Signer{KeyID: id, IDs: set(r.IDs), Runtimes: set(r.Runtimes), Signed: set(r.Signed)})
			return nil
		}
		if st.dists == nil {
			st.dists = []string{operations.Release}
		}
		if slices.Contains(st.dists, id) {
			return refused("distributions lists the key %s already", id)
		}
		st.dists = append(st.dists, id)
		return nil
	})
	ch := operations.Change{Notes: notes}
	if err != nil {
		return ch, err
	}
	ch.Written, ch.Outcome = []string{filepath.Join(r.Home, "trust.yaml")}, operations.NotRunning
	if s.f.cfg.Running {
		ch.Outcome, ch.Reason = operations.RestartRequired, operations.ReasonNotSubmitted
	}
	return ch, nil
}

// publicKey returns the id of the public key file p: one the fake generated, or a
// file of one PEM block PUBLIC KEY of an ECDSA P-256 key.
func (f *Fake) publicKey(p string) (string, error) {
	f.mu.Lock()
	k, ok := f.keys[p]
	f.mu.Unlock()
	switch {
	case ok && k.private:
		return "", refused("%s is a private key file", p)
	case ok:
		return k.id, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", refused("read the public key: %v", err)
	}
	blk, _ := pem.Decode(b)
	if blk == nil || !strings.HasPrefix(strings.TrimSpace(string(b)), "-----BEGIN ") || blk.Type != "PUBLIC KEY" {
		return "", refused("%s holds no PEM block PUBLIC KEY", p)
	}
	pk, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if ec, ok := pk.(*ecdsa.PublicKey); err != nil || !ok || ec.Curve != elliptic.P256() {
		return "", refused("%s is not an ECDSA P-256 public key", p)
	}
	sum := sha256.Sum256(blk.Bytes)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

func (s served) GenerateKey(_ context.Context, r operations.KeyRequest) (operations.Written, error) {
	leave, err := s.enter("GenerateKey")
	if err != nil {
		return operations.Written{}, err
	}
	defer leave()
	if !filepath.IsAbs(r.Prefix) {
		return operations.Written{}, usage("the prefix %q is not an absolute path", r.Prefix)
	}
	pw, err := password(r.Caller)
	if err != nil {
		return operations.Written{}, err
	}
	paths := []string{r.Prefix + ".key", r.Prefix + ".pub"}
	f := s.f
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range paths {
		_, statErr := os.Lstat(p)
		if _, ok := f.keys[p]; ok || statErr == nil {
			return operations.Written{}, refused("%s exists", p)
		}
	}
	id := newID()
	f.keys[paths[0]] = key{id: id, password: pw, private: true}
	f.keys[paths[1]] = key{id: id}
	return operations.Written{Paths: paths}, nil
}

func (s served) SignDist(_ context.Context, r operations.SignDistRequest) (operations.Written, error) {
	leave, err := s.enter("SignDist")
	if err != nil {
		return operations.Written{}, err
	}
	defer leave()
	if !filepath.IsAbs(r.Image) || !filepath.IsAbs(r.Key) {
		return operations.Written{}, usage("the image and the key file are absolute paths")
	}
	pw, err := password(r.Caller)
	if err != nil {
		return operations.Written{}, err
	}
	f := s.f
	f.mu.Lock()
	k, ok := f.keys[r.Key]
	f.mu.Unlock()
	if !ok || !k.private || k.password != pw {
		return operations.Written{}, refused("%s is no private key this fake made, or the password is not its own", r.Key)
	}
	digest, err := manifest(r.Image)
	if err != nil {
		return operations.Written{}, err
	}
	f.mu.Lock()
	f.images[r.Image] = signature{digest: digest, id: k.id}
	f.mu.Unlock()
	return operations.Written{Paths: []string{filepath.Join(r.Image, "manifest.signature")}}, nil
}

// manifest returns the digest of the manifest of the local image img.
func manifest(img string) (string, error) {
	b, err := os.ReadFile(filepath.Join(img, "manifest"))
	if err != nil {
		return "", refused("the image %s holds no distribution manifest: %v", img, err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (s served) UseDist(ctx context.Context, r operations.UseRequest) (operations.Change, error) {
	leave, err := s.enter("UseDist")
	if err != nil {
		return operations.Change{}, err
	}
	defer leave()
	if err := homeOf(r.Home); err != nil {
		return operations.Change{}, err
	}
	digest, err := manifest(r.Image)
	if err != nil {
		return operations.Change{}, err
	}
	s.f.mu.Lock()
	sig, signed := s.f.images[r.Image]
	s.f.mu.Unlock()
	notes, err := s.f.write(ctx, r.Home, func(st *state) error {
		if !signed || sig.digest != digest || !slices.Contains(st.dists, sig.id) {
			return refused("no key of distributions verifies the signature of the manifest %s of %s", digest, r.Image)
		}
		st.records = slices.DeleteFunc(st.records, func(x operations.Record) bool { return x.Digest == digest })
		st.records = append(st.records, operations.Record{Digest: digest, Kind: "distribution", Signer: sig.id})
		slices.SortFunc(st.records, func(a, b operations.Record) int { return strings.Compare(a.Digest, b.Digest) })
		st.current = digest
		return nil
	})
	ch := operations.Change{Notes: notes}
	if err != nil {
		return ch, err
	}
	rec := filepath.Join(r.Home, "verified", strings.Replace(digest, ":", "-", 1))
	ch.Written, ch.Outcome, ch.Reason = []string{rec, filepath.Join(r.Home, "verified", "current")}, operations.RestartRequired, operations.ReasonImage
	return ch, nil
}

// newID returns a key id: the base64 of 32 random bytes, as a real id is of the
// SHA-256 of a key.
func newID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func kebab(s string) bool {
	for i, w := range strings.Split(s, "-") {
		if w == "" || i == 0 && (w[0] < 'a' || w[0] > 'z') || strings.Trim(w, "abcdefghijklmnopqrstuvwxyz0123456789") != "" {
			return false
		}
	}
	return true
}

func okHook(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, c := range s {
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && (i == 0 || !strings.ContainsRune("._-/", c)) {
			return false
		}
	}
	return true
}

func (s served) Render(ctx context.Context, r operations.RenderRequest) (launch.Rendered, error) {
	leave, err := s.enter("Render")
	if err != nil {
		return launch.Rendered{}, err
	}
	defer leave()
	return s.f.render(ctx, r)
}

// render answers the Rendered of the config for the profile of r, the dumps of r only.
func (f *Fake) render(ctx context.Context, r operations.RenderRequest) (launch.Rendered, error) {
	if r.Home != "" {
		if err := homeOf(r.Home); err != nil {
			return launch.Rendered{}, err
		}
	}
	if err := profileName(r.Profile); err != nil {
		return launch.Rendered{}, err
	}
	out := f.cfg.Rendered[r.Profile]
	if out.Dumps != nil {
		dumps := map[string]string{}
		for _, d := range r.Dumps {
			dumps[d] = out.Dumps[d]
		}
		out.Dumps = dumps
	}
	return out, ctx.Err()
}

func (s served) VerifyProfile(ctx context.Context, r operations.RenderRequest) (launch.Rendered, error) {
	leave, err := s.enter("VerifyProfile")
	if err != nil {
		return launch.Rendered{}, err
	}
	defer leave()
	if r.Profile == "" {
		r.Profile = "cli"
	}
	r.Dumps = nil
	return s.f.render(ctx, r)
}

func profileName(p string) error {
	if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `/\`) {
		return usage("%q is not the name of a profile", p)
	}
	return nil
}

func (s served) InitProfile(ctx context.Context, r operations.ProfileRequest) (operations.Change, error) {
	leave, err := s.enter("InitProfile")
	if err != nil {
		return operations.Change{}, err
	}
	defer leave()
	if err := homeOf(r.Home); err != nil {
		return operations.Change{}, err
	}
	if err := profileName(r.Profile); err != nil {
		return operations.Change{}, err
	}
	rel := "profiles/" + r.Profile + "/profile.yaml"
	if _, ok := s.f.cfg.Shipped[rel]; !ok {
		return operations.Change{}, refused("no shipped profile %s", r.Profile)
	}
	kept := false
	notes, err := s.f.write(ctx, r.Home, func(st *state) error {
		kept = slices.Contains(st.profiles, r.Profile)
		if !kept {
			st.profiles = append(st.profiles, r.Profile)
			slices.Sort(st.profiles)
		}
		return nil
	})
	ch := operations.Change{Notes: notes}
	switch {
	case err != nil:
		return ch, err
	case kept:
		ch.Notes = append(ch.Notes, "the home holds "+rel+", which is kept")
		return ch, nil
	}
	ch.Written = []string{filepath.Join(r.Home, filepath.FromSlash(rel))}
	ch.Notes = append(ch.Notes, "copied the shipped profile "+r.Profile+" to "+rel)
	ch.Outcome = operations.NotRunning
	if s.f.cfg.Running {
		ch.Outcome, ch.Reason = operations.RestartRequired, operations.ReasonNotSubmitted
	}
	return ch, nil
}

// SanitizeProfile sets aside each layer file that a compose diagnostic of the
// Rendered of the config names, the profile layer and the site layer; the fake holds
// no file, so it reports the paths it would write.
func (s served) SanitizeProfile(ctx context.Context, r operations.ProfileRequest) (operations.Change, error) {
	leave, err := s.enter("SanitizeProfile")
	if err != nil {
		return operations.Change{}, err
	}
	defer leave()
	if err := homeOf(r.Home); err != nil {
		return operations.Change{}, err
	}
	p := r.Profile
	if p == "" {
		p = "cli"
	}
	if err := profileName(p); err != nil {
		return operations.Change{}, err
	}
	layers := []string{"profiles/" + p + "/aiops.patch.yml", "aiops.patch.yml"}
	var written []string
	notes, err := s.f.write(ctx, r.Home, func(*state) error {
		for _, d := range s.f.cfg.Rendered[p].Diagnostics {
			if !d.Warning && d.Check == "compose" && slices.Contains(layers, d.File) {
				f := filepath.Join(r.Home, filepath.FromSlash(d.File))
				written = append(written, f+".bak", f)
			}
		}
		return nil
	})
	ch := operations.Change{Notes: notes, Written: written}
	if err != nil || len(written) == 0 {
		ch.Notes = append(ch.Notes, "no layer file of the home stops the composition of "+p)
		return operations.Change{Notes: ch.Notes}, err
	}
	ch.Outcome = operations.NotRunning
	if s.f.cfg.Running {
		ch.Outcome, ch.Reason = operations.RestartRequired, operations.ReasonNotSubmitted
	}
	return ch, nil
}
