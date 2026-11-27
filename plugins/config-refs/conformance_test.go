// SPDX-License-Identifier: Apache-2.0

package configrefs_test

import (
	"testing"

	configrefs "github.com/mutex-solutions/aiops-platform/plugins/config-refs"
	"github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The config-refs row passes the configrefs definition's suite, as its fake does
// (T2, 07-c4-plugin-sdk.md §8.2). The launcher fake serves the setup's environment
// through cmdline.
func TestConformance(t *testing.T) { conformance.Run(t, MountConfigrefs) }

// MountConfigrefs mounts the config-refs row on a root of its own for the suite of configrefs, as step 5 of plugin
// check does.
func MountConfigrefs(t *testing.T, s conformance.Setup) (*kernel.Root, *kernel.Fiber) {
	l, lp := fake.New()
	l.Environment = s.Env
	root := kerneltest.New(t)
	kerneltest.Mount(root, lp)
	f := root.Plugin(configrefs.Plugin, map[string]any{"maxFileBytes": int64(1 << 10)})
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	return root, f
}
