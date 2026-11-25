// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing/fstest"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/internal/builder"
	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// verify is the build of plugin verify of in-process code (ADR-060 decision 46; ADR-044
// decision 1): a throwaway site build of the release that q.Against names and the
// module at the version and sum that the artifact records, resolved through the module
// source against that sum, whose tests, the suites among them, the builder runs on the
// release's build list. What it wrote is removed.
func (t Tools) verify(ctx context.Context, q launch.BuildRequest) (launch.Built, error) {
	tmp, err := os.MkdirTemp("", "aiopsctl-verify-")
	if err != nil {
		return launch.Built{}, err
	}
	defer os.RemoveAll(tmp)
	type plugin struct {
		Module  string `json:"module"`
		Version string `json:"version"`
		Sum     string `json:"sum"`
	}
	site, _ := json.Marshal(struct { // JSON is YAML, and quotes what the artifact supplies
		Release string   `json:"release"`
		Plugins []plugin `json:"plugins"`
	}{q.Against, []plugin{{q.Module, q.Version, q.Sum}}})
	q.DistYAML, q.Module = filepath.Join(tmp, "dist.yaml"), ""
	if err := os.WriteFile(q.DistYAML, site, 0o600); err != nil {
		return launch.Built{}, err
	}
	b, err := t.Build(ctx, q)
	return launch.Built{Manifest: b.Manifest}, err
}

// smoke is the build of plugin smoke (ADR-044 decision 1; ADR-060 decision 58, reading
// 1): the throwaway distribution of the release that q.Against names and the module of
// the plugin directory, then the boot of its test-only profile. On the image path the
// image's aiopsctl runs both, the boot inside its container, and prints the rows.
func (t Tools) smoke(ctx context.Context, q launch.BuildRequest) (launch.Built, error) {
	if q.Builder == "image" {
		var out bytes.Buffer
		_, err := builder.RunImage(ctx, builder.ImageRequest{Smoke: q.Plugin, Against: q.Against, ModCache: q.ModCache, Mirrors: q.Mirrors,
			Progress: t.Progress, Stdout: &out})
		return launch.Built{Rows: ParseRows(out.String())}, err
	}
	if q.Builder != "local" {
		return launch.Built{}, fmt.Errorf("the builder is image or local, not %q", q.Builder)
	}
	tmp, err := os.MkdirTemp("", "aiopsctl-smoke-")
	if err != nil {
		return launch.Built{}, err
	}
	defer os.RemoveAll(tmp)
	cache, err := os.UserCacheDir()
	if err != nil {
		return launch.Built{}, err
	}
	r, err := builder.Build(ctx, builder.Request{Smoke: q.Plugin, Against: q.Against, Release: t.Signed, ModCache: q.ModCache, Mirrors: q.Mirrors,
		Proxy: os.Getenv("GOPROXY"), Out: filepath.Join(tmp, "build"), Cache: filepath.Join(cache, "aiopsctl"), Progress: t.Progress})
	if err != nil {
		return launch.Built{}, err
	}
	rows, err := Boot(ctx, r.Aiops, r.Manifest, q.Plugin, filepath.Join(tmp, "home"), t.Progress)
	return launch.Built{Manifest: r.Manifest, Rows: rows}, err
}

// rowLine is a row as plugin smoke prints it on stdout.
var rowLine = regexp.MustCompile(`^row (\S+): (\S+)(?: and waits for (.+))?$`)

// ParseRows reads the rows that plugin smoke printed, each a line
// "row <id>: <state>[ and waits for <key>, <key>]", "-" for a row with no fiber.
func ParseRows(out string) []launch.RowWait {
	var rows []launch.RowWait
	for _, l := range strings.Split(out, "\n") {
		if m := rowLine.FindStringSubmatch(l); m != nil {
			r := launch.RowWait{ID: m[1], State: kernel.State(strings.TrimPrefix(m[2], "-"))}
			if m[3] != "" {
				r.WaitsFor = strings.Split(m[3], ", ")
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// smokeBound bounds the boot of the test-only profile and its stop.
const smokeBound = 2 * time.Minute

// Boot boots the test-only profile of plugin smoke with the server aiops in a new home:
// the release's aiops-core bundle, as this aiopsctl ships it, and the bundle of the plugin
// in dir, each of whose rows a --patch file enables and which disables reload-socket,
// as nothing submits in smoke (ADR-060 decision 58, reading 1; ADR-062 decision 27). It
// returns each row's state and what it waits for once the boot settled, from
// run/status.json, and stops the server; a row of the plugin that did not mount, as one
// the gate denied, fails it. The home trusts the distribution whose signed manifest
// the server embeds, manifest, which this smoke built: trust.yaml lists a throwaway key
// among distributions, and the record of the manifest names it, as dist use writes it
// once a signature verifies; boot verifies no signature (ADR-020 decisions 10 and 12).
func Boot(ctx context.Context, aiops string, manifest []byte, dir, home string, progress io.Writer) ([]launch.RowWait, error) {
	bundle, err := os.ReadFile(filepath.Join(dir, "bundle.yaml"))
	if err != nil {
		return nil, err
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	id, digest := sha256.Sum256(der), sha256.Sum256(manifest)
	pub := strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	trust := "distributions:\n  - key: |\n      " + strings.ReplaceAll(pub, "\n", "\n      ") + "\n"
	record := fmt.Sprintf("kind: distribution\ndigest: sha256:%x\nsigner: %s\n", digest, base64.StdEncoding.EncodeToString(id[:]))
	// The rows of the plugin's bundle, by their ids in the profile.
	alone := fstest.MapFS{"profiles/smoke/profile.yaml": {Data: []byte("bundles: [plugin]\n")}, "bundles/plugin/bundle.yaml": {Data: bundle}}
	c, _, err := boot.Compose(boot.Sources{Profile: "smoke", Home: alone, Bundles: alone}, boot.Options{})
	if err != nil {
		return nil, err
	}
	patch := "# plugin smoke: the rows of the plugin enabled, and reload-socket disabled (ADR-062 decision 27).\n- {id: reload-socket, enabled: false}\n"
	for _, r := range c.Rows {
		patch += fmt.Sprintf("- {id: %q, enabled: true}\n", r.ID)
	}
	core, err := fs.ReadFile(dist.Shipped(), "bundles/aiops-core/bundle.yaml") // the release's, which this aiopsctl ships
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"profiles/smoke/profile.yaml": []byte("bundles: [aiops-core, plugin]\n"), "bundles/aiops-core/bundle.yaml": core,
		"bundles/plugin/bundle.yaml": bundle, "smoke.patch.yml": []byte(patch), "trust.yaml": []byte(trust),
		fmt.Sprintf("verified/sha256-%x/record.yaml", digest): []byte(record), "verified/current": fmt.Appendf(nil, "sha256:%x\n", digest)}
	for name, data := range files {
		p := filepath.Join(home, filepath.FromSlash(name))
		if err := errors.Join(os.MkdirAll(filepath.Dir(p), 0o700), os.WriteFile(p, data, 0o600)); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, smokeBound)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.Command(aiops, "--profile", "smoke", "--home", home, "--patch", filepath.Join(home, "smoke.patch.yml"))
	cmd.Env, cmd.Stdout, cmd.Stderr = []string{"HOME=" + home}, &out, &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(smokeBound):
			_ = cmd.Process.Kill()
			<-exited
		}
		fmt.Fprintf(progress, "smoke: the server of the throwaway distribution:\n%s", out.String())
	}()
	status := filepath.Join(runfile.Dir(home), serve.StatusFile)
	for tick := time.NewTicker(50 * time.Millisecond); ; {
		var s serve.Status
		b, err := os.ReadFile(status)
		if err == nil && json.Unmarshal(b, &s) == nil && (s.Phase == string(boot.PhaseRunning) || s.Phase == string(boot.PhaseStopped)) {
			var rows []launch.RowWait
			mounted := map[string]bool{}
			for _, r := range s.Rows {
				rows, mounted[r.ID] = append(rows, launch.RowWait{ID: r.ID, State: kernel.State(r.State), WaitsFor: r.WaitsFor}), true
			}
			if s.Phase == string(boot.PhaseStopped) {
				return rows, fmt.Errorf("the test-only profile did not boot: exit %d: %s", s.Exit, s.Detail)
			}
			var missing []string
			for _, r := range c.Rows {
				if !mounted[r.ID] {
					missing = append(missing, r.ID)
				}
			}
			if len(missing) > 0 {
				return rows, fmt.Errorf("the rows %s of the plugin did not mount in the test-only profile: the server says why", strings.Join(missing, ", "))
			}
			return rows, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("the test-only profile did not boot within %s: %w", smokeBound, ctx.Err())
		case err := <-exited:
			exited <- err
			return nil, fmt.Errorf("the server of the throwaway distribution exited before it ran the test-only profile: %v: %s", err, strings.TrimSpace(out.String()))
		case <-tick.C:
		}
	}
}
