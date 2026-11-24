// SPDX-License-Identifier: Apache-2.0

// Package conformance is the suite of the commands definition (ADR-061 decision 5;
// 07-c4-plugin-sdk.md:1463). The obligations half is what a command owes; what that
// is cannot be decided for an arbitrary command without cases of its own, so each
// obligation commands/<name> reads its cases from the contribution's fixtures, at
// <fixtures>/commands/<name>.json, and dispatches them through the recording fake. A
// missing file fails the obligation with its id and the path it read. The guard half
// is what a dispatcher owes: each rule of parsing, help and exit codes of decisions 8
// to 10, the lease on the fiber of a command, the refusals of a path, and the
// refusals of a released registry; it runs on the cli row and on the fake.
//
// The two-tenant and disabled-for-tenant cases join at S4a with a raised suite
// revision (ADR-061 decision 5; ADR-060 decision 43). Every case has an id, and a
// failed case prints one line that starts with it, so plugin check prints one line per
// failed obligation (07-c4-plugin-sdk.md:1561). The package imports the standard
// library, sdk/kernel, the definition, its fake and sdk/def/internal/deftest only.
package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/def/internal/deftest"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Subject is the implementation under test, made by Dispatcher or Contribution.
type Subject struct {
	fiber    *kernel.Fiber
	guard    bool
	pend     func(commands.Pending)
	fixtures string
}

// Dispatcher names the fiber of a dispatcher under test, which provides commands.Key,
// and pend, which makes the dispatcher see p as a row that needs commands and is not
// ACTIVE: Run runs the guard half on it.
func Dispatcher(f *kernel.Fiber, pend func(p commands.Pending)) Subject {
	return Subject{fiber: f, guard: true, pend: pend}
}

// Contribution names the fiber of a command row under test, mounted on a root where
// nothing provides commands.Key, and the directory of its fixtures, "fixtures" for the
// one beside the plugin: Run runs the obligations half on it, with the recording fake
// as its dispatcher. The fake gives every invocation an empty home and an empty
// working directory of its own, and no environment.
func Contribution(f *kernel.Fiber, fixtures string) Subject {
	return Subject{fiber: f, fixtures: fixtures}
}

// Mount mounts the implementation under test on a root of its own and returns it with
// the root (ADR-059 decision 32). Run calls it once per case.
type Mount func(t *testing.T) (Subject, *kernel.Root)

// Obligations and Guards are the ids of the cases of each half, in the order Run runs
// them.
var (
	Obligations = ids(obligations)
	Guards      = ids(guards)
)

type check struct {
	id  string
	run func(e *env)
}

// Run runs the half that the subject of the first mount asks for, each case on a mount
// of its own; the first case runs on that first mount.
func Run(t *testing.T, mount Mount) {
	t.Helper()
	s, root := mount(t)
	cases := guards
	if !s.guard {
		cases = obligations
	}
	run(t, mount, s, root, cases)
}

// Paths runs the obligation commands/paths alone, on a command row whose commands are
// not built yet: such a row has no case that passes commands/run, so it is held to its
// declared paths until its commands are built.
func Paths(t *testing.T, mount Mount) {
	t.Helper()
	s, root := mount(t)
	run(t, mount, s, root, obligations[:1])
}

// Help holds the help of each command of a command row, aiopsctl help <path>, to the
// golden file testdata/help/<the words of its path joined by "-">.golden beside the row,
// so a change to a name or a flag is a change of that file (ADR-061 decision 30). With
// update it writes the files instead.
func Help(t *testing.T, mount Mount, update bool) {
	t.Helper()
	s, root := mount(t)
	e := &env{t: t, id: "commands/help", root: root, s: s}
	e.obligation()
	for _, name := range e.rec.Inventory() {
		code, err := e.dispatch(append([]string{"help"}, strings.Fields(name)...))
		file := filepath.Join("testdata", "help", strings.ReplaceAll(name, " ", "-")+".golden")
		if code != 0 || err != nil {
			e.fail("help %s: exit %d, %v", name, code, err)
		}
		if update {
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				e.fail("%v", err)
			}
			if err := os.WriteFile(file, e.out.Bytes(), 0o644); err != nil {
				e.fail("%v", err)
			}
			continue
		}
		if want, err := os.ReadFile(file); err != nil || string(want) != e.out.String() {
			e.t.Errorf("%s: %s differs from the help of %s (go test -update writes it; %v):\n%s", e.id, file, name, err, e.out.String())
		}
	}
}

func run(t *testing.T, mount Mount, s Subject, root *kernel.Root, cases []check) {
	t.Helper()
	for i, c := range cases {
		t.Run(strings.TrimPrefix(c.id, "commands/"), func(t *testing.T) {
			if i > 0 {
				s, root = mount(t)
			}
			e := &env{t: t, id: c.id, root: root, s: s}
			if s.guard {
				e.guard()
			} else {
				e.obligation()
			}
			c.run(e)
		})
	}
}

func ids(cs []check) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.id)
	}
	return out
}

// cancelBound is how long a command has to return once the context of its invocation
// ends: half the drain deadline of ADR-059 decision 30, inside which the cli row must
// return at a signal (ADR-061 decision 5).
const cancelBound = 5 * time.Second

// The cases of the fixtures, one file per obligation.
type (
	pathCase struct {
		Path    string `json:"path"`
		Summary string `json:"summary"`
	}
	runCase struct {
		Argv    []string `json:"argv"`
		Stdin   string   `json:"stdin"`
		Outcome string   `json:"outcome"` // ok, usage or error
		Stdout  string   `json:"stdout"`
		Stderr  string   `json:"stderr"` // a text that stderr contains
	}
	cancelCase struct {
		Argv  []string `json:"argv"`
		Delay string   `json:"delay"` // a Go duration
	}
)

var outcomes = map[int]string{0: "ok", 1: "error", 2: "usage"}

var obligations = []check{
	{"commands/paths", func(e *env) {
		var want []pathCase
		e.read("paths", &want)
		var names []string
		for _, p := range want {
			names = append(names, p.Path)
		}
		slices.Sort(names)
		if got := e.rec.Inventory(); !slices.Equal(got, names) {
			e.fail("the row registered %q, and paths.json lists %q", got, names)
		}
		for _, p := range want {
			if s, _ := e.rec.Spec(p.Path); s.Summary != p.Summary {
				e.fail("%s has the summary %q, and paths.json gives %q", p.Path, s.Summary, p.Summary)
			}
		}
	}},
	{"commands/run", func(e *env) {
		var cases []runCase
		e.read("run", &cases)
		passed := map[string]bool{}
		for i, c := range cases {
			var out, errb bytes.Buffer
			restore := hold()
			code, err := e.rec.Dispatch(e.t.Context(), c.Argv, commands.Streams{In: strings.NewReader(c.Stdin), Out: &out, Err: &errb})
			stray := restore()
			switch got := outcomes[code]; {
			case got != c.Outcome || (code == 2) != errors.Is(err, commands.ErrUsage):
				e.fail("case %d %q: the outcome is %s (exit %d, %v), want %s", i, c.Argv, got, code, err, c.Outcome)
			case out.String() != c.Stdout:
				e.fail("case %d %q: stdout is %q, want %q", i, c.Argv, out.String(), c.Stdout)
			case !strings.Contains(errb.String(), c.Stderr):
				e.fail("case %d %q: stderr %q does not hold %q", i, c.Argv, errb.String(), c.Stderr)
			case stray != "":
				e.fail("case %d %q: the command wrote %q to the process's own stdout or stderr, not to the streams of its invocation", i, c.Argv, stray)
			case got == "ok":
				for _, name := range e.rec.Inventory() {
					passed[name] = passed[name] || strings.HasPrefix(strings.Join(c.Argv, " ")+" ", name+" ")
				}
			}
		}
		for _, name := range e.rec.Inventory() {
			if !passed[name] {
				e.fail("%s has no case of outcome ok in run.json", name)
			}
		}
	}},
	{"commands/cancel", func(e *env) {
		var cases []cancelCase
		e.read("cancel", &cases)
		if len(cases) == 0 {
			e.fail("cancel.json holds no case")
		}
		for i, c := range cases {
			d, err := time.ParseDuration(c.Delay)
			if err != nil {
				e.fail("case %d: delay %q: %v", i, c.Delay, err)
			}
			ctx, cancel := context.WithCancel(e.t.Context())
			if d <= 0 { // ended before the call
				cancel()
			}
			stop := time.AfterFunc(d, cancel)
			res := make(chan error, 1)
			go func() {
				_, err := e.rec.Dispatch(ctx, c.Argv, commands.Streams{})
				res <- err
			}()
			select {
			case err = <-res:
			case <-ctx.Done():
				bound := time.NewTimer(cancelBound)
				select {
				case err = <-res:
				case <-bound.C:
					err = fmt.Errorf("no return within %s of the end of its context", cancelBound)
				}
				bound.Stop()
			}
			stop.Stop()
			cancel()
			if !errors.Is(err, context.Canceled) {
				e.fail("case %d %q: Run answered %v, want the error of its context once it ended", i, c.Argv, err)
			}
		}
	}},
}

// hold points the process's own stdout and stderr at a pipe while a case runs, and
// returns the function that restores them and returns what reached the pipe.
func hold() func() string {
	r, w, err := os.Pipe()
	if err != nil {
		return func() string { return "" }
	}
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	got := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		got <- b
	}()
	return func() string {
		os.Stdout, os.Stderr = stdout, stderr
		_ = w.Close()
		b := <-got
		_ = r.Close()
		return string(b)
	}
}

var (
	errDenied = errors.New("conformance: denied")
	errUse    = fmt.Errorf("%w: conformance: no such color", commands.ErrUsage)
)

// parsed is the spec of the parsing case: two arguments of which one is optional, and a
// flag of each kind, one of them required.
var parsed = commands.Spec{Path: []string{"alpha", "one"}, Summary: "Parse a command line.", Args: []string{"<a>", "[<b>]"}, Min: 1, Max: 2,
	Flags: []commands.Flag{{Name: "b", Kind: commands.Bool, Usage: "a bool"}, {Name: "s", Kind: commands.String, Value: "<s>", Default: "d", Usage: "a string"},
		{Name: "l", Kind: commands.List, Value: "<l>", Usage: "a list"}, {Name: "r", Kind: commands.String, Value: "<r>", Required: true, Usage: "required"}}}

var guards = []check{
	{"commands/guard-register", func(e *env) {
		fa := e.contribute(commands.Spec{Path: []string{"alpha", "one"}, Summary: "One."}, &recorder{})
		fb := e.contribute(commands.Spec{Path: []string{"beta"}, Summary: "Beta."}, &recorder{})
		if got := e.reg.Inventory(); !slices.Equal(got, []string{"alpha one", "beta"}) {
			e.fail("Inventory = %q, want [alpha one beta]", got)
		}
		la, _ := fa.Effects(e.t.Context())
		lb, _ := fb.Effects(e.t.Context())
		a, b := commands.Label([]string{"alpha", "one"}), commands.Label([]string{"beta"})
		if !slices.Contains(la, a) || slices.Contains(la, b) || !slices.Contains(lb, b) {
			e.fail("each registration is not an effect of its own fiber: alpha holds %q, beta holds %q", la, lb)
		}
	}},
	{"commands/guard-refuse", func(e *env) {
		e.contribute(commands.Spec{Path: []string{"alpha", "one"}, Summary: "One."}, &recorder{})
		for _, p := range [][]string{{"alpha", "one"}, {"alpha"}, {"alpha", "one", "two"}, {"Alpha"}, {"help", "me"}, {"a_b"}, nil} {
			if err := e.refused(commands.Spec{Path: p}, &recorder{}); !errors.Is(err, commands.ErrPath) {
				e.fail("Register of the path %q = %v, want ErrPath", p, err)
			}
		}
		if got := e.reg.Inventory(); !slices.Equal(got, []string{"alpha one"}) {
			e.fail("a refused registration changed the registry: Inventory = %q", got)
		}
	}},
	{"commands/guard-parse", func(e *env) {
		r := &recorder{}
		e.contribute(parsed, r)
		for _, c := range []struct {
			argv []string
			args []string
			b, s string
			l    []string
		}{
			{[]string{"x", "--r", "v"}, []string{"x"}, "false", "d", nil},
			{[]string{"--r=v", "-b", "x", "--l", "1", "--l=2", "--s", "w", "y"}, []string{"x", "y"}, "true", "w", []string{"1", "2"}},
			{[]string{"-r", "v", "--b=false", "x", "-s=z"}, []string{"x"}, "false", "z", nil},
			{[]string{"--r", "v", "--", "--l"}, []string{"--l"}, "false", "d", nil},
		} {
			code, err := e.dispatch(append([]string{"alpha", "one"}, c.argv...))
			inv, ok := r.last()
			if code != 0 || err != nil || !ok || !slices.Equal(inv.Args, c.args) || inv.Value("b") != c.b || inv.Value("s") != c.s ||
				!slices.Equal(inv.List("l"), c.l) || inv.Value("r") != "v" {
				e.fail("alpha one %q: exit %d, %v; the command got %+v", c.argv, code, err, inv)
			}
		}
		calls := r.count()
		for _, argv := range [][]string{{"x"}, {"--r", "v"}, {"--r", "v", "a", "b", "c"}, {"--r", "v", "x", "--nope"},
			{"x", "--r"}, {"--r", "v", "x", "-b=maybe"}} {
			code, err := e.dispatch(append([]string{"alpha", "one"}, argv...))
			if code != 2 || !errors.Is(err, commands.ErrUsage) || !strings.Contains(e.err.String(), "usage: aiopsctl alpha one <a> [<b>]") {
				e.fail("alpha one %q: exit %d, %v, stderr %q; want a usage error with the synopsis", argv, code, err, e.err.String())
			}
		}
		if r.count() != calls {
			e.fail("a command line with a usage error ran the command")
		}
	}},
	{"commands/guard-help", func(e *env) {
		r := &recorder{}
		e.contribute(parsed, r)
		e.contribute(commands.Spec{Path: []string{"beta"}, Summary: "Beta."}, r)
		for _, argv := range [][]string{{"help"}, {"--help"}, {"-h"}} {
			if code, err := e.dispatch(argv); code != 0 || err != nil || e.err.Len() > 0 || !strings.Contains(e.out.String(), "Parse a command line.") ||
				!strings.Contains(e.out.String(), "beta") || !strings.Contains(e.out.String(), "Go panic") {
				e.fail("%q: exit %d, %v, stdout %q, stderr %q; want every command and the exit codes on stdout", argv, code, err, e.out.String(), e.err.String())
			}
		}
		for _, argv := range [][]string{{"help", "alpha", "one"}, {"alpha", "one", "--help"}, {"alpha", "one", "x", "-h"}} {
			if code, err := e.dispatch(argv); code != 0 || err != nil ||
				!strings.Contains(e.out.String(), "usage: aiopsctl alpha one <a> [<b>] [--b] [--s <s>] [--l <l>]... --r <r>") {
				e.fail("%q: exit %d, %v, stdout %q; want the synopsis of alpha one", argv, code, err, e.out.String())
			}
		}
		if code, err := e.dispatch([]string{"help", "alpha"}); code != 0 || err != nil || !strings.Contains(e.out.String(), "alpha one") || strings.Contains(e.out.String(), "beta") {
			e.fail("help alpha: exit %d, %v, stdout %q; want the commands of alpha alone", code, err, e.out.String())
		}
		if code, err := e.dispatch(nil); code != 2 || !errors.Is(err, commands.ErrUsage) || e.out.Len() > 0 || !strings.Contains(e.err.String(), "Parse a command line.") {
			e.fail("no argument: exit %d, %v, stdout %q, stderr %q; want the list on stderr and exit 2", code, err, e.out.String(), e.err.String())
		}
		if r.count() != 0 {
			e.fail("help ran a command")
		}
	}},
	{"commands/guard-exit", func(e *env) {
		for _, c := range []struct {
			name   string
			err    error
			code   int
			stderr string
		}{
			{"ok", nil, 0, ""},
			{"use", errUse, 2, "usage: aiopsctl use"},
			{"fail", errDenied, 1, "aiopsctl: fail: conformance: denied"},
			{"boom", nil, 1, "goroutine"},
		} {
			e.contribute(commands.Spec{Path: []string{c.name}, Summary: c.name}, &recorder{err: c.err, panics: c.name == "boom"})
			code, err := e.dispatch([]string{c.name})
			if code != c.code || (code == 0) != (err == nil) || !strings.Contains(e.err.String(), c.stderr) || e.out.Len() > 0 {
				e.fail("%s: exit %d, %v, stderr %q; want exit %d with %q on stderr", c.name, code, err, e.err.String(), c.code, c.stderr)
			}
		}
		for _, argv := range [][]string{{"nope"}, {"ok", "extra"}} {
			if code, err := e.dispatch(argv); code != 2 || !errors.Is(err, commands.ErrUsage) {
				e.fail("%q: exit %d, %v; want a usage error", argv, code, err)
			}
		}
	}},
	{"commands/guard-pending", func(e *env) {
		e.contribute(commands.Spec{Path: []string{"beta"}, Summary: "Beta."}, &recorder{})
		e.s.pend(commands.Pending{Row: "late-row", State: "PENDING", WaitsFor: []string{"elsewhere"}})
		if code, err := e.dispatch([]string{"nope"}); code != 1 || err == nil || !strings.Contains(e.err.String(), "late-row") ||
			!strings.Contains(e.err.String(), "elsewhere") {
			e.fail("a path that no row registered while a row is not ACTIVE: exit %d, %v, stderr %q; want exit 1 naming the row", code, err, e.err.String())
		}
		if code, _ := e.dispatch([]string{"help"}); code != 0 || !strings.Contains(e.out.String(), "late-row") {
			e.fail("help does not name the row that is not ACTIVE: stdout %q", e.out.String())
		}
	}},
	{"commands/guard-effect", func(e *env) {
		f, late := e.hold()
		e.dispose(f)
		if err := e.reg.Register(late, commands.Spec{Path: []string{"late"}}, &recorder{}); !errors.Is(err, kernel.ErrInactiveEffect) {
			e.fail("Register from a disposed fiber = %v, want kernel.ErrInactiveEffect", err)
		}
		if slices.Contains(e.reg.Inventory(), "late") {
			e.fail("a registration from a disposed fiber is listed")
		}
	}},
	{"commands/guard-lease", func(e *env) {
		x := &slow{entered: make(chan struct{}), release: make(chan struct{})}
		f := e.mount(x.plugin())
		reply := make(chan int, 1)
		go func() {
			code, _ := e.reg.Dispatch(e.t.Context(), []string{"slow"}, commands.Streams{})
			reply <- code
		}()
		<-x.entered
		if err := f.Dispose(e.t.Context()); err != nil {
			close(x.release)
			e.fail("dispose: %v", err)
		}
		<-x.quiescing
		code, err := e.dispatch([]string{"slow"})
		st, _ := f.State(e.t.Context())
		close(x.release)
		if code != 1 || !errors.Is(err, kernel.ErrInactive) {
			e.fail("a call into a command whose fiber is quiescing: exit %d, %v; want exit 1 and kernel.ErrInactive", code, err)
		}
		if st == kernel.Disposed {
			e.fail("the fiber of a command was disposed while a call to it was in flight: the dispatcher holds no lease on it")
		}
		if got := <-reply; got != 0 {
			e.fail("the call in flight while the fiber of its command was disposed answered exit %d, want 0", got)
		}
		_ = f.Await(e.t.Context())
		if st, _ := f.State(e.t.Context()); st != kernel.Disposed {
			e.fail("after the call returned the fiber of the command is %s, want DISPOSED", st)
		}
	}},
	{"commands/guard-dispose", func(e *env) {
		base := e.reg.Inventory()
		fa := e.contribute(commands.Spec{Path: []string{"alpha"}, Summary: "Alpha."}, &recorder{})
		e.contribute(commands.Spec{Path: []string{"beta"}, Summary: "Beta."}, &recorder{})
		e.dispose(fa)
		codeA, _ := e.dispatch([]string{"alpha"})
		codeB, errB := e.dispatch([]string{"beta"})
		if got := e.reg.Inventory(); !slices.Equal(got, append(base, "beta")) || codeA != 2 || codeB != 0 || errB != nil {
			e.fail("after alpha's fiber was disposed: Inventory = %q, alpha exit %d, beta exit %d, %v", got, codeA, codeB, errB)
		}
	}},
	{"commands/guard-release", func(e *env) {
		e.contribute(commands.Spec{Path: []string{"alpha"}, Summary: "Alpha."}, &recorder{})
		_, live := e.hold()
		e.dispose(e.s.fiber)
		code, errCall := e.reg.Dispatch(e.t.Context(), []string{"alpha"}, commands.Streams{})
		errReg := e.reg.Register(live, commands.Spec{Path: []string{"x"}}, &recorder{})
		if code != 1 || !errors.Is(errCall, kernel.ErrInactive) || !errors.Is(errReg, kernel.ErrInactive) || len(e.reg.Inventory()) != 0 {
			e.fail("after the dispatcher was disposed: Dispatch = %d, %v, Register = %v, Inventory = %q; want kernel.ErrInactive twice and nothing listed",
				code, errCall, errReg, e.reg.Inventory())
		}
	}},
	{"commands/guard-remount", func(e *env) {
		old := e.reg
		_, live := e.hold()
		e.restart()
		e.contribute(commands.Spec{Path: []string{"alpha"}, Summary: "Alpha."}, &recorder{})
		_, errCall := old.Dispatch(e.t.Context(), []string{"alpha"}, commands.Streams{})
		errReg := old.Register(live, commands.Spec{Path: []string{"x"}}, &recorder{})
		if !errors.Is(errCall, kernel.ErrInactive) || !errors.Is(errReg, kernel.ErrInactive) || len(old.Inventory()) != 0 {
			e.fail("the registry of the first activation, after the second started: Dispatch = %v, Register = %v, Inventory = %q; want kernel.ErrInactive twice and nothing listed",
				errCall, errReg, old.Inventory())
		}
	}},
	{"commands/guard-side-door", func(e *env) {
		if doors := deftest.SideDoors[commands.Registry](e.reg); len(doors) > 0 {
			e.fail("the provided registry has methods outside commands.Registry: %v", doors)
		}
	}},
}

// env is what one case runs on.
type env struct {
	t        *testing.T
	id       string
	root     *kernel.Root
	s        Subject
	reg      commands.Registry
	rec      *fake.Registry // the obligations half: the dispatcher the suite mounted
	out, err bytes.Buffer   // the streams of the last dispatch
	seq      int
}

// fail ends the case with its one line.
func (e *env) fail(format string, a ...any) {
	e.t.Helper()
	e.t.Fatalf(e.id+": "+format, a...)
}

// read reads <fixtures>/commands/<name>.json into v.
func (e *env) read(name string, v any) {
	e.t.Helper()
	p := filepath.Join(e.s.fixtures, "commands", name+".json")
	b, err := os.ReadFile(p)
	if err == nil {
		err = json.Unmarshal(b, v)
	}
	if err != nil {
		e.fail("the cases of %s: %v", p, err)
	}
}

// obligation mounts the fake under the contribution and checks that the contribution
// registered something.
func (e *env) obligation() {
	e.t.Helper()
	rec, p := fake.New()
	rec.Home, rec.Dir = e.t.TempDir(), e.t.TempDir()
	e.mount(p)
	if err := e.root.Settle(e.t.Context()); err != nil {
		e.fail("the kernel did not settle: %v", err)
	}
	err := e.s.fiber.Await(e.t.Context())
	if st, _ := e.s.fiber.State(e.t.Context()); st != kernel.Active {
		e.fail("the contribution is %s with commands served, want ACTIVE: %v", st, err)
	}
	if len(rec.Inventory()) == 0 {
		e.fail("the contribution registered no command")
	}
	e.reg, e.rec = rec, rec
}

// guard waits for the dispatcher and reads the registry through a consumer of the key.
func (e *env) guard() {
	e.t.Helper()
	err := e.s.fiber.Await(e.t.Context())
	if st, _ := e.s.fiber.State(e.t.Context()); st != kernel.Active {
		e.fail("the dispatcher is %s, want ACTIVE: %v", st, err)
	}
	e.mount(kernel.Define(kernel.Plugin{ID: "conformance/probe", Inject: []kernel.Dep{kernel.Need(commands.Key)},
		Apply: func(c *kernel.Context, _ any) error { e.reg = commands.Key.Of(c); return nil }}))
}

// dispatch dispatches argv through the registry under test into fresh streams.
func (e *env) dispatch(argv []string) (int, error) {
	e.out.Reset()
	e.err.Reset()
	return e.reg.Dispatch(e.t.Context(), argv, commands.Streams{In: strings.NewReader(""), Out: &e.out, Err: &e.err})
}

// mount mounts p and fails the case unless it is ACTIVE.
func (e *env) mount(p kernel.Plugin) *kernel.Fiber {
	e.t.Helper()
	f := e.root.Plugin(p, nil)
	err := f.Await(e.t.Context())
	if st, _ := f.State(e.t.Context()); st != kernel.Active {
		e.fail("%s is %s, want ACTIVE: %v", p.ID, st, err)
	}
	return f
}

// contribute mounts a row of the suite that registers cmd under s.
func (e *env) contribute(s commands.Spec, cmd commands.Command) *kernel.Fiber {
	e.t.Helper()
	e.seq++
	return e.mount(register(fmt.Sprintf("conformance/command-%d", e.seq), s, cmd))
}

// refused mounts a row that registers cmd under s and returns why it failed; it fails
// the case if the row is ACTIVE.
func (e *env) refused(s commands.Spec, cmd commands.Command) error {
	e.t.Helper()
	e.seq++
	f := e.root.Plugin(register(fmt.Sprintf("conformance/refused-%d", e.seq), s, cmd), nil)
	err := f.Await(e.t.Context())
	if st, _ := f.State(e.t.Context()); st != kernel.Failed {
		e.fail("the registration of %q is %s, want FAILED", s.Path, st)
	}
	return err
}

func register(id string, s commands.Spec, cmd commands.Command) kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: id, Inject: []kernel.Dep{kernel.Need(commands.Key)},
		Apply: func(c *kernel.Context, _ any) error { return commands.Key.Of(c).Register(c, s, cmd) }})
}

// hold mounts a fiber that needs nothing and returns it with its context.
func (e *env) hold() (*kernel.Fiber, *kernel.Context) {
	e.t.Helper()
	var held *kernel.Context
	f := e.mount(kernel.Define(kernel.Plugin{ID: "conformance/holder",
		Apply: func(c *kernel.Context, _ any) error { held = c; return nil }}))
	return f, held
}

// restart restarts the subject's fiber, so a second activation provides the key, and
// waits until it is ACTIVE.
func (e *env) restart() {
	e.t.Helper()
	if err := e.s.fiber.Update(e.t.Context(), nil); err != nil {
		e.fail("restart: %v", err)
	}
	err := e.root.Settle(e.t.Context())
	if st, _ := e.s.fiber.State(e.t.Context()); st != kernel.Active {
		e.fail("the restarted subject is %s, want ACTIVE: %v", st, err)
	}
}

// dispose disposes f and waits until it is.
func (e *env) dispose(f *kernel.Fiber) {
	e.t.Helper()
	if err := f.Dispose(e.t.Context()); err != nil {
		e.fail("dispose: %v", err)
	}
	_ = f.Await(e.t.Context())
}

// recorder is a command of the guard half: it records each invocation and answers err,
// or panics.
type recorder struct {
	err    error
	panics bool
	mu     sync.Mutex
	invs   []commands.Invocation
}

func (r *recorder) Run(_ context.Context, inv commands.Invocation) error {
	r.mu.Lock()
	r.invs = append(r.invs, inv)
	r.mu.Unlock()
	if r.panics {
		panic("conformance: the command panics")
	}
	return r.err
}

func (r *recorder) last() (commands.Invocation, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.invs) == 0 {
		return commands.Invocation{}, false
	}
	return r.invs[len(r.invs)-1], true
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.invs)
}

// slow is the command of guard-lease: its Run blocks until release is closed, and
// quiescing is closed when the unload of its fiber begins.
type slow struct {
	entered, release chan struct{}
	quiescing        <-chan struct{}
}

func (x *slow) plugin() kernel.Plugin {
	return kernel.Define(kernel.Plugin{ID: "conformance/command-slow", Inject: []kernel.Dep{kernel.Need(commands.Key)},
		Apply: func(c *kernel.Context, _ any) error {
			x.quiescing = c.Std().Done()
			return commands.Key.Of(c).Register(c, commands.Spec{Path: []string{"slow"}, Summary: "Slow."}, x)
		}})
}

func (x *slow) Run(ctx context.Context, _ commands.Invocation) error {
	close(x.entered)
	select {
	case <-x.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
