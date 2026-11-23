// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"testing"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

type echoFunc func(ctx context.Context, msg string) (string, error)

func (f echoFunc) Echo(ctx context.Context, msg string) (string, error) { return f(ctx, msg) }

// contribution mounts a plugin that registers what echoer returns for its context.
func contribution(echoer func(c *kernel.Context) echoers.Echoer) conformance.Mount {
	return func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		p := kernel.Define(kernel.Plugin{ID: "test/under-test", Inject: []kernel.Dep{kernel.Need(echoers.Key)},
			Apply: func(c *kernel.Context, _ any) error { return echoers.Key.Of(c).Register(c, "under-test", echoer(c)) }})
		return conformance.Contribution(root.Plugin(p, nil)), root
	}
}

// An echoer that keeps the contract passes the obligations half.
func TestObligationsPassOnACorrectEchoer(t *testing.T) {
	conformance.Run(t, contribution(func(c *kernel.Context) echoers.Echoer {
		return echoFunc(func(ctx context.Context, msg string) (string, error) {
			leave, ok := c.Enter()
			if !ok {
				return "", kernel.ErrInactive
			}
			defer leave()
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "echo: " + msg, nil
		})
	}))
}

const stubEnv = "ECHOERS_CONFORMANCE_STUB"

var failLine = regexp.MustCompile(`(?m)^\s+\S+\.go:\d+: (\S+): `)

// A stub that answers "not implemented", as each stub of a fresh template does, fails
// every obligation with exactly one line that starts with its id (07-c4-plugin-sdk.md:1561,
// :1563). The suite runs in a child process, whose failure is the point.
func TestStubFailsOncePerObligation(t *testing.T) {
	if os.Getenv(stubEnv) == "1" {
		conformance.Run(t, contribution(func(*kernel.Context) echoers.Echoer {
			return echoFunc(func(context.Context, string) (string, error) { return "", errors.New("not implemented") })
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
