// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
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

// machine is one environment of the exit (ADR-060 decision 30; the owner's answer of
// 2026-11-26): containers of an image that holds the environment's programs, one per
// command, run as the test's user in the network namespace of a container on a network
// of its own (networks), with, where the environment has one, a container runtime of
// its own in that namespace (startRuntime). Its directory, outside the repository, with a home, a
// temporary directory and caches of its own, is bound into them read-write at its own
// path, and each other path it reads is bound read-only at its own path, so that a path
// names the same file inside and outside them. It has the Go environment of ADR-060
// decision 21: no variable names the repository, its module cache or a replace line,
// GOPROXY names only the exit's module proxy, at http://proxy unless extra sets it, and
// the go command runs no version control.
type machine struct {
	t            *testing.T
	name, root   string
	work         string   // its working directory
	image, netns string   // the image of its containers, and the container whose network namespace they join
	opts, env    []string // the options of docker run of its containers, and their variables
	runtime      bool     // its containers have a container runtime of its own
}

// newMachine returns the environment name under root, whose containers run image in the
// network namespace of netns, with the options opts, path as their PATH and the
// variables extra over the others.
func newMachine(t *testing.T, root, name, image, netns, path string, opts []string, extra ...string) *machine {
	t.Helper()
	m := &machine{t: t, name: name, root: filepath.Join(root, name), image: image, netns: netns}
	m.work = filepath.Join(m.root, "work")
	for _, d := range []string{"home", "tmp", "cache", "config", "gopath", "work"} {
		if err := os.MkdirAll(filepath.Join(m.root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { writable(filepath.Join(m.root, "gopath")) })
	m.opts = append([]string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--mount", "type=bind,src=" + m.root + ",dst=" + m.root}, opts...)
	m.env = append([]string{"PATH=" + path, "HOME=" + filepath.Join(m.root, "home"), "TMPDIR=" + filepath.Join(m.root, "tmp"),
		"XDG_CACHE_HOME=" + filepath.Join(m.root, "cache"), "XDG_CONFIG_HOME=" + filepath.Join(m.root, "config"),
		"GOPATH=" + filepath.Join(m.root, "gopath"), "GOCACHE=" + filepath.Join(m.root, "cache", "go-build"),
		// The Go environment of ADR-060 decision 21: GOPRIVATE and GONOPROXY are unset.
		"GOPROXY=http://proxy", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly",
		"DOCKER_CONFIG=" + filepath.Join(m.root, "config", "docker"), "AIOPS_KEY_PASSWORD=" + password}, extra...)
	return m
}

// bound returns the options of docker run that bind each of paths of the host,
// read-only, at its own path.
func bound(paths ...string) []string {
	var opts []string
	for _, p := range paths {
		opts = append(opts, "--mount", "type=bind,readonly,src="+p+",dst="+p)
	}
	return opts
}

// runtimePrograms are the programs of the job's container runtime that the daemon of an
// environment runs.
var runtimePrograms = []string{"dockerd", "containerd", "containerd-shim-runc-v2", "runc"}

// programs adds to files, at bin/<name> of an image, each program of the host named,
// and each shared library it loads at its own path, and returns files.
func programs(t *testing.T, files map[string]string, names ...string) map[string]string {
	t.Helper()
	for _, n := range names {
		p, err := exec.LookPath(n)
		if err != nil {
			t.Fatal(err)
		}
		files["bin/"+n] = p
		libs, _ := exec.Command("ldd", p).Output() // a static program loads none
		for _, f := range strings.Fields(string(libs)) {
			if filepath.IsAbs(f) {
				files[f[1:]] = f
			}
		}
	}
	return files
}

// startRuntime gives the environment a container runtime of its own (the owner's answer
// of 2026-11-26): the daemon of the job's container runtime, run from the image daemon,
// which holds its programs and exitnet, in a privileged container in the environment's
// network namespace, so that what it pulls and the containers it runs reach what the
// environment reaches and nothing else. It keeps its images in a volume that is removed
// with it. The environment's directory is bound into it read-write and each of paths
// read-only, each at its own path, so that a path that a command of the environment
// binds into a container names the same file. It runs no bridge, so a container on its
// default network has no network. It listens on a socket in the environment's directory
// that the test's group owns, which DOCKER_HOST names in the environment's containers.
func (m *machine) startRuntime(daemon string, paths ...string) {
	t := m.t
	t.Helper()
	sock := filepath.Join(m.root, "runtime", "docker.sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"run", "-d", "--privileged", "--network", "container:" + m.netns, "-e", "PATH=/bin",
		"--mount", "type=volume,dst=/var/lib/docker", "--mount", "type=bind,src=" + m.root + ",dst=" + m.root}, bound(paths...)...)
	id := docker(t, append(args, daemon, "/exitnet", "daemon", "dockerd", "--host", "unix://"+sock, "--group", strconv.Itoa(os.Getgid()),
		"--bridge", "none", "--iptables=false", "--ip6tables=false", "--userland-proxy=false")...)
	removed(t, id)
	for deadline := time.Now().Add(time.Minute); exec.Command("docker", "--host", "unix://"+sock, "version").Run() != nil; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			out, _ := exec.Command("docker", "logs", id).CombinedOutput()
			t.Fatalf("the container runtime of the %s did not answer within a minute:\n%s", m.name, out)
		}
	}
	m.env, m.runtime = append(m.env, "DOCKER_HOST=unix://"+sock), true
}

// removed removes the container id with its volumes when the test ends, and returns
// the names of its volumes; a volume left behind fails the test, which then removes it.
func removed(t *testing.T, id string) []string {
	t.Helper()
	var volumes []string
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", "-v", id).Run()
		for _, v := range volumes {
			if exec.Command("docker", "volume", "inspect", v).Run() == nil {
				t.Errorf("the container %s left its volume %s", id, v)
				_ = exec.Command("docker", "volume", "rm", "-f", v).Run()
			}
		}
	})
	volumes = strings.Fields(docker(t, "inspect", "-f", "{{range .Mounts}}{{.Name}} {{end}}", id))
	return volumes
}

// image imports into the container runtime an image of one layer on no base layer that
// holds files: at each path of the image, the file of the host that it names.
func (x *exit) image(files map[string]string) string {
	t := x.t
	t.Helper()
	f, err := os.CreateTemp(x.root, "layer-*.tar")
	if err != nil {
		t.Fatal(err)
	}
	w := tar.NewWriter(f)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		b, err := os.ReadFile(files[name])
		if err == nil {
			err = w.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o755, Size: int64(len(b))})
		}
		if err == nil {
			_, err = w.Write(b)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(w.Close(), f.Close()); err != nil {
		t.Fatal(err)
	}
	id := docker(t, "import", f.Name())
	x.images = append(x.images, id)
	return id
}

// exitnet builds the program of testdata/exitnet, static, into the directory of the exit.
func (x *exit) exitnet() string {
	out := filepath.Join(x.root, "exitnet", "exitnet")
	cmd := exec.Command("go", "build", "-o", out, "./testdata/exitnet")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=readonly")
	if b, err := cmd.CombinedOutput(); err != nil {
		x.t.Fatalf("go build of testdata/exitnet: %v\n%s", err, b)
	}
	return out
}

// networks makes the network of each environment and returns, by name, the container
// whose network namespace its containers join (ADR-060 decision 21; the owner's answer
// of 2026-11-26). Each is an internal network whose bridge has no address on the host,
// so that it routes to no address outside it, the host's among them. The registry is on
// each. The module proxy, the image netd serving the module proxy's directory at
// http://proxy, is on the author's repository's alone, whose go command resolves
// modules through it: the pack author's machine reaches the registry alone (ADR-060
// decision 30), and the clean machine's only module reader is the builder, whose module
// source is that directory until L14 clears (ADR-044 decision 7). The container of each
// network forwards, on its loopback interface, the registry's host and port to the
// registry, so that the registry has inside each environment the name it has on the
// host, where the container runtime pulls from it, and aiopsctl speaks plain HTTP to it
// there as it does on the host. Each network is removed with the test.
func (x *exit) networks(registry, netd string) map[string]string {
	t := x.t
	netns := map[string]string{}
	for _, name := range []string{"author", "packer", "clean"} {
		n := fmt.Sprintf("aiops-exit-%d-%s", time.Now().UnixNano(), name)
		docker(t, "network", "create", "--internal", "-o", "com.docker.network.bridge.inhibit_ipv4=true", n)
		t.Cleanup(func() {
			_ = exec.Command("docker", "network", "disconnect", "-f", n, registry).Run()
			_ = exec.Command("docker", "network", "rm", n).Run()
		})
		docker(t, "network", "connect", "--alias", "registry", n, registry)
		reach := []string{x.reg}
		if name == "author" {
			proxy := docker(t, append(append([]string{"run", "-d", "--network", n, "--network-alias", "proxy"}, bound(x.proxy)...), netd, "/exitnet", "serve", ":80", x.proxy)...)
			t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", proxy).Run() })
			reach = append(reach, "proxy:80")
		}
		id := docker(t, "run", "-d", "--network", n, netd, "/exitnet", "forward", x.reg, "registry:5000")
		t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
		netns[name] = id
		// Its containers start once what it reaches answers.
		for deadline := time.Now().Add(time.Minute); ; time.Sleep(100 * time.Millisecond) {
			got := dial(t, id, reach...)
			if !slices.ContainsFunc(reach, func(a string) bool { return got[a] != "connected" }) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the network of the %s reaches no %v within a minute: %v", name, reach, got)
			}
		}
	}
	return netns
}

// dial dials each of addrs from the network namespace of the container netns and
// returns, by address, "connected" or why it did not connect.
func dial(t *testing.T, netns string, addrs ...string) map[string]string {
	t.Helper()
	return dialed(docker(t, append([]string{"exec", netns, "/exitnet", "dial"}, addrs...)...))
}

// dialed reads what exitnet dial printed.
func dialed(out string) map[string]string {
	got := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		a, r, _ := strings.Cut(l, " ")
		got[a] = r
	}
	return got
}

// dial dials each of addrs from the environment.
func (m *machine) dial(addrs ...string) map[string]string {
	m.t.Helper()
	return dial(m.t, m.netns, addrs...)
}

// load loads the image of the container runtime of the job into the container runtime
// of the environment, through a file in its directory, and returns its id there.
func (m *machine) load(image string) string {
	m.t.Helper()
	f := filepath.Join(m.root, "tmp", "load.tar")
	docker(m.t, "save", "-o", f, image)
	id, ok := strings.CutPrefix(strings.TrimSpace(m.must("", "docker", "load", "-i", f).out), "Loaded image ID: ")
	if !ok {
		m.t.Fatalf("docker load of the %s names no image id", m.name)
	}
	return id
}

// dialFrom dials each of addrs from a container of image that the environment's
// container runtime runs on network, or on its default network when network is "".
func (m *machine) dialFrom(image, network string, addrs ...string) map[string]string {
	m.t.Helper()
	args := []string{"docker", "run", "--rm"}
	if network != "" {
		args = append(args, "--network", network)
	}
	return dialed(m.must("", append(append(args, image, "/exitnet", "dial"), addrs...)...).out)
}

// holds returns the paths of the files of a container of the environment's image that
// hold a byte, and of its links but those into /proc that the container runtime makes.
func (m *machine) holds() []string {
	m.t.Helper()
	id := docker(m.t, "create", m.image, "aiopsctl")
	defer exec.Command("docker", "rm", id).Run()
	cmd := exec.Command("docker", "export", id)
	pipe, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		m.t.Fatal(err)
	}
	var out []string
	for r := tar.NewReader(pipe); ; {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			m.t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg && h.Size > 0 || h.Typeflag == tar.TypeSymlink && !strings.HasPrefix(h.Linkname, "/proc/") {
			out = append(out, h.Name)
		}
	}
	if err := cmd.Wait(); err != nil {
		m.t.Fatal(err)
	}
	return out
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

// run runs args in dir, the working directory when it is "", in a container of m:
// args[0] is a program on its PATH or an absolute path.
func (m *machine) run(dir string, args ...string) result {
	m.t.Helper()
	a := append([]string{"run", "--rm", "--network", "container:" + m.netns, "-w", cmp(dir, m.work)}, m.opts...)
	for _, e := range m.env {
		a = append(a, "-e", e)
	}
	cmd := exec.Command("docker", append(append(a, m.image), args...)...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	r := result{out: out.String(), errOut: errOut.String()}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && exit.ExitCode() < 125:
		r.code = exit.ExitCode()
	case err != nil: // docker run did not start the program: the image holds none of that name, or the runtime failed
		m.t.Fatalf("the %s: %s: %v\n%s", m.name, strings.Join(args, " "), err, r.errOut)
	}
	m.t.Logf("%s$ %s: exit %d\n%s%s", m.name, strings.Join(args, " "), r.code, r.out, r.errOut)
	return r
}

// with returns m with the variables env set over its own.
func (m *machine) with(env ...string) *machine {
	c := *m
	c.env = append(slices.Clone(m.env), env...) // docker run takes the last value of a variable
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
// interface, and returns its host and port and its container, which networks attaches to
// the network of each environment. It removes the container with the volume that the
// image declares, which holds what the exit pushed, and the image when the job held none
// before; a volume left behind fails the test, which then removes it.
func startRegistry(t *testing.T) (string, string) {
	t.Helper()
	if exec.Command("docker", "image", "inspect", registryImage).Run() != nil {
		t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", registryImage).Run() })
	}
	id := docker(t, "run", "-d", "-p", "127.0.0.1::5000", registryImage)
	if len(removed(t, id)) == 0 {
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
				return host, id
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
