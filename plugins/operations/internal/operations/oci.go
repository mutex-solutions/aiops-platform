// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// The OCI client of ADR-047 decision 4, oras-go: the repository of a reference with
// the credentials of the caller's config.json, the referrers of a manifest, and the
// artifacts the library pushes, each an OCI image manifest with the empty config.

// maxBlob bounds a blob that the library reads whole: a layer of a plugin artifact.
// plugin.yaml and the bundle of an artifact, which it parses as YAML, an OCI image
// manifest, a sigstore bundle and a distribution manifest are bounded by maxFile, the 1
// MiB that boot reads of a file (boot.MaxFileBytes); and the referrers of one type that
// it reads of a manifest by maxReferrers, so that what it reads before a trust decision
// stays small.
const (
	maxBlob      = 64 << 20
	maxFile      = 1 << 20
	maxReferrers = 16
)

// registryWait bounds the dial of a registry and the wait for the headers of each of
// its answers.
const registryWait = 30 * time.Second

// remoteRepo returns the repository of ref, an OCI reference with oci:// optional,
// that reads the credentials of config.json under DOCKER_CONFIG, or under .docker/ of
// HOME, of the caller's environment (ADR-061 decision 17). A registry on the loopback
// interface is reached over plain HTTP, as Docker reaches one.
func remoteRepo(c def.Caller, ref string) (*remote.Repository, error) {
	r, err := remote.NewRepository(strings.TrimPrefix(ref, "oci://"))
	if err != nil {
		return nil, usage("%q is not an OCI reference <registry>/<repository>[:<tag>|@<digest>]: %v", ref, err)
	}
	var store credentials.Store = credentials.NewMemoryStore()
	if p := configJSON(c); p != "" {
		if store, err = credentials.NewStore(p, credentials.StoreOptions{}); err != nil {
			return nil, refused("the registry credentials: %v", err)
		}
	}
	r.Client = &auth.Client{Client: registryClient(registryWait), Cache: auth.NewCache(), Credential: credentials.Credential(store)}
	r.ReferrerListMaxPages, r.MaxMetadataBytes = maxReferrers, maxFile // what referrers lists before a trust decision
	host, _, err := net.SplitHostPort(r.Reference.Registry)
	if err != nil {
		host = r.Reference.Registry
	}
	ip := net.ParseIP(host)
	r.PlainHTTP = host == "localhost" || ip != nil && ip.IsLoopback()
	return r, nil
}

// registryClient returns the HTTP client of a registry: oras-go's retries of a reset or
// of an answer that asks for one, but none of a timeout, and wait on a dial and on the
// headers of each answer, so that a registry that cannot be reached, or does not answer,
// fails the command in that time.
func registryClient(wait time.Duration) *http.Client {
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DialContext = (&net.Dialer{Timeout: wait}).DialContext
	base.ResponseHeaderTimeout = wait
	p := *retry.DefaultPolicy.(*retry.GenericPolicy)
	p.Retryable = func(resp *http.Response, err error) (bool, error) {
		if e, ok := err.(net.Error); ok && e.Timeout() {
			return false, err
		}
		return retry.DefaultPredicate(resp, err)
	}
	return &http.Client{Transport: &retry.Transport{Base: base, Policy: func() retry.Policy { return &p }}}
}

// configJSON returns the path of the caller's config.json, "" with neither variable.
func configJSON(c def.Caller) string {
	for _, v := range []struct{ name, sub string }{{"DOCKER_CONFIG", ""}, {"HOME", ".docker"}} {
		if c.Env != nil {
			if dir, ok := c.Env(v.name); ok && dir != "" {
				return filepath.Join(dir, v.sub, "config.json")
			}
		}
	}
	return ""
}

// resolve returns the descriptor of the manifest that the reference of r names.
func resolve(ctx context.Context, r *remote.Repository) (ocispec.Descriptor, error) {
	ref := r.Reference.String()
	if r.Reference.Reference == "" {
		return ocispec.Descriptor{}, usage("%s names no tag or digest", ref)
	}
	d, err := r.Resolve(ctx, r.Reference.Reference)
	if errors.Is(err, errdef.ErrNotFound) {
		return d, refused("the registry holds no %s", ref)
	}
	return d, err
}

// fetch returns the blob or manifest d of src, checked against its digest and size,
// and refuses one larger than its media type may be before it fetches it. A failure
// names d, so that a blob changed after signing is named by its media type and the
// digest that the signed manifest records.
func fetch(ctx context.Context, src content.Fetcher, d ocispec.Descriptor) ([]byte, error) {
	limit := int64(maxBlob)
	if slices.Contains([]string{layerManifest, layerBundle, bundleType, def.DistributionLayer, ocispec.MediaTypeImageManifest}, d.MediaType) {
		limit = maxFile
	}
	if d.Size > limit {
		return nil, refused("%s is %d bytes, more than the %d a %s may hold", d.Digest, d.Size, limit, d.MediaType)
	}
	b, err := content.FetchAll(ctx, src, d)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", d.MediaType, d.Digest, err)
	}
	return b, nil
}

// manifest returns the image manifest d of src.
func manifest(ctx context.Context, src content.Fetcher, d ocispec.Descriptor) (ocispec.Manifest, error) {
	var m ocispec.Manifest
	b, err := fetch(ctx, src, d)
	if err == nil && (json.Unmarshal(b, &m) != nil || m.MediaType != ocispec.MediaTypeImageManifest) {
		err = refused("%s is not an OCI image manifest", d.Digest)
	}
	return m, err
}

// layer returns the one layer of m of mediaType, nil when it has none.
func layer(m ocispec.Manifest, mediaType string) *ocispec.Descriptor {
	i := slices.IndexFunc(m.Layers, func(l ocispec.Descriptor) bool { return l.MediaType == mediaType })
	if i < 0 {
		return nil
	}
	return &m.Layers[i]
}

// referrer is a referrer manifest with its descriptor.
type referrer struct {
	ocispec.Manifest
	desc ocispec.Descriptor
}

// referrers returns, sorted by digest, the image manifests of artifactType in src
// whose subject is d. A registry's listing stops at the page that passes maxReferrers
// of the type, and at the page bound of remoteRepo.
func referrers(ctx context.Context, src content.ReadOnlyGraphStorage, d ocispec.Descriptor, artifactType string) ([]referrer, error) {
	var refs []ocispec.Descriptor
	keep := func(page []ocispec.Descriptor) error {
		for _, r := range page {
			if r.ArtifactType == "" || r.ArtifactType == artifactType {
				refs = append(refs, r)
			}
		}
		if len(refs) > maxReferrers {
			return refused("%s has more referrers of %s than the %d the library reads", d.Digest, artifactType, maxReferrers)
		}
		return nil
	}
	var all []ocispec.Descriptor
	var err error
	if l, ok := src.(registry.ReferrerLister); ok {
		err = l.Referrers(ctx, d, artifactType, keep)
	} else if all, err = registry.Referrers(ctx, src, d, artifactType); err == nil {
		err = keep(all)
	}
	if err != nil {
		return nil, err
	}
	slices.SortFunc(refs, func(a, b ocispec.Descriptor) int { return strings.Compare(string(a.Digest), string(b.Digest)) })
	var out []referrer
	for _, r := range refs {
		m, err := manifest(ctx, src, r)
		if err != nil {
			return nil, err
		}
		if m.Subject != nil && m.Subject.Digest == d.Digest && m.ArtifactType == artifactType {
			out = append(out, referrer{m, r})
		}
	}
	return out, nil
}

// blob is one layer to push: its media type, bytes and annotations.
type blob struct {
	mediaType   string
	data        []byte
	annotations map[string]string
}

// pushBlob pushes data to dst as d unless dst holds it.
func pushBlob(ctx context.Context, dst content.Pusher, d ocispec.Descriptor, data []byte) error {
	if err := dst.Push(ctx, d, bytes.NewReader(data)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return err
	}
	return nil
}

// manifestOf returns the OCI image manifest of artifactType with the empty config and
// the layers, a referrer of subject when it is not nil, and its descriptor, which
// carries its artifactType. It holds no time, so the same layers give the same digest.
func manifestOf(artifactType string, subject *ocispec.Descriptor, annotations map[string]string, layers []blob) (ocispec.Descriptor, []byte, error) {
	m := ocispec.Manifest{MediaType: ocispec.MediaTypeImageManifest, ArtifactType: artifactType, Config: ocispec.DescriptorEmptyJSON,
		Layers: []ocispec.Descriptor{}, Subject: subject, Annotations: annotations}
	m.SchemaVersion = 2
	for _, l := range layers {
		d := content.NewDescriptorFromBytes(l.mediaType, l.data)
		d.Annotations = l.annotations
		m.Layers = append(m.Layers, d)
	}
	b, err := json.Marshal(m)
	d := content.NewDescriptorFromBytes(m.MediaType, b)
	d.ArtifactType = artifactType
	return d, b, err
}

// push pushes to dst the empty config, the layers and then the manifest raw as d.
func push(ctx context.Context, dst content.Pusher, d ocispec.Descriptor, raw []byte, layers []blob) error {
	err := pushBlob(ctx, dst, ocispec.DescriptorEmptyJSON, ocispec.DescriptorEmptyJSON.Data)
	for _, l := range layers {
		if err == nil {
			err = pushBlob(ctx, dst, content.NewDescriptorFromBytes(l.mediaType, l.data), l.data)
		}
	}
	if err == nil {
		err = pushBlob(ctx, dst, d, raw)
	}
	return err
}

// pushArtifact pushes to dst the artifact that manifestOf describes and returns its
// descriptor.
func pushArtifact(ctx context.Context, dst content.Pusher, artifactType string, subject *ocispec.Descriptor, annotations map[string]string, layers ...blob) (ocispec.Descriptor, error) {
	d, raw, err := manifestOf(artifactType, subject, annotations, layers)
	if err == nil {
		err = push(ctx, dst, d, raw, layers)
	}
	return d, err
}

// distribution is one distribution referrer of an image: the manifest's bytes and,
// once signed, its bundle.
type distribution struct{ manifest, bundle []byte }

// imageStore is where an image is: a repository, or an OCI image layout.
type imageStore interface {
	source
	content.Pusher
}

// openImage returns the store of the image ref, its descriptor and its name: an OCI
// reference, oci:// optional, named by its repository, or the absolute path of the OCI
// image layout of one image tagged latest, which dist build writes (ADR-044 decision
// 1), named by its path.
func openImage(ctx context.Context, c def.Caller, ref string) (imageStore, ocispec.Descriptor, string, error) {
	if filepath.IsAbs(ref) {
		if _, err := os.Stat(filepath.Join(ref, ocispec.ImageLayoutFile)); err != nil {
			return nil, ocispec.Descriptor{}, "", refused("%s is not an OCI image layout: %v", ref, err)
		}
		s, err := oci.NewWithContext(ctx, ref)
		if err != nil {
			return nil, ocispec.Descriptor{}, "", err
		}
		d, err := s.Resolve(ctx, "latest")
		if err != nil {
			return nil, ocispec.Descriptor{}, "", refused("the layout %s holds no image tagged latest: %v", ref, err)
		}
		return s, d, ref, nil
	}
	repo, err := remoteRepo(c, ref)
	if err != nil {
		return nil, ocispec.Descriptor{}, "", err
	}
	d, err := resolve(ctx, repo)
	return repo, d, repo.Reference.Registry + "/" + repo.Reference.Repository, err
}

// distributions returns the distribution referrers of img in r, the store of name.
func distributions(ctx context.Context, r source, name string, img ocispec.Descriptor) ([]distribution, error) {
	ms, err := referrers(ctx, r, img, def.DistributionType)
	if err != nil {
		return nil, err
	}
	var out []distribution
	for _, m := range ms {
		var d distribution
		if l := layer(m.Manifest, def.DistributionLayer); l != nil {
			d.manifest, err = fetch(ctx, r, *l)
		}
		if l := layer(m.Manifest, bundleType); l != nil && err == nil {
			d.bundle, err = fetch(ctx, r, *l)
		}
		if err != nil {
			return nil, err
		}
		if d.manifest != nil {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, refused("the referrers of %s hold no distribution manifest", name)
	}
	return out, nil
}

// SignDist is dist sign: the distribution manifest of the image signed as a blob and
// pushed with its bundle as one referrer of the image, and the image's digest signed
// as cosign sign signs an image, in its own referrer, since the manifest names no image
// (ADR-020 decisions 2 and 12).
func (l Library) SignDist(ctx context.Context, r def.SignDistRequest) (def.Written, error) {
	if err := absolute("the key file", r.Key); err != nil {
		return def.Written{}, err
	}
	k, err := privateKey(r.Caller, r.Key)
	if err != nil {
		return def.Written{}, err
	}
	repo, img, name, err := openImage(ctx, r.Caller, r.Image)
	if err != nil {
		return def.Written{}, err
	}
	dists, err := distributions(ctx, repo, name, img)
	if err != nil {
		return def.Written{}, err
	}
	manifest := dists[0].manifest
	for _, d := range dists[1:] {
		if !bytes.Equal(d.manifest, manifest) {
			return def.Written{}, refused("the referrers of %s hold two distribution manifests", r.Image)
		}
	}
	b, err := SignBlob(k, manifest)
	if err != nil {
		return def.Written{}, err
	}
	d, err := pushArtifact(ctx, repo, def.DistributionType, &img, nil, blob{mediaType: def.DistributionLayer, data: manifest},
		blob{mediaType: bundleType, data: b})
	var sig ocispec.Descriptor
	if err == nil {
		b, err = SignStatement(k, signatureOf(name, img.Digest.Encoded()))
	}
	if err == nil {
		sig, err = pushBundle(ctx, repo, &img, signURI, b)
	}
	if err != nil {
		return def.Written{}, err
	}
	var w def.Written
	for _, p := range []ocispec.Descriptor{d, sig} {
		w.Paths = append(w.Paths, name+"@"+p.Digest.String())
	}
	return w, nil
}

// privateKey reads the private key file p with the password of the caller's
// environment.
func privateKey(c def.Caller, p string) (*ecdsa.PrivateKey, error) {
	pw, err := password(c)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(p)
	if err != nil {
		return nil, refused("read the private key: %v", err)
	}
	k, err := LoadPrivateKey(keyPEM, pw)
	if err != nil {
		return nil, refused("%s: %v", p, err)
	}
	return k, nil
}

// UseDist is dist use: the bundle of each distribution referrer of the image verified
// with each key of distributions in trust.yaml, and with the same key a signature of
// the image's digest, which the manifest does not name; then the record of the
// manifest's digest, verified/current and image.yaml, or nothing (ADR-020 decisions 10
// and 12; ADR-047 decision 13). Where trust.yaml trusts the default release root, so
// with no trust.yaml at all, the key that a release binary carries verifies too, and
// the record names the signer release (ADR-020 decision 13).
func (l Library) UseDist(ctx context.Context, r def.UseRequest) (def.Change, error) {
	h, err := l.home(r.Home)
	if err != nil {
		return def.Change{}, err
	}
	repo, img, name, err := openImage(ctx, r.Caller, r.Image)
	if err != nil {
		return def.Change{}, err
	}
	dists, err := distributions(ctx, repo, name, img)
	var sigs [][]byte
	if err == nil {
		sigs, err = bundles(ctx, repo, img, signURI)
	}
	if err != nil {
		return def.Change{}, err
	}
	signs := func(k *ecdsa.PublicKey) bool {
		return slices.ContainsFunc(sigs, func(b []byte) bool {
			st, err := VerifyStatement(k, b, mustDecode(img.Digest.Encoded()))
			_, pt := statementOf(st)
			return err == nil && pt == signURI
		})
	}
	done, err := h.Write(ctx, Write{
		Command: "dist use " + r.Image,
		Plan: func() ([]Change, error) {
			t, err := readTrust(h.path("trust.yaml"))
			if err != nil {
				return nil, err
			}
			if t.dists == nil || slices.Contains(t.dists, def.Release) {
				t.distKeys = append(t.distKeys, l.releaseKey(ctx)...)
			}
			for _, d := range dists {
				i := slices.IndexFunc(t.distKeys, func(k trustKey) bool {
					return d.bundle != nil && VerifyBlob(k.pub, d.bundle, d.manifest) == nil && signs(k.pub)
				})
				if i < 0 {
					continue
				}
				sum := sha256.Sum256(d.manifest)
				dg := "sha256:" + hex.EncodeToString(sum[:])
				return []Change{
					{Path: "verified/sha256-" + hex.EncodeToString(sum[:]), Files: map[string][]byte{
						"record.yaml": recordYAML("distribution", dg, t.distKeys[i].id), "manifest": d.manifest, bundleFile: d.bundle}},
					{Path: "verified/current", Data: []byte(dg + "\n")},
					{Path: "image.yaml", Data: fmt.Appendf(nil, "image: %s@%s\n", name, img.Digest)},
				}, nil
			}
			return nil, refused("no key of distributions in trust.yaml verifies both a distribution manifest of %s and a signature of the image %s (ADR-020 decision 12)", r.Image, img.Digest)
		},
	})
	if err != nil {
		return def.Change{Notes: done.Notes}, err
	}
	return def.Change{Written: done.Written, Notes: done.Notes, Outcome: def.RestartRequired, Reason: def.ReasonImage}, nil
}

// releaseKey returns the default release root, the key that this binary carries if it
// is a release binary, or none.
func (l Library) releaseKey(ctx context.Context) []trustKey {
	if l.Box == nil {
		return nil
	}
	r, err := l.Box.Release(ctx, "")
	pub, _, kerr := LoadPublicKey(r.Key)
	if err != nil || r.Key == nil || kerr != nil {
		return nil
	}
	return []trustKey{{id: def.Release, pub: pub}}
}
