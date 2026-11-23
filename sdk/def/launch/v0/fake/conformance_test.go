// SPDX-License-Identifier: Apache-2.0

package fake_test

import (
	"context"
	"testing"

	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The fake passes the suite that the launcher passes. Its catalog holds the
// consumer and the six launch keys, and Compose returns one composition.
func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (*kernel.Root, func()) {
		l, p := fake.New()
		l.ProfileName, l.RoleName, l.Argv, l.Flags, l.Environ = s.Profile, s.Role, s.Args, s.Flags, s.Env
		l.Switches = map[string]bool{}
		for _, n := range s.Switches {
			l.Switches[n] = true
		}
		l.Modules = []launch.CatalogEntry{{ID: s.Consumer.ID}}
		l.Keys = map[string]kernel.AnyKey{}
		for _, k := range []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey,
			launch.ReadyKey, launch.KillSwitchesKey, launch.AppExitKey} {
			l.Keys[k.Name()] = k
		}
		c := launch.Composition{Rows: []launch.Row{{ID: "a", Plugin: s.Consumer.ID, Enabled: true}}, Digest: launch.Digest{1}}
		l.ComposeFn = func(context.Context) (launch.Composition, error) { return c, nil }
		root := kerneltest.New(t)
		f := root.Plugin(p, nil)
		deftest.Active(t, f)
		return root, func() { deftest.Dispose(t, root, f) }
	})
}
