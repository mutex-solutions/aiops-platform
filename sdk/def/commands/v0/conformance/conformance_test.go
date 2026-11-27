// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"testing"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

type runFunc func(ctx context.Context, inv commands.Invocation) error

func (f runFunc) Run(ctx context.Context, inv commands.Invocation) error { return f(ctx, inv) }

var greet = commands.Spec{Path: []string{"greet"}, Summary: "Greet someone.", Max: 0,
	Flags: []commands.Flag{{Name: "name", Kind: commands.String, Value: "<name>", Default: "world", Usage: "whom to greet"}}}

// contribution mounts a row that registers greet with run, and reads its cases from
// fixtures.
func contribution(fixtures string, run runFunc) conformance.Mount {
	return func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		p := kernel.Define(kernel.Plugin{ID: "test/under-test", Inject: []kernel.Dep{kernel.Need(commands.Key)},
			Apply: func(c *kernel.Context, _ any) error { return commands.Key.Of(c).Register(c, greet, run) }})
		return conformance.Contribution(root.Plugin(p, nil), fixtures), root
	}
}

// A command that keeps the contract passes the obligations half on its cases.
func TestObligationsPassOnACorrectCommand(t *testing.T) {
	conformance.Run(t, contribution("testdata/greet", func(ctx context.Context, inv commands.Invocation) error {
		switch name := inv.Value("name"); {
		case ctx.Err() != nil:
			return ctx.Err()
		case name == "":
			return fmt.Errorf("%w: --name is empty", commands.ErrUsage)
		case name == "nobody":
			return errors.New("nobody is there")
		default:
			_, err := fmt.Fprintf(inv.Out, "hello %s\n", name)
			return err
		}
	}))
}

const stubEnv = "COMMANDS_CONFORMANCE_STUB"

var failLine = regexp.MustCompile(`(?m)^\s+\S+\.go:\d+: (\S+): `)

// A stub that answers "not implemented" with no case, as the template writes it, fails
// every obligation with exactly one line that starts with its id (ADR-061 decision 6;
// 07-c4-plugin-sdk.md:1561, :1563). The suite runs in a child process, whose failure is
// the point.
func TestStubFailsOncePerObligation(t *testing.T) {
	if os.Getenv(stubEnv) == "1" {
		conformance.Run(t, contribution("testdata/none", func(context.Context, commands.Invocation) error {
			return errors.New("not implemented")
		}))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStubFailsOncePerObligation$", "-test.count=1")
	cmd.Env = append(os.Environ(), stubEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the obligations half passed a stub:\n%s", out)
	}
	var got []string
	for _, m := range failLine.FindAllStringSubmatch(string(out), -1) {
		got = append(got, m[1])
	}
	if !slices.Equal(got, conformance.Obligations) {
		t.Fatalf("failure lines name %v, want each of %v once:\n%s", got, conformance.Obligations, out)
	}
}

var update = flag.Bool("update", false, "rewrite the help goldens")

// Help holds the help of each command to its golden file.
func TestHelp(t *testing.T) {
	conformance.Help(t, contribution("", func(context.Context, commands.Invocation) error { return nil }), *update)
}

// Paths holds a row whose commands are not built yet to the paths it declares.
func TestPathsAlone(t *testing.T) {
	conformance.Paths(t, contribution("testdata/greet", func(context.Context, commands.Invocation) error {
		return errors.New("not built yet")
	}))
}
