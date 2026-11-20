// SPDX-License-Identifier: Apache-2.0

// Package serve is the command line of the server binaries: it parses the flags,
// builds boot.RunOptions from the operating system and runs the launcher. The
// production binary cmd/aiops and the test binary test/dist/cmd/aiops-test both
// call it with their own distribution, so that they differ in the module table and
// in nothing else (07-c4-plugin-sdk.md:47, :564: no fake or test plugin reaches a
// server binary).
//
// The flags are those of 08-profiles-roadmap-adrs.md:1205 and :122-124: --profile,
// --role, --patch (repeatable, layer 4) and the home, --home or $AIOPS_HOME. Two
// more are S1 additions of this package: --bundles, the directory of
// bundles/<name>/bundle.yaml (default: the home) and --limits, a limits.yaml that
// replaces the limits of the distribution (boot.ParseLimits). A third, --refs-dir,
// is the one directory that ${file:} reads when config-refs names the flag
// "refs-dir" (08-profiles-roadmap-adrs.md:162: "one directory named by flag"), so
// that a file reference need not read the home or the bundles.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/runfile"
)

// Distribution is what a server binary links: the module table as Go values, the
// part of the distribution manifest the launcher reads, the limits, and the bundles
// and profiles shipped inside the binary ("bundles/<bundle>/bundle.yaml ... embedded
// in the binary", 08-profiles-roadmap-adrs.md:1233; "profiles/<profile>/ shipped
// profile.yaml templates"). Shipped may be nil. A file of the home or of --bundles
// wins over a shipped one of the same path.
type Distribution struct {
	Registry boot.Registry
	Manifest boot.DistManifest
	Limits   boot.Limits
	Shipped  fs.FS
}

// Env is everything Run takes from the process.
type Env struct {
	Args    []string // program name excluded
	Getenv  func(string) string
	Stdout  io.Writer
	Stderr  io.Writer
	Signals <-chan os.Signal
}

// StatusFile is the name, in the runtime directory of the home, of the file that
// holds the launcher's last status as JSON.
const StatusFile = "status.json"

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

const usage = `usage: %s --profile <name> [flags]

  --profile <name>   profile to run (required)
  --home <dir>       home with profiles/<name>/ and the runtime directory run/ (default $AIOPS_HOME)
  --bundles <dir>    directory with bundles/<name>/bundle.yaml (default: the home)
  --role <role>      edge, api, worker or all (default: all)
  --patch <file>     layer 4 overlay; repeat for several
  --limits <file>    limits.yaml replacing the limits of the distribution
  --refs-dir <dir>   absolute directory that ${file:} references read (flag "refs-dir")
`

// Run parses the flags and runs the launcher until it exits, and returns the exit
// code of boot.Run, or boot.ExitUsage for a bad command line.
func Run(ctx context.Context, name string, e Env, d Distribution) int {
	fl := flag.NewFlagSet(name, flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	var patches list
	profile := fl.String("profile", "", "")
	home := fl.String("home", e.Getenv("AIOPS_HOME"), "")
	bundles := fl.String("bundles", "", "")
	role := fl.String("role", "", "")
	limits := fl.String("limits", "", "")
	refsDir := fl.String("refs-dir", "", "")
	fl.Var(&patches, "patch", "")
	if err := fl.Parse(e.Args); err != nil {
		fmt.Fprintf(e.Stderr, "%s: %v\n"+usage, name, err, name)
		return boot.ExitUsage
	}
	if *profile == "" || *home == "" || fl.NArg() > 0 {
		fmt.Fprintf(e.Stderr, "%s: --profile and a home (--home or $AIOPS_HOME) are required, and there are no arguments\n"+usage, name, name)
		return boot.ExitUsage
	}
	if *bundles == "" {
		*bundles = *home
	}
	lim := d.Limits
	if *limits != "" {
		data, err := os.ReadFile(*limits)
		if err == nil {
			lim, err = boot.ParseLimits(*limits, data)
		}
		if err != nil {
			fmt.Fprintf(e.Stderr, "error: %v\n", err)
			return boot.ExitRefused
		}
	}
	runDir := runfile.Dir(*home)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		fmt.Fprintf(e.Stderr, "error: %v\n", err)
		return boot.ExitInternal
	}
	opts := boot.RunOptions{
		Profile: *profile, Role: *role,
		Args:  e.Args,
		Flags: map[string]string{"profile": *profile, "home": *home, "bundles": *bundles, "role": *role, "refs-dir": *refsDir},
		Home:  overlay{os.DirFS(*home), d.Shipped}, Bundles: overlay{os.DirFS(*bundles), d.Shipped},
		Env: e.Getenv, Signals: e.Signals, Stdout: e.Stdout, Stderr: e.Stderr,
		Registry: d.Registry, Dist: d.Manifest, Limits: lim, Clock: boot.SystemClock(),
		Publish: func(s boot.ProcessStatus) {
			if err := runfile.Write(runDir, StatusFile, statusJSON(s)); err != nil {
				fmt.Fprintf(e.Stderr, "warning: status file: %v\n", err)
			}
		},
	}
	for _, p := range patches {
		opts.Patches = append(opts.Patches, patchFile(p))
	}
	return bootRun(ctx, opts)
}

// bootRun is boot.Run; a test replaces it to see the options and the status file.
var bootRun = boot.Run

// Status is the JSON form of boot.ProcessStatus in the status file.
type Status struct {
	Phase   string `json:"phase"`
	Ready   bool   `json:"ready"`
	Profile string `json:"profile"`
	Role    string `json:"role"`
	Digest  string `json:"digest"`
	Exit    int    `json:"exit"`
	Detail  string `json:"detail"`
}

func statusJSON(s boot.ProcessStatus) []byte {
	b, _ := json.MarshalIndent(Status{string(s.Phase), s.Ready, s.Profile, s.Role, s.Digest, s.Exit, s.Detail}, "", "  ")
	return append(b, '\n')
}

// patchFile returns a --patch file. A relative path that stays under the working
// directory is read through it and keeps its relative name in provenance; any other
// is read from the root of the file system.
func patchFile(p string) boot.File {
	if clean := path.Clean(filepath.ToSlash(p)); !filepath.IsAbs(p) && fs.ValidPath(clean) {
		return boot.File{FS: os.DirFS("."), Path: clean}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	vol := filepath.VolumeName(abs)
	return boot.File{FS: os.DirFS(vol + string(filepath.Separator)), Path: filepath.ToSlash(strings.TrimPrefix(abs, vol+string(filepath.Separator)))}
}

// overlay serves the files of first, and those of second where first has none.
type overlay struct{ first, second fs.FS }

func (o overlay) Open(name string) (fs.File, error) {
	f, err := o.first.Open(name)
	if err != nil && o.second != nil && errors.Is(err, fs.ErrNotExist) {
		return o.second.Open(name)
	}
	return f, err
}
