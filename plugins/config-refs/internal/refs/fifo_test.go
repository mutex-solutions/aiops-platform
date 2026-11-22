// SPDX-License-Identifier: Apache-2.0

//go:build unix

package refs_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	refsdef "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
)

func TestFileRefusesAFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	pipe := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := resolveFile(resolver(dir, 64), "pipe"); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, refsdef.ErrUnresolved) || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(10 * time.Second): // a bound on a hang, not a wait for a result
		// A writer lets the blocked open return, so the goroutine ends.
		if w, err := os.OpenFile(pipe, os.O_WRONLY, 0); err == nil {
			_ = w.Close()
		}
		<-done
		t.Fatal("Resolve blocked on a FIFO with no writer")
	}
}
