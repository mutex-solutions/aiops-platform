// SPDX-License-Identifier: Apache-2.0

package failurematrix

// The harness: it runs every cell of matrix.yaml whose since is S1 on the built test
// binary (test/dist/cmd/aiops-test), once for each fault the cell names, and asserts
// the cell's expect (ADR-059 decision 3: S1 passes every cell marked S1 and may skip
// none; a cell of a later step is "not yet" and is never a pass).
//
// HOW A COLUMN ENTERS THE PROCESS (08-profiles-roadmap-adrs.md section 1.6). Each
// scenario is a profile ("smoke", or a variant of it), layer 4 patch files from
// test/dist/patches, a fresh home and the short limits of harness.Limits.
//
//	| Column               | Procedure                                                       |
//	|----------------------|-----------------------------------------------------------------|
//	| Optional row at boot | the fault is an optional row (id "fault", with a row "dependant" |
//	|                      | that injects what it provides) in a --patch file; or, for a      |
//	|                      | layer failure, a missing --patch file. Run until it exits or     |
//	|                      | the launcher says it is running. A fault that is an installed    |
//	|                      | artifact is in the home before boot (installFault), and a patch  |
//	|                      | enables the row "fault" that its bundle inserted disabled.       |
//	| Required at boot     | the same with "required: true" on the row; for a layer failure   |
//	|                      | a profile that names a bundle that does not exist.               |
//	| Live recomposition   | the process boots healthy with a live layer file as the last     |
//	|                      | --patch; the test writes the fault into that file and submits    |
//	|                      | one generation through test-submit (the control file); the       |
//	|                      | verdict is read from the state file of test-state. A fault that  |
//	|                      | is a failure after the generation passed (a tracked goroutine, a |
//	|                      | give-up) has a trigger: the test creates run/fault.go once the   |
//	|                      | generation has passed, as in the next column. A fault that fails |
//	|                      | the verdict is rolled back, and the cell says so.                |
//	| While running        | the process boots with the fault latent in a row; once it is     |
//	|                      | running the test creates run/fault.go, the trigger of the fault  |
//	|                      | plugin (a provider fails, a goroutine panics). A fault that is   |
//	|                      | an unload (a disposer or a drain that passes its deadline) is    |
//	|                      | made by a live generation that disables the row: no other event  |
//	|                      | unloads a running fiber without a failure. Where the outcome has |
//	|                      | the provider come back, the test then removes run/fault.go, and  |
//	|                      | the next retry of the provider succeeds.                         |
//	| Tenant tree          | not yet: S4b (the column has no cell of S1)                      |
//
// WHAT IS ASSERTED. exit "running": the launcher's status says phase running, never
// that the process exited, and then the readiness latch, the fiber state of each named
// row (the state file of test-state) and the diagnostic hold in one snapshot, within a
// deadline. exit "nonzero": the process exits, with a code that the scenario fixes
// when boot/run.go does (3 for a required set that is not met, 1 for a refusal, 4 for a
// leak, 5 for a give-up), the diagnostic is in its output, the states are those the
// required-set audit printed (the process is gone, so the state file only says
// DISPOSED), and readiness is not claimed by a final status. A launcher that
// crashed on a Go panic leaves no final status; a dead process is not ready. An outcome
// that one process cannot show whole is run twice, the second time with an expect of
// the harness: an infrastructure provider that fails while running waits an hour for
// its retry in the first run, and in the second, with the short backoff of the smoke
// bundle, restart-policy retries it until it gives up and the process exits with 5.
//
// THE DIAGNOSTIC is a substring of what the process printed (stdout and stderr, which
// hold the log) and, for a live recomposition, of the verdict of reload that test-state
// writes: "reload op <id>: <status> at <stage>: <error>; <row>=<outcome> ...". Reload
// logs nothing itself.
//
// "fault", "dependant" and "required" are row ids of the patch files of test/dist.

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/test/dist/harness"
	"github.com/mutex-solutions/aiops-platform/test/dist/plugins/teststate"
)

func TestMain(m *testing.M) {
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

// deviation is a known failure: why in one line, and the text that the failure must
// show among its problems, so that a cell that fails for another reason is not hidden
// by its entry.
type deviation struct{ reason, problem string }

// knownDeviations are the S1 cells whose real behaviour contradicts the outcome the
// table documents. The cell id is "<row>.<column>/<fault>" (row and column are the
// one-based positions in the table, "/<fault>" is left out for a cell with one fault
// "none"). It is empty: the owner decided the twelve cells that were listed here on
// 2026-11-20 (ADR-059 decisions 12 to 15) and each now does what the table says. An entry
// makes the suite report the cell as a deviation and fail; a cell that is listed and
// passes is an error: remove the entry.
var knownDeviations = map[string]deviation{}

// cellBudget bounds the run of one (cell, fault).
const cellBudget = 30 * time.Second

// Column kinds.
const (
	colOpt  = "Optional row at boot"
	colReq  = "Required at boot"
	colLive = "Live recomposition"
	colRun  = "While running"
	colTen  = "Tenant tree"
)

// failure rows by the start of their text in the table.
var rowKeys = []struct{ key, prefix string }{
	{"files", "Profile, bundle or"},
	{"unknown", "Plugin id not in"},
	{"gate", "Gate or signature"},
	{"config", "Config fails"},
	{"static", "Static graph error"},
	{"apply", "`Apply` errors"},
	{"noprov", "An injected service"},
	{"infra", "An infrastructure provider"},
	{"tracked", "A tracked goroutine"},
	{"untracked", "An untracked goroutine"},
	{"leak", "A process-scope drain"},
	{"giveup", "`restart-policy` gives up"},
}

func rowKey(failure string) string {
	for _, r := range rowKeys {
		if strings.HasPrefix(failure, r.prefix) {
			return r.key
		}
	}
	return ""
}

// plan is how one (row, column, fault) is run.
type plan struct {
	profile string   // default smoke
	base    []string // patch files of test/dist/patches at boot
	missing bool     // add a --patch file that does not exist
	live    string   // a live generation: the patch file whose content is written to the live layer
	trigger bool     // once running (and, with live, once the generation passed), create run/fault.go
	code    int      // the exit code that boot/run.go fixes for this exit; 0: any non-zero
	env     []string // extra environment variables of the process, NAME=value
	// refsFile: a file of that name in a fresh --refs-dir, written before boot and removed
	// before the live generation is submitted, so a ${file:} reference to it resolves at
	// boot and no longer in the generation.
	refsFile string
	// rolledBack: the live generation's failed verdict must leave the tree as it was,
	// "the composer rolls the scope back" ("Rolled back").
	rolledBack bool
	// rejected: the live generation must be rejected at admission with nothing touched,
	// "Rejected; the row keeps its instance" (ADR-059 decision 16): the process boots to a
	// settled tree first, and every row and every kernel fiber is then as it was.
	rejected bool
	// heal: once the cell's expect holds, remove run/fault.go, so the fault recovers at
	// its next retry; the fault and every row of the expect must then be ACTIVE again
	// ("they return with the provider").
	heal bool
	// label: the label that the kernel's record of the leak must name ("Logged with its
	// label"): the effect or tracked goroutine of the fault that is still open, which
	// boot's log handler prints as label=<label>.
	label string
	// then are more runs of the cell, each with an expect of its own, for the parts of the
	// outcome that the first run cannot show in the same process.
	then []variant
	// install: the fault plugin that the fresh home holds as an installed artifact
	// (installFault).
	install string
}

// leakLabel is the label of the open work of each leak fault (test/dist/plugins/<fault>).
var leakLabel = map[string]string{"leaky-goroutine": "leaky", "slow-dispose": "slow-dispose"}

type variant struct {
	name string
	plan
	expect boot.Expect
}

// rejectedLive is a run of the live column whose generation is rejected with nothing
// touched, with the diagnostic that says why.
func rejectedLive(name, diagnostic string, base []string, live string, env ...string) variant {
	return variant{name, plan{base: base, live: live, env: env, rejected: true},
		boot.Expect{Exit: "running", Ready: "true", Diagnostic: diagnostic}}
}

// withRefsFile is v with the --refs-dir file name of plan.refsFile.
func withRefsFile(name string, v variant) variant {
	v.refsFile = name
	return v
}

// liveBase is what the live layer file holds at boot: a valid layer that changes nothing.
const liveBase = "- {id: extra, enabled: false}\n"

// planFor returns the scenario for a cell and one of its faults, and false when this
// harness has none (the test then fails: no S1 cell may be skipped).
func planFor(row, col, fault string) (plan, bool) {
	opt, req := fault+".optional", fault+".required"
	switch row {
	case "files":
		switch col {
		case colOpt:
			return plan{missing: true, code: boot.ExitRefused}, true
		case colReq:
			return plan{profile: "smoke-missing-bundle", code: boot.ExitRefused}, true
		case colLive:
			return plan{live: "layer-malformed.yml"}, true
		}
	case "unknown":
		return byColumn(col, plan{base: []string{"unknown-plugin.optional.yml"}},
			plan{base: []string{"unknown-plugin.required.yml"}, code: boot.ExitRefused},
			plan{live: "unknown-plugin.optional.yml"}, plan{}), true
	case "gate":
		if fault != "rogue-provider" { // an installed artifact, whose row a patch enables
			return byColumn(col, plan{install: fault, base: []string{"fault-enabled.yml"}},
				plan{install: fault, base: []string{"fault-enabled.required.yml"}, code: boot.ExitRefused},
				plan{install: fault, live: "fault-enabled.yml"}, plan{}), true
		}
		return byColumn(col, plan{base: []string{"rogue-provider.optional.yml"}},
			plan{base: []string{"rogue-provider.required.yml"}, code: boot.ExitRefused},
			plan{live: "rogue-provider.optional.yml"}, plan{}), true
	case "config":
		// Live: the generation is rejected with nothing touched whether it changes, creates
		// or enables the row, whether the config fails Decode or holds a reference that does
		// not resolve, for the child of a group it replaces, and for any reference while
		// config-refs is not ACTIVE (ADR-059 decision 16). Disabling config-refs is a valid
		// profile while no required row holds a reference (decision 18) and, by decision
		// 24, while no active provider of a required key in the root realm holds one.
		return byColumn(col, plan{base: []string{"bad-config.optional.yml"}},
			plan{base: []string{"bad-config.required.yml"}, code: boot.ExitRequired},
			plan{base: []string{"bad-config.ok.yml"}, live: "bad-config.change.yml", rejected: true, then: []variant{
				rejectedLive("a created row whose config fails Decode", "invalid config", nil, "bad-config.optional.yml"),
				rejectedLive("an enabled row whose config fails Decode", "invalid config", []string{"bad-config.disabled.yml"}, "fault-enabled.yml"),
				rejectedLive("a created row with a reference that does not resolve", "does not resolve", nil, "refs-unresolved.optional.yml"),
				rejectedLive("an enabled row with a reference that does not resolve", "does not resolve", []string{"refs-unresolved.disabled.yml"}, "fault-enabled.yml"),
				rejectedLive("a row given a first reference that does not resolve", "does not resolve", []string{"refs-literal.yml"}, "refs-unresolved.change.yml"),
				// The child's config does not change: only the replacement of its group
				// makes the generation check its reference, which no longer resolves.
				withRefsFile("note", rejectedLive("the child of a replaced group with a reference that does not resolve", "does not resolve",
					[]string{"refs-group.yml"}, "refs-unresolved.group.yml")),
				rejectedLive("a created row with a reference while config-refs is disabled", "rejected", []string{"refs-disabled.yml"}, "refs-env.optional.yml", "AIOPS_REF_NOTE=from-env"),
				// The raw reference decodes and its resolved value does not: only a check of
				// the resolved config refuses it.
				rejectedLive("a created row whose resolved reference fails Decode", "invalid config", nil, "bad-config.refs.optional.yml", "AIOPS_REF_NOTE=refused"),
				rejectedLive("a created row with a reference in the generation that disables config-refs", "config-refs changes in this generation",
					nil, "refs-env.refs-disabled.yml", "AIOPS_REF_NOTE=from-env"),
			}}, plan{}), true
	case "static":
		return byColumn(col, plan{base: []string{"rogue-provider.isolate.yml"}, code: boot.ExitRefused},
			plan{base: []string{"rogue-provider.isolate.required.yml"}, code: boot.ExitRefused},
			plan{live: "rogue-provider.isolate.yml"}, plan{}), true
	case "apply":
		return byColumn(col, plan{base: []string{opt + ".yml"}},
			plan{base: []string{req + ".yml"}, code: boot.ExitRequired},
			plan{live: opt + ".yml", rolledBack: true}, plan{}), true
	case "noprov":
		return byColumn(col, plan{base: []string{"no-provider.optional.yml"}},
			plan{base: []string{"no-provider.required.yml"}, code: boot.ExitRefused},
			plan{live: "no-provider.optional.yml"},
			plan{base: []string{"fail-apply.trigger.optional.yml", "restart-policy.patient.yml"}, trigger: true, heal: true}), true
	case "infra":
		// restart-policy lists toy.fault among its infrastructure keys. The live generation
		// has a slow row, so reload judges it after restart-policy could have retried the
		// fault to its limit; restart-policy holds its retries and its give-up while the
		// operation is in flight (ADR-059 decision 19), so the process is not replaced.
		// While running, the first run sees the fault wait for its retry; the second, with
		// the short backoff, sees restart-policy give up and the process replaced.
		slow, fast := "restart-policy.infra.slow.yml", "restart-policy.infra.yml"
		trig := "fail-apply.trigger.required.yml"
		return byColumn(col, plan{},
			plan{base: []string{slow, "fail-apply.required.yml"}, code: boot.ExitRequired},
			plan{base: []string{fast}, live: "fail-apply.required.slow-row.yml", rolledBack: true},
			plan{base: []string{slow, trig}, trigger: true, then: []variant{{"the give-up",
				plan{base: []string{fast, trig}, trigger: true, code: boot.ExitGaveUp},
				boot.Expect{Exit: "nonzero", Ready: "false", Diagnostic: "infrastructure=true"}}}}), true
	case "tracked":
		return byColumn(col, plan{base: []string{"panic-apply-tracked.optional.yml"}},
			plan{base: []string{"panic-apply-tracked.required.yml"}, code: boot.ExitRequired},
			plan{base: []string{"restart-policy.slow.yml"}, live: "panic-apply-tracked.trigger.required.yml", trigger: true},
			plan{base: []string{"panic-apply-tracked.trigger.required.yml", "restart-policy.slow.yml"}, trigger: true}), true
	case "untracked":
		return byColumn(col, plan{base: []string{"panic-apply-untracked.optional.yml"}, code: 2},
			plan{base: []string{"panic-apply-untracked.required.yml"}, code: 2},
			plan{live: "panic-apply-untracked.optional.yml", code: 2},
			plan{base: []string{"panic-apply-untracked.trigger.optional.yml"}, trigger: true, code: 2}), true
	case "leak":
		p := byColumn(col, plan{base: []string{fault + ".fail-apply.optional.yml"}, code: boot.ExitLeak},
			plan{base: []string{fault + ".fail-apply.required.yml"}, code: boot.ExitRequired},
			plan{live: fault + ".fail-apply.optional.yml", code: boot.ExitLeak},
			plan{base: []string{opt + ".yml"}, live: "fault-disabled.yml", code: boot.ExitLeak})
		p.label = leakLabel[fault]
		return p, true
	case "giveup":
		return byColumn(col, plan{base: []string{"fail-apply.optional.yml"}},
			plan{base: []string{"fail-apply.required.yml"}, code: boot.ExitRequired},
			plan{live: "fail-apply.trigger.required.yml", trigger: true},
			plan{base: []string{"fail-apply.trigger.required.yml"}, trigger: true}), true
	}
	return plan{}, false
}

func byColumn(col string, opt, req, live, run plan) plan {
	switch col {
	case colOpt:
		return opt
	case colReq:
		return req
	case colLive:
		return live
	}
	return run
}

// result of one run.
type result struct {
	problems []string // empty: the cell passed
	output   string
}

var stateLine = regexp.MustCompile(`"([a-z-]+)" is ([A-Z]+)`)

// reloadText renders the last operation of reload as the diagnostic corpus.
func reloadText(s harness.Snapshot) string {
	op := s.State.Reload
	if op == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "reload op %d: %s at %s: %s;", op.ID, op.Status, op.Stage, op.Error)
	for _, v := range op.Verdict {
		fmt.Fprintf(&b, " %s=%s(%s %s)", v.ID, v.Outcome, v.State, v.Detail)
	}
	for _, d := range op.Denied {
		fmt.Fprintf(&b, " denied=%s", d)
	}
	return b.String()
}

// check returns what in s does not match e, for a process that is running.
func checkRunning(e boot.Expect, p plan, s harness.Snapshot) []string {
	var bad []string
	wantReady := e.Ready == "true"
	switch {
	case s.Exited:
		return []string{fmt.Sprintf("the process exited with %d, expect: running", s.Code)}
	case !s.Running():
		bad = append(bad, "the launcher is not in phase running")
	case s.Status.Ready != wantReady:
		bad = append(bad, fmt.Sprintf("ready is %v, expect %v", s.Status.Ready, wantReady))
	}
	if p.live != "" && !s.OpDone(1) {
		bad = append(bad, "the live generation has no verdict yet")
	}
	for _, st := range e.States {
		if got := s.RowState(st.Row); got != st.State {
			bad = append(bad, fmt.Sprintf("row %s is %q, expect %s", st.Row, got, st.State))
		}
	}
	if !strings.Contains(s.Output+"\n"+reloadText(s), e.Diagnostic) {
		bad = append(bad, fmt.Sprintf("the diagnostic %q is not in the output or the verdict", e.Diagnostic))
	}
	return bad
}

// checkExited returns what does not match e, for a process that exited.
func checkExited(e boot.Expect, p plan, s harness.Snapshot) []string {
	var bad []string
	switch {
	case !s.Exited:
		return []string{"the process did not exit, expect: nonzero"}
	case s.Code == 0:
		bad = append(bad, "the process exited with 0, expect non-zero")
	case p.code != 0 && s.Code != p.code:
		bad = append(bad, fmt.Sprintf("the process exited with %d, boot/run.go fixes %d here", s.Code, p.code))
	}
	if s.HaveStatus && s.Status.Phase == "stopped" && s.Status.Ready {
		bad = append(bad, "the final status says ready")
	}
	if !strings.Contains(s.Output+"\n"+reloadText(s), e.Diagnostic) {
		bad = append(bad, fmt.Sprintf("the diagnostic %q is not in the output", e.Diagnostic))
	}
	if p.label != "" && !strings.Contains(s.Output, "label="+p.label) {
		bad = append(bad, fmt.Sprintf("no record of the leak names its label %q", p.label))
	}
	seen := map[string]string{}
	for _, m := range stateLine.FindAllStringSubmatch(s.Output, -1) {
		seen[m[1]] = m[2]
	}
	for _, st := range e.States {
		if !slices.Contains(strings.Split(st.State, "|"), seen[st.Row]) {
			bad = append(bad, fmt.Sprintf("the required-set audit says row %s is %q, expect %s", st.Row, seen[st.Row], st.State))
		}
	}
	return bad
}

// rolledBackProblems says whether the live generation was rolled back and the tree is what
// it was before it: reload says "rolled back" with a rollback that did not fail, numbered
// after the failed generation, and the rows that generation created are gone.
func rolledBackProblems(s harness.Snapshot) []string {
	op := s.State.Reload
	if op == nil || op.Status != "rolled back" || op.Rollback == nil {
		return []string{"expected the failed verdict to be rolled back, but the operation is not"}
	}
	var bad []string
	if op.Rollback.Error != "" || op.Rollback.Generation != op.Generation+1 {
		bad = append(bad, fmt.Sprintf("the rollback is %+v, want no error and generation %d", *op.Rollback, op.Generation+1))
	}
	for _, r := range s.State.Rows {
		if (r.ID == "fault" || r.ID == "dependant" || r.ID == "slow") && r.Mount != "unknown" {
			bad = append(bad, fmt.Sprintf("not rolled back: row %s of the failed generation is still in the tree (%s %s)", r.ID, r.Mount, r.State))
		}
	}
	return bad
}

// settled says whether every mounted row is ACTIVE and the loader's rows are the kernel's
// fibers: a tree that changes no more by itself.
func settled(s harness.Snapshot) bool {
	for _, r := range s.State.Rows {
		if r.Mount == "mounted" && r.State != "ACTIVE" {
			return false
		}
	}
	return len(s.TreeProblems()) == 0
}

// rejectedProblems says whether the live generation was rejected with nothing touched:
// reload rejected it at admit, applied nothing and rolled nothing back, and every row and
// every kernel fiber is what it was in before, the settled tree it was submitted to.
func rejectedProblems(before, s harness.Snapshot) []string {
	op := s.State.Reload
	if op == nil || op.Status != "rejected" || op.Stage != "admit" || len(op.Applied) > 0 || op.Rollback != nil {
		var applied map[string][]string
		if op != nil {
			applied = op.Applied
		}
		return []string{fmt.Sprintf("expected a rejection at admit with nothing applied (ADR-059 decision 16): %s applied=%v", reloadText(s), applied)}
	}
	var bad []string
	if !slices.Equal(before.State.Rows, s.State.Rows) {
		bad = append(bad, fmt.Sprintf("the rows changed: %+v, then %+v", before.State.Rows, s.State.Rows))
	}
	byID := func(a, b teststate.Fiber) int { return cmp.Compare(a.ID, b.ID) }
	was, now := slices.SortedFunc(slices.Values(before.Kernel), byID), slices.SortedFunc(slices.Values(s.Kernel), byID)
	if !slices.Equal(was, now) {
		bad = append(bad, fmt.Sprintf("the kernel's fibers changed: %v, then %v", was, now))
	}
	return bad
}

func runScenario(bin string, e boot.Expect, p plan) result {
	opts := harness.Options{Profile: p.profile, Env: p.env}
	if p.install != "" {
		opts.Install = func(home string) error { return installFault(home, p.install) }
	}
	var livePath string
	for _, f := range p.base {
		opts.Patches = append(opts.Patches, harness.PatchFile(f))
	}
	if p.missing {
		opts.Patches = append(opts.Patches, filepath.Join(os.TempDir(), "aiops-matrix-no-such-patch.yml"))
	}
	var dir string
	if p.live != "" {
		d, err := os.MkdirTemp("", "aiops-matrix-live-")
		if err != nil {
			return result{problems: []string{err.Error()}}
		}
		dir = d
		defer os.RemoveAll(dir)
		livePath = filepath.Join(dir, "live.yml")
		if err := os.WriteFile(livePath, []byte(liveBase), 0o644); err != nil {
			return result{problems: []string{err.Error()}}
		}
		opts.Patches = append(opts.Patches, livePath)
	}
	var refsFile string
	if p.refsFile != "" {
		d, err := os.MkdirTemp("", "aiops-matrix-refs-")
		if err != nil {
			return result{problems: []string{err.Error()}}
		}
		defer os.RemoveAll(d)
		opts.RefsDir, refsFile = d, filepath.Join(d, p.refsFile)
		if err := os.WriteFile(refsFile, []byte("from-file"), 0o600); err != nil {
			return result{problems: []string{err.Error()}}
		}
	}
	proc, err := harness.Start(bin, opts)
	if err != nil {
		return result{problems: []string{err.Error()}}
	}
	defer proc.Release()
	fail := func(format string, a ...any) result {
		return result{problems: []string{fmt.Sprintf(format, a...)}, output: proc.Output()}
	}
	deadline := time.Now().Add(cellBudget)
	left := func() time.Duration { return max(time.Until(deadline), time.Millisecond) }

	needsBoot := p.live != "" || p.trigger
	var before harness.Snapshot
	if needsBoot {
		// The process must be running healthy before the live generation or the
		// trigger. A boot that fails here is a failed cell, not a pass.
		s, ok := proc.Until(left(), func(s harness.Snapshot) bool {
			return s.Running() && s.State.Ready && (!p.rejected || settled(s))
		})
		if !ok {
			return result{problems: []string{"the process did not boot healthy before the fault: " + status(s)}, output: s.Output}
		}
		before = s
	}
	if p.live != "" {
		if refsFile != "" {
			if err := os.Remove(refsFile); err != nil {
				return fail("%v", err)
			}
		}
		body, err := os.ReadFile(harness.PatchFile(p.live))
		if err != nil {
			return fail("%v", err)
		}
		if err := os.WriteFile(livePath, body, 0o644); err != nil {
			return fail("%v", err)
		}
		if err := proc.Submit("1"); err != nil {
			return fail("%v", err)
		}
		if p.trigger {
			// A fault that fails after the generation passed: the trigger comes once the
			// verdict is in, and the verdict must be pass.
			s, ok := proc.Until(left(), func(s harness.Snapshot) bool { return s.OpDone(1) })
			if !ok || s.State.Reload.Status != "passed" {
				return result{problems: []string{"the live generation did not pass before the trigger: " + reloadText(s)}, output: s.Output}
			}
		}
	}
	if p.trigger {
		if err := proc.Touch("fault.go"); err != nil {
			return fail("%v", err)
		}
	}

	var last []string
	var snap harness.Snapshot
	if e.Exit == "running" {
		snap, _ = proc.Until(left(), func(s harness.Snapshot) bool {
			last = checkRunning(e, p, s)
			if len(last) == 0 && p.rolledBack {
				last = rolledBackProblems(s)
			}
			if p.rejected { // also when the diagnostic is missing: what reload did says why
				last = append(last, rejectedProblems(before, s)...)
			}
			if len(last) == 0 && p.live != "" { // the loader's rows are the kernel's fibers
				last = s.TreeProblems()
			}
			return len(last) == 0
		})
		if p.heal && len(last) == 0 {
			if err := os.Remove(filepath.Join(proc.RunDir(), "fault.go")); err != nil {
				return fail("%v", err)
			}
			healed := e
			healed.States = []boot.StateExpect{{Row: "fault", State: "ACTIVE"}}
			for _, st := range e.States {
				healed.States = append(healed.States, boot.StateExpect{Row: st.Row, State: "ACTIVE"})
			}
			snap, _ = proc.Until(left(), func(s harness.Snapshot) bool {
				last = checkRunning(healed, p, s)
				return len(last) == 0
			})
			for i := range last {
				last[i] = "once run/fault.go was removed: " + last[i]
			}
		}
	} else {
		snap, _ = proc.Until(left(), func(s harness.Snapshot) bool {
			last = checkExited(e, p, s)
			return s.Exited && len(last) == 0
		})
		if !snap.Exited {
			last = append(last, "the process did not exit within the budget")
		}
	}
	out := snap.Output + "\n" + reloadText(snap)
	if why := faultDenial[p.install]; !strings.Contains(out, why) {
		last = append(last, fmt.Sprintf("the denial does not say %q: the artifact carries another defect", why))
	}
	return result{problems: last, output: out}
}

func status(s harness.Snapshot) string {
	return fmt.Sprintf("exited=%v code=%d status=%+v ready=%v", s.Exited, s.Code, s.Status, s.State.Ready)
}

// cellRun is the outcome of one cell, over its faults.
type cellRun struct {
	id        string
	mark      string // pass, FAIL, not yet, n/a
	deviation bool
}

func TestFailureMatrix(t *testing.T) {
	if testing.Short() {
		t.Log(harness.SkipMessage)
		t.Skip(harness.SkipMessage)
	}
	data, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := boot.ParseMatrix(matrixPath, data)
	if err != nil {
		t.Fatal(err)
	}
	bin := harness.Binary(t)
	workers := min(runtime.GOMAXPROCS(0), 4)
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	active := map[string]bool{}
	runs := make([]cellRun, len(m.Cells))
	var rowOrder []string
	rowIdx := map[string]int{}
	for i := range m.Cells {
		f := m.Cells[i].Failure
		if _, ok := rowIdx[f]; !ok {
			rowIdx[f] = len(rowOrder) + 1
			rowOrder = append(rowOrder, f)
		}
	}
	colIdx := map[string]int{colOpt: 1, colReq: 2, colLive: 3, colRun: 4, colTen: 5}

	t.Run("cells", func(t *testing.T) {
		for i, c := range m.Cells {
			id := fmt.Sprintf("%d.%d", rowIdx[c.Failure], colIdx[c.Column])
			run := cellRun{id: id}
			switch {
			case c.Since == boot.NotApplicable:
				run.mark = "n/a"
				runs[i] = run
				continue
			case c.Since != "S1":
				run.mark = "not yet (" + c.Since + ")"
				runs[i] = run
				continue
			}
			key := rowKey(c.Failure)
			for _, fault := range c.Fault {
				cid := id
				if fault != "none" {
					cid += "/" + fault
				}
				name := fmt.Sprintf("%s_%s_%s", strings.ReplaceAll(cid, "/", "_"), key, strings.ReplaceAll(c.Column, " ", "_"))
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					sem <- struct{}{}
					defer func() { <-sem }()
					p, ok := planFor(key, c.Column, fault)
					var res result
					if !ok {
						res = result{problems: []string{"the harness has no procedure for this cell: an S1 cell may not be skipped"}}
					} else {
						res = runScenario(bin, c.Expect, p)
						for _, v := range p.then {
							r := runScenario(bin, v.expect, v.plan)
							for _, pr := range r.problems {
								res.problems = append(res.problems, v.name+": "+pr)
							}
							if len(r.problems) > 0 {
								res.output += "\n--- output of the run " + v.name + ":\n" + r.output
							}
						}
					}
					dv, known := knownDeviations[cid]
					if known && len(res.problems) > 0 && !strings.Contains(strings.Join(res.problems, "\n"), dv.problem) {
						known = false // it fails, but not in the way that the entry says
					}
					mu.Lock()
					defer mu.Unlock()
					r := &runs[i]
					r.id = id
					switch {
					case len(res.problems) == 0 && known:
						r.mark = "FAIL"
						t.Errorf("cell %s passes, but knownDeviations lists it (%s): remove the entry", cid, dv.reason)
					case len(res.problems) == 0:
						if r.mark == "" {
							r.mark = "pass"
						}
					case known:
						r.mark, r.deviation = "FAIL", true
						active[cid] = true
						t.Logf("KNOWN DEVIATION %s: %s\n%s\noutput:\n%s", cid, dv.reason, strings.Join(res.problems, "\n"), res.output)
					default:
						r.mark = "FAIL"
						t.Errorf("cell %s (%q, %s, fault %s) failed:\n  %s\noutput:\n%s", cid, c.Failure, c.Column, fault, strings.Join(res.problems, "\n  "), res.output)
					}
					if os.Getenv("MATRIX_VERBOSE") != "" {
						t.Logf("cell %s output:\n%s", cid, res.output)
					}
				})
			}
		}
	})

	// Summary: the parallel subtests have all ended.
	var b strings.Builder
	counts := map[string]int{}
	cols := []string{colOpt, colReq, colLive, colRun, colTen}
	fmt.Fprintf(&b, "\nfailure matrix: %d rows x %d columns, %d cells\n", len(rowOrder), len(cols), len(m.Cells))
	fmt.Fprintf(&b, "%-4s %-60s", "row", "failure")
	for _, c := range cols {
		fmt.Fprintf(&b, " | %-14s", c)
	}
	b.WriteString("\n")
	for r, f := range rowOrder {
		short := strings.NewReplacer("`", "").Replace(f)
		if len(short) > 58 {
			short = short[:55] + "..."
		}
		fmt.Fprintf(&b, "%-4d %-60s", r+1, short)
		for _, col := range cols {
			mark := "?"
			for i, c := range m.Cells {
				if c.Failure == f && c.Column == col {
					mark = runs[i].mark
				}
			}
			label := mark
			if strings.HasPrefix(mark, "not yet") {
				counts["not yet"]++
			} else {
				counts[mark]++
			}
			fmt.Fprintf(&b, " | %-14s", label)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "cells: %d pass, %d FAIL, %d not yet, %d n/a\n", counts["pass"], counts["FAIL"], counts["not yet"], counts["n/a"])
	var ids []string
	for id := range active {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) > 0 {
		b.WriteString("known deviations from the documented outcomes (cell id: reason):\n")
		for _, id := range ids {
			fmt.Fprintf(&b, "  %s: %s\n", id, knownDeviations[id].reason)
		}
	}
	t.Log(b.String())
	if len(ids) > 0 {
		t.Errorf("%d known deviation(s) from the documented outcomes; the suite fails until they are fixed", len(ids))
	}
}
