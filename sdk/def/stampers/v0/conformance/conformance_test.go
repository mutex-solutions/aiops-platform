// SPDX-License-Identifier: Apache-2.0

package conformance_test

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"testing"

	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

type stampFunc func(ctx context.Context, body string) (string, error)

func (f stampFunc) Stamp(ctx context.Context, body string) (string, error) { return f(ctx, body) }

// contribution mounts a plugin that registers what stamper returns for its context.
func contribution(stamper func(c *kernel.Context) stampers.Stamper) conformance.Mount {
	return func(t *testing.T) (conformance.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		p := kernel.Define(kernel.Plugin{ID: "test/under-test", Inject: []kernel.Dep{kernel.Need(stampers.Key)},
			Apply: func(c *kernel.Context, _ any) error { return stampers.Key.Of(c).Register(c, "under-test", stamper(c)) }})
		return conformance.Contribution(root.Plugin(p, nil)), root
	}
}

// A stamper that keeps the contract passes the obligations half.
func TestObligationsPassOnACorrectStamper(t *testing.T) {
	conformance.Run(t, contribution(func(c *kernel.Context) stampers.Stamper {
		return stampFunc(func(ctx context.Context, body string) (string, error) {
			leave, ok := c.Enter()
			if !ok {
				return "", kernel.ErrInactive
			}
			defer leave()
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return fmt.Sprintf("stamp:%08x", crc32.ChecksumIEEE([]byte(body))), nil
		})
	}))
}

const stubEnv = "STAMPERS_CONFORMANCE_STUB"

var failLine = regexp.MustCompile(`(?m)^\s+\S+\.go:\d+: (\S+): `)

// A stub that answers "not implemented", as each stub of a fresh template does, fails
// every obligation with exactly one line that starts with its id (07-c4-plugin-sdk.md:1561,
// :1563). The suite runs in a child process, whose failure is the point.
func TestStubFailsOncePerObligation(t *testing.T) {
	if os.Getenv(stubEnv) == "1" {
		conformance.Run(t, contribution(func(*kernel.Context) stampers.Stamper {
			return stampFunc(func(context.Context, string) (string, error) { return "", errors.New("not implemented") })
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
