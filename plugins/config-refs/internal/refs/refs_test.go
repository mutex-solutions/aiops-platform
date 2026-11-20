// SPDX-License-Identifier: Apache-2.0

package refs_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/plugins/config-refs/internal/refs"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// resolver reads ${file:} from dir, which the flag refs-dir holds. The other cases of
// the package are tested through the row, in plugins/config-refs.
func resolver(dir string, maxBytes int64) *refs.Resolver {
	return &refs.Resolver{
		Flag:     func(n string) (string, bool) { return dir, n == "refs-dir" },
		RootFlag: "refs-dir",
		MaxBytes: maxBytes,
	}
}

func resolveFile(r *refs.Resolver, name string) (any, error) {
	v, _, err := r.Resolve(launch.Ref{Scheme: "file", Name: name})
	return v, err
}

func TestFileLimitOfMaxInt64ReadsTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("tok-123"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, err := resolveFile(resolver(dir, math.MaxInt64), "token"); err != nil || v != "tok-123" {
		t.Fatalf("value %q, err %v", v, err)
	}
}

// A relative root would be read from the working directory of the process.
func TestFileRootMustBeAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("d", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("d", "x"), []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveFile(resolver("d", 8), "x"); !errors.Is(err, refs.ErrUnresolved) || !strings.Contains(err.Error(), "not an absolute path") {
		t.Fatalf("err = %v", err)
	}
}
