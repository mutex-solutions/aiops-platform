// SPDX-License-Identifier: Apache-2.0

// Package commandprofile is the row command-profile of aiops-cli: the commands profile
// render, profile verify, profile init and profile sanitize (ADR-061 decisions 27, 30
// and 41). profile render composes with boot's code and this binary's module table, so
// a dump is what the server would compose (08-profiles-roadmap-adrs.md:359); its flags
// are those of S1, --dist included, which renders against the signed manifest of a
// directory.
//
// The row reaches the toolbox of the launcher through profileContext until the
// operations library carries Render, VerifyProfile, InitProfile and SanitizeProfile
// (ADR-061 decisions 16 and 20): the release manifest lists it among the consumers of
// profileContext meanwhile. The two commands that write the home do so as decision 17
// asks of the library: under the write lock of the home, $AIOPS_HOME/write.lock, held
// with flock(2) from before the checks that read the home to the end of the submission
// (decision 19), in a home that the effective user owns, each file of mode 0600
// replaced by a rename in its directory. ponytail: no snapshot, since each change is
// one rename and a failed second rename of sanitize puts the first back; the library's
// snapshot replaces this code with the methods.
package commandprofile

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/command-profile"

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the row.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:        ID,
	Inject:    []kernel.Dep{kernel.Need(commands.Key), kernel.Need(launch.ProfileContextKey)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		box := launch.ProfileContextKey.Of(c).Toolbox()
		r := commands.Key.Of(c)
		for _, x := range []struct {
			s   commands.Spec
			run func(context.Context, launch.Toolbox, commands.Invocation) error
		}{{renderSpec(), render}, {verifySpec(), verify}, {initSpec(), initProfile}, {sanitizeSpec(), sanitize}} {
			run := x.run
			if err := r.Register(c, x.s, command(func(ctx context.Context, inv commands.Invocation) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				return run(ctx, box, inv)
			})); err != nil {
				return err
			}
		}
		return nil
	},
})

type command func(ctx context.Context, inv commands.Invocation) error

func (f command) Run(ctx context.Context, inv commands.Invocation) error { return f(ctx, inv) }

func renderSpec() commands.Spec {
	f := func(name, value, usage string, kind commands.Kind) commands.Flag {
		return commands.Flag{Name: name, Kind: kind, Value: value, Usage: usage}
	}
	return commands.Spec{Path: []string{"profile", "render"}, Summary: "Compose a profile as the server would, admit it, and print the dumps asked for.",
		Flags: []commands.Flag{
			{Name: "profile", Kind: commands.String, Value: "<p>", Required: true, Usage: "the profile to compose"},
			f("home", "<dir>", "the home with profiles/<p>/, trust.yaml and verified/ (default $AIOPS_HOME; none: the shipped profiles)", commands.String),
			f("bundles", "<dir>", "the directory of bundles/<name>/bundle.yaml (default: the home)", commands.String),
			f("dist", "<dir>", "render against distribution.yaml, the signed manifest of this directory (default: the one aiopsctl embeds)", commands.String),
			f("patch", "<file>", "a layer 4 overlay", commands.List),
			f("role", "<role>", "edge, api or worker, or several comma-separated (default: all)", commands.List),
			f("server", "", "every inserted row must state roles", commands.Bool),
			f("dump", "", "print the composed tree", commands.Bool),
			f("dump-hooks", "", "print every hook chain", commands.Bool),
			f("dump-config-schema", "", "print the merged config schema", commands.Bool),
		}}
}

func verifySpec() commands.Spec {
	return commands.Spec{Path: []string{"profile", "verify"}, Summary: "Print every departure of the distribution from the release bindings."}
}

func initSpec() commands.Spec {
	return commands.Spec{Path: []string{"profile", "init"}, Summary: "Copy the shipped template of a profile into the home.",
		Args: []string{"<p>"}, Min: 1, Max: 1}
}

func sanitizeSpec() commands.Spec {
	return commands.Spec{Path: []string{"profile", "sanitize"},
		Summary: "Set aside each layer file of the home that stops the composition of a profile, cli by default.",
		Args:    []string{"[<p>]"}, Max: 1}
}

// abs resolves p against the working directory of the invocation; "" stays "".
func abs(inv commands.Invocation, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(inv.Dir, p)
}

// rendered prints the diagnostics of r on stderr and reports whether one is an error.
func rendered(inv commands.Invocation, r launch.Rendered) (refused bool) {
	for _, d := range r.Diagnostics {
		if d.Warning {
			fmt.Fprintln(inv.Err, "warning: "+d.Text)
		} else {
			fmt.Fprintln(inv.Err, "error: "+d.Text)
			refused = true
		}
	}
	return refused
}

func render(ctx context.Context, box launch.Toolbox, inv commands.Invocation) error {
	q := launch.RenderRequest{Profile: inv.Value("profile"), Home: abs(inv, inv.Value("home")), Bundles: abs(inv, inv.Value("bundles")),
		Server: inv.Bool("server")}
	if q.Home == "" {
		q.Home = inv.Home
	}
	if d := inv.Value("dist"); d != "" {
		q.Against = filepath.Join(abs(inv, d), "distribution.yaml")
	}
	for _, p := range inv.List("patch") {
		q.Patches = append(q.Patches, abs(inv, p))
	}
	for _, r := range inv.List("role") {
		for _, s := range strings.Split(r, ",") {
			if s = strings.TrimSpace(s); s != "edge" && s != "api" && s != "worker" {
				return fmt.Errorf("%w: role %q is not one of edge, api, worker", commands.ErrUsage, s)
			}
			q.Roles = append(q.Roles, s)
		}
	}
	for _, d := range []string{"--dump", "--dump-hooks", "--dump-config-schema"} {
		if inv.Bool(d[2:]) {
			q.Dumps = append(q.Dumps, d)
		}
	}
	r, err := box.Render(ctx, q)
	if err != nil {
		return err
	}
	refused := rendered(inv, r)
	for _, d := range r.Departures {
		fmt.Fprintln(inv.Err, "bindings: "+d)
	}
	for _, d := range q.Dumps {
		if len(q.Dumps) > 1 {
			fmt.Fprintf(inv.Out, "=== %s ===\n", d)
		}
		fmt.Fprint(inv.Out, r.Dumps[d])
	}
	if refused {
		return errors.New("the profile is refused")
	}
	return nil
}

// verify prints each departure from the release bindings of the distribution that
// renders the profile cli of the home, one per line (08-profiles-roadmap-adrs.md:366).
func verify(ctx context.Context, box launch.Toolbox, inv commands.Invocation) error {
	r, err := box.Render(ctx, launch.RenderRequest{Home: inv.Home, Profile: "cli"})
	if err != nil {
		return err
	}
	for _, d := range r.Departures {
		fmt.Fprintln(inv.Out, d)
	}
	return nil
}

// home returns the home of a command that writes it, or a usage error that names the
// variable (ADR-061 decision 17).
func home(inv commands.Invocation) (string, error) {
	if inv.Home == "" {
		return "", fmt.Errorf("%w: AIOPS_HOME is not set, and %s writes the home", commands.ErrUsage, strings.Join(inv.Path, " "))
	}
	return inv.Home, nil
}

func profileName(p string) error {
	if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `/\`) {
		return fmt.Errorf("%w: %q is not the name of a profile", commands.ErrUsage, p)
	}
	return nil
}

// initProfile copies the shipped profiles/<p>/profile.yaml into the home, unless the
// home holds one (ADR-060 decision 27).
func initProfile(ctx context.Context, box launch.Toolbox, inv commands.Invocation) error {
	h, err := home(inv)
	if err != nil {
		return err
	}
	p := inv.Args[0]
	if err := profileName(p); err != nil {
		return err
	}
	shipped, err := box.Shipped(ctx)
	if err != nil {
		return err
	}
	rel := filepath.Join("profiles", p, "profile.yaml")
	data, ok := shipped[filepath.ToSlash(rel)]
	if !ok {
		return fmt.Errorf("no shipped profile %s", p)
	}
	unlock, err := lock(ctx, h, inv)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(filepath.Join(h, rel)); err == nil {
		fmt.Fprintf(inv.Err, "the home holds %s, which is kept\n", rel)
		return nil
	}
	if err := write(filepath.Join(h, rel), data); err != nil {
		return err
	}
	fmt.Fprintf(inv.Err, "copied the shipped profile %s to %s\n", p, rel)
	submit(inv, h, p)
	return nil
}

// sanitize renames each layer file of the home that stops the composition of the
// profile, its profile layer and the site layer, to <name>.bak-<timestamp>, as dsh's
// sanitizeProfile does (ADR-061 decision 41; dsh §3.4). It reads them with boot's reader
// through Render, which loads no plugin.
func sanitize(ctx context.Context, box launch.Toolbox, inv commands.Invocation) error {
	h, err := home(inv)
	if err != nil {
		return err
	}
	p := "cli"
	if len(inv.Args) > 0 {
		p = inv.Args[0]
	}
	if err := profileName(p); err != nil {
		return err
	}
	unlock, err := lock(ctx, h, inv)
	if err != nil {
		return err
	}
	defer unlock()
	layers := []string{"profiles/" + p + "/aiops.patch.yml", "aiops.patch.yml"}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	var moved [][2]string
	for range layers {
		r, err := box.Render(ctx, launch.RenderRequest{Home: h, Profile: p})
		if err != nil {
			return errors.Join(err, restore(moved))
		}
		i := -1
		for j, d := range r.Diagnostics {
			if !d.Warning && d.Check == "compose" && (d.File == layers[0] || d.File == layers[1]) {
				i = j
			}
		}
		if i < 0 {
			break
		}
		d := r.Diagnostics[i]
		from := filepath.Join(h, filepath.FromSlash(d.File))
		if err := os.Rename(from, from+".bak-"+stamp); err != nil {
			return errors.Join(err, restore(moved))
		}
		moved = append(moved, [2]string{from, from + ".bak-" + stamp})
		fmt.Fprintf(inv.Err, "%s: set aside as %s\n", d.Text, filepath.Base(from)+".bak-"+stamp)
	}
	if len(moved) == 0 {
		fmt.Fprintf(inv.Err, "no layer file of the home stops the composition of %s\n", p)
		return nil
	}
	submit(inv, h, "")
	return nil
}

// restore puts back what sanitize renamed, last first.
func restore(moved [][2]string) error {
	var errs []error
	for i := len(moved) - 1; i >= 0; i-- {
		errs = append(errs, os.Rename(moved[i][1], moved[i][0]))
	}
	return errors.Join(errs...)
}

// lockWait is how long a second writer waits for the write lock: dsh's lock wait
// (ADR-061 decision 19; dsh §4.2).
const lockWait = 120 * time.Second

// lock takes the write lock of the home h, creating the home with mode 0700 where it is
// missing, and writes the holder's record, its process id and its command, into the
// file in place (ADR-061 decision 19). write.lock is never renamed or deleted, since
// flock binds to one inode. It refuses a home that the effective user does not own.
func lock(ctx context.Context, h string, inv commands.Invocation) (func(), error) {
	if fi, err := os.Stat(h); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
			return nil, fmt.Errorf("the home %s belongs to the user %d, and aiopsctl writes no home of another user", h, st.Uid)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(h, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(h, "write.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	bound, tick := time.NewTimer(lockWait), time.NewTicker(50*time.Millisecond)
	defer bound.Stop()
	defer tick.Stop()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-bound.C:
			holder, _ := os.ReadFile(f.Name())
			_ = f.Close()
			return nil, fmt.Errorf("write.lock of the home is held by %q after %s", cmp.Or(strings.TrimSpace(string(holder)), "an unknown holder"), lockWait)
		case <-tick.C:
		}
	}
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(fmt.Sprintf("%d %s\n", os.Getpid(), strings.Join(inv.Path, " "))), 0)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// write writes data to p with mode 0600, replaced by a rename in its directory, which
// it creates with mode 0700 where it is missing (ADR-047 decision 14).
func write(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".write-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	err = errors.Join(err, tmp.Sync(), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// submit tells the outcome of a write of the home that version 1 of the wire of ADR-062
// does not carry (ADR-061 decision 17): not-running where run/status.json of the home
// names no running process of profile, any profile where it is "", and restart-required
// with the reason not-submitted otherwise.
func submit(inv commands.Invocation, h, profile string) {
	var st struct{ Phase, Profile string }
	b, err := os.ReadFile(filepath.Join(h, "run", "status.json"))
	if err != nil || json.Unmarshal(b, &st) != nil || st.Phase == "stopped" || profile != "" && st.Profile != profile {
		fmt.Fprintln(inv.Err, "not-running: the change applies at the next start")
		return
	}
	fmt.Fprintln(inv.Err, "restart-required: not-submitted")
}
