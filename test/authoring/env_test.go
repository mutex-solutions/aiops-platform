// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
)

// registryImage is the registry the exit runs inside the job, pinned by digest, with its
// register entry distribution-registry (ADR-047 decision 4; ADR-060 decision 34).
const registryImage = "registry:3.1.2@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c"

// password is the password of every key the exit makes, in AIOPS_KEY_PASSWORD.
const password = "a throwaway key of the exit"

// machine is one environment of the exit (ADR-060 decision 30): a directory of its own
// outside the repository, with a bin/ that holds only the programs the environment
// holds and is its whole PATH, a home, a temporary directory and caches of its own, and
// the Go environment of ADR-060 decision 21. No variable names the repository, its
// module cache or a replace line, and GOPROXY names only the exit's module proxy. The
// network limit is the one a Go program keeps: every HTTP request goes through a proxy
// that refuses, and Go never sends a request to the loopback interface through one, so
// the registry of the exit, on that interface, is all it reaches; the go command
// reaches the module proxy through file:// and runs no version control.
type machine struct {
	t          *testing.T
	name, root string
	work       string // its working directory
	env        []string
}

// newMachine returns the environment name under root, whose bin/ links tools, by name,
// to the programs of the host; system adds the directories of the host's own programs
// to PATH, a compiler among them, as on an author's computer.
func newMachine(t *testing.T, root, name, proxy string, tools map[string]string, system bool, extra ...string) *machine {
	t.Helper()
	m := &machine{t: t, name: name, root: filepath.Join(root, name)}
	m.work = filepath.Join(m.root, "work")
	for _, d := range []string{"bin", "home", "tmp", "cache", "config", "gopath", "work"} {
		if err := os.MkdirAll(filepath.Join(m.root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { writable(filepath.Join(m.root, "gopath")) })
	for _, n := range slices.Sorted(maps.Keys(tools)) {
		if err := os.Symlink(tools[n], filepath.Join(m.root, "bin", n)); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(m.root, "bin")
	if system {
		path += ":/usr/local/bin:/usr/bin:/bin"
	}
	m.env = append([]string{"PATH=" + path, "HOME=" + filepath.Join(m.root, "home"), "TMPDIR=" + filepath.Join(m.root, "tmp"),
		"XDG_CACHE_HOME=" + filepath.Join(m.root, "cache"), "XDG_CONFIG_HOME=" + filepath.Join(m.root, "config"),
		"GOPATH=" + filepath.Join(m.root, "gopath"), "GOCACHE=" + filepath.Join(m.root, "cache", "go-build"),
		// The Go environment of ADR-060 decision 21: GOPRIVATE and GONOPROXY are unset.
		"GOPROXY=file://" + filepath.ToSlash(proxy), "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly",
		// The network: no request leaves the loopback interface.
		"HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=",
		"DOCKER_CONFIG=" + filepath.Join(m.root, "config", "docker"), "AIOPS_KEY_PASSWORD=" + password}, extra...)
	return m
}

// writable makes the directories under dir writable: the go command writes its module
// cache read-only, and the cleanup of the test removes it after this.
func writable(dir string) {
	_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && e.IsDir() {
			err = os.Chmod(p, 0o755)
		}
		return err
	})
}

// result is what a program printed and its exit code.
type result struct {
	code        int
	out, errOut string
}

// run runs args in dir, the working directory when it is "", with the environment of
// m: args[0] is a program of its bin/ or an absolute path.
func (m *machine) run(dir string, args ...string) result {
	m.t.Helper()
	prog := args[0]
	if !filepath.IsAbs(prog) {
		prog = filepath.Join(m.root, "bin", prog)
		if _, err := os.Stat(prog); err != nil {
			m.t.Fatalf("the %s holds no %s", m.name, args[0])
		}
	}
	cmd := exec.Command(prog, args[1:]...)
	var out, errOut bytes.Buffer
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = cmp(dir, m.work), m.env, &out, &errOut
	err := cmd.Run()
	r := result{out: out.String(), errOut: errOut.String()}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		r.code = exit.ExitCode()
	case err != nil:
		m.t.Fatalf("%s: %v", strings.Join(args, " "), err)
	}
	m.t.Logf("%s$ %s: exit %d\n%s%s", m.name, strings.Join(args, " "), r.code, r.out, r.errOut)
	return r
}

// with returns m with the variables env set over its own.
func (m *machine) with(env ...string) *machine {
	c := *m
	c.env = append(slices.Clone(m.env), env...) // os/exec takes the last value of a variable
	return &c
}

// must runs args and fails unless they exit 0.
func (m *machine) must(dir string, args ...string) result {
	m.t.Helper()
	r := m.run(dir, args...)
	if r.code != 0 {
		m.t.Fatalf("%s: %s: exit %d\n%s%s", m.name, strings.Join(args, " "), r.code, r.out, r.errOut)
	}
	return r
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// docker runs the container runtime of the job and returns its standard output.
func docker(t *testing.T, args ...string) string {
	t.Helper()
	var stderr bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s%s", strings.Join(args, " "), err, out, stderr.Bytes())
	}
	return strings.TrimSpace(string(out))
}

// startRegistry runs the pinned registry image with its port published on the loopback
// interface, and returns its host and port. It removes the container with the volume
// that the image declares, which holds what the exit pushed, and the image when the job
// held none before; a volume left behind fails the test, which then removes it.
func startRegistry(t *testing.T) string {
	t.Helper()
	if exec.Command("docker", "image", "inspect", registryImage).Run() != nil {
		t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", registryImage).Run() })
	}
	var volumes string
	t.Cleanup(func() {
		for _, v := range strings.Fields(volumes) {
			if exec.Command("docker", "volume", "inspect", v).Run() == nil {
				t.Errorf("the registry's container left its volume %s", v)
				_ = exec.Command("docker", "volume", "rm", "-f", v).Run()
			}
		}
	})
	id := docker(t, "run", "-d", "-p", "127.0.0.1::5000", registryImage)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	if volumes = docker(t, "inspect", "-f", "{{range .Mounts}}{{.Name}} {{end}}", id); volumes == "" {
		t.Errorf("the registry's container mounts no volume: the pinned image declares /var/lib/registry one")
	}
	host := docker(t, "port", id, "5000/tcp")
	if i := strings.IndexByte(host, '\n'); i >= 0 {
		host = host[:i]
	}
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(100 * time.Millisecond) {
		if r, err := http.Get("http://" + host + "/v2/"); err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				return host
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the registry %s did not answer within a minute: %s", host, docker(t, "logs", id))
		}
	}
}

// pushLayout copies each manifest of the OCI image layout dir, with what it points to,
// into the repository repo of the registry on the loopback interface, tagged as the
// layout tags it, referrers after their subjects, and returns the index of the layout.
func pushLayout(t *testing.T, dir, repo string) []ocispec.Descriptor {
	t.Helper()
	ctx := context.Background()
	src, err := oci.NewFromFS(ctx, os.DirFS(dir))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := remote.NewRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	dst.PlainHTTP = true
	idx := readIndex(t, dir)
	slices.SortStableFunc(idx, func(a, b ocispec.Descriptor) int {
		return boolInt(manifestOf(t, dir, a).Subject != nil) - boolInt(manifestOf(t, dir, b).Subject != nil)
	})
	for _, d := range idx {
		if err := oras.CopyGraph(ctx, src, dst, d, oras.DefaultCopyGraphOptions); err != nil {
			t.Fatalf("push %s of %s to %s: %v", d.Digest, dir, repo, err)
		}
		if tag := d.Annotations[ocispec.AnnotationRefName]; tag != "" {
			if err := dst.Tag(ctx, d, tag); err != nil {
				t.Fatal(err)
			}
		}
	}
	return idx
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// readIndex returns the manifests that index.json of the layout dir lists.
func readIndex(t *testing.T, dir string) []ocispec.Descriptor {
	t.Helper()
	var idx ocispec.Index
	b, err := os.ReadFile(filepath.Join(dir, ocispec.ImageIndexFile))
	if err == nil {
		err = json.Unmarshal(b, &idx)
	}
	if err != nil {
		t.Fatal(err)
	}
	return idx.Manifests
}

func writeIndex(t *testing.T, dir string, ms []ocispec.Descriptor) {
	t.Helper()
	idx := ocispec.Index{MediaType: ocispec.MediaTypeImageIndex, Manifests: ms}
	idx.SchemaVersion = 2
	b, err := json.Marshal(idx)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, ocispec.ImageIndexFile), b, 0o644)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// blob returns the blob d of the layout dir.
func blob(t *testing.T, dir string, d ocispec.Descriptor) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "blobs", "sha256", d.Digest.Encoded()))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// put writes data into the layout dir as a blob of mediaType and returns its descriptor.
func put(t *testing.T, dir, mediaType string, data []byte) ocispec.Descriptor {
	t.Helper()
	d := content.NewDescriptorFromBytes(mediaType, data)
	if err := os.WriteFile(filepath.Join(dir, "blobs", "sha256", d.Digest.Encoded()), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func manifestOf(t *testing.T, dir string, d ocispec.Descriptor) ocispec.Manifest {
	t.Helper()
	var m ocispec.Manifest
	if err := json.Unmarshal(blob(t, dir, d), &m); err != nil {
		t.Fatalf("%s of %s: %v", d.Digest, dir, err)
	}
	return m
}

// putManifest writes m into the layout dir and returns its descriptor, with the
// artifact type and the annotations that index.json carries.
func putManifest(t *testing.T, dir string, m ocispec.Manifest, annotations map[string]string) ocispec.Descriptor {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	d := put(t, dir, ocispec.MediaTypeImageManifest, b)
	d.ArtifactType, d.Annotations = m.ArtifactType, annotations
	return d
}

// copyDir copies the regular files under from into to.
func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, e fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(from, p)
		switch {
		case err != nil:
			return err
		case e.IsDir():
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err == nil {
			err = os.WriteFile(filepath.Join(to, rel), b, 0o644)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// tree returns the SHA-256 and the mode of each file under each of dirs, by path, but
// under the directories of skip: what a step must leave unchanged.
func tree(t *testing.T, skip []string, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			switch {
			case errors.Is(err, fs.ErrNotExist) && p == dir:
				return nil
			case err != nil:
				return err
			case slices.Contains(skip, p) && e.IsDir():
				return filepath.SkipDir
			case slices.Contains(skip, p):
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			switch {
			case e.IsDir():
				out[p] = info.Mode().String()
			case e.Type()&fs.ModeSocket != 0:
				out[p] = "socket"
			default:
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				out[p] = fmt.Sprintf("%s %x", info.Mode(), sha256.Sum256(b))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// homeKeeps are the paths of a home that a step which writes nothing leaves to others:
// write.lock, in which each command that may write the home records itself while it
// holds the lock (ADR-061 decision 19), and run/, which the kernel writes.
func homeKeeps(home string) []string {
	return []string{filepath.Join(home, "write.lock"), filepath.Join(home, "run")}
}

// contents returns the SHA-256 of each regular file under dir, by its slash path
// relative to dir.
func contents(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || !e.Type().IsRegular() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// changed returns the paths whose entry differs between two trees.
func changed(before, after map[string]string) []string {
	var out []string
	for p, v := range after {
		if before[p] != v {
			out = append(out, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}
