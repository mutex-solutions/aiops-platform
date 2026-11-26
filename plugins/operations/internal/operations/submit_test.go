// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
)

// accepting runs a row on the socket of h that takes one request, answers that it
// queued it as operation 3, calls then, and keeps the connection open.
func accepting(t *testing.T, h Home, then func()) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(h.Dir, "run", "reload.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { _ = ln.Close(); <-done })
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadBytes('\n')
		w := reload.WireResult{ID: 3, Source: reload.SourcePrefix + reload.CommandDisable, Status: reload.Queued}
		_, _ = conn.Write(reload.Line(reload.Reply{V: reload.WireVersion, Result: &w}))
		then()
		_, _ = conn.Read(make([]byte, 1)) // until the client closes
	}()
}

// A command interrupted after the row accepted its operation keeps the files: the
// process goes on to apply that generation, so a restore would leave the files and the
// running tree apart. It ends with verdict-unknown, naming the operation and its last
// status (ADR-062 decision 11).
func TestAnInterruptAfterAcceptanceKeepsTheFiles(t *testing.T) {
	rows := []launch.Row{{ID: "echo", Plugin: "acme/echo", Enabled: true}}
	l := Library{Box: rowsBox{installBox{&packBox{}}, launch.Rendered{Composition: launch.Composition{Rows: rows}}}, LockWait: time.Second}
	h := Home{Dir: filepath.Join(t.TempDir(), "home")}
	put(t, h, "profiles/dev/profile.yaml", "bundles: [aiops-core]\n")
	put(t, h, "run/status.json", `{"phase": "running", "profile": "dev"}`)
	ctx, cancel := context.WithCancel(t.Context())
	accepting(t, h, cancel)
	ch, err := l.Disable(ctx, def.RowRequest{Home: h.Dir, Profile: "dev", Row: "echo", ConnectWait: time.Second})
	if !errors.Is(err, def.ErrSubmission) || !strings.Contains(err.Error(), def.VerdictUnknown) || len(ch.IDs) != 1 || ch.Last != reload.Queued {
		t.Errorf("an interrupted disable: %+v, %v; want ErrSubmission of %s with operation 3, queued", ch, err, def.VerdictUnknown)
	}
	if got, _ := os.ReadFile(filepath.Join(h.Dir, "profiles", "dev", "aiops.patch.yml")); !strings.Contains(string(got), "{id: echo, enabled: false}") {
		t.Errorf("the profile layer after the interrupt: %q; want the departure kept", got)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "snapshot")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the snapshot stays: %v", err)
	}
}
