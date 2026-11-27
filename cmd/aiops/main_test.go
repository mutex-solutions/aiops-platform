// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// The server binary embeds the release's shipped bundles and profiles as
// serve.Distribution.Shipped (ADR-060 decision 27). On a home that trusts its signed
// manifest and holds no profile, --profile dev boots the shipped dev: each row of
// aiops-core is ACTIVE, reload-socket included, which listens on the socket of the home
// (ADR-062 decision 2). A file of the home wins over the shipped one: a dev of the home
// that lists a bundle nothing ships is refused, naming it.
func TestBootsTheShippedDevProfile(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "aiops")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build of cmd/aiops: %v\n%s", err, out)
	}
	_, signed, _, err := dist.Load()
	if err != nil {
		t.Fatal(err)
	}
	home := func(files map[string][]byte) string {
		h := t.TempDir()
		for name, data := range trusted.Files(t, signed) {
			files[name] = data
		}
		for name, data := range files {
			p := filepath.Join(h, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return h
	}

	h := home(map[string][]byte{})
	log, err := os.Create(filepath.Join(t.TempDir(), "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(bin, "--profile", "dev", "--home", h)
	cmd.Env, cmd.Stdout, cmd.Stderr = []string{"HOME=" + h}, log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	output := func() string { b, _ := os.ReadFile(log.Name()); return string(b) }
	var st serve.Status
	active := func() bool {
		return len(st.Rows) > 0 && !slices.ContainsFunc(st.Rows, func(r serve.StatusRow) bool { return r.State != "ACTIVE" })
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for deadline := time.After(time.Minute); st.Phase != "stopped" && !(st.Phase == "running" && active()); {
		select {
		case <-deadline:
			t.Fatalf("dev did not settle within a minute: %+v\n%s", st, output())
		case <-tick.C:
		}
		if b, err := os.ReadFile(filepath.Join(runfile.Dir(h), serve.StatusFile)); err == nil {
			_ = json.Unmarshal(b, &st)
		}
	}
	_, serr := os.Stat(filepath.Join(runfile.Dir(h), "reload.sock"))
	_ = cmd.Process.Signal(syscall.SIGTERM)
	werr := cmd.Wait()
	var ids []string
	for _, r := range st.Rows {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	if want := []string{"clock", "config-refs", "loader", "readiness", "reload", "reload-socket", "restart-policy"}; st.Phase != "running" || !active() || !slices.Equal(ids, want) {
		t.Fatalf("dev on a home with no profile: %+v; want each of %v ACTIVE\n%s", st, want, output())
	}
	if serr != nil || werr != nil {
		t.Errorf("the socket of reload-socket: %v; the stop: %v\n%s", serr, werr, output())
	}

	h = home(map[string][]byte{"profiles/dev/profile.yaml": []byte("bundles: [aiops-core, unshipped]\n")})
	out, err := exec.Command(bin, "--profile", "dev", "--home", h).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != boot.ExitRefused || !strings.Contains(string(out), "unshipped") {
		t.Errorf("dev of the home that lists a bundle nothing ships: %v; want exit %d naming it\n%s", err, boot.ExitRefused, out)
	}
}
