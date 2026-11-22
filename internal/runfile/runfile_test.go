// SPDX-License-Identifier: Apache-2.0

package runfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesAtomically(t *testing.T) {
	dir := filepath.Join(Dir(t.TempDir()), "sub")
	for _, want := range []string{"one", "two"} {
		if err := Write(dir, "f.json", []byte(want)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "f.json"))
		if err != nil || string(got) != want {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("a temporary file is left: %v", entries)
	}
}

func TestWriteErrors(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if Write(file, "f", nil) == nil { // dir is a file
		t.Fatal("no error for a dir that is a file")
	}
	ro := filepath.Join(root, "ro")
	if err := os.Mkdir(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if os.Getuid() != 0 && Write(ro, "f", nil) == nil { // cannot create the temporary file
		t.Fatal("no error for a read-only dir")
	}
	if Write(root, "d/x", []byte("x")) == nil { // the target's directory does not exist: the rename fails
		t.Fatal("no error for a name with a missing directory")
	}
}
