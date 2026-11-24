// SPDX-License-Identifier: Apache-2.0

package commandplugin_test

import (
	"bytes"
	"flag"
	"strings"
	"testing"

	row "github.com/mutex-solutions/aiops-platform/plugins/command-plugin"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

func mount(t *testing.T) (conformance.Subject, *kernel.Root) {
	root := kerneltest.New(t)
	return conformance.Contribution(root.Plugin(row.Plugin, nil), "fixtures"), root
}

// The row registers the paths that fixtures/commands/paths.json lists, which the name
// lint reads (ADR-061 decision 5). Its commands are not built yet, so commands/run and
// commands/cancel wait for them.
func TestPaths(t *testing.T) { conformance.Paths(t, mount) }

// The help of each command is the CLI spec (ADR-061 decision 30).
func TestHelp(t *testing.T) { conformance.Help(t, mount, *update) }

// Each command answers that it is not built yet, with exit 1, on a command line that
// its spec reads.
func TestNotBuiltYet(t *testing.T) {
	root := kerneltest.New(t)
	rec, p := fake.New()
	kerneltest.Mount(root, p, row.Plugin)
	for _, name := range rec.Inventory() {
		s, _ := rec.Spec(name)
		argv := s.Path
		for range s.Min {
			argv = append(argv, "x")
		}
		for _, f := range s.Flags {
			if f.Required {
				argv = append(argv, "--"+f.Name, "x")
			}
		}
		var errb bytes.Buffer
		if code, _ := rec.Dispatch(t.Context(), argv, commands.Streams{Err: &errb}); code != 1 || !strings.Contains(errb.String(), name+" is not built yet") {
			t.Errorf("%q: exit %d, stderr %q", argv, code, errb.String())
		}
	}
}
