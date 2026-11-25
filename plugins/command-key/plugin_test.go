// SPDX-License-Identifier: Apache-2.0

package commandkey_test

import (
	"bytes"
	"context"
	"flag"
	"strings"
	"testing"

	row "github.com/mutex-solutions/aiops-platform/plugins/command-key"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	opsfake "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

// stub provides operations to the obligations of commands, whose invocations carry no
// environment and so no key password: GenerateKey answers the two paths of its prefix
// and writes nothing.
type stub struct{ operations.Operations }

func (stub) GenerateKey(ctx context.Context, r operations.KeyRequest) (operations.Written, error) {
	return operations.Written{Paths: []string{r.Prefix + ".key", r.Prefix + ".pub"}}, ctx.Err()
}

// MountCommands mounts the row on a root of its own for the obligations of commands,
// as step 5 of plugin check does.
func MountCommands(t *testing.T) (conformance.Subject, *kernel.Root) {
	root := kerneltest.New(t)
	kerneltest.Mount(root, kernel.Define(kernel.Plugin{ID: "test/operations", Provides: []kernel.AnyKey{operations.Key},
		Apply: func(c *kernel.Context, _ any) error { return operations.Key.Provide(c, stub{}) }}))
	return conformance.Contribution(root.Plugin(row.Plugin, nil), "fixtures"), root
}

// The row keeps the obligations of a command on the cases of its fixtures (ADR-061
// decision 5).
func TestObligations(t *testing.T) { conformance.Run(t, MountCommands) }

// The help of each command is the CLI spec (ADR-061 decision 30).
func TestHelp(t *testing.T) { conformance.Help(t, MountCommands, *update) }

// world mounts the operations fake, the recording dispatcher with the working directory
// /work and env, and the row on a root of its own.
func world(t *testing.T, env map[string]string) *fake.Registry {
	root := kerneltest.New(t)
	_, ops := opsfake.New()
	rec, p := fake.New()
	rec.Dir, rec.Env = "/work", env
	kerneltest.Mount(root, ops, p, row.Plugin)
	return rec
}

func dispatch(t *testing.T, rec *fake.Registry, argv ...string) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code, _ = rec.Dispatch(t.Context(), argv, commands.Streams{Out: &o, Err: &e})
	return code, o.String(), e.String()
}

// key generate writes the pair through GenerateKey of operations, with the prefix
// resolved against the working directory and the environment of the invocation for the
// password (ADR-061 decisions 16, 17 and 20; ADR-020 decisions 4 and 21). It names both
// files on stderr and prints nothing on stdout; a pair that exists is a denial, exit 1;
// with AIOPS_KEY_PASSWORD unset it is a usage error, exit 2, that names the variable.
func TestGenerate(t *testing.T) {
	rec := world(t, map[string]string{operations.KeyPasswordVar: ""})
	if code, out, errOut := dispatch(t, rec, "key", "generate", "k"); code != 0 || out != "" ||
		!strings.Contains(errOut, "/work/k.key") || !strings.Contains(errOut, "/work/k.pub") {
		t.Errorf("key generate k: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, _, errOut := dispatch(t, rec, "key", "generate", "/keys/site"); code != 0 || !strings.Contains(errOut, "/keys/site.key") {
		t.Errorf("key generate /keys/site: exit %d, stderr %q", code, errOut)
	}
	if code, out, errOut := dispatch(t, rec, "key", "generate", "k"); code != 1 || out != "" || !strings.Contains(errOut, "exists") {
		t.Errorf("a pair that exists: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, _, errOut := dispatch(t, world(t, nil), "key", "generate", "k"); code != 2 || !strings.Contains(errOut, operations.KeyPasswordVar) {
		t.Errorf("no password: exit %d, stderr %q", code, errOut)
	}
}
