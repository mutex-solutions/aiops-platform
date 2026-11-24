// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// Library is the operations library behind the operations key: the methods of
// def.Operations over the write of the home, the signing library and the records
// under verified/ (ADR-061 decisions 16 and 17; ADR-020 decisions 10 and 12).
type Library struct {
	LockWait time.Duration  // how long a second writer waits for write.lock
	Box      launch.Toolbox // the launcher's toolbox, which renders and ships the profiles
}

// bundleFile is the name of the bundle of a distribution manifest: beside it in the
// local form of an image, and in its record (ADR-020 decision 10).
const bundleFile = "manifest.sigstore.json"

func usage(format string, a ...any) error {
	return fmt.Errorf("%w: %s", def.ErrUsage, fmt.Sprintf(format, a...))
}

func refused(format string, a ...any) error {
	return fmt.Errorf("%w: %s", def.ErrRefused, fmt.Sprintf(format, a...))
}

// home returns the home of a request: with none, $AIOPS_HOME was unset or empty, and a
// method that needs one is a usage error (ADR-061 decision 17).
func (l Library) home(dir string) (Home, error) {
	if dir == "" {
		return Home{}, usage("no home: %s is unset or empty", def.HomeVar)
	}
	if !filepath.IsAbs(dir) {
		return Home{}, usage("the home %s is not an absolute path", dir)
	}
	return Home{Dir: dir, LockWait: l.LockWait}, nil
}

func absolute(what, p string) error {
	if !filepath.IsAbs(p) {
		return usage("%s %q is not an absolute path", what, p)
	}
	return nil
}

// password reads the password of a private key from the caller's environment; it may
// be empty, as cosign allows, but not unset (ADR-061 decision 17; ADR-020 decision 4).
func password(c def.Caller) ([]byte, error) {
	if c.Env != nil {
		if v, ok := c.Env(def.KeyPasswordVar); ok {
			return []byte(v), nil
		}
	}
	return nil, usage("%s is unset: it holds the password of the private key, and may be empty", def.KeyPasswordVar)
}

// Home reads what the home holds, each file whole and without the lock (ADR-061
// decision 19).
func (l Library) Home(_ context.Context, r def.HomeRequest) (def.Home, error) {
	h, err := l.home(r.Home)
	if err != nil {
		return def.Home{}, err
	}
	t, err := readTrust(h.path("trust.yaml"))
	if err != nil {
		return def.Home{}, err
	}
	out := def.Home{Signers: t.signers, Distributions: t.dists, AllowUnsigned: t.allow}
	if out.Distributions == nil {
		out.Distributions = []string{def.Release}
	}
	files, _ := filepath.Glob(h.path("verified/sha256-*/record.yaml"))
	for _, f := range files {
		var rec struct{ Kind, Digest, Signer, ID string }
		if err := readYAML(f, &rec); err != nil {
			return def.Home{}, err
		}
		out.Records = append(out.Records, def.Record{Digest: rec.Digest, Kind: rec.Kind, Signer: rec.Signer, ID: rec.ID})
	}
	b, err := os.ReadFile(h.path("verified/current"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return def.Home{}, err
	}
	out.Current = strings.TrimSpace(string(b))
	var img struct{ Image string }
	if err := readYAML(h.path("image.yaml"), &img); err != nil {
		return def.Home{}, err
	}
	out.Image = img.Image
	_, err = os.Stat(h.path(snapshotDir))
	out.Snapshot = err == nil
	return out, nil
}

// readYAML decodes the file p into v; a missing file leaves v as it is.
func readYAML(p string, v any) error {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = yaml.Unmarshal(b, v)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	return nil
}

// AddTrust is trust add (ADR-020 decisions 5, 6 and 13; ADR-061 decision 17).
func (l Library) AddTrust(ctx context.Context, r def.TrustRequest) (def.Change, error) {
	h, err := l.home(r.Home)
	if err != nil {
		return def.Change{}, err
	}
	if err := absolute("the key file", r.KeyFile); err != nil {
		return def.Change{}, err
	}
	e, err := trustEntry(r)
	if err != nil {
		return def.Change{}, err
	}
	var ch def.Change
	done, err := h.Write(ctx, Write{
		Command: "trust add " + r.KeyFile,
		Plan: func() ([]Change, error) {
			data, err := appendTrust(h.path("trust.yaml"), e)
			return []Change{{Path: "trust.yaml", Data: data}}, err
		},
		Submit: func(context.Context) error { ch.Outcome, ch.Reason = submission(h); return nil },
	})
	ch.Written, ch.Notes = done.Written, done.Notes
	return ch, err
}

// submission is the outcome of a write of the home that version 1 of the wire of
// ADR-062 does not carry, read from run/status.json alone (ADR-061 decision 17). With
// no running process the change applies at its next start; with one, it is not
// submitted and the process reads it at its next start.
func submission(h Home) (def.Outcome, string) {
	var st struct{ Phase, Profile string }
	b, err := os.ReadFile(h.path("run/status.json"))
	if err != nil || json.Unmarshal(b, &st) != nil || st.Phase == "stopped" {
		return def.NotRunning, ""
	}
	return def.RestartRequired, def.ReasonNotSubmitted
}

// GenerateKey is key generate: <prefix>.key with mode 0600 and <prefix>.pub with mode
// 0644, as cosign writes them, neither over a file that exists (ADR-020 decisions 4
// and 21).
func (l Library) GenerateKey(_ context.Context, r def.KeyRequest) (def.Written, error) {
	if err := absolute("the prefix", r.Prefix); err != nil {
		return def.Written{}, err
	}
	pw, err := password(r.Caller)
	if err != nil {
		return def.Written{}, err
	}
	priv, pub, err := GenerateKeyPair(pw)
	if err != nil {
		return def.Written{}, err
	}
	paths := []string{r.Prefix + ".key", r.Prefix + ".pub"}
	for i, data := range [][]byte{priv, pub} {
		err := create(paths[i], data, []os.FileMode{0o600, 0o644}[i])
		if errors.Is(err, fs.ErrExist) {
			err = refused("%s exists, and key generate writes over no file", paths[i])
		}
		if err != nil {
			if i == 1 {
				_ = os.Remove(paths[0])
			}
			return def.Written{}, err
		}
	}
	return def.Written{Paths: paths}, nil
}

// create writes data to the new file p with mode.
func create(p string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		_ = os.Remove(p)
	}
	return err
}

// SignDist is dist sign on the local form of an image: the manifest's bundle written
// beside it (ADR-020 decisions 2 and 12).
func (l Library) SignDist(_ context.Context, r def.SignDistRequest) (def.Written, error) {
	if err := errors.Join(absolute("the image", r.Image), absolute("the key file", r.Key)); err != nil {
		return def.Written{}, err
	}
	pw, err := password(r.Caller)
	if err != nil {
		return def.Written{}, err
	}
	keyPEM, err := os.ReadFile(r.Key)
	if err != nil {
		return def.Written{}, refused("read the private key: %v", err)
	}
	k, err := LoadPrivateKey(keyPEM, pw)
	if err != nil {
		return def.Written{}, refused("%s: %v", r.Key, err)
	}
	manifest, err := os.ReadFile(filepath.Join(r.Image, "manifest"))
	if err != nil {
		return def.Written{}, refused("the image %s holds no distribution manifest: %v", r.Image, err)
	}
	b, err := SignBlob(k, manifest)
	if err != nil {
		return def.Written{}, err
	}
	out := filepath.Join(r.Image, bundleFile)
	tmp := out + ".new"
	err = writeFile(tmp, b)
	if err == nil {
		err = os.Chmod(tmp, 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, out)
	}
	if err != nil {
		return def.Written{}, errors.Join(err, os.Remove(tmp))
	}
	return def.Written{Paths: []string{out}}, nil
}

// UseDist is dist use on the local form of an image: the manifest's bundle verified
// with each key of distributions in trust.yaml, then the record of the manifest's
// digest and verified/current, or nothing (ADR-020 decisions 10 and 12). The default
// release root is verified with the key that a release binary carries, which no
// binary built at S2a has, so only the keys trust.yaml holds verify here.
func (l Library) UseDist(ctx context.Context, r def.UseRequest) (def.Change, error) {
	h, err := l.home(r.Home)
	if err != nil {
		return def.Change{}, err
	}
	if err := absolute("the image", r.Image); err != nil {
		return def.Change{}, err
	}
	manifest, err := os.ReadFile(filepath.Join(r.Image, "manifest"))
	if err != nil {
		return def.Change{}, refused("the image %s holds no distribution manifest: %v", r.Image, err)
	}
	bundle, err := os.ReadFile(filepath.Join(r.Image, bundleFile))
	if err != nil {
		return def.Change{}, refused("the image %s holds no signature: %v", r.Image, err)
	}
	sum := sha256.Sum256(manifest)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	done, err := h.Write(ctx, Write{
		Command: "dist use " + r.Image,
		Plan: func() ([]Change, error) {
			t, err := readTrust(h.path("trust.yaml"))
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(t.distKeys, func(k trustKey) bool { return VerifyBlob(k.pub, bundle, manifest) == nil })
			if i < 0 {
				return nil, refused("no key of distributions in trust.yaml verifies the signature of the manifest %s of %s", digest, r.Image)
			}
			return []Change{
				{Path: "verified/" + strings.Replace(digest, ":", "-", 1), Files: map[string][]byte{
					"record.yaml": recordYAML("distribution", digest, t.distKeys[i].id), "manifest": manifest, bundleFile: bundle}},
				{Path: "verified/current", Data: []byte(digest + "\n")},
			}, nil
		},
	})
	if err != nil {
		return def.Change{Notes: done.Notes}, err
	}
	return def.Change{Written: done.Written, Notes: done.Notes, Outcome: def.RestartRequired, Reason: def.ReasonImage}, nil
}

// recordYAML is record.yaml of a record of a distribution, in the form boot reads
// (boot.ReadRecord; ADR-020 decision 10).
func recordYAML(kind, digest, signer string) []byte {
	return fmt.Appendf(nil, "kind: %s\ndigest: %s\nsigner: %s\n", kind, digest, signer)
}
