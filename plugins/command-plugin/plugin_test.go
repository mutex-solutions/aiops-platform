// SPDX-License-Identifier: Apache-2.0

package commandplugin_test

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	row "github.com/mutex-solutions/aiops-platform/plugins/command-plugin"
	commands "github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/def/commands/v0/fake"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

var update = flag.Bool("update", false, "rewrite the help goldens")

// stub provides operations: the five methods of the artifact record each request and
// answer the values of the stub, or err; New and Check record each request and answer
// w, rep and err, so the tests read what the row asks of the library.
type stub struct {
	operations.Operations
	in  operations.Inspected
	v   operations.Verified
	w   operations.Written
	rep operations.CheckReport
	ch  operations.Change
	vd  launch.Verdict
	err error

	mu     sync.Mutex
	reqs   []any
	news   []operations.NewRequest
	checks []operations.CheckRequest
}

func (s *stub) record(ctx context.Context, r any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r)
	return cmp.Or(ctx.Err(), s.err)
}

func (s *stub) last() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return operations.SpecRequest{Spec: "no request"}
	}
	return s.reqs[len(s.reqs)-1]
}

func (s *stub) Pack(ctx context.Context, r operations.PackRequest) (operations.Written, error) {
	return operations.Written{Paths: []string{r.Out}}, s.record(ctx, r)
}

func (s *stub) Sign(ctx context.Context, r operations.SignRequest) (operations.Written, error) {
	return operations.Written{Paths: []string{r.Out}}, s.record(ctx, r)
}

func (s *stub) Push(ctx context.Context, r operations.PushRequest) (operations.Pushed, error) {
	return operations.Pushed{Spec: "oci://" + r.Registry + "@sha256:ab"}, s.record(ctx, r)
}

func (s *stub) Inspect(ctx context.Context, r operations.SpecRequest) (operations.Inspected, error) {
	return s.in, s.record(ctx, r)
}

func (s *stub) Verify(ctx context.Context, r operations.SpecRequest) (operations.Verified, error) {
	return s.v, s.record(ctx, r)
}

func (s *stub) New(ctx context.Context, r operations.NewRequest) (operations.Written, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.news = append(s.news, r)
	return s.w, cmp.Or(ctx.Err(), s.err)
}

func (s *stub) Check(ctx context.Context, r operations.CheckRequest) (operations.CheckReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = append(s.checks, r)
	return s.rep, cmp.Or(ctx.Err(), s.err)
}

// mountRow mounts s as the provider of operations and the row on a root of its own.
func mountRow(t *testing.T, s *stub) (*kernel.Root, *kernel.Fiber) {
	root := kerneltest.New(t)
	kerneltest.Mount(root, kernel.Define(kernel.Plugin{ID: "test/operations", Provides: []kernel.AnyKey{operations.Key},
		Apply: func(c *kernel.Context, _ any) error { return operations.Key.Provide(c, s) }}))
	return root, root.Plugin(row.Plugin, nil)
}

func mount(t *testing.T) (conformance.Subject, *kernel.Root) {
	root, f := mountRow(t, &stub{})
	return conformance.Contribution(f, "fixtures"), root
}

// The row registers the paths that fixtures/commands/paths.json lists, which the name
// lint reads (ADR-061 decision 5). Most of its commands are not built yet, so
// commands/run and commands/cancel wait for them.
func TestPaths(t *testing.T) { conformance.Paths(t, mount) }

// The help of each command is the CLI spec (ADR-061 decision 30).
func TestHelp(t *testing.T) { conformance.Help(t, mount, *update) }

// world is the row mounted on s, with the recording dispatcher in the working directory
// /work and the home /site.
func world(t *testing.T, s *stub) *fake.Registry {
	root, _ := mountRow(t, s)
	rec, p := fake.New()
	rec.Dir, rec.Home = "/work", "/site"
	kerneltest.Mount(root, p)
	if err := root.Settle(t.Context()); err != nil {
		t.Fatal(err)
	}
	return rec
}

func dispatch(t *testing.T, rec *fake.Registry, argv ...string) (code int, out, errOut string) {
	var o, e bytes.Buffer
	code, _ = rec.Dispatch(t.Context(), argv, commands.Streams{Out: &o, Err: &e})
	return code, o.String(), e.String()
}

// built are the commands that run through the operations library (ADR-061 decision
// 16; ADR-047 decision 1; ADR-060 decisions 23 and 24).
var built = []string{"plugin check", "plugin disable", "plugin enable", "plugin gate", "plugin inspect", "plugin install", "plugin new",
	"plugin pack", "plugin push", "plugin remove", "plugin sign", "plugin verify"}

// Every other command answers that it is not built yet, with exit 1, on a command line
// that its spec reads.
func TestNotBuiltYet(t *testing.T) {
	rec := world(t, &stub{})
	for _, name := range rec.Inventory() {
		if slices.Contains(built, name) {
			continue
		}
		s, _ := rec.Spec(name)
		argv := s.Path
		for range s.Min {
			argv = append(argv, "x")
		}
		for _, f := range s.Flags {
			if f.Required {
				argv = append(argv, "--"+f.Name, "x")
			}
		}
		if code, _, errOut := dispatch(t, rec, argv...); code != 1 || !strings.Contains(errOut, name+" is not built yet") {
			t.Errorf("%q: exit %d, stderr %q", argv, code, errOut)
		}
	}
}

// pack, sign and push call the library with the plugin directory, the layout and the
// key file resolved against the working directory, the layout artifact/ of the
// directory by default (ADR-047 decision 1), and the environment of the invocation,
// from which the library reads the key password and the registry credentials. pack and
// sign name the layout on stderr; push prints the spec by digest on stdout (ADR-061
// decisions 11 and 30).
func TestPackSignAndPushCallTheLibrary(t *testing.T) {
	s := &stub{}
	rec := world(t, s)
	for _, c := range []struct {
		argv     []string
		want     any
		out, err string
	}{
		{[]string{"plugin", "pack"}, operations.PackRequest{Dir: "/work", Out: "/work/artifact"}, "", "wrote /work/artifact"},
		{[]string{"plugin", "pack", "x", "--out", "/layouts/x"}, operations.PackRequest{Dir: "/work/x", Out: "/layouts/x"}, "", "wrote /layouts/x"},
		{[]string{"plugin", "sign", "--key", "a.key"}, operations.SignRequest{Out: "/work/artifact", Key: "/work/a.key"}, "", "signed /work/artifact"},
		{[]string{"plugin", "push", "registry.example/acme/x", "--out", "o"}, operations.PushRequest{Out: "/work/o", Registry: "registry.example/acme/x"},
			"oci://registry.example/acme/x@sha256:ab\n", ""},
	} {
		code, out, errOut := dispatch(t, rec, c.argv...)
		if code != 0 || out != c.out || !strings.Contains(errOut, c.err) {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", c.argv, code, out, errOut)
		}
		if got := fmt.Sprintf("%+v", strip(s.last())); got != fmt.Sprintf("%+v", c.want) {
			t.Errorf("%q: request %s, want %+v", c.argv, got, c.want)
		}
	}
}

// strip drops the caller of a request, after checking that it carries the environment.
func strip(r any) any {
	switch q := r.(type) {
	case operations.PackRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	case operations.SignRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	case operations.PushRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	case operations.SpecRequest:
		if q.Env != nil {
			q.Caller = operations.Caller{}
		}
		return q
	case operations.GateRequest:
		q.Caller = operations.Caller{}
		return q
	case operations.InstallRequest:
		q.Caller = operations.Caller{}
		return q
	case operations.RowRequest:
		q.Caller = operations.Caller{}
		return q
	case operations.RemoveRequest:
		q.Caller = operations.Caller{}
		return q
	}
	return r
}

// inspect prints what the library read of a spec, a layout resolved against the
// working directory or an oci:// spec as written, on stdout, with the line that no
// signer was verified (ADR-047 decision 2), and exits 0.
func TestInspectPrintsTheArtifact(t *testing.T) {
	s := &stub{in: operations.Inspected{Digest: "sha256:ab", ID: "acme/x", Version: "0.1.0",
		Rows:    []operations.Row{{ID: "logs", Plugin: "aiops/echoer", Config: map[string]any{"endpoint": "http://x.example.test"}}},
		Content: []string{"labels.yaml"}}}
	rec := world(t, s)
	for spec, want := range map[string]string{"artifact": "/work/artifact", "oci://registry.example/acme/x:0.1.0": "oci://registry.example/acme/x:0.1.0"} {
		code, out, errOut := dispatch(t, rec, "plugin", "inspect", spec)
		if q := strip(s.last()).(operations.SpecRequest); code != 0 || errOut != "" || q.Spec != want {
			t.Errorf("inspect %s: exit %d, stderr %q, request %+v", spec, code, errOut, q)
		}
		for _, line := range []string{"digest: sha256:ab", "id: acme/x", "version: 0.1.0", "runtimes: []", "signer: not verified",
			`row logs: plugin aiops/echoer, config {"endpoint":"http://x.example.test"}`, "content: labels.yaml"} {
			if !strings.Contains(out, line+"\n") {
				t.Errorf("inspect %s: stdout %q lacks %q", spec, out, line)
			}
		}
	}
}

// inspect, verify, gate and install print no control character of what an artifact
// supplies, on stdout or stderr: the clause reasons of the gate, the dist.yaml lines
// of the module of rebuild-required, the notes and the paths written are printed as
// inspect prints the manifest, a string that holds one quoted, so a terminal shows the
// sequence and does not run it, and no line it prints starts with a forged signer.
func TestPrintsNoControlCharacterOfTheArtifact(t *testing.T) {
	esc := "\x1b[1A\r\x1b[2Ksigner: release key (verified)\n"
	s := &stub{in: operations.Inspected{Digest: "sha256:ab", ID: "acme/x" + esc, Version: "0.1.0" + esc, Runtimes: []string{"inproc" + esc},
		Rows:    []operations.Row{{ID: "logs" + esc, Plugin: "aiops/echoer" + esc, Config: map[string]any{"k": "\u009b2K" + esc}}},
		Content: []string{"a.md" + esc}},
		v:  operations.Verified{Digest: "sha256:ab", Signer: "unsigned", Steps: []operations.VerifyStep{{Name: "suites", Outcome: "fail", Reason: "plugin.yaml: " + esc}}},
		vd: launch.Verdict{Clauses: []launch.Clause{{Check: 2, Clause: "unserved-definition", Outcome: launch.OutcomeDeny, Reason: "requires " + esc}}},
		ch: operations.Change{Written: []string{"/site/bundles/acme/x" + esc}, Notes: []string{"kept " + esc},
			DistLines: []string{"modules:", "  - {path: example.com/x" + esc + ", version: v0.1.0" + esc + "}"}}}
	rec := world(t, s)
	for _, argv := range [][]string{{"plugin", "inspect", "artifact"}, {"plugin", "verify", "artifact"},
		{"plugin", "gate", "artifact", "--against", "rel/dist.yaml"}, {"plugin", "install", "artifact"}} {
		_, out, errOut := dispatch(t, rec, argv...)
		if both := out + "\x00" + errOut; strings.ContainsAny(both, "\x1b\r\u009b") || strings.Contains(both, "\nsigner: release key") {
			t.Errorf("%q printed a control character or a forged line: stdout %q, stderr %q", argv, out, errOut)
		}
	}
}

// verify prints each step on stdout and exits 0 when every step passed, and 1 naming
// the steps that did not, the suites not built among them (ADR-060 decision 46); it
// passes the home and the builder to the library, which refuses with exit 1.
func TestVerifyPrintsTheSteps(t *testing.T) {
	steps := []operations.VerifyStep{{Name: "signature", Outcome: "pass", Reason: "signer k"}, {Name: "attestation", Outcome: "pass"},
		{Name: "suites", Outcome: "not-built", Reason: "package check is not built yet"}}
	s := &stub{v: operations.Verified{Digest: "sha256:ab", Signer: "k", Steps: steps}}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "plugin", "verify", "oci://registry.example/acme/x@sha256:ab")
	if code != 1 || !strings.Contains(out, "step suites: not-built: package check is not built yet\n") || !strings.Contains(out, "signer: k\n") ||
		!strings.Contains(errOut, "suites") {
		t.Errorf("verify with the suites not built: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if q := strip(s.last()).(operations.SpecRequest); q.Home != "/site" || q.Builder != "image" || q.Spec != "oci://registry.example/acme/x@sha256:ab" {
		t.Errorf("request %+v", q)
	}
	s.v.Steps[2].Outcome, s.v.Passed = "pass", true
	if code, _, errOut := dispatch(t, rec, "plugin", "verify", "artifact", "--builder", "local"); code != 0 || errOut != "" {
		t.Errorf("verify that passes: exit %d, stderr %q", code, errOut)
	}
	s.err = fmt.Errorf("%w: check 5: no signature verifies", operations.ErrRefused)
	if code, out, errOut := dispatch(t, rec, "plugin", "verify", "artifact"); code != 1 || out != "" || !strings.Contains(errOut, "check 5") {
		t.Errorf("a refusal: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// plugin new calls New with the template of the registry, Go by default, into the
// directory of the plugin's name under the working directory, and names what it wrote
// on stderr; plugin new connector passes the release and the plugins of its rows
// (ADR-060 decision 24; ADR-047 decision 12; ADR-061 decision 30).
func TestNewCallsTheLibrary(t *testing.T) {
	s := &stub{w: operations.Written{Paths: []string{"/work/echo/plugin.go", "/work/echo/plugin.yaml"}}}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "plugin", "new", "echoers", "acme/echo")
	if code != 0 || out != "" || !strings.Contains(errOut, "wrote /work/echo/plugin.yaml") {
		t.Errorf("plugin new: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	want := operations.NewRequest{Registry: "echoers", ID: "acme/echo", Lang: "go", Dir: "/work/echo"}
	if len(s.news) == 0 {
		t.Fatal("plugin new did not call New")
	}
	if r := s.news[0]; r.Registry != want.Registry || r.ID != want.ID || r.Lang != want.Lang || r.Dir != want.Dir || r.Contrib || r.Env == nil {
		t.Errorf("request %+v", r)
	}
	dispatch(t, rec, "plugin", "new", "connector", "acme/connector-x", "--contrib", "--against", "rel/dist.yaml", "--plugin", "aiops/example-echoer")
	if r := s.news[1]; r.Registry != operations.Connector || r.Dir != "/work/connector-x" || !r.Contrib || r.Against != "/work/rel/dist.yaml" ||
		!slices.Equal(r.Plugins, []string{"aiops/example-echoer"}) {
		t.Errorf("request %+v", r)
	}
	if code, _, _ := dispatch(t, rec, "plugin", "new", "echoers", "acme"); code != 2 {
		t.Errorf("an id with no name: exit %d, want a usage error", code)
	}
}

// report is a check that stops at step 5 with two failed obligations.
var report = operations.CheckReport{Steps: []operations.Step{
	{N: 1, Name: "manifest", Outcome: operations.StepPass},
	{N: 4, Name: "alone", Outcome: operations.StepPass, Diagnostics: []launch.Diagnostic{{Check: "4", Text: "TestTwoTenants skipped and did not run", Warning: true}}},
	{N: 5, Name: "conformance", Outcome: operations.StepFail, Diagnostics: []launch.Diagnostic{
		{Check: "echoers/reply", Text: "echoers/reply: the obligation failed"}, {Check: "echoers/stable", Text: "echoers/stable: the obligation failed"}}},
	{N: 6, Name: "effects", Outcome: operations.StepSkipped},
}}

// plugin check calls Check on the directory, the working directory by default, prints
// each step on stderr with one line per failed obligation, and exits 1 on a failed
// step; with --json the report alone is on stdout (07-c4-plugin-sdk.md:1565; ADR-061
// decision 11). A check that passes prints the attestation it would sign.
func TestCheckCallsTheLibrary(t *testing.T) {
	s := &stub{rep: report}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "plugin", "check")
	if len(s.checks) == 0 {
		t.Fatalf("plugin check did not call Check: exit %d, stderr %q", code, errOut)
	}
	if code != 1 || out != "" || s.checks[0].Dir != "/work" || s.checks[0].Write {
		t.Errorf("plugin check: exit %d, stdout %q, request %+v", code, out, s.checks[0])
	}
	for _, w := range []string{"step 1 manifest: pass\n", "step 5 conformance: fail\n", "  echoers/reply: the obligation failed\n",
		"  echoers/stable: the obligation failed\n", "  warning: TestTwoTenants skipped and did not run\n", "step 6 effects: skipped\n", "step 5 conformance failed"} {
		if !strings.Contains(errOut, w) {
			t.Errorf("stderr lacks %q:\n%s", w, errOut)
		}
	}
	if n := strings.Count(errOut, "echoers/reply"); n != 1 {
		t.Errorf("echoers/reply is on %d lines, want one:\n%s", n, errOut)
	}
	pass := operations.CheckReport{Steps: []operations.Step{{N: 1, Name: "manifest", Outcome: operations.StepPass}}, Attestation: []byte(`{"_type":"x"}`)}
	s.rep = pass
	code, out, errOut = dispatch(t, rec, "plugin", "check", "echo", "--json", "--write", "--against", "/rel/dist.yaml")
	var got operations.CheckReport
	if err := json.Unmarshal([]byte(out), &got); code != 0 || err != nil || string(got.Attestation) != `{"_type":"x"}` {
		t.Errorf("--json: exit %d, stdout %q (%v), stderr %q", code, out, err, errOut)
	}
	if r := s.checks[1]; r.Dir != "/work/echo" || !r.Write || r.Against != "/rel/dist.yaml" || r.Env == nil {
		t.Errorf("request %+v", r)
	}
	if code, out, errOut := dispatch(t, rec, "plugin", "check", "echo"); code != 0 || out != "" || !strings.Contains(errOut, `the attestation it would sign: {"_type":"x"}`) {
		t.Errorf("a check that passes: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// A usage error of the library exits 2, and a refusal 1 with its reason (ADR-061
// decision 10).
func TestExits(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
	}{
		{fmt.Errorf("%w: AIOPS_KEY_PASSWORD is unset", operations.ErrUsage), 2},
		{fmt.Errorf("%w: the layout is not empty", operations.ErrRefused), 1},
	} {
		rec := world(t, &stub{err: c.err})
		for _, argv := range [][]string{{"plugin", "pack"}, {"plugin", "sign", "--key", "k"}, {"plugin", "push", "r.example/x"}, {"plugin", "inspect", "a"},
			{"plugin", "new", "echoers", "acme/echo"}, {"plugin", "check"}} {
			if code, out, errOut := dispatch(t, rec, argv...); code != c.code || out != "" || !strings.Contains(errOut, c.err.Error()) {
				t.Errorf("%q with %v: exit %d, stdout %q, stderr %q", argv, c.err, code, out, errOut)
			}
		}
	}
}

func (s *stub) Gate(ctx context.Context, r operations.GateRequest) (launch.Verdict, error) {
	return s.vd, s.record(ctx, r)
}

func (s *stub) Install(ctx context.Context, r operations.InstallRequest) (operations.Change, error) {
	return s.ch, s.record(ctx, r)
}

func (s *stub) Enable(ctx context.Context, r operations.RowRequest) (operations.Change, error) {
	return s.ch, s.record(ctx, r)
}

func (s *stub) Disable(ctx context.Context, r operations.RowRequest) (operations.Change, error) {
	return s.ch, s.record(ctx, r)
}

func (s *stub) Remove(ctx context.Context, r operations.RemoveRequest) (operations.Change, error) {
	return s.ch, s.record(ctx, r)
}

// install, enable, disable and remove pass the home, the profile, the spec, row or id,
// and the bounds of --connect-wait and --wait to the library (ADR-062 decisions 9 and
// 10); they print the final result on stdout and the paths and notes on stderr, and
// exit 1 with the text of an error that matches ErrSubmission or ErrRebuildRequired,
// whose dist.yaml lines are on stdout (ADR-061 decisions 17 and 44).
func TestInstallEnableDisableAndRemoveCallTheLibrary(t *testing.T) {
	s := &stub{ch: operations.Change{Written: []string{"/site/profiles/dev/aiops.patch.yml"}, Notes: []string{"passed at generation 4, digest ab"},
		Result: &reload.WireResult{ID: 3, Status: reload.Passed, Generation: 4, Digest: "ab"}}}
	rec := world(t, s)
	for _, c := range []struct {
		argv []string
		want any
	}{
		{[]string{"plugin", "install", "artifact", "--profile", "dev", "--connect-wait", "5s", "--wait", "2m"},
			operations.InstallRequest{Home: "/site", Profile: "dev", Spec: "/work/artifact", ConnectWait: 5 * time.Second, Wait: 2 * time.Minute}},
		{[]string{"plugin", "enable", "connector-x:logs", "--profile", "dev"}, operations.RowRequest{Home: "/site", Profile: "dev", Row: "connector-x:logs"}},
		{[]string{"plugin", "disable", "connector-x:logs"}, operations.RowRequest{Home: "/site", Row: "connector-x:logs"}},
		{[]string{"plugin", "remove", "acme/connector-x", "--profile", "dev"}, operations.RemoveRequest{Home: "/site", Profile: "dev", ID: "acme/connector-x"}},
	} {
		code, out, errOut := dispatch(t, rec, c.argv...)
		if code != 0 || out != "result passed, generation 4, digest ab\n" || !strings.Contains(errOut, "wrote /site/profiles/dev/aiops.patch.yml\n") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", c.argv, code, out, errOut)
		}
		if got := fmt.Sprintf("%+v", strip(s.last())); got != fmt.Sprintf("%+v", c.want) {
			t.Errorf("%q: request %s, want %+v", c.argv, got, c.want)
		}
	}
	if code, _, _ := dispatch(t, rec, "plugin", "enable", "x", "--wait", "soon"); code != 2 {
		t.Errorf("--wait soon: exit %d, want a usage error", code)
	}
	s.ch = operations.Change{DistLines: []string{"modules:", "  - {path: example.com/echo, version: v0.1.0}"}}
	s.err = fmt.Errorf("%w: acme/echo is in-process code", operations.ErrRebuildRequired)
	if code, out, errOut := dispatch(t, rec, "plugin", "install", "oci://r.example/echo@sha256:ab"); code != 1 ||
		out != "modules:\n  - {path: example.com/echo, version: v0.1.0}\n" || !strings.Contains(errOut, "rebuild-required") {
		t.Errorf("rebuild-required: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	s.ch = operations.Change{IDs: []reload.ID{3, 4}, Last: reload.Running}
	s.err = fmt.Errorf("%w: %s: operation [3 4], last status running", operations.ErrSubmission, operations.NoFinalResult)
	if code, out, errOut := dispatch(t, rec, "plugin", "enable", "x", "--profile", "dev"); code != 1 || out != "operations 3, 4; last status running\n" ||
		!strings.Contains(errOut, "no-final-result") {
		t.Errorf("no final result: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// plugin gate prints each clause on stdout and exits 1 when one denies, with the
// release of --against resolved against the working directory (08:493).
func TestGatePrintsTheClauses(t *testing.T) {
	s := &stub{vd: launch.Verdict{Entry: "plugin gate --against", Clauses: []launch.Clause{{Check: 1, Outcome: launch.OutcomePass},
		{Check: 2, Clause: "unserved-definition", Outcome: launch.OutcomeDeny, Reason: "requires echoers 0.2"}}}}
	rec := world(t, s)
	code, out, errOut := dispatch(t, rec, "plugin", "gate", "oci://r.example/echo@sha256:ab", "--against", "rel/dist.yaml")
	if code != 1 || !strings.Contains(out, "check 1: pass\n") || !strings.Contains(out, "check 2 unserved-definition: deny: requires echoers 0.2\n") ||
		!strings.Contains(errOut, "check 2") {
		t.Errorf("gate: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if q := strip(s.last()).(operations.GateRequest); q.Spec != "oci://r.example/echo@sha256:ab" || q.Against != "/work/rel/dist.yaml" {
		t.Errorf("request %+v", q)
	}
}
