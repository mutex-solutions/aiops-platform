// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Table is the registry of one activation of its provider, with the rules of decisions
// 8 to 10: the cli row provides one per activation, and the fake runs one, so the two
// dispatch by one text of the rules. Every method returns kernel.ErrInactive, or lists
// nothing, once that activation is released.
type Table struct {
	act *kernel.Context
	env Environment

	mu   sync.Mutex
	cmds map[string]*entry // by Spec.Name
}

type entry struct {
	c    *kernel.Context
	spec Spec
	cmd  Command
}

var _ Registry = (*Table)(nil)

// NewTable returns an empty registry of the activation act, whose invocations get the
// working directory, the environment and the home of env.
func NewTable(act *kernel.Context, env Environment) *Table {
	return &Table{act: act, env: env, cmds: map[string]*entry{}}
}

// word is the rule of a word of a path and of a flag name (decision 2).
var word = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Register adds cmd under s as an effect of c's fiber.
func (t *Table) Register(c *kernel.Context, s Spec, cmd Command) error {
	leave, ok := t.act.Enter()
	if !ok {
		return kernel.ErrInactive
	}
	defer leave()
	if err := check(s, cmd); err != nil {
		return err
	}
	s.Path, s.Args, s.Flags = slices.Clone(s.Path), slices.Clone(s.Args), slices.Clone(s.Flags)
	e := &entry{c: c, spec: s, cmd: cmd}
	name := s.Name()
	return c.Effect(Label(s.Path), func() (kernel.Disposer, error) {
		t.mu.Lock()
		defer t.mu.Unlock()
		for _, o := range t.cmds {
			if n := min(len(o.spec.Path), len(s.Path)); slices.Equal(o.spec.Path[:n], s.Path[:n]) {
				return nil, fmt.Errorf("%w: %q overlaps %q", ErrPath, name, o.spec.Name())
			}
		}
		t.cmds[name] = e
		return func(context.Context) error {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.cmds[name] == e {
				delete(t.cmds, name)
			}
			return nil
		}, nil
	})
}

// check refuses a spec that the rules of decision 2 do not allow.
func check(s Spec, cmd Command) error {
	if len(s.Path) == 0 || s.Path[0] == "help" || slices.ContainsFunc(s.Path, func(w string) bool { return !word.MatchString(w) }) {
		return fmt.Errorf("%w: %q: each word is [a-z][a-z0-9-]*, and no path starts with help", ErrPath, s.Name())
	}
	seen := map[string]bool{}
	for _, f := range s.Flags {
		if !word.MatchString(f.Name) || f.Name == "help" || f.Name == "h" || seen[f.Name] || f.Kind < Bool || f.Kind > List {
			return fmt.Errorf("commands: %s: flag %q is not a word, is the dispatcher's, is listed twice or has no kind", s.Name(), f.Name)
		}
		seen[f.Name] = true
	}
	if cmd == nil || s.Min < 0 || s.Max >= 0 && s.Max < s.Min {
		return fmt.Errorf("commands: %s: no command, or a number of arguments outside 0 <= Min <= Max", s.Name())
	}
	return nil
}

// Inventory lists the registered paths, sorted.
func (t *Table) Inventory() []string {
	leave, ok := t.act.Enter()
	if !ok {
		return nil
	}
	defer leave()
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Sorted(maps.Keys(t.cmds))
}

// errPanic marks the error of a Run that panicked.
var errPanic = errors.New("panic")

// Dispatch runs one command line.
func (t *Table) Dispatch(ctx context.Context, argv []string, s Streams) (int, error) {
	leave, ok := t.act.Enter()
	if !ok {
		return 1, kernel.ErrInactive
	}
	defer leave()
	s.In = cmp.Or[io.Reader](s.In, strings.NewReader(""))
	s.Out, s.Err = cmp.Or[io.Writer](s.Out, io.Discard), cmp.Or[io.Writer](s.Err, io.Discard)
	help := len(argv) > 0 && argv[0] == "help"
	if help {
		argv = argv[1:]
	}
	words := argv[:slices.IndexFunc(append(slices.Clone(argv), ""), func(a string) bool { return !word.MatchString(a) })]
	e, group := t.lookup(words)
	rest := argv[len(words):]
	if e != nil {
		rest = argv[len(e.spec.Path):]
	}
	help = help || slices.ContainsFunc(rest[:cmp.Or(slices.Index(rest, "--")+1, len(rest)+1)-1], func(a string) bool { return a == "-h" || a == "--help" })
	switch {
	case len(argv) == 0 && !help:
		t.list(s.Err, nil)
		return 2, fmt.Errorf("%w: no command", ErrUsage)
	case e != nil && help:
		t.command(s.Out, e.spec)
		return 0, nil
	case e == nil && help && (len(words) == 0 || len(group) > 0):
		t.list(s.Out, group)
		return 0, nil
	case e == nil:
		return t.unknown(s.Err, words, group)
	}
	inv, err := parse(e.spec, rest)
	if err != nil {
		fmt.Fprintf(s.Err, "aiopsctl: %s: %v\nusage: aiopsctl %s\n", e.spec.Name(), err, synopsis(e.spec))
		return 2, err
	}
	inv.Streams, inv.Dir, inv.Home, inv.Env = s, t.env.Dir, t.env.Home, t.env.Env
	if inv.Env == nil {
		inv.Env = func(string) (string, bool) { return "", false }
	}
	done, ok := e.c.Enter()
	if !ok {
		fmt.Fprintf(s.Err, "aiopsctl: %s: the row of the command is not active\n", e.spec.Name())
		return 1, kernel.ErrInactive
	}
	defer done()
	err = run(ctx, e.cmd, inv)
	switch {
	case err == nil:
		return 0, nil
	case errors.Is(err, errPanic):
		fmt.Fprintf(s.Err, "aiopsctl: %s: %v\n", e.spec.Name(), err)
		return 1, err
	case errors.Is(err, ErrUsage):
		fmt.Fprintf(s.Err, "aiopsctl: %s: %v\nusage: aiopsctl %s\n", e.spec.Name(), err, synopsis(e.spec))
		return 2, err
	}
	fmt.Fprintf(s.Err, "aiopsctl: %s: %v\n", e.spec.Name(), err)
	return 1, err
}

// run calls cmd and answers a panic of its Run as an error with the stack, so a
// panicking command still ends the process with a code (decision 3).
func run(ctx context.Context, cmd Command, inv Invocation) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v\n%s", errPanic, r, debug.Stack())
		}
	}()
	return cmd.Run(ctx, inv)
}

// lookup returns the command whose path words starts with, or, with none, the paths
// that share the longest common start with words, which is nil when no path starts
// with the first word.
func (t *Table) lookup(words []string) (*entry, []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var group []string
	best := 0
	for _, name := range slices.Sorted(maps.Keys(t.cmds)) {
		p := t.cmds[name].spec.Path
		n := 0
		for n < len(p) && n < len(words) && p[n] == words[n] {
			n++
		}
		switch {
		case n == len(p):
			return t.cmds[name], nil
		case n > best:
			best, group = n, []string{name}
		case n == best && n > 0:
			group = append(group, name)
		}
	}
	return nil, group
}

// unknown answers a path that no row registered: exit 2 while every row that needs
// commands is ACTIVE, and exit 1, naming the rows that are not, otherwise.
func (t *Table) unknown(w io.Writer, words, group []string) (int, error) {
	path := strings.Join(words, " ")
	if p := t.pending(); len(p) > 0 {
		fmt.Fprintf(w, "aiopsctl: %s: no row registered this command, and a row that needs commands is not ACTIVE:\n", path)
		writePending(w, p)
		return 1, fmt.Errorf("%s: a row that needs commands is not ACTIVE", path)
	}
	fmt.Fprintf(w, "aiopsctl: %s: no such command\n", path)
	t.list(w, group)
	return 2, fmt.Errorf("%w: no such command %q", ErrUsage, path)
}

func (t *Table) pending() []Pending {
	if t.env.Pending == nil {
		return nil
	}
	return t.env.Pending()
}

// exitCodes are the three exit codes of decision 10, which help prints.
const exitCodes = `Exit codes:
  0  success, or help that was asked for
  1  a denial, a failed step, rebuild-required, an interrupt or a failed boot; the reason is on stderr
  2  a usage error; a Go panic outside every fiber also exits 2, with the trace of the runtime
`

// list prints the commands grouped by their first word with their summaries, or only
// those of names when it is not empty, then the exit codes and the rows that need
// commands and are not ACTIVE (decision 9).
func (t *Table) list(w io.Writer, names []string) {
	t.mu.Lock()
	var specs []Spec
	for _, name := range slices.Sorted(maps.Keys(t.cmds)) {
		if len(names) == 0 || slices.Contains(names, name) {
			specs = append(specs, t.cmds[name].spec)
		}
	}
	t.mu.Unlock()
	width := 0
	for _, s := range specs {
		width = max(width, len(s.Name()))
	}
	fmt.Fprint(w, "usage: aiopsctl <command> [flags and arguments]\n")
	for i, s := range specs {
		if i == 0 || s.Path[0] != specs[i-1].Path[0] {
			fmt.Fprintf(w, "\n%s\n", s.Path[0])
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, s.Name(), s.Summary)
	}
	fmt.Fprint(w, "\naiopsctl help <command> prints the synopsis and the flags of a command.\n\n", exitCodes)
	if p := t.pending(); len(p) > 0 {
		fmt.Fprint(w, "\nRows that need commands and are not ACTIVE:\n")
		writePending(w, p)
	}
}

// command prints the synopsis, the summary and the flags of one command, then the exit
// codes (decision 9).
func (t *Table) command(w io.Writer, s Spec) {
	fmt.Fprintf(w, "usage: aiopsctl %s\n\n%s\n", synopsis(s), s.Summary)
	if len(s.Flags) > 0 {
		fmt.Fprint(w, "\nFlags:\n")
		width := 0
		for _, f := range s.Flags {
			width = max(width, len(flagForm(f)))
		}
		for _, f := range s.Flags {
			fmt.Fprintf(w, "  %-*s  %s\n", width, flagForm(f), f.Usage)
		}
	}
	fmt.Fprint(w, "\n", exitCodes)
}

func writePending(w io.Writer, p []Pending) {
	for _, r := range p {
		fmt.Fprintf(w, "  %s  %s", r.Row, r.State)
		if len(r.WaitsFor) > 0 {
			fmt.Fprintf(w, ", waits for %s", strings.Join(r.WaitsFor, ", "))
		}
		fmt.Fprintln(w)
	}
}

// flagForm is how the synopsis writes a flag, without its brackets.
func flagForm(f Flag) string {
	if f.Kind == Bool {
		return "--" + f.Name
	}
	return "--" + f.Name + " " + cmp.Or(f.Value, "<value>")
}

// synopsis is the path, the arguments and the flags of s: an optional flag in
// brackets, and a list flag followed by an ellipsis.
func synopsis(s Spec) string {
	parts := slices.Concat(s.Path, s.Args)
	for _, f := range s.Flags {
		x := flagForm(f)
		if !f.Required {
			x = "[" + x + "]"
		}
		if f.Kind == List {
			x += "..."
		}
		parts = append(parts, x)
	}
	return strings.Join(parts, " ")
}

func usage(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUsage}, a...)...)
}

// parse reads args against s (decision 8): flags and positional arguments in any
// order; a flag is --name value, --name=value or -name; a bool flag takes no value
// unless written --name=false or --name=true; a list flag repeats; and -- ends the
// flags. An unknown flag, a missing value, a missing required flag or a number of
// arguments outside the spec is a usage error.
func parse(s Spec, args []string) (Invocation, error) {
	inv := Invocation{Path: s.Path, Flags: map[string][]string{}}
	given := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			inv.Args = append(inv.Args, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			inv.Args = append(inv.Args, a)
			continue
		}
		name, value, valued := strings.Cut(strings.TrimPrefix(a[1:], "-"), "=")
		j := slices.IndexFunc(s.Flags, func(f Flag) bool { return f.Name == name })
		if j < 0 {
			return Invocation{}, usage("--%s is not a flag of %s", name, s.Name())
		}
		f := s.Flags[j]
		switch {
		case f.Kind == Bool && !valued:
			value = "true"
		case f.Kind == Bool && value != "true" && value != "false":
			return Invocation{}, usage("--%s takes true or false, not %q", name, value)
		case !valued && i+1 == len(args):
			return Invocation{}, usage("--%s needs a value", name)
		case !valued:
			i++
			value = args[i]
		}
		if f.Kind != List {
			inv.Flags[name] = nil
		}
		inv.Flags[name], given[name] = append(inv.Flags[name], value), true
	}
	for _, f := range s.Flags {
		switch {
		case given[f.Name] || f.Kind == List && !f.Required:
		case f.Required:
			return Invocation{}, usage("--%s is required", f.Name)
		case f.Kind == Bool:
			inv.Flags[f.Name] = []string{cmp.Or(f.Default, "false")}
		default:
			inv.Flags[f.Name] = []string{f.Default}
		}
	}
	if n := len(inv.Args); n < s.Min || s.Max >= 0 && n > s.Max {
		return Invocation{}, usage("%s takes %s, not %d", s.Name(), count(s), n)
	}
	return inv, nil
}

// count says how many positional arguments s takes.
func count(s Spec) string {
	switch {
	case s.Max < 0:
		return fmt.Sprintf("at least %d arguments", s.Min)
	case s.Min == s.Max:
		return fmt.Sprintf("%d arguments", s.Min)
	}
	return fmt.Sprintf("%d to %d arguments", s.Min, s.Max)
}
