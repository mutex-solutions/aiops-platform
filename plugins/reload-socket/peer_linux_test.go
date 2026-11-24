// SPDX-License-Identifier: Apache-2.0

package reloadsocket

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
)

// The peer check across users needs passwordless sudo, so make check does not run it;
// make peer-check does, in the test job of CI (ADR-062 decision 18). With
// AIOPS_PEER_SUDO=1 every failure to run a case fails the test: it never skips.
const (
	peerSudo   = "AIOPS_PEER_SUDO"
	peerSocket = "AIOPS_PEER_SOCKET" // the socket the client dials
	peerWant   = "AIOPS_PEER_WANT"   // "peer": refused with peer; "denied": cannot open it
)

// A client run as root enters the directory and is refused with peer; a client run as
// another unprivileged user cannot open the socket.
func TestPeerCredentialsAcrossUsers(t *testing.T) {
	if os.Getenv(peerSudo) != "1" {
		t.Skip("needs passwordless sudo; make peer-check runs it with " + peerSudo + "=1")
	}
	if os.Geteuid() == 0 {
		t.Fatal("runs as root, so a root client would be the site user; run it as an unprivileged user")
	}
	w := newWorld(t)
	w.serve(short)
	bin := sharedCopy(t)
	sock := filepath.Join(w.dir, reload.SocketFile)
	for _, c := range []struct {
		want string
		sudo []string
	}{
		{"peer", []string{"-n"}},
		{"denied", []string{"-n", "-u", "nobody"}},
	} {
		args := append(c.sudo, "env", peerSocket+"="+sock, peerWant+"="+c.want, bin, "-test.run=^TestPeerClient$", "-test.count=1", "-test.v")
		cmd := exec.CommandContext(t.Context(), "sudo", args...)
		cmd.Dir = filepath.Dir(bin)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), "--- PASS: TestPeerClient") {
			t.Errorf("sudo %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	if subs := w.rl.Submissions(); len(subs) != 0 {
		t.Errorf("a refused peer submitted %v", subs)
	}
}

// sharedCopy copies the test binary into a directory that every user may enter and
// run it from.
func sharedCopy(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "reload-socket-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	bin := filepath.Join(dir, "reload-socket.test")
	dst, err := os.OpenFile(bin, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestPeerClient is the client that TestPeerCredentialsAcrossUsers runs as another
// user.
func TestPeerClient(t *testing.T) {
	sock := os.Getenv(peerSocket)
	if sock == "" {
		t.Skip("the client of TestPeerCredentialsAcrossUsers")
	}
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: sock, Net: "unix"})
	switch want := os.Getenv(peerWant); {
	case want == "denied":
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("uid %d dialled %s: %v; want permission denied", os.Geteuid(), sock, err)
		}
	case want == "peer" && err == nil:
		defer conn.Close()
		c := &client{t: t, conn: conn, r: bufio.NewReader(conn)}
		c.refused(reload.CausePeer, "the peer runs as uid 0")
	default:
		t.Fatalf("uid %d, want %q: %v", os.Geteuid(), want, err)
	}
}
