package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime/debug"
	"strings"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// aiopsctl is the launcher of the cli profile (ADR-061 decisions 24 and 25): it boots
// the profile cli with the module table of aiops, in Command mode, and the cli row
// dispatches the command line, so every command is a row. It parses no flag of its own.
// What it passes to boot:
//
//   - the profile cli and the role all, with the server rule off (Command);
//   - the home from $AIOPS_HOME, a relative one under the working directory, as the
//     flag home that cmdline serves; with none it composes the shipped cli profile
//     alone, and a command that writes the home is a usage error;
//   - every argument as argv, the three streams, the working directory and the
//     environment for cmdline;
//   - nothing that the launcher writes on stdout, which carries only what a command was
//     asked to print, and no status file, since run/status.json is the readiness channel
//     of a server (ADR-059 decision 31).
//
// When a layer file of the home stops the boot of the cli profile, it boots once more
// with the shipped cli profile alone and the home still passed, and says which file
// stopped the first boot, so profile sanitize can set it aside (ADR-061 decision 41).

// process is everything run takes from the process. Files is rooted at "/".
type process struct {
	In       io.Reader
	Out, Err io.Writer
	Environ  []string
	Dir      string
	Files    fs.FS
	Signals  <-chan os.Signal
}

// distribution is what the binary links: the module table, the signed distribution
// manifest it embeds, the release key of a release binary, the limits and the shipped
// bundles and profiles.
type distribution struct {
	Registry boot.Registry
	Signed   []byte
	Release  *boot.Release
	Limits   boot.Limits
	Shipped  fs.FS
}

// run boots the cli profile in Command mode and returns the exit code of aiopsctl.
func run(ctx context.Context, args []string, p process, d distribution) int {
	env := map[string]string{}
	for _, kv := range p.Environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	home := env["AIOPS_HOME"]
	if home != "" && !path.IsAbs(home) {
		home = path.Join(p.Dir, home) // as aiops resolves its home, and S1 aiopsctl did
	}
	layers := d.Shipped
	if home != "" {
		h, err := fs.Sub(p.Files, strings.TrimPrefix(path.Clean("/"+home), "/"))
		if err != nil {
			fmt.Fprintf(p.Err, "aiopsctl: AIOPS_HOME %q: %v\n", home, err)
			return 1
		}
		layers = overlay{h, d.Shipped}
		if err := brokenLayer(layers); err != nil {
			fmt.Fprintf(p.Err, "aiopsctl: %v\naiopsctl: the layer stops the boot of the cli profile, so this run composes the shipped cli profile alone, with the home still passed: a command does not see the layers of the home for the cli profile; aiopsctl profile sanitize sets the file aside\n", err)
			layers = shippedCLI{layers, d.Shipped}
		}
	}
	o := boot.RunOptions{
		Profile: "cli", Role: "all", Args: args, Flags: map[string]string{"home": home},
		Home: layers, Bundles: layers,
		Env: func(k string) string { return env[k] }, Signals: p.Signals,
		Stdout: io.Discard, Stderr: p.Err,
		Stdio: boot.Stdio{In: p.In, Out: p.Out, Err: p.Err}, Dir: p.Dir, Environ: p.Environ,
		Registry: d.Registry, Signed: d.Signed, Release: d.Release, Limits: d.Limits, Clock: boot.SystemClock(),
		Command: true, Shipped: d.Shipped, Files: p.Files,
	}
	o.BuildInfo, _ = debug.ReadBuildInfo()
	return exitCode(boot.Run(ctx, o), p.Err)
}

// brokenLayer returns the error of a layer file of the home, the site layer or the
// profile layer of cli, that stops the composition of the cli profile as boot reads it.
func brokenLayer(layers fs.FS) error {
	_, _, err := boot.Compose(boot.Sources{Profile: "cli", Home: layers, Bundles: layers}, boot.Options{})
	var e *boot.Error
	if errors.As(err, &e) && (e.File == "aiops.patch.yml" || e.File == "profiles/cli/aiops.patch.yml") {
		return err
	}
	return nil
}

// exitCode maps the code of the launcher under Command to the three of aiopsctl (ADR-061
// decision 10): 0 and 2 are the command's; 8 is a signal before the command returned,
// which exits 1; every other code is a failure of the launcher, whose reason boot wrote,
// and exits 1. A Go panic outside every fiber exits 2 from the runtime.
func exitCode(code int, stderr io.Writer) int {
	switch code {
	case boot.ExitOK, boot.ExitUsage:
		return code
	case boot.ExitInterrupted:
		fmt.Fprintln(stderr, "aiopsctl: interrupted")
	}
	return 1
}

// overlay serves the files of first, and those of second where first has none: a file
// of the home wins over a shipped one.
type overlay struct{ first, second fs.FS }

func (o overlay) Open(name string) (fs.File, error) {
	f, err := o.first.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return o.second.Open(name)
	}
	return f, err
}

// shippedCLI serves the profile cli and the site layer from the shipped files alone, and
// every other file from the home: its trust, its records and its locks.
type shippedCLI struct{ home, shipped fs.FS }

func (s shippedCLI) Open(name string) (fs.File, error) {
	if name == "aiops.patch.yml" || strings.HasPrefix(name, "profiles/cli/") || strings.HasPrefix(name, "bundles/") {
		return s.shipped.Open(name)
	}
	return s.home.Open(name)
}
