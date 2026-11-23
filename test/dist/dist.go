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

// Modules is the test table: the modules of the release distribution, then the test
// ones. The count of FailApply is per plugin value, so each call makes new values.
func Modules() []dist.Module {
	return append(dist.Modules(),
		dist.Module{Plugin: toyprovider.New()},
		dist.Module{Plugin: toyconsumer.New(), Volatile: []string{"config.note"}},
		dist.Module{Plugin: toyspine.New()},
		dist.Module{Plugin: failapply.New()},
		dist.Module{Plugin: panicapply.New()},
		dist.Module{Plugin: slowapply.New()},
		dist.Module{Plugin: badconfig.New()},
		dist.Module{Plugin: rogueprovider.New()},
		dist.Module{Plugin: leakygoroutine.New()},
		dist.Module{Plugin: slowdispose.New()},
		dist.Module{Plugin: teststate.New()},
		dist.Module{Plugin: testsubmit.New()},
	)
}

// Distribution returns what the test binary links.
func Distribution() (serve.Distribution, error) {
	reg := dist.Table(Modules()...)
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
