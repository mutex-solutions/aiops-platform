// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/cli"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	compositionfake "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// world is a root with the launch fake and the composition fake mounted.
type world struct {
	root     *kernel.Root
	l        *launchfake.Launcher
	rd       *compositionfake.Reader
	out, err bytes.Buffer
}

func newWorld(t *testing.T, argv ...string) *world {
	w := &world{root: kerneltest.New(t)}
	var lp, cp kernel.Plugin
	w.l, lp = launchfake.New()
	w.rd, cp = compositionfake.New()
	w.l.Argv, w.l.In, w.l.Out, w.l.ErrOut = argv, strings.NewReader(""), &w.out, &w.err
	w.l.WorkDir, w.l.Flags, w.l.Environment = "/work", map[string]string{"home": "/home"}, map[string]string{"NAME": "v"}
	kerneltest.Mount(w.root, lp, cp)
	return w
}

// awaitExit waits for the row's exit request; the bound only turns a row that never
// requests one into a failure.
func (w *world) awaitExit(t *testing.T) {
	t.Helper()
	select {
	case <-w.l.ExitRequested():
	case <-time.After(10 * time.Second):
		t.Fatal("the row requested no exit")
	}
}

// The cli row passes the guard half of the commands suite (ADR-061 decision 5). A row
// that the guard names as not ACTIVE is a row of the composition whose plugin needs
// commands and the keys it waits for.
func TestGuard(t *testing.T) { conformance.Run(t, MountCommands) }

// MountCommands mounts the cli row, the dispatcher, on a root of its own for the suite of commands, as step 5 of plugin
// check does.
func MountCommands(t *testing.T) (conformance.Subject, *kernel.Root) {
	w := newWorld(t)
	var rows []composition.Row
	pend := func(p commands.Pending) {
		id := "test/" + p.Row
		w.l.Modules = append(w.l.Modules, launch.CatalogEntry{ID: id, Needs: append([]string{commands.Key.Name()}, p.WaitsFor...)})
		rows = append(rows, composition.Row{ID: p.Row, Plugin: id, State: kernel.State(p.State)})
		w.rd.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: rows})
	}
	return conformance.Dispatcher(w.root.Plugin(cli.Plugin, nil), pend), w.root
}

type runFunc func(ctx context.Context, inv commands.Invocation) error

func (f runFunc) Run(ctx context.Context, inv commands.Invocation) error { return f(ctx, inv) }

// say registers "say <word>", which writes the directory, the home, the environment
// variable NAME and the word of its invocation.
var say = kernel.Define(kernel.Plugin{ID: "test/say", Inject: []kernel.Dep{kernel.Need(commands.Key)},
	Apply: func(c *kernel.Context, _ any) error {
		return commands.Key.Of(c).Register(c, commands.Spec{Path: []string{"say"}, Summary: "Say a word.", Args: []string{"<word>"}, Min: 1, Max: 1},
			runFunc(func(_ context.Context, inv commands.Invocation) error {
				name, _ := inv.Env("NAME")
				_, err := fmt.Fprintf(inv.Out, "%s %s %s %s\n", inv.Dir, inv.Home, name, inv.Args[0])
				return err
			}))
	}})

// The row dispatches the argv of cmdline once boot has finished, with the streams, the
// directory, the environment and the home of cmdline, and requests the exit with the
// code of the command (ADR-061 decisions 7 and 13).
func TestDispatchesOnceBooted(t *testing.T) {
	for _, c := range []struct {
		argv []string
		code int
		out  string
	}{
		{[]string{"say", "hi"}, 0, "/work /home v hi\n"},
		{[]string{"say"}, 2, ""},
		{[]string{"nope"}, 2, ""},
	} {
		w := newWorld(t, c.argv...)
		kerneltest.Mount(w.root, cli.Plugin, say)
		if ex := w.l.Exits(); len(ex) != 0 {
			t.Fatalf("%q: the row requested an exit before boot ended: %v", c.argv, ex)
		}
		w.l.Boot()
		w.awaitExit(t)
		if ex := w.l.Exits(); len(ex) != 1 || ex[0].Reason != launch.ExitCommand || ex[0].Code != c.code || w.out.String() != c.out {
			t.Errorf("%q: exits %+v, stdout %q, stderr %q; want code %d and stdout %q", c.argv, ex, w.out.String(), w.err.String(), c.code, c.out)
		}
	}
}

// A row that is disposed before boot ends dispatches nothing.
func TestNoDispatchBeforeBoot(t *testing.T) {
	w := newWorld(t, "say", "hi")
	f := w.root.Plugin(cli.Plugin, nil)
	if err := f.Await(t.Context()); err != nil {
		t.Fatal(err)
	}
	kerneltest.Mount(w.root, say)
	if err := f.Dispose(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = f.Await(t.Context())
	if ex := w.l.Exits(); len(ex) != 0 || w.out.Len() > 0 {
		t.Fatalf("exits %v, stdout %q", ex, w.out.String())
	}
}

// Help and a path that no row registered name each row of the composition that needs
// commands and is not ACTIVE, with the keys it waits for: those that no ACTIVE row
// provides, less the keys of the launcher and the loader (ADR-061 decision 9). A plugin
// of the module table that needs commands and has no mounted row, disabled or detached
// by the gate, is named by its id, since the composition lists mounted rows only.
func TestRowsThatAreNotActive(t *testing.T) {
	w := newWorld(t, "help")
	w.l.Modules = []launch.CatalogEntry{
		{ID: cli.ID, Provides: []string{"commands"}, Needs: []string{"cmdline", "ready", "appExit", "composition", "catalog"}},
		{ID: "aiops/command-key", Needs: []string{"commands", "operations", "logger"}},
		{ID: "aiops/command-profile", Needs: []string{"commands", "profileContext"}},
		{ID: "aiops/other", Needs: []string{"elsewhere"}},
		{ID: "aiops/command-gone", Needs: []string{"commands"}},
	}
	w.rd.Commit(composition.Generation{Scope: composition.ScopeProcess, Rows: []composition.Row{
		{ID: "cli", Plugin: cli.ID, State: kernel.Active},
		{ID: "command-key", Plugin: "aiops/command-key", State: kernel.Pending},
		{ID: "command-profile", Plugin: "aiops/command-profile", State: kernel.Active},
		{ID: "other", Plugin: "aiops/other", State: kernel.Pending},
		{ID: "ghost", Plugin: "aiops/ghost", State: kernel.Pending},
	}})
	kerneltest.Mount(w.root, cli.Plugin)
	w.l.Boot()
	w.awaitExit(t)
	if got := w.out.String(); !strings.Contains(got, "  command-key  PENDING, waits for operations\n") || strings.Contains(got, "  other ") ||
		!strings.Contains(got, "  aiops/command-gone  not mounted: disabled, detached by the gate, or of another role\n") ||
		strings.Contains(got, "ghost") || strings.Contains(got, "command-profile ") {
		t.Fatalf("help:\n%s", got)
	}
}
