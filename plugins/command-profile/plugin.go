// SPDX-License-Identifier: Apache-2.0

// Package commandprofile is the row command-profile of aiops-cli: the commands profile
// render, profile verify, profile init and profile sanitize (ADR-061 decisions 27, 30
// and 41). Each calls one method of the operations library, Render, VerifyProfile,
// InitProfile or SanitizeProfile, and prints the result: the row consumes commands and
// operations and no launch key (decision 20). The library composes with boot's code and
// this binary's module table through the launcher's toolbox, so a dump is what the
// server would compose (08-profiles-roadmap-adrs.md:359), and writes the home under its
// write lock and snapshot (decisions 17 and 19). The flags of profile render are those
// of S1, --dist included, which renders against the signed manifest of a directory.
package commandprofile

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
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
	Inject:    []kernel.Dep{kernel.Need(commands.Key), kernel.Need(operations.Key)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		ops := operations.Key.Of(c)
		r := commands.Key.Of(c)
		for _, x := range []struct {
			s   commands.Spec
			run func(context.Context, operations.Operations, commands.Invocation) error
		}{{renderSpec(), render}, {verifySpec(), verify}, {initSpec(), initProfile}, {sanitizeSpec(), sanitize}} {
			run := x.run
			if err := r.Register(c, x.s, command(func(ctx context.Context, inv commands.Invocation) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				err := run(ctx, ops, inv)
				if errors.Is(err, operations.ErrUsage) {
					return fmt.Errorf("%w: %w", commands.ErrUsage, err)
				}
				return err
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

func caller(inv commands.Invocation) operations.Caller {
	return operations.Caller{Env: inv.Env, Progress: inv.Err}
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

func render(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	q := launch.RenderRequest{Profile: inv.Value("profile"), Home: abs(inv, inv.Value("home")), Bundles: abs(inv, inv.Value("bundles")),
		Server: inv.Bool("server"), Patches: inv.List("patch"), Dir: inv.Dir}
	if q.Home == "" {
		q.Home = inv.Home
	}
	if d := inv.Value("dist"); d != "" {
		q.Against = filepath.Join(abs(inv, d), "distribution.yaml")
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
	r, err := ops.Render(ctx, operations.RenderRequest{Caller: caller(inv), RenderRequest: q})
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
func verify(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	r, err := ops.VerifyProfile(ctx, operations.RenderRequest{Caller: caller(inv), RenderRequest: launch.RenderRequest{Home: inv.Home, Profile: "cli"}})
	if err != nil {
		return err
	}
	for _, d := range r.Departures {
		fmt.Fprintln(inv.Out, d)
	}
	return nil
}

// initProfile copies the shipped profiles/<p>/profile.yaml into the home, unless the
// home holds one (ADR-060 decision 27).
func initProfile(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	ch, err := ops.InitProfile(ctx, operations.ProfileRequest{Caller: caller(inv), Home: inv.Home, Profile: inv.Args[0]})
	changed(inv, ch)
	return err
}

// sanitize sets aside each layer file of the home that stops the composition of the
// profile, cli when none is named (ADR-061 decision 41; dsh §3.4).
func sanitize(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	r := operations.ProfileRequest{Caller: caller(inv), Home: inv.Home}
	if len(inv.Args) > 0 {
		r.Profile = inv.Args[0]
	}
	ch, err := ops.SanitizeProfile(ctx, r)
	changed(inv, ch)
	return err
}

// changed prints what a write of the home did on stderr: its notes, then its outcome
// (ADR-061 decision 17).
func changed(inv commands.Invocation, ch operations.Change) {
	for _, n := range ch.Notes {
		fmt.Fprintln(inv.Err, n)
	}
	switch ch.Outcome {
	case "":
	case operations.NotRunning:
		fmt.Fprintln(inv.Err, "not-running: the change applies at the next start")
	default:
		fmt.Fprintf(inv.Err, "%s: %s\n", ch.Outcome, ch.Reason)
	}
}
