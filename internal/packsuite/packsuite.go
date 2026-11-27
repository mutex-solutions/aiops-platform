// SPDX-License-Identifier: Apache-2.0

// Package packsuite runs the suites and the scenario of a pack inside the test binaries
// that the release build compiles, with the first-party instantiable plugins of its
// module table, and that the release aiopsctl embeds and runs as child processes for
// plugin check, plugin pack and plugin verify (ADR-060 decisions 46 and 49). aiopsctl
// starts a binary with PackVar, the pack directory, FixturesVar, its fixtures, and
// ReportVar, the file of the JSON report: a launch.SuiteResult or a
// launch.ScenarioReport. The builder generates the test files that call this package,
// and they pass in the fake and the root of sdk/testkit, which a non-test file outside
// sdk/def, sdk/testkit, test/ and cmd/aiopsctl does not import. No other binary links
// it.
package packsuite

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/boot"
	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The environment that aiopsctl sets for a binary.
const (
	PackVar     = "AIOPS_PACK"
	FixturesVar = "AIOPS_FIXTURES"
	ReportVar   = "AIOPS_REPORT"
)

// report is the report of what ran, from the names of the tests that failed: set by
// Suite or Echo.
var (
	mu     sync.Mutex
	report func(failed []string) any
)

func setReport(f func(failed []string) any) {
	mu.Lock()
	defer mu.Unlock()
	report = f
}

// Main runs the tests of the binary with -test.v=test2json, copies its standard output
// on and reads from its framed lines which tests failed, then writes the report of the
// suite or the scenario that ran to the file that ReportVar names. It returns the code
// of m.Run, or 2 when the report cannot be written.
func Main(m *testing.M) int {
	r, w, err := os.Pipe()
	if err == nil {
		err = flag.Set("test.v", "test2json")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	stdout := os.Stdout
	os.Stdout = w
	done := make(chan []string)
	go func() { done <- framed(r, stdout) }()
	code := m.Run()
	os.Stdout = stdout
	_ = w.Close()
	failed := <-done
	mu.Lock()
	f := report
	mu.Unlock()
	if f == nil {
		return code
	}
	b, err := json.Marshal(f(failed))
	if err == nil {
		err = os.WriteFile(os.Getenv(ReportVar), b, 0o600)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return code
}

// framed copies r to w and returns the name of each test that a framed line of the
// output of -test.v=test2json says failed.
func framed(r io.Reader, w io.Writer) []string {
	var out []string
	s := bufio.NewScanner(io.TeeReader(r, w))
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		if rest, ok := strings.CutPrefix(s.Text(), "\x16--- FAIL: "); ok {
			name, _, _ := strings.Cut(rest, " ")
			out = append(out, name)
		}
	}
	_, _ = io.Copy(w, r) // the rest after a line longer than the buffer
	return out
}

// Instantiable is a first-party instantiable plugin of the release, which a pack's rows
// may name, and the registries it feeds.
type Instantiable struct {
	kernel.Plugin
	Feeds []string
}

// Table returns the first-party instantiable plugins by id.
func Table(ps ...Instantiable) map[string]Instantiable {
	out := map[string]Instantiable{}
	for _, p := range ps {
		out[p.ID] = p
	}
	return out
}

// Row is one row that the pack's bundle inserts, its config parsed as the launcher
// parses a row config.
type Row struct {
	ID, Plugin string
	Config     any
}

// Rows reads the rows of the bundle of the pack in dir whose plugin feeds the registry
// def, and refuses a row whose plugin the table does not hold: no row of the pack is
// left out of the suites unseen.
func Rows(dir string, table map[string]Instantiable, def string) ([]Row, error) {
	src, err := os.ReadFile(filepath.Join(dir, "bundle.yaml"))
	if err != nil {
		return nil, err
	}
	var items []struct {
		Insert []struct {
			ID, Plugin string
			Config     yaml.Node
		}
	}
	if err := yaml.Unmarshal(src, &items); err != nil {
		return nil, fmt.Errorf("bundle.yaml: %w", err)
	}
	var out []Row
	var errs []error
	for _, it := range items {
		for _, r := range it.Insert {
			row := Row{ID: r.ID, Plugin: r.Plugin}
			if r.Config.Kind != 0 {
				b, err := yaml.Marshal(&r.Config)
				if err == nil {
					row.Config, err = boot.ParseConfig("bundle.yaml", b)
				}
				errs = append(errs, err)
			}
			p, ok := table[r.Plugin]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("bundle.yaml: row %s names %s, which is not an instantiable first-party plugin of this release", r.ID, r.Plugin))
			case slices.Contains(p.Feeds, def):
				out = append(out, row)
			}
		}
	}
	return out, errors.Join(errs...)
}

// Suite runs the obligations of the suite of the registry def, of the revision, on each
// row of the pack that feeds it, with the plugin of table that the row names: run
// mounts that plugin with the row's config as a contribution and runs the suite. Each
// obligation takes its recorded case from <fixtures>/<def>/<name>.json, and one whose
// case is missing fails with its id and the path it read (07-c4-plugin-sdk.md:1571).
// The report names each failed obligation once, in the order of obligations.
func Suite(t *testing.T, def string, revision int, obligations []string, table map[string]Instantiable,
	run func(t *testing.T, p kernel.Plugin, config any, fixtures string)) {
	dir, fixtures := os.Getenv(PackVar), os.Getenv(FixturesVar)
	rows, err := Rows(dir, table, def)
	if err != nil {
		t.Fatal(err)
	}
	failed := map[string]bool{}
	for _, id := range obligations {
		p := filepath.Join(fixtures, def, strings.TrimPrefix(id, def+"/")+".json")
		if b, err := os.ReadFile(p); err != nil || !json.Valid(b) {
			failed[id] = true
			t.Errorf("%s: no recorded case in %s: %v", id, p, orNotJSON(err))
		}
	}
	setReport(func(names []string) any {
		for _, n := range names {
			if parts := strings.Split(n, "/"); len(parts) == 3 {
				failed[def+"/"+parts[2]] = true
			}
		}
		r := launch.SuiteResult{Name: def, Revision: revision, Halves: []string{"obligations"}}
		for _, id := range obligations {
			if failed[id] {
				r.Failed = append(r.Failed, id)
			}
		}
		return r
	})
	for _, r := range rows {
		t.Run(r.ID, func(t *testing.T) { run(t, table[r.Plugin].Plugin, r.Config, fixtures) })
	}
}

func orNotJSON(err error) error {
	if err == nil {
		return errors.New("it is not JSON")
	}
	return err
}

// The echo scenario (ADR-060 decision 49): its name, version and its one half.
const (
	EchoName    = "echo"
	EchoVersion = "1"
	EchoHalf    = "kernel"
)

// EchoCall is one call of the echo scenario, scenario/echo.json under the fixtures: the
// row whose echoer it calls through the dispatcher, the message and the reply it
// recorded.
type EchoCall struct {
	Row     string `json:"row"`
	Message string `json:"message"`
	Reply   string `json:"reply"`
}

// Echo runs the echo scenario on the pack: mount returns a root, the echoers dispatcher
// that it provides and the plugin that provides it; each row is mounted with its
// config, and each call of scenario/echo.json is made through the dispatcher to the
// echoer that its row registered. A row with no call, a call of a row that the pack
// does not hold and a reply that differs from the recorded one each fail the scenario
// with one line.
func Echo(t *testing.T, table map[string]Instantiable, mount func(t *testing.T) (*kernel.Root, echoers.Registry, kernel.Plugin)) {
	var failures []string
	fail := func(format string, args ...any) {
		failures = append(failures, fmt.Sprintf(format, args...))
		t.Error(failures[len(failures)-1])
	}
	setReport(func([]string) any {
		return launch.ScenarioReport{Name: EchoName, Version: EchoVersion, Halves: []string{EchoHalf}, Failures: failures}
	})
	dir, fixtures := os.Getenv(PackVar), os.Getenv(FixturesVar)
	rows, err := Rows(dir, table, "echoers")
	if err != nil {
		fail("%v", err)
		return
	}
	file := filepath.Join(fixtures, "scenario", EchoName+".json")
	var script struct{ Calls []EchoCall }
	b, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(b, &script)
	}
	if err != nil {
		fail("no recorded scenario in %s: %v", file, err)
		return
	}
	root, reg, dispatcher := mount(t)
	ctx := t.Context()
	if f := root.Plugin(dispatcher, nil); f.Await(ctx) != nil {
		fail("the echoers dispatcher did not start")
		return
	}
	names := map[string]string{} // the echoer that each row registered
	for _, r := range rows {
		f := root.Plugin(table[r.Plugin].Plugin, r.Config)
		_ = f.Await(ctx)
		st, _ := f.State(ctx)
		labels, _ := f.Effects(ctx)
		var own []string
		for _, l := range labels {
			if name, ok := strings.CutPrefix(l, echoers.Label("")); ok {
				own = append(own, name)
			}
		}
		if len(own) != 1 {
			fail("row %s is %s and registered %d echoers, want one", r.ID, st, len(own))
			continue
		}
		names[r.ID] = own[0]
	}
	called := map[string]bool{}
	for i, c := range script.Calls {
		called[c.Row] = true
		name, ok := names[c.Row]
		if !ok {
			fail("%s: call %d names row %q, which registered no echoer of the pack", file, i, c.Row)
			continue
		}
		if got, err := reg.Echo(ctx, name, c.Message); err != nil || got != c.Reply {
			fail("%s: call %d: row %s answered %q, %v; the recorded reply is %q", file, i, c.Row, got, err, c.Reply)
		}
	}
	for _, r := range rows {
		if _, ok := names[r.ID]; ok && !called[r.ID] {
			fail("%s calls no echoer of row %s: every row of the pack is called", file, r.ID)
		}
	}
}
