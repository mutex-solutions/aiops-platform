// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.yaml.in/yaml/v3"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"

	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// BuilderAnnotation is the annotation of the release artifact, the referrer of the
// release image that holds the release's signed manifest and its bundle, whose value is
// the digest of the release's builder image in the same repository (ADR-044 decision
// 1). bundleType is the media type of a Sigstore bundle (ADR-020 decision 2).
const (
	BuilderAnnotation = "vnd.aiops.builder.image"
	bundleType        = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

// definition is the definition of the builder image, dist/builder/image.yaml: what it
// bakes in, its Go environment (ADR-060 decision 21) and its source offer (ADR-044
// decisions 4, 5 and 17).
type definition struct {
	Bakes []struct {
		Entry   string `yaml:"entry"`
		Version string `yaml:"version"`
	} `yaml:"bakes"`
	Env         []string `yaml:"env"`
	SourceOffer string   `yaml:"sourceOffer"`
}

// readDefinition reads a definition, refusing a field it does not know and one that
// bakes in anything but the toolchain of the builder's pin.
func readDefinition(data []byte) (definition, error) {
	var d definition
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("the definition of the builder image: %w", err)
	}
	if len(d.Bakes) != 1 || d.Bakes[0].Entry != "go-toolchain" || !strings.Contains(ToolchainVersion, "-"+d.Bakes[0].Version+".") {
		return d, fmt.Errorf("the definition of the builder image bakes in %v: the builder's pin is the toolchain %s, and nothing else (ADR-044 decision 4)", d.Bakes, ToolchainVersion)
	}
	return d, nil
}

// keyID returns the id of the release public key, a PEM PUBLIC KEY block of an ECDSA
// P-256 key: the base64 of the SHA-256 of its DER form, as boot reads it (ADR-020
// decisions 2 and 4).
func keyID(pubPEM []byte) (string, error) {
	b, _ := pem.Decode(pubPEM)
	if b == nil || b.Type != "PUBLIC KEY" {
		return "", errors.New("the release public key is not a PEM PUBLIC KEY block")
	}
	k, err := x509.ParsePKIXPublicKey(b.Bytes)
	if ec, ok := k.(*ecdsa.PublicKey); err != nil || !ok || ec.Curve != elliptic.P256() {
		return "", errors.New("the release public key is not an ECDSA P-256 key (ADR-020 decision 2)")
	}
	sum := sha256.Sum256(b.Bytes)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

// toolchainFiles returns the licence and notice files of the toolchain tree goroot, by
// a walk with the names of decision 3, each module zip under lib/fips140/ opened, and
// lib/time/README, which no name marks (ADR-044 decision 5): files of the toolchain
// module at their paths in the tree, a zip's at <zip path>/<entry>.
func toolchainFiles(goroot string) ([]File, error) {
	var out []File
	add := func(rel string, data []byte) {
		out = append(out, File{Module: Toolchain, Version: ToolchainVersion, Path: rel, Data: data, Notice: true})
	}
	err := filepath.WalkDir(goroot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(goroot, p)
		rel = filepath.ToSlash(rel)
		zipped := strings.HasPrefix(rel, "lib/fips140/") && strings.HasSuffix(rel, ".zip")
		switch {
		case zipped:
			z, err := zip.OpenReader(p)
			if err != nil {
				return err
			}
			defer z.Close()
			for _, f := range z.File {
				if kindOf(path.Base(f.Name)) != notLicence {
					b, err := readZip(f)
					if err != nil {
						return err
					}
					add(rel+"/"+f.Name, b)
				}
			}
		case kindOf(d.Name()) != notLicence || rel == "lib/time/README":
			b, err := os.ReadFile(p)
			add(rel, b)
			return err
		}
		return nil
	})
	return out, err
}

// component is one package of an SBOM: a module, or a binary or an image with no version.
type component struct{ name, version, sum, licence string }

// treeComponents returns the toolchain module, the module of each line of the
// modules.txt of a vendor directory of the tree, and the module of each zip under
// lib/fips140/, each with the licence of the LICENSE at its root among files.
func treeComponents(goroot string, files []File) ([]component, error) {
	licence := func(rel string) (string, error) {
		i := slices.IndexFunc(files, func(f File) bool { return f.Path == rel })
		if i < 0 {
			return "", fmt.Errorf("the toolchain tree holds no %s", rel)
		}
		return Classify(files[i].Data)
	}
	var out []component
	var errs []error
	add := func(c component, rel string) {
		var err error
		c.licence, err = licence(rel)
		out, errs = append(out, c), append(errs, err)
	}
	add(component{name: Toolchain, version: ToolchainVersion, sum: ToolchainSum}, "LICENSE")
	err := filepath.WalkDir(goroot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Name() != "modules.txt" || filepath.Base(filepath.Dir(p)) != "vendor" {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(goroot, filepath.Dir(p))
		for _, l := range strings.Split(string(b), "\n") {
			if fs := strings.Fields(l); len(fs) >= 3 && fs[0] == "#" {
				add(component{name: fs[1], version: fs[2]}, filepath.ToSlash(rel)+"/"+fs[1]+"/LICENSE")
			}
		}
		return err
	})
	for _, f := range files {
		if _, entry, ok := strings.Cut(f.Path, ".zip/"); ok && path.Base(entry) == "LICENSE" {
			mod, version, _ := strings.Cut(path.Dir(entry), "@")
			add(component{name: mod, version: version}, f.Path)
		}
	}
	return out, errors.Join(append(errs, err)...)
}

// layer is the files of an image layer by name, each a file of the host or bytes; a
// directory's name ends in "/".
type layer map[string]layerFile

type layerFile struct {
	src  string
	data []byte
	mode int64
}

// parents adds the directories above name.
func (y layer) parents(name string) {
	for d := path.Dir(strings.TrimSuffix(name, "/")); d != "." && d != "/"; d = path.Dir(d) {
		y[d+"/"] = layerFile{mode: 0o755}
	}
}

// tree adds the files under dir at prefix, executable where the host's file is.
func (y layer) tree(dir, prefix string) error {
	y.parents(path.Join(prefix, "x"))
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		name := path.Join(prefix, filepath.ToSlash(rel))
		info, err := d.Info()
		switch {
		case err != nil:
			return err
		case d.IsDir():
			y[name+"/"] = layerFile{mode: 0o755}
		case info.Mode()&0o111 != 0:
			y[name] = layerFile{src: p, mode: 0o755}
		default:
			y[name] = layerFile{src: p, mode: 0o644}
		}
		return nil
	})
}

// file adds data at name.
func (y layer) file(name string, data []byte) {
	y.parents(name)
	y[name] = layerFile{data: data, mode: 0o644}
}

// pushImage pushes into s an image for linux/amd64 of the one layer y, on no base layer
// (ADR-044 decisions 17 and 18), with the config c, and returns its descriptor. Every
// entry is owned by root and dated at the epoch, so the same files give one digest.
// ponytail: the layer is built in memory, about 300 MB for the builder image; write it
// to a file when an image outgrows the memory of a release runner.
func pushImage(ctx context.Context, s *oci.Store, y layer, c ocispec.ImageConfig) (ocispec.Descriptor, error) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, name := range sortedKeys(y) {
		f := y[name]
		h := &tar.Header{Name: name, Mode: f.mode, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		data, err := f.data, error(nil)
		if f.src != "" {
			data, err = os.ReadFile(f.src)
		}
		if strings.HasSuffix(name, "/") {
			h.Typeflag = tar.TypeDir
		}
		h.Size = int64(len(data))
		if err == nil {
			err = tw.WriteHeader(h)
		}
		if err == nil {
			_, err = tw.Write(data)
		}
		if err != nil {
			return ocispec.Descriptor{}, err
		}
	}
	if err := tw.Close(); err != nil {
		return ocispec.Descriptor{}, err
	}
	ld := content.NewDescriptorFromBytes(ocispec.MediaTypeImageLayer, b.Bytes())
	rootfs := ocispec.RootFS{Type: "layers"}
	rootfs.DiffIDs = append(rootfs.DiffIDs, ld.Digest)
	cfg, _ := json.Marshal(ocispec.Image{Platform: ocispec.Platform{Architecture: "amd64", OS: "linux"}, Config: c, RootFS: rootfs})
	cd := content.NewDescriptorFromBytes(ocispec.MediaTypeImageConfig, cfg)
	for _, p := range []struct {
		d    ocispec.Descriptor
		data []byte
	}{{ld, b.Bytes()}, {cd, cfg}} {
		if err := s.Push(ctx, p.d, bytes.NewReader(p.data)); err != nil {
			return ocispec.Descriptor{}, err
		}
	}
	return oras.PackManifest(ctx, s, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{Layers: []ocispec.Descriptor{ld}, ConfigDescriptor: &cd,
		ManifestAnnotations: map[string]string{ocispec.AnnotationCreated: epoch}})
}

// referrer pushes into s the distribution referrer of subject (ADR-044 decision 1): the
// signed manifest and, for a release, its bundle, with notes among its annotations.
func referrer(ctx context.Context, s *oci.Store, subject ocispec.Descriptor, manifest, bundle []byte, notes map[string]string) error {
	var layers []ocispec.Descriptor
	for _, l := range []struct {
		mediaType string
		data      []byte
	}{{operations.DistributionLayer, manifest}, {bundleType, bundle}} {
		if l.data == nil {
			continue
		}
		d := content.NewDescriptorFromBytes(l.mediaType, l.data)
		if err := s.Push(ctx, d, bytes.NewReader(l.data)); err != nil {
			return err
		}
		layers = append(layers, d)
	}
	ann := map[string]string{ocispec.AnnotationCreated: epoch}
	maps.Copy(ann, notes)
	_, err := oras.PackManifest(ctx, s, oras.PackManifestVersion1_1, operations.DistributionType, oras.PackManifestOptions{Subject: &subject, Layers: layers, ManifestAnnotations: ann})
	return err
}

// builderImage writes the builder image into s from the definition def, the toolchain
// tree goroot and the directory ctl that holds the release aiopsctl with its
// THIRD_PARTY_LICENSES/, NOTICE and SBOM, whose graph is pkgs with the licences l
// (ADR-044 decisions 4, 5 and 17; dist/builder/image.yaml says what goes where), and
// returns its descriptor.
func builderImage(ctx context.Context, s *oci.Store, def []byte, goroot, ctl string, pkgs []Package, l Licences, created time.Time) (ocispec.Descriptor, error) {
	d, err := readDefinition(def)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	files, err := toolchainFiles(goroot)
	var cs []component
	if err == nil {
		cs, err = treeComponents(goroot, files)
	}
	bins := []component{{name: "aiopsctl"}} // and the test binaries it embeds, whose graphs pkgs holds
	for _, s := range SuiteBinaries() {
		bins = append(bins, component{name: "aiopsctl/suites/" + s + ".test"})
	}
	var sbom []byte
	if err == nil {
		sbom, err = spdxOf("builder", slices.Concat(cs, bins, components(pkgs, l)), created)
	}
	root, y := "cache/aiopsctl/toolchain/"+ToolchainVersion, layer{}
	if err == nil {
		err = errors.Join(y.tree(goroot, root), y.tree(ctl, ""))
	}
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	y.file(root+".sum", []byte(ToolchainSum))
	for _, f := range files {
		y.file("THIRD_PARTY_LICENSES/"+Toolchain+"/"+f.Path, f.Data)
	}
	y.file("builder.spdx.json", sbom)
	y.file("SOURCE-OFFER", []byte(d.SourceOffer))
	return pushImage(ctx, s, y, ocispec.ImageConfig{Entrypoint: []string{"/aiopsctl"}, Env: append(slices.Clone(d.Env), "XDG_CACHE_HOME=/cache")})
}

// ImageRequest is dist build on the image path: the site's dist.yaml and the module source,
// both absolute, the mirrors by prefix and the modules whose raise is allowed; or plugin
// smoke, with the plugin directory and the release artifact, whose rows go to Stdout.
type ImageRequest struct {
	DistYAML, ModCache, Smoke, Against string
	Mirrors                            map[string]string
	AllowBump                          []string
	Progress, Stdout                   io.Writer
}

// RunImage is dist build on the image path (ADR-044 decisions 1 and 9; ADR-061 decision
// 21): it reads the release artifact that dist.yaml names, by digest, through the mirrors,
// and runs the builder image that its annotation names in the same repository, by digest,
// which the container runtime pulls and checks: the image's aiopsctl, with the same
// command and --builder local, no network, the caller's user, dist.yaml and the module
// source bound read-only, build/ beside dist.yaml read-write, which a failed build leaves
// as it found it, and the Go environment of ADR-060 decision 21. It returns what that
// aiopsctl wrote. For plugin smoke it runs plugin smoke on the local path, the plugin
// directory bound read-only, and boots the throwaway distribution inside the container.
func RunImage(ctx context.Context, q ImageRequest) (Result, error) {
	q.Progress, q.Stdout = cmp.Or[io.Writer](q.Progress, io.Discard), cmp.Or[io.Writer](q.Stdout, q.Progress)
	if !filepath.IsAbs(cmp.Or(q.DistYAML, q.Smoke)) || !filepath.IsAbs(q.ModCache) {
		return Result{}, errors.New("the image path takes dist.yaml or the plugin directory and --modcache, each an absolute path: every module source is a directory until L14 clears (ADR-044 decision 7)")
	}
	site, err := Site{Release: q.Against}, error(nil)
	if q.Smoke == "" {
		site, err = readSite(q.DistYAML)
	} else if !byDigest.MatchString(q.Against) {
		err = fmt.Errorf("plugin smoke --against names the release artifact by digest, not %q (08:401)", q.Against)
	}
	if err != nil {
		return Result{}, err
	}
	m, data, repo, err := releaseArtifact(ctx, site.Release, q.Mirrors)
	if err == nil && m.Annotations[BuilderAnnotation] == "" {
		err = fmt.Errorf("the release artifact %s names no builder image in the annotation %s (ADR-044 decision 1)", site.Release, BuilderAnnotation)
	}
	if err != nil {
		return Result{}, err
	}
	image := repo + "@" + m.Annotations[BuilderAnnotation]
	out := filepath.Join(filepath.Dir(q.DistYAML), "build")
	entries, err := os.ReadDir(out)
	if q.Smoke == "" && err == nil && len(entries) > 0 {
		return Result{}, fmt.Errorf("%s exists and is not empty", out)
	}
	made := q.Smoke == "" && err != nil // a build that fails leaves no build/ that it made
	scratch, err := os.MkdirTemp("", "aiopsctl-image-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(scratch)
	// The image's aiopsctl, with no network, reads the release artifact from the scratch
	// directory, through a mirror, and makes the check of the local path on it.
	name, dg, _ := strings.Cut(strings.TrimPrefix(site.Release, "oci://"), "@")
	blob := filepath.Join(scratch, "release", "blobs", "sha256", strings.TrimPrefix(dg, "sha256:"))
	if err := errors.Join(os.MkdirAll(filepath.Dir(blob), 0o755), os.WriteFile(blob, data, 0o644)); err != nil {
		return Result{}, err
	}
	args := []string{"run", "--rm", "--network", "none", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", q.ModCache + ":/modcache:ro", "-v", scratch + ":/scratch",
		"-e", "TMPDIR=/scratch", "-e", "GOPROXY=file:///modcache", "-e", "GOSUMDB=off", "-e", "GOTOOLCHAIN=local", "-e", "GOFLAGS=-mod=readonly"}
	if q.Smoke != "" {
		args = append(args, "-v", q.Smoke+":/work/plugin:ro", image, "plugin", "smoke", "/work/plugin", "--against", site.Release)
	} else {
		if err := os.MkdirAll(out, 0o755); err != nil {
			return Result{}, err
		}
		args = append(args, "-v", q.DistYAML+":/work/dist.yaml:ro", "-v", out+":/work/build", image, "dist", "build", "/work/dist.yaml")
	}
	args = append(args, "--modcache", "/modcache", "--builder", "local", "--mirror", name+"=/scratch/release")
	for _, m := range q.AllowBump {
		args = append(args, "--allow-bump", m)
	}
	fmt.Fprintf(q.Progress, "the builder image %s, which the release artifact %s names\n", image, site.Release)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = q.Stdout, q.Progress
	if err := cmd.Run(); err != nil {
		if made {
			_ = os.Remove(out)
		}
		return Result{}, fmt.Errorf("the builder image %s: %w", image, err)
	}
	if q.Smoke != "" {
		return Result{}, nil
	}
	manifest, err := os.ReadFile(filepath.Join(out, "distribution.yaml"))
	return Result{Manifest: manifest, Aiops: filepath.Join(out, "aiops", "aiops"), Image: filepath.Join(out, "image"), Aiopsctl: filepath.Join(out, "aiopsctl", "aiopsctl")}, err
}

// releaseArtifact returns the release artifact that release names by digest, read through
// mirrors (ADR-044 decision 9): its OCI manifest, as read and parsed, and the repository
// that holds it, from the registry or, where a mirror's site prefix is an absolute
// directory, from blobs/sha256/ under it, as an OCI image layout holds it, the way the
// image path hands it to the image's aiopsctl. An object whose digest differs fails.
func releaseArtifact(ctx context.Context, release string, mirrors map[string]string) (ocispec.Manifest, []byte, string, error) {
	ref := mirrored(strings.TrimPrefix(release, "oci://"), mirrors)
	repo, dg, _ := strings.Cut(ref, "@")
	var data []byte
	var err error
	if filepath.IsAbs(repo) {
		data, err = os.ReadFile(filepath.Join(repo, "blobs", "sha256", strings.TrimPrefix(dg, "sha256:")))
		if sum := fmt.Sprintf("sha256:%x", sha256.Sum256(data)); err == nil && sum != dg {
			err = fmt.Errorf("its digest is %s", sum)
		}
	} else {
		data, repo, err = pull(ctx, ref)
	}
	var m ocispec.Manifest
	if err == nil && (json.Unmarshal(data, &m) != nil || m.ArtifactType != operations.DistributionType) {
		err = fmt.Errorf("it is no artifact of type %s", operations.DistributionType)
	}
	if err != nil {
		err = fmt.Errorf("the release artifact %s: %w", ref, err)
	}
	return m, data, repo, err
}

// pull fetches the manifest that ref names by digest from its registry, with the
// caller's credentials and plain HTTP on the loopback interface, and returns it and
// its repository.
func pull(ctx context.Context, ref string) ([]byte, string, error) {
	r, err := remote.NewRepository(ref)
	var store credentials.Store
	if err == nil {
		store, err = credentials.NewStoreFromDocker(credentials.StoreOptions{})
	}
	if err != nil {
		return nil, ref, err
	}
	r.Client = &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache(), Credential: credentials.Credential(store)}
	u, _ := url.Parse("//" + r.Reference.Registry)
	r.PlainHTTP = u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()
	_, data, err := oras.FetchBytes(ctx, r, r.Reference.Reference, oras.FetchBytesOptions{MaxBytes: 1 << 20})
	return data, r.Reference.Registry + "/" + r.Reference.Repository, err
}

// sameRelease refuses the release that dist.yaml names unless the signed manifest it
// holds is, by digest, the one this aiopsctl embeds, and names the release to use: a
// release builds only with its own builder (ADR-044 decision 1).
func (b *build) sameRelease(ctx context.Context) error {
	m, _, _, err := releaseArtifact(ctx, b.site.Release, b.q.Mirrors)
	want := fmt.Sprintf("sha256:%x", sha256.Sum256(b.q.Release))
	if err == nil && !slices.ContainsFunc(m.Layers, func(l ocispec.Descriptor) bool {
		return l.MediaType == operations.DistributionLayer && l.Digest.String() == want
	}) {
		err = fmt.Errorf("dist.yaml names the release %s, whose signed manifest is not %s, the one this aiopsctl embeds, and a release builds only with its own builder: name the release artifact of %s, or run the aiopsctl of the release that dist.yaml names (ADR-044 decision 1)",
			b.site.Release, want, b.releaseVersion(rootModule))
	}
	return err
}

// mirrored rewrites the registry and repository prefix of ref that the longest prefix
// of mirrors names, and keeps the rest of it, the digest among it (ADR-044 decision 9).
func mirrored(ref string, mirrors map[string]string) string {
	best := ""
	for p := range mirrors {
		if rest, ok := strings.CutPrefix(ref, p); ok && (rest == "" || rest[0] == '/' || rest[0] == '@') && len(p) > len(best) {
			best = p
		}
	}
	if best == "" {
		return ref
	}
	return mirrors[best] + strings.TrimPrefix(ref, best)
}
