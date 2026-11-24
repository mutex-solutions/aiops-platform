// SPDX-License-Identifier: Apache-2.0

// Package cli is the row cli of aiops-cli: the dispatcher of the commands registry and
// the startup row of aiopsctl that parses the leftover argv (ADR-061 decisions 7 to 11;
// dsh §7). It provides commands with the rules of commands.Table and starts one
// goroutine, which waits until boot has finished, dispatches the argv and the streams
// of cmdline, and requests the exit with the code that the dispatch returned
// (ADR-061 decisions 7 and 13). It dispatches after boot because the tree has then
// settled, so a path that no row registered is told from one whose row is not ACTIVE,
// which it reads from composition and catalog.
//
// At S2a the dispatcher applies the lease on the fiber of each command and the rules of
// decisions 8 to 10, and recovers a panic of a command; the deadline, the concurrency
// cap and the breaker of 07-c4-plugin-sdk.md:511 arrive with the first caller that
// dispatches more than once in a process, admin-api at S4a (ADR-061 decision 39).
package cli

import (
	"context"
	"slices"
	"strings"

	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	composition "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/cli"

// Config is the config of the row, which has none.
type Config struct{}

// Plugin is the cli row.
var Plugin = plugin.Define(plugin.Spec[Config]{
	ID: ID,
	Inject: []kernel.Dep{kernel.Need(launch.CmdlineKey), kernel.Need(launch.ReadyKey), kernel.Need(launch.AppExitKey),
		kernel.Need(composition.Key), kernel.Need(launch.CatalogKey)},
	Provides: []kernel.AnyKey{commands.Key},
	Apply:    apply,
})

func apply(c *kernel.Context, _ *Config) error {
	cl := launch.CmdlineKey.Of(c)
	home, _ := cl.Flag("home")
	env := map[string]string{}
	for _, kv := range cl.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	rd, cat := composition.Key.Of(c), launch.CatalogKey.Of(c)
	t := commands.NewTable(c, commands.Environment{Dir: cl.Dir(), Home: home,
		Env:     func(name string) (string, bool) { v, ok := env[name]; return v, ok },
		Pending: func() []commands.Pending { return pending(rd, cat) }})
	if err := commands.Key.Provide(c, struct{ commands.Registry }{t}); err != nil {
		return err
	}
	ready, exit := launch.ReadyKey.Of(c), launch.AppExitKey.Of(c)
	c.Go("dispatch", func(ctx context.Context) error {
		select {
		case <-ready.Booted():
		case <-ctx.Done():
			return nil
		}
		code, _ := t.Dispatch(ctx, cl.Args(), commands.Streams{In: cl.Stdin(), Out: cl.Stdout(), Err: cl.Stderr()})
		exit.RequestExit(launch.ExitRequest{Reason: launch.ExitCommand, Detail: "the command returned", Code: code})
		return nil
	})
	return nil
}

// served reports whether the launcher or the loader serves the key k, which no row of
// the composition provides.
func served(k string) bool {
	for _, key := range []kernel.AnyKey{launch.ProfileContextKey, launch.CatalogKey, launch.CmdlineKey, launch.ReadyKey,
		launch.KillSwitchesKey, launch.AppExitKey, composition.Key, loaderdef.Key, kernel.ReflectKey, kernel.RegistryKey,
		kernel.HooksKey, kernel.LoggerKey} {
		if key.Name() == k {
			return true
		}
	}
	return false
}

// pending lists each row of the committed composition that needs commands and is not
// ACTIVE, with the keys it needs that no ACTIVE row provides (ADR-061 decision 9). The
// composition lists mounted rows only, so a plugin of the module table that needs
// commands and has no mounted row, one that a layer disabled or the gate detached, is
// listed by its id.
func pending(rd composition.Reader, cat launch.Catalog) []commands.Pending {
	g, _ := rd.Committed(composition.ScopeProcess)
	provided, mounted := map[string]bool{}, map[string]bool{}
	for _, r := range g.Rows {
		mounted[r.Plugin] = true
		if e, ok := cat.Lookup(r.Plugin); ok && r.State == kernel.Active {
			for _, k := range e.Provides {
				provided[k] = true
			}
		}
	}
	var out []commands.Pending
	for _, e := range cat.Entries() {
		if !mounted[e.ID] && slices.Contains(e.Needs, commands.Key.Name()) {
			out = append(out, commands.Pending{Row: e.ID, State: "not mounted: disabled, detached by the gate, or of another role"})
		}
	}
	for _, r := range g.Rows {
		e, ok := cat.Lookup(r.Plugin)
		if !ok || r.State == kernel.Active || !slices.Contains(e.Needs, commands.Key.Name()) {
			continue
		}
		p := commands.Pending{Row: r.ID, State: string(r.State)}
		for _, k := range e.Needs {
			if !provided[k] && !served(k) {
				p.WaitsFor = append(p.WaitsFor, k)
			}
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b commands.Pending) int { return strings.Compare(a.Row, b.Row) })
	return out
}
