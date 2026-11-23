// SPDX-License-Identifier: Apache-2.0

// Package testdist is the test distribution: the release module table of dist plus
// the toy plugins, the seven fault plugins and the two test-only control rows, with
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

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/test/dist/def"
	badconfig "github.com/mutex-solutions/aiops-platform/test/dist/plugins/bad-config"
	failapply "github.com/mutex-solutions/aiops-platform/test/dist/plugins/fail-apply"
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
)

// Shipped is what the test binary carries: dist.yaml and limits.yaml and the bundles
// and profiles of the test distribution, so that the binary and its fixtures cannot
// drift. The layer 4 patches in patches/ are not embedded: --patch reads a file by
// path, and the harness names the files of this directory.
//
//go:embed dist.yaml limits.yaml all:bundles all:profiles
var shipped embed.FS

// manifests holds the plugin.yaml of each test plugin, which the table reads
// (ADR-048 decision 18).
//
//go:embed plugins/*/plugin.yaml
var manifests embed.FS

// Modules is the test table: the modules of the release distribution, then the test
// ones, each with the manifest beside its package. The count of fail-apply is per
// plugin value, so each call makes new values.
func Modules() []dist.Module {
	m := func(p kernel.Plugin, dir string) dist.Module {
		return dist.Module{Plugin: p, Manifest: dist.Manifest(manifests, "plugins/"+dir)}
	}
	return append(dist.Modules(),
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
		m(teststate.New(), "teststate"),
		m(testsubmit.New(), "testsubmit"),
	)
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
	dd, err := fs.ReadFile(shipped, "dist.yaml")
	if err != nil {
		return serve.Distribution{}, err
	}
	man, err := boot.ParseDist("dist.yaml", dd)
	if err != nil {
		return serve.Distribution{}, fmt.Errorf("testdist: %w", err)
	}
	ld, err := fs.ReadFile(shipped, "limits.yaml")
	if err != nil {
		return serve.Distribution{}, err
	}
	lim, err := boot.ParseLimits("limits.yaml", ld)
	if err != nil {
		return serve.Distribution{}, fmt.Errorf("testdist: %w", err)
	}
	return serve.Distribution{Registry: reg, Manifest: man, Limits: lim, Shipped: shipped, Kernel: teststate.RecordKernel}, nil
}
