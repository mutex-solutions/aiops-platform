// SPDX-License-Identifier: Apache-2.0

// Package runfile writes the small files that a server process leaves in the
// runtime directory of its home, for an operator or a test to read.
package runfile

import (
	"os"
	"path/filepath"
)

// Dir is the runtime directory of a home, "<home>/run" (ADR-059 decision 9; the
// documents name no such directory for S1 and this is an open point).
func Dir(home string) string { return filepath.Join(home, "run") }

// Write puts data in dir/name atomically: it writes a temporary file in dir and
// renames it, so a reader sees the old file or the whole new one, never part of
// one. It creates dir.
func Write(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(f.Name())
		return werr
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}
