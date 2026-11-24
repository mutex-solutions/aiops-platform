// SPDX-License-Identifier: Apache-2.0

// Package commandplugin is the row command-plugin of aiops-cli: the commands of the
// authoring path and of the plugins of a profile, plugin new, check, smoke, pack,
// sign, push, inspect, verify, gate, install, enable, disable and remove (ADR-061
// decision 30; 07-c4-plugin-sdk.md:1565, :1578; 08-profiles-roadmap-adrs.md:373-375).
// pack, sign, push, inspect and verify call the methods of the operations library of
// the same names on the OCI artifact of ADR-047; plugin new and plugin check call New
// and Check and print what was written or the steps of the check (ADR-061 decisions 16
// and 20; ADR-060 decisions 23 and 24); gate, install, enable, disable and remove call
// Gate, Install, Enable, Disable and Remove, which submit over the socket of ADR-062
// within --connect-wait and --wait, and print the outcome (ADR-061 decisions 17 and
// 44). plugin smoke is declared with its synopsis and flags, so the module table and
// the name lint hold it, and answers that it is not built yet: it joins with the
// builder of ADR-044. plugin purge joins at S3 (ADR-060 decision 47).
package commandplugin

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/command-plugin"

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the row.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID:        ID,
	Inject:    []kernel.Dep{kernel.Need(commands.Key), kernel.Need(operations.Key)},
	Registers: []kernel.AnyKey{commands.Key},
	Apply: func(c *kernel.Context, _ *Config) error {
		r, ops := commands.Key.Of(c), operations.Key.Of(c)
		runs := map[string]func(context.Context, operations.Operations, commands.Invocation) error{
			"new": newPlugin, "check": check, "pack": pack, "sign": sign, "push": push, "inspect": inspect, "verify": verify,
			"gate": gate, "install": install, "enable": enable, "disable": disable, "remove": remove}
		for _, s := range specs() {
			var cmd commands.Command = notBuilt(s.Name())
			if run, ok := runs[s.Path[1]]; ok {
				cmd = library(ops, run)
			}
			if err := r.Register(c, s, cmd); err != nil {
				return err
			}
		}
		return nil
	},
})

func spec(path, summary string, args []string, min, max int, flags ...commands.Flag) commands.Spec {
	return commands.Spec{Path: []string{"plugin", path}, Summary: summary, Args: args, Min: min, Max: max, Flags: flags}
}

func required(f commands.Flag) commands.Flag {
	f.Required = true
	return f
}

func specs() []commands.Spec {
	// The flags that several commands share.
	builder := commands.Flag{Name: "builder", Kind: commands.String, Value: "image|local", Default: "image", Usage: "the builder: the builder image, or the local toolchain"}
	out := commands.Flag{Name: "out", Kind: commands.String, Value: "<dir>", Usage: "the directory of the artifact's layout"}
	profile := commands.Flag{Name: "profile", Kind: commands.String, Value: "<p>", Usage: "the profile to change (default: the one that run/status.json of the home names)"}
	against := commands.Flag{Name: "against", Kind: commands.String, Value: "<dist.yaml>", Usage: "the published signed manifest of the release to check against"}
	// The bounds of a submission over the socket of reload-socket (ADR-062 decisions 9
	// and 10).
	connect := commands.Flag{Name: "connect-wait", Kind: commands.String, Value: "<duration>", Usage: "how long to try the socket of a running process (default: 60s)"}
	wait := commands.Flag{Name: "wait", Kind: commands.String, Value: "<duration>", Usage: "how long to wait for the final result (default: 10m)"}
	return []commands.Spec{
		spec("new", "Write a new plugin from the template of a registry, or a connector pack of a release.", []string{"<registry>", "<id>"}, 2, 2,
			commands.Flag{Name: "lang", Kind: commands.String, Value: "go", Usage: "the language of the plugin"},
			commands.Flag{Name: "contrib", Kind: commands.Bool, Usage: "write the plugin under the owner contrib"},
			against, commands.Flag{Name: "plugin", Kind: commands.List, Value: "<id>", Usage: "an instantiable plugin of the release that a connector pack rows"}),
		spec("check", "Run the eight steps of the plugin check on a plugin directory.", []string{"[<dir>]"}, 0, 1,
			commands.Flag{Name: "write", Kind: commands.Bool, Usage: "write the generated half of plugin.yaml and the generated tests"},
			commands.Flag{Name: "json", Kind: commands.Bool, Usage: "print the report as JSON"}, against),
		spec("smoke", "Build a throwaway distribution with the plugin and boot its test-only profile.", []string{"<dir>"}, 1, 1,
			required(commands.Flag{Name: "against", Kind: commands.String, Value: "<release>", Usage: "the release to build against"}), builder),
		spec("pack", "Pack a plugin directory into the layout of an artifact.", []string{"[<dir>]"}, 0, 1, out),
		spec("sign", "Sign the packed artifact and its attestation.", nil, 0, 0,
			required(commands.Flag{Name: "key", Kind: commands.String, Value: "<file>", Usage: "the private key file"}), out),
		spec("push", "Push the signed artifact to a registry.", []string{"<registry>"}, 1, 1, out),
		spec("inspect", "Show what a spec names, fetching only its manifest and its bundle.", []string{"<spec>"}, 1, 1),
		spec("verify", "Verify the signer of an artifact and run its suites.", []string{"<spec>"}, 1, 1, builder),
		spec("gate", "Run the clauses of the gate on an artifact against a published release.", []string{"<spec>"}, 1, 1, required(against)),
		spec("install", "Install an artifact into a profile of the home.", []string{"<spec>"}, 1, 1, profile, connect, wait),
		spec("enable", "Enable a row in a profile of the home.", []string{"<row>"}, 1, 1, profile, connect, wait),
		spec("disable", "Disable a row in a profile of the home.", []string{"<row>"}, 1, 1, profile, connect, wait),
		spec("remove", "Remove an installed plugin from a profile of the home.", []string{"<id>"}, 1, 1, profile, connect, wait),
	}
}

// notBuilt is a command whose operation lands with its method of the operations
// library.
type notBuilt string

func (n notBuilt) Run(ctx context.Context, _ commands.Invocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s is not built yet: it arrives with its method of the operations library (ADR-061 decision 16)", string(n))
}

type command func(ctx context.Context, inv commands.Invocation) error

func (f command) Run(ctx context.Context, inv commands.Invocation) error { return f(ctx, inv) }

// library is a command that calls the operations library: a usage error of the library
// is a usage error of the command (ADR-061 decision 10).
func library(ops operations.Operations, run func(context.Context, operations.Operations, commands.Invocation) error) commands.Command {
	return command(func(ctx context.Context, inv commands.Invocation) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := run(ctx, ops, inv)
		if errors.Is(err, operations.ErrUsage) {
			return fmt.Errorf("%w: %w", commands.ErrUsage, err)
		}
		return err
	})
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

// newPlugin is plugin new <registry> <id>: the template written into the directory of
// the plugin's name under the working directory, Go unless --lang names another.
func newPlugin(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	_, name, _ := strings.Cut(inv.Args[1], "/")
	if name == "" {
		return fmt.Errorf("%w: %q is not a plugin id <owner>/<name>", commands.ErrUsage, inv.Args[1])
	}
	w, err := ops.New(ctx, operations.NewRequest{Caller: caller(inv), Registry: inv.Args[0], ID: inv.Args[1],
		Lang: cmp.Or(inv.Value("lang"), "go"), Contrib: inv.Bool("contrib"), Against: abs(inv, inv.Value("against")),
		Plugins: inv.List("plugin"), Dir: abs(inv, name)})
	if err != nil {
		return err
	}
	for _, p := range w.Paths {
		fmt.Fprintln(inv.Err, "wrote "+p)
	}
	return nil
}

// check is plugin check [<dir>]: each step on stderr with its findings, one line per
// failed obligation, and the attestation it would sign; with --json the report alone on
// stdout (07-c4-plugin-sdk.md:1565; ADR-061 decision 11). A failed step is an error.
func check(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	dir := "."
	if len(inv.Args) > 0 {
		dir = inv.Args[0]
	}
	r, err := ops.Check(ctx, operations.CheckRequest{Caller: caller(inv), Dir: abs(inv, dir), Write: inv.Bool("write"),
		Against: abs(inv, inv.Value("against"))})
	if err != nil {
		return err
	}
	if inv.Bool("json") {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(inv.Out, "%s\n", b)
	}
	var failed string
	for _, s := range r.Steps {
		fmt.Fprintf(inv.Err, "step %d %s: %s\n", s.N, s.Name, s.Outcome)
		for _, d := range s.Diagnostics {
			line := d.Text
			if d.File != "" && d.Line > 0 {
				line = fmt.Sprintf("%s:%d: %s", d.File, d.Line, d.Text)
			} else if d.File != "" {
				line = d.File + ": " + d.Text
			}
			if d.Warning {
				line = "warning: " + line
			}
			fmt.Fprintln(inv.Err, "  "+line)
		}
		if s.Outcome == operations.StepFail && failed == "" {
			failed = fmt.Sprintf("step %d %s failed", s.N, s.Name)
		}
	}
	if failed != "" {
		return errors.New(failed)
	}
	fmt.Fprintf(inv.Err, "the attestation it would sign: %s\n", r.Attestation)
	return nil
}

// spec resolves an artifact's spec: oci:// and a reference as written, or a layout.
func specOf(inv commands.Invocation, s string) string {
	if strings.HasPrefix(s, "oci://") {
		return s
	}
	return abs(inv, s)
}

// layout is the layout of --out, artifact/ of the working directory by default.
func layout(inv commands.Invocation) string {
	if o := inv.Value("out"); o != "" {
		return abs(inv, o)
	}
	return abs(inv, "artifact")
}

// pack is plugin pack: the directory, the working directory by default, packed into
// --out, or artifact/ beside its plugin.yaml.
func pack(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	dir := inv.Dir
	if len(inv.Args) > 0 {
		dir = abs(inv, inv.Args[0])
	}
	out := filepath.Join(dir, "artifact")
	if o := inv.Value("out"); o != "" {
		out = abs(inv, o)
	}
	w, err := ops.Pack(ctx, operations.PackRequest{Caller: caller(inv), Dir: dir, Out: out})
	for _, p := range w.Paths {
		fmt.Fprintln(inv.Err, "wrote "+p)
	}
	return err
}

// sign is plugin sign: the artifact of the layout signed with the private key file.
func sign(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	w, err := ops.Sign(ctx, operations.SignRequest{Caller: caller(inv), Out: layout(inv), Key: abs(inv, inv.Value("key"))})
	for _, p := range w.Paths {
		fmt.Fprintln(inv.Err, "signed "+p)
	}
	return err
}

// push is plugin push: the spec by digest on stdout, which a script reads.
func push(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	p, err := ops.Push(ctx, operations.PushRequest{Caller: caller(inv), Out: layout(inv), Registry: inv.Args[0]})
	if err == nil {
		fmt.Fprintln(inv.Out, p.Spec)
	}
	return err
}

// inspect is plugin inspect: what the manifest and the bundle say, on stdout.
func inspect(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	in, err := ops.Inspect(ctx, operations.SpecRequest{Caller: caller(inv), Spec: specOf(inv, inv.Args[0])})
	if err != nil {
		return err
	}
	fmt.Fprintf(inv.Out, "digest: %s\nid: %s\nversion: %s\nruntimes: [%s]\n", in.Digest, shown(in.ID), shown(in.Version), shown(strings.Join(in.Runtimes, ", ")))
	fmt.Fprintln(inv.Out, "signer: not verified") // install verifies the signature over the digest (ADR-047 decision 2)
	for _, r := range in.Rows {
		config, _ := json.Marshal(r.Config)
		fmt.Fprintf(inv.Out, "row %s: plugin %s, config %s\n", shown(r.ID), shown(r.Plugin), shown(string(config)))
	}
	for _, p := range in.Content {
		fmt.Fprintln(inv.Out, "content: "+shown(p))
	}
	return nil
}

// shown returns s, or s quoted where it is not UTF-8 or holds a control character: what
// an artifact supplies is shown on a terminal, and never run by it.
func shown(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	return strconv.Quote(s)
}

// verify is plugin verify: each step on stdout, and exit 1 naming the steps that did
// not pass.
func verify(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	v, err := ops.Verify(ctx, operations.SpecRequest{Caller: caller(inv), Spec: specOf(inv, inv.Args[0]), Home: inv.Home, Builder: inv.Value("builder")})
	if err != nil {
		return err
	}
	fmt.Fprintf(inv.Out, "digest: %s\nsigner: %s\n", v.Digest, v.Signer)
	var failed []string
	for _, s := range v.Steps {
		fmt.Fprintf(inv.Out, "step %s: %s: %s\n", s.Name, s.Outcome, shown(s.Reason))
		if s.Outcome != "pass" {
			failed = append(failed, s.Name+" "+s.Outcome)
		}
	}
	if !v.Passed {
		return fmt.Errorf("plugin verify did not pass: %s", strings.Join(failed, ", "))
	}
	return nil
}

// gate is plugin gate <spec> --against <dist.yaml>: each clause on stdout, and exit 1
// when one denies (08:493).
func gate(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	v, err := ops.Gate(ctx, operations.GateRequest{Caller: caller(inv), Spec: specOf(inv, inv.Args[0]), Against: abs(inv, inv.Value("against"))})
	if err != nil {
		return err
	}
	var denied []string
	for _, c := range v.Clauses {
		line := fmt.Sprintf("check %d", c.Check)
		if c.Clause != "" {
			line += " " + c.Clause
		}
		line += ": " + c.Outcome
		if c.Reason != "" {
			line += ": " + shown(c.Reason)
		}
		fmt.Fprintln(inv.Out, line)
		if c.Outcome == launch.OutcomeDeny {
			denied = append(denied, line)
		}
	}
	if len(denied) > 0 {
		return fmt.Errorf("the gate at plugin gate --against denies: %s", strings.Join(denied, "; "))
	}
	return nil
}

// waits returns the bounds of --connect-wait and --wait, zero for the library's
// defaults.
func waits(inv commands.Invocation) (connect, wait time.Duration, err error) {
	for _, f := range []struct {
		name string
		d    *time.Duration
	}{{"connect-wait", &connect}, {"wait", &wait}} {
		if v := inv.Value(f.name); v != "" && err == nil {
			*f.d, err = time.ParseDuration(v)
		}
	}
	if err != nil {
		err = fmt.Errorf("%w: --connect-wait and --wait take a duration, 60s or 10m: %v", commands.ErrUsage, err)
	}
	return connect, wait, err
}

func install(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	c, w, err := waits(inv)
	if err != nil {
		return err
	}
	return changed(inv)(ops.Install(ctx, operations.InstallRequest{Caller: caller(inv), Home: inv.Home, Profile: inv.Value("profile"),
		Spec: specOf(inv, inv.Args[0]), ConnectWait: c, Wait: w}))
}

func enable(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	return row(ctx, inv, ops.Enable)
}

func disable(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	return row(ctx, inv, ops.Disable)
}

func row(ctx context.Context, inv commands.Invocation, m func(context.Context, operations.RowRequest) (operations.Change, error)) error {
	c, w, err := waits(inv)
	if err != nil {
		return err
	}
	return changed(inv)(m(ctx, operations.RowRequest{Caller: caller(inv), Home: inv.Home, Profile: inv.Value("profile"), Row: inv.Args[0],
		ConnectWait: c, Wait: w}))
}

func remove(ctx context.Context, ops operations.Operations, inv commands.Invocation) error {
	c, w, err := waits(inv)
	if err != nil {
		return err
	}
	return changed(inv)(ops.Remove(ctx, operations.RemoveRequest{Caller: caller(inv), Home: inv.Home, Profile: inv.Value("profile"), ID: inv.Args[0],
		ConnectWait: c, Wait: w}))
}

// changed prints a change of the home: the paths written and the notes on stderr; on
// stdout the dist.yaml lines of rebuild-required, the final result, or with none the
// operation ids and the last status, and the outcome (ADR-061 decisions 11 and 44).
// The library's error decides the exit. The paths, the notes and the dist.yaml lines
// carry what an artifact supplies, so they are shown as inspect shows it.
func changed(inv commands.Invocation) func(operations.Change, error) error {
	return func(ch operations.Change, err error) error {
		for _, p := range ch.Written {
			fmt.Fprintln(inv.Err, "wrote "+shown(p))
		}
		for _, n := range ch.Notes {
			fmt.Fprintln(inv.Err, shown(n))
		}
		for _, l := range ch.DistLines {
			fmt.Fprintln(inv.Out, shown(l))
		}
		switch r := ch.Result; {
		case r != nil:
			fmt.Fprintf(inv.Out, "result %s, generation %d, digest %s\n", r.Status, r.Generation, r.Digest)
		case len(ch.IDs) > 0:
			ids := make([]string, len(ch.IDs))
			for i, id := range ch.IDs {
				ids[i] = fmt.Sprint(id)
			}
			fmt.Fprintf(inv.Out, "operations %s; last status %s\n", strings.Join(ids, ", "), ch.Last)
		}
		if ch.Outcome != "" {
			fmt.Fprintln(inv.Out, strings.TrimSuffix(string(ch.Outcome)+": "+ch.Reason, ": "))
		}
		return err
	}
}
