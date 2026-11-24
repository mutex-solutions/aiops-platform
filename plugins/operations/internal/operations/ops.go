// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	profiles, _ := filepath.Glob(h.path("profiles/*/profile.yaml"))
	for _, p := range profiles {
		out.Profiles = append(out.Profiles, filepath.Base(filepath.Dir(p)))
	}
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
	if r.Profile == "" {
		return out, nil
	}
	var f struct{ Bundles []string }
	var items []struct {
		ID      string
		Enabled *bool
	}
	out.Locked, err = readLock(h, r.Profile)
	err = errors.Join(err, readYAML(h.path("profiles/"+r.Profile+"/profile.yaml"), &f), readYAML(h.path("profiles/"+r.Profile+"/aiops.patch.yml"), &items))
	out.Bundles = f.Bundles
	for _, it := range items {
		if it.Enabled != nil && out.Enabled == nil {
			out.Enabled = map[string]bool{}
		}
		if it.Enabled != nil {
			out.Enabled[it.ID] = *it.Enabled
		}
	}
	return out, err
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
		Submit: func(context.Context) error { ch.Outcome, ch.Reason = submission(h, ""); return nil },
	})
	ch.Written, ch.Notes = done.Written, done.Notes
	return ch, err
}

// submission is the outcome of a write of the home that version 1 of the wire of
// ADR-062 does not carry, read from run/status.json alone (ADR-061 decision 17). With
// no running process of profile, of any profile when it is "", the change applies at
// its next start; with one, it is not submitted and the process reads it at its next
// start.
func submission(h Home, profile string) (def.Outcome, string) {
	var st struct{ Phase, Profile string }
	b, err := os.ReadFile(h.path("run/status.json"))
	if err != nil || json.Unmarshal(b, &st) != nil || st.Phase == "stopped" || profile != "" && st.Profile != profile {
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

// recordYAML is record.yaml of a record of a distribution, in the form boot reads
// (boot.ReadRecord; ADR-020 decision 10).
func recordYAML(kind, digest, signer string) []byte {
	return fmt.Appendf(nil, "kind: %s\ndigest: %s\nsigner: %s\n", kind, digest, signer)
}
