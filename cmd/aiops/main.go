// SPDX-License-Identifier: Apache-2.0

// Command aiops is the server binary: the launcher (boot) and the module table of
// the release distribution (dist). It must never link a test plugin or a fake
// (07-c4-plugin-sdk.md:47); test/dist/cmd/aiops-test is the separate binary that does.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/dist"
	"github.com/mutex-solutions/aiops-platform/internal/serve"
)

func main() {
	reg, signed, lim, err := dist.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(boot.ExitInternal)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	os.Exit(serve.Run(context.Background(), "aiops", serve.Env{
		Args: os.Args[1:], Getenv: os.Getenv, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Signals: sigs,
	}, serve.Distribution{Registry: reg, Signed: signed, Limits: lim}))
}
