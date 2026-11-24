package main

import (
	"context"
	"io"
	"io/fs"
	"os"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// process is everything run takes from the process.
type process struct {
	In       io.Reader
	Out, Err io.Writer
	Environ  []string
	Dir      string
	Files    fs.FS
	Signals  <-chan os.Signal
}

// distribution is what the binary links.
type distribution struct {
	Registry boot.Registry
	Signed   []byte
	Release  *boot.Release
	Limits   boot.Limits
	Shipped  fs.FS
}

// run is not built yet.
func run(context.Context, []string, process, distribution) int { return 1 }

// exitCode is not built yet.
func exitCode(code int, _ io.Writer) int { return code }
