// SPDX-License-Identifier: Apache-2.0

// Package testdist is the test distribution: the release module table of dist plus
// the toy plugins, the ten fault plugins, the two test-only control rows and the toy
// plugins of the S2a test release under examples/ (ADR-060 decision 38), with
// the bundles, profiles and layer 4 patches that the loader smoke test and the
// failure matrix run on. test/dist/cmd/aiops-test is its binary.
//
// It is the only place that links a test plugin into a server. The production binary
// (cmd/aiops) links dist and never this package, and no shipped distribution holds
// the control rows (ADR-059 decision 9; 07-c4-plugin-sdk.md:47, :564; 08-profiles-
// roadmap-adrs.md:1238). The tests of test/loader-smoke build and run the binary.
package testdist

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"testing/fstest"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/examples"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	badconfig "github.com/mutex-solutions/aiops-platform/test/dist/plugins/bad-config"
	failapply "github.com/mutex-solutions/aiops-platform/test/dist/plugins/fail-apply"
	foreignattestation "github.com/mutex-solutions/aiops-platform/test/dist/plugins/foreign-attestation"
	leakygoroutine "github.com/mutex-solutions/aiops-platform/test/dist/plugins/leaky-goroutine"
	panicapply "github.com/mutex-solutions/aiops-platform/test/dist/plugins/panic-apply"
	rogueprovider "github.com/mutex-solutions/aiops-platform/test/dist/plugins/rogue-provider"
	slowapply "github.com/mutex-solutions/aiops-platform/test/dist/plugins/slow-apply"
	slowdispose "github.com/mutex-solutions/aiops-platform/test/dist/plugins/slow-dispose"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/testsubmit"
	toyconsumer "github.com/mutex-solutions/aiops-platform/test/dist/plugins/toy-consumer"
	toyprovider "github.com/mutex-solutions/aiops-platform/test/dist/plugins/toy-provider"
	toyspine "github.com/mutex-solutions/aiops-platform/test/dist/plugins/toy-spine"
	unservedversion "github.com/mutex-solutions/aiops-platform/test/dist/plugins/unserved-version"
	unsignedrow "github.com/mutex-solutions/aiops-platform/test/dist/plugins/unsigned-row"
)

// shipped is what the test binary carries: limits.yaml and the bundles and profiles of
// the test distribution, so that the binary and its fixtures cannot drift; Distribution
// ships them over the release's, as the test release does (ADR-060 decision 27). The
// layer 4 patches in patches/ are not embedded: --patch reads a file by path, and the
// harness names the files of this directory.
//
//go:embed limits.yaml all:bundles all:profiles
var shipped embed.FS

// Signed is the signed distribution manifest that the test binary embeds:
// go run ./tools/plugingen --write . writes it from dist.yaml, the bindings and the
// required set of the test distribution, and from the binary's package graph (ADR-060
// decisions 20 and 28). The suites trust it with a throwaway key (test/trusted).
//
//go:embed distribution.yaml
var Signed []byte

// manifests holds the plugin.yaml of each test plugin, which the table reads
// (ADR-048 decision 18).
//
//go:embed plugins/*/plugin.yaml
var manifests embed.FS

// Modules is the test table: the modules of the release distribution, then the test
// ones, each with the manifest beside its package, then the toy plugins of examples/,
// whose rows an installed pack instantiates. The count of fail-apply is per plugin
// value, so each call makes new values.
func Modules() []dist.Module {
	m := func(p kernel.Plugin, dir string) dist.Module {
		return dist.Module{Plugin: p, Manifest: dist.Manifest(manifests, "plugins/"+dir)}
	}
	mods := append(dist.Modules(),
		m(toyprovider.New(), "toy-provider"),
		m(toyconsumer.New(), "toy-consumer"),
		m(toyspine.New(), "toy-spine"),
		m(failapply.New(), "fail-apply"),
		m(panicapply.New(), "panic-apply"),
		m(slowapply.New(), "slow-apply"),
		m(badconfig.New(), "bad-config"),
		m(rogueprovider.New(), "rogue-provider"),
		m(leakygoroutine.New(), "leaky-goroutine"),
		m(slowdispose.New(), "slow-dispose"),
		m(unservedversion.New(), "unserved-version"),
		m(unsignedrow.New(), "unsigned-row"),
		m(foreignattestation.New(), "foreign-attestation"),
		m(teststate.New(), "teststate"),
		m(testsubmit.New(), "testsubmit"),
	)
	for _, p := range examples.Plugins() {
		_, name, _ := strings.Cut(p.ID, "/example-")
		mods = append(mods, dist.Module{Plugin: p, Manifest: dist.Manifest(examples.Manifests, name)})
	}
	return mods
}

// Distribution returns what the test binary links.
func Distribution() (serve.Distribution, error) {
	reg, err := dist.Table(Modules()...)
	if err != nil {
		return serve.Distribution{}, fmt.Errorf("testdist: %w", err)
	}
	reg.Keys = map[string]kernel.AnyKey{}
	for _, k := range def.Keys() {
		reg.Keys[k.Name()] = k
	}
	ld, err := fs.ReadFile(shipped, "limits.yaml")
	if err != nil {
		return serve.Distribution{}, err
	}
	lim, err := boot.ParseLimits("limits.yaml", ld)
	if err != nil {
		return serve.Distribution{}, fmt.Errorf("testdist: %w", err)
	}
	// One file system whose walk lists both, as the launcher's toolbox walks it.
	files := fstest.MapFS{}
	for _, fsys := range []fs.FS{dist.Shipped(), shipped} {
		err := fs.WalkDir(fsys, ".", func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				f := &fstest.MapFile{}
				f.Data, err = fs.ReadFile(fsys, p)
				files[p] = f
			}
			return err
		})
		if err != nil {
			return serve.Distribution{}, err
		}
	}
	return serve.Distribution{Registry: reg, Signed: Signed, Limits: lim, Shipped: files, Kernel: teststate.RecordKernel}, nil
}
