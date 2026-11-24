// SPDX-License-Identifier: Apache-2.0

// Command aiopsctl is the command line of the platform: the launcher of the cli profile
// (ADR-061 decisions 24 and 25). See run.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mutex-solutions/aiops-platform/dist"
)

func main() {
	reg, signed, lim, err := dist.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aiopsctl: %v\n", err)
		os.Exit(1)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	dir, _ := os.Getwd()
	os.Exit(run(context.Background(), os.Args[1:],
		process{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Environ: os.Environ(), Dir: dir, Files: os.DirFS("/"), Signals: sigs},
		distribution{Registry: reg, Signed: signed, Limits: lim, Shipped: dist.Shipped()}))
}
