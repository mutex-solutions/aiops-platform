// SPDX-License-Identifier: Apache-2.0

package authoring

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// Module reads the module of in-process code at the root q.Dir for plugin pack (ADR-047
// decision 1): the path of its main module and the h1 sum of the module zip of q.Dir at
// q.Version, as go mod download records it, and the rest of its build list, each
// module with its path, its version with its replacement and its sum, as go list -m
// -json all reads them with the author's toolchain. The toolchain writes no go.mod or
// go.sum here (-mod=readonly). The packages of the module zip are linked here, in the
// toolbox, and enter no server binary (ADR-044, Consequences).
func (Tools) Module(ctx context.Context, q launch.ModuleRequest) (launch.Module, error) {
	if _, err := os.Stat(filepath.Join(q.Dir, "go.mod")); err != nil {
		return launch.Module{}, fmt.Errorf("in-process code is packed from the root of its module: %v", err)
	}
	type listed struct {
		Path, Version, Sum string
		Main               bool
		Replace            *listed
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "go", "list", "-mod=readonly", "-m", "-json", "all")
	cmd.Dir, cmd.Env, cmd.Stderr = q.Dir, append(os.Environ(), "GOWORK=off"), &stderr
	out, err := cmd.Output()
	if err != nil {
		return launch.Module{}, fmt.Errorf("go list -m -json all in %s, the build list of its SBOM: %v: %s", q.Dir, err, bytes.TrimSpace(stderr.Bytes()))
	}
	var mods []listed
	for d := json.NewDecoder(bytes.NewReader(out)); d.More(); {
		var l listed
		if err := d.Decode(&l); err != nil {
			return launch.Module{}, err
		}
		mods = append(mods, l)
	}
	if len(mods) == 0 || !mods[0].Main {
		return launch.Module{}, fmt.Errorf("go list -m -json all in %s names no main module first", q.Dir)
	}
	m := module.Version{Path: mods[0].Path, Version: q.Version}
	var z bytes.Buffer
	if err := modzip.CreateFromDir(&z, m, q.Dir); err != nil {
		return launch.Module{}, fmt.Errorf("the module zip of %s: %v", q.Dir, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(z.Bytes()), int64(z.Len()))
	if err != nil {
		return launch.Module{}, err
	}
	files := map[string]*zip.File{}
	for _, zf := range zr.File {
		files[zf.Name] = zf
	}
	sum, err := dirhash.Hash1(slices.Collect(maps.Keys(files)), func(name string) (io.ReadCloser, error) { return files[name].Open() })
	if err != nil {
		return launch.Module{}, err
	}
	r := launch.Module{Path: m.Path, Version: m.Version, Sum: sum}
	for _, l := range mods[1:] {
		if p := l.Replace; p != nil {
			l.Version, l.Sum = strings.TrimSpace(l.Version+" => "+p.Path+" "+p.Version), p.Sum
		}
		r.Deps = append(r.Deps, launch.Module{Path: l.Path, Version: l.Version, Sum: l.Sum})
	}
	return r, nil
}
