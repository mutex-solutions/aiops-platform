// SPDX-License-Identifier: Apache-2.0

// Command aiops-test is the test binary: the same launcher as cmd/aiops with the test
// distribution (test/dist), which adds the toy plugins, the fault plugins and the two
// test-only control rows. It is never a release artifact (07-c4-plugin-sdk.md:47).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
)

func main() {
	d, err := testdist.Distribution()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(boot.ExitInternal)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	os.Exit(serve.Run(context.Background(), "aiops-test", serve.Env{
		Args: os.Args[1:], Getenv: os.Getenv, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Signals: sigs,
	}, d))
}
