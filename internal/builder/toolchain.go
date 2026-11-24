// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

// Tree returns the GOROOT of the pinned toolchain module for the local path (ADR-044
// decision 16): the module zip that the module source modcache serves, in the
// download layout of the module proxy protocol, checked against sum, the hash the
// signed manifest records, then extracted under cache, the builder's own cache, with
// its commands executable. An extraction whose recorded sum is sum and whose files
// still hash to it is used again, with no module source: the builder image ships the
// tree so (ADR-044 decision 4); one that does not is refused, and extracted again from
// the module source (decision 14). The host's own Go, if any, is never run. The pin is
// for linux-amd64, the toolchain the builder image ships, so another host is refused
// until its module is read.
func Tree(modcache, cache, sum string) (string, error) {
	if platform := runtime.GOOS + "-" + runtime.GOARCH; !strings.HasSuffix(ToolchainVersion, "."+platform) {
		return "", fmt.Errorf("the release pins the toolchain %s; no toolchain module for %s is read yet (ADR-044 decision 4)", ToolchainVersion, platform)
	}
	m := module.Version{Path: Toolchain, Version: ToolchainVersion}
	if sum == "" {
		return "", fmt.Errorf("the signed manifest records no sum of %s %s", m.Path, m.Version)
	}
	dir := filepath.Join(cache, "toolchain", m.Version)
	if b, err := os.ReadFile(dir + ".sum"); err == nil && string(b) == sum {
		if h, err := dirhash.HashDir(dir, m.String(), dirhash.Hash1); err == nil && h == sum {
			return dir, nil
		}
	}
	esc, _ := module.EscapePath(m.Path)
	zip := filepath.Join(modcache, esc, "@v", m.Version+".zip")
	got, err := dirhash.HashZip(zip, dirhash.Hash1)
	if err != nil {
		return "", fmt.Errorf("the toolchain module: %w", err)
	}
	if got != sum {
		return "", fmt.Errorf("the module source serves %s %s with the sum %s, and the signed manifest records %s: refused (ADR-044 decision 16)", m.Path, m.Version, got, sum)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := modzip.Unzip(dir, m, zip); err != nil {
		return "", fmt.Errorf("extract the toolchain module: %w", err)
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		mode := fs.FileMode(0o444)
		if rel, _ := filepath.Rel(dir, p); strings.HasPrefix(rel, "bin"+string(filepath.Separator)) || strings.HasPrefix(rel, filepath.Join("pkg", "tool")+string(filepath.Separator)) {
			mode = 0o555
		}
		return os.Chmod(p, mode)
	})
	if err != nil {
		return "", errors.Join(err, os.RemoveAll(dir))
	}
	return dir, os.WriteFile(dir+".sum", []byte(sum), 0o644)
}
