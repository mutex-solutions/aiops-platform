// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mutex-solutions/aiops-platform/test/spike/s0/scale"
)

// Config selects one execution. Dry never satisfies the gate.
type Config struct {
	Plan     Plan
	Dry      bool
	Out      string // Fresh directory; created exclusively.
	Identity *Identity
	Root     string // Repository root relative to the package directory.
	Backends []scale.Backend
	Progress func(string)
}

// Report is the summary written last; raw per-run files are the evidence.
type Report struct {
	Schema         string               `json:"schema"`
	Protocol       int                  `json:"protocolAmendment"`
	Label          string               `json:"label"`
	Acceptance     bool                 `json:"acceptanceEligible"`
	StartedUTC     time.Time            `json:"startedUTC"`
	EndedUTC       time.Time            `json:"endedUTC"`
	Classification scale.Classification `json:"classification"`
	Reasons        []string             `json:"reasons"`
	Plan           Plan                 `json:"plan"`
	Warmup         []WarmupSummary      `json:"warmup"` // Amendment 2: recorded and listed, never classified.
	Aggregates     []Aggregate          `json:"aggregates"`
	Stress         []StressSummary      `json:"stress"`
	Hosts          []HostDelta          `json:"hostNoise"`
	Manifest       []ManifestEntry      `json:"rawRecords"`
}

const dryLabel = "DRY RUN - NOT ACCEPTANCE - reduced counts; this output never satisfies the S0 gate"

// BaselineRecord is the raw record of one fresh-fixture baseline run.
type BaselineRecord struct {
	Protocol   int        `json:"protocolAmendment"`
	Warmup     bool       `json:"warmup"` // Amendment 2: labelled, excluded from classification.
	Label      string     `json:"label,omitempty"`
	FixtureSeq uint64     `json:"fixtureSeq"`
	Candidate  string     `json:"candidate"`
	Rep        int        `json:"repetition"`
	Order      []string   `json:"armOrder"`
	StartedUTC time.Time  `json:"startedUTC"`
	EndedUTC   time.Time  `json:"endedUTC"`
	HostBefore HostSample `json:"hostBefore"`
	HostAfter  HostSample `json:"hostAfter"`
	Arms       []Arm      `json:"arms"`
	Checkpoint Checkpoint `json:"checkpointAfter"`
	Violations int64      `json:"inactiveBodyExecutions"`
	Errors     []string   `json:"errors,omitempty"`
}

type stressFile struct {
	Protocol   int           `json:"protocolAmendment"`
	FixtureSeq uint64        `json:"fixtureSeq"`
	Candidate  string        `json:"candidate"`
	Rep        int           `json:"repetition"`
	StartedUTC time.Time     `json:"startedUTC"`
	EndedUTC   time.Time     `json:"endedUTC"`
	HostBefore HostSample    `json:"hostBefore"`
	HostAfter  HostSample    `json:"hostAfter"`
	Record     StressRecord  `json:"record"`
	Summary    StressSummary `json:"summary"`
	Errors     []string      `json:"errors,omitempty"`
}

func throughput(a Arm) float64 { return a.ThroughputPS }

// WarmupSummary lists one warm-up record in the report. Nothing in it is a
// classification input except Errors, through foldWarmup.
type WarmupSummary struct {
	Candidate  string      `json:"candidate"`
	File       string      `json:"file"`
	FixtureSeq uint64      `json:"fixtureSeq"`
	Arms       []WarmupArm `json:"arms"`
	Errors     []string    `json:"errors,omitempty"`
}

// WarmupArm is the listed reading of one warm-up window.
type WarmupArm struct {
	Operation     Operation      `json:"operation"`
	Callers       int            `json:"callers"`
	Mallocs       uint64         `json:"mallocsInWindow"`
	ThroughputPS  float64        `json:"throughputPerSecond"`
	OutcomeCounts map[string]int `json:"outcomeCounts"`
}

// foldWarmup is the only way a warm-up record reaches classification: its
// integrity errors fail the run and its protocol amendment is checked. Its
// measurements (latency, allocation, throughput, host noise) are listed in the
// returned summary and enter no aggregate, variation or count.
func foldWarmup(in *EvalInput, rec BaselineRecord, file string) WarmupSummary {
	in.RunErrors = append(in.RunErrors, prefixed(fmt.Sprintf("warmup %s", rec.Candidate), rec.Errors)...)
	in.RunProtocols = append(in.RunProtocols, rec.Protocol)
	s := WarmupSummary{Candidate: rec.Candidate, File: file, FixtureSeq: rec.FixtureSeq, Errors: rec.Errors}
	for _, a := range rec.Arms {
		s.Arms = append(s.Arms, WarmupArm{Operation: a.Operation, Callers: a.Callers, Mallocs: a.Mallocs, ThroughputPS: a.ThroughputPS, OutcomeCounts: a.OutcomeCounts})
	}
	return s
}

type aggKey struct {
	arm, cand string
	op        Operation
	callers   int
}

// Run executes the protocol. Raw records are written and fsynced to the
// exclusive output directory run by run, before the next run starts.
func Run(ctx context.Context, cfg Config) (Report, error) {
	label := "full protocol; candidate evidence, not final-port evidence"
	if cfg.Dry {
		label = dryLabel
	}
	rep := Report{Schema: "s0-callpath-report/v1", Protocol: ProtocolAmendment, Label: label, StartedUTC: time.Now().UTC(), Plan: cfg.Plan, Classification: scale.Inconclusive}
	if err := cfg.Plan.validate(); err != nil {
		return rep, err
	}
	if !cfg.Dry && !cfg.Plan.Frozen() {
		return rep, errors.New("an acceptance run requires exactly the frozen plan; use Dry for reduced counts")
	}
	if cfg.Out == "" {
		return rep, errors.New("output directory required")
	}
	if err := os.Mkdir(cfg.Out, 0o755); err != nil { // Exclusive: refuses an existing path.
		return rep, fmt.Errorf("fresh output directory required: %w", err)
	}
	progress := cfg.Progress
	if progress == nil {
		progress = func(string) {}
	}
	w := &evidenceWriter{dir: cfg.Out}
	identity, closure := CollectIdentity(cfg.Root, cfg.Plan, cfg.Dry, cfg.Identity)
	rep.Acceptance = identity.Acceptance
	if err := w.write("identity.json", identity); err != nil {
		return rep, err
	}
	if err := w.write("source-closure.json", closure); err != nil {
		return rep, err
	}

	backends := cfg.Backends
	if len(backends) == 0 {
		backends = scale.Candidates()
	}
	in := EvalInput{Plan: cfg.Plan, Dry: cfg.Dry, IdentityProblems: identity.Problems, Invalid: identity.Invalid}
	for _, b := range backends {
		in.Candidates = append(in.Candidates, b.Name)
	}
	groups := make(map[aggKey]*Aggregate)
	group := func(k aggKey) *Aggregate {
		a := groups[k]
		if a == nil {
			a = &Aggregate{Candidate: k.cand, Arm: k.arm, Operation: k.op, Callers: k.callers}
			groups[k] = a
		}
		return a
	}
	pooled := make(map[aggKey][]int64)
	add := func(k aggKey, arm Arm) {
		a := group(k)
		a.Runs++
		a.Count += arm.Samples
		a.PerRun = append(a.PerRun, arm.Summary)
		a.Mallocs = append(a.Mallocs, arm.Mallocs)
		a.Throughput = append(a.Throughput, throughput(arm))
		a.NonOK += arm.Samples - arm.OutcomeCounts[OutcomeOK.String()]
		a.Refusals += arm.OutcomeCounts[OutcomeRefusedRead.String()] + arm.OutcomeCounts[OutcomeRefusedEnter.String()]
		a.Errors += arm.OutcomeCounts[OutcomeError.String()]
		pooled[k] = append(pooled[k], arm.LatencyNS...)
	}

	// Amendment 2: the warm-up exercises every baseline window shape of every
	// candidate once, on its own fresh fixtures, before the first measured window.
	for range cfg.Plan.WarmupReps {
		for _, b := range backends {
			if err := ctx.Err(); err != nil {
				in.RunErrors = append(in.RunErrors, "driver: "+err.Error())
				goto finish
			}
			progress("warmup baseline " + b.Name)
			rec := runBaseline(ctx, cfg.Plan, b, 0, true)
			file := fmt.Sprintf("baseline-%s-warmup.json", b.Name)
			rep.Warmup = append(rep.Warmup, foldWarmup(&in, rec, file))
			if err := w.write(file, rec); err != nil {
				return rep, err
			}
		}
	}
	for r := 0; r < cfg.Plan.Repetitions; r++ {
		order := slices.Clone(backends)
		baselineFirst := r%2 == 0
		if r%2 == 1 {
			slices.Reverse(order)
		}
		arms := []string{"baseline", "stress"}
		if !baselineFirst {
			slices.Reverse(arms)
		}
		for _, arm := range arms {
			for _, b := range order {
				if err := ctx.Err(); err != nil {
					in.RunErrors = append(in.RunErrors, "driver: "+err.Error())
					goto finish
				}
				if arm == "baseline" {
					progress(fmt.Sprintf("rep %d baseline %s", r, b.Name))
					rec := runBaseline(ctx, cfg.Plan, b, r, false)
					in.RunErrors = append(in.RunErrors, prefixed(fmt.Sprintf("baseline %s rep%d", b.Name, r), rec.Errors)...)
					in.RunProtocols = append(in.RunProtocols, rec.Protocol)
					for _, a := range rec.Arms {
						k := aggKey{"baseline", b.Name, a.Operation, a.Callers}
						if a.Operation == Timer {
							k.arm = "timer"
						}
						add(k, a)
					}
					in.Hosts = append(in.Hosts, hostDelta(fmt.Sprintf("baseline/%s/rep%02d", b.Name, r), rec.HostBefore, rec.HostAfter))
					if err := w.write(fmt.Sprintf("baseline-%s-rep%02d.json", b.Name, r), rec); err != nil {
						return rep, err
					}
					continue
				}
				for _, callers := range cfg.Plan.Callers {
					progress(fmt.Sprintf("rep %d stress %s callers %d", r, b.Name, callers))
					file := runStress(ctx, cfg.Plan, b, r, callers)
					in.RunErrors = append(in.RunErrors, prefixed(fmt.Sprintf("stress %s c%d rep%d", b.Name, callers, r), file.Errors)...)
					in.Stress = append(in.Stress, file.Summary)
					in.RunProtocols = append(in.RunProtocols, file.Protocol)
					a := group(aggKey{"stress", b.Name, StressObservationOp(), callers})
					a.Runs++
					a.Count += file.Record.Observations.Samples
					a.PerRun = append(a.PerRun, file.Record.Observations.Summary)
					a.NonOK += file.Record.Observations.Samples - file.Record.Observations.OutcomeCounts[OutcomeOK.String()]
					a.Refusals += file.Summary.OutcomeCounts[OutcomeRefusedRead.String()] + file.Summary.OutcomeCounts[OutcomeRefusedEnter.String()]
					a.Errors += file.Summary.OutcomeCounts[OutcomeError.String()]
					k := aggKey{"stress", b.Name, StressObservationOp(), callers}
					pooled[k] = append(pooled[k], file.Record.Observations.LatencyNS...)
					in.Hosts = append(in.Hosts, hostDelta(fmt.Sprintf("stress/%s/c%d/rep%02d", b.Name, callers, r), file.HostBefore, file.HostAfter))
					name := fmt.Sprintf("stress-%s-c%02d-rep%02d.json", b.Name, callers, r)
					file.Summary.File = name
					if err := w.write(name, file); err != nil {
						return rep, err
					}
				}
			}
		}
	}
finish:
	for k, a := range groups {
		a.Dist = scale.Summarize(pooled[k])
		if a.Arm != "stress" {
			a.CV, _ = scale.CV(a.Throughput)
		} else {
			a.Throughput = nil // The offered window is fixed; no call throughput requirement is invented.
		}
		a.AllocsPerOp = 0
		var total uint64
		for _, m := range a.Mallocs {
			total += m
		}
		if a.Count > 0 && a.Arm != "stress" {
			a.AllocsPerOp = float64(total) / float64(a.Count)
		}
	}
	for _, k := range sortedKeys(groups) {
		in.Aggregates = append(in.Aggregates, *groups[k])
	}
	in.EvidenceProblems = w.verify()
	rep.Aggregates, rep.Stress, rep.Hosts = in.Aggregates, in.Stress, in.Hosts
	rep.Classification, rep.Reasons = Evaluate(in)
	rep.Manifest, rep.EndedUTC = w.manifest, time.Now().UTC()
	if err := w.write("report.json", rep); err != nil {
		return rep, err
	}
	return rep, nil
}

func sortedKeys(m map[aggKey]*Aggregate) []aggKey {
	keys := make([]aggKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b aggKey) int {
		for _, c := range []int{strings.Compare(a.arm, b.arm), strings.Compare(a.cand, b.cand), a.callers - b.callers, strings.Compare(string(a.op), string(b.op))} {
			if c != 0 {
				return c
			}
		}
		return 0
	})
	return keys
}

func closeFixture(fx *Fixture, errs *[]string) {
	// A fresh guard: an expired run context must not leak the candidate's workers.
	cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := fx.Close(cleanup); err != nil {
		*errs = append(*errs, "close: "+err.Error())
	}
}

// runBaseline measures the mutation-free arm on one fresh fixture. Operation
// and concurrency order rotate with the repetition so no operation always runs
// first on a cold or hot machine.
func runBaseline(ctx context.Context, p Plan, b scale.Backend, rep int, warmup bool) BaselineRecord {
	rec := BaselineRecord{Protocol: ProtocolAmendment, Warmup: warmup, Candidate: b.Name, Rep: rep, StartedUTC: time.Now().UTC()}
	if warmup {
		rec.Label = "warmup: recorded, excluded from classification"
	}
	fx, err := NewFixture(ctx, b, FixtureOptions{Leaves: p.Leaves})
	if err != nil {
		rec.Errors = append(rec.Errors, "fixture: "+err.Error())
		rec.EndedUTC = time.Now().UTC()
		return rec
	}
	defer closeFixture(fx, &rec.Errors)
	rec.FixtureSeq = fx.Seq
	ops := []Operation{LookupView, Lease, Dispatch, Timer}
	if rep%2 == 1 {
		slices.Reverse(ops)
	}
	callers := slices.Clone(p.Callers)
	if rep%2 == 1 {
		slices.Reverse(callers)
	}
	rec.HostBefore = ReadHost()
	for _, c := range callers {
		for _, op := range ops {
			rec.Order = append(rec.Order, fmt.Sprintf("%s/c%d", op, c))
			rec.Arms = append(rec.Arms, MeasureArm(op, fx.Operation(op), c, p.SamplesPerRun))
		}
	}
	rec.HostAfter = ReadHost()
	rec.Checkpoint = fx.Checkpoint(ctx, "after-baseline", -1, 0)
	rec.Violations = fx.Violations()
	if rec.Violations != 0 {
		rec.Errors = append(rec.Errors, fmt.Sprintf("%d inactive-owner bodies executed", rec.Violations))
	}
	for _, a := range rec.Arms {
		if a.Operation != Timer && a.OutcomeCounts[OutcomeOK.String()] != a.Samples {
			rec.Errors = append(rec.Errors, fmt.Sprintf("%s/c%d: %v", a.Operation, a.Callers, a.OutcomeCounts))
		}
	}
	rec.EndedUTC = time.Now().UTC()
	return rec
}

func runStress(ctx context.Context, p Plan, b scale.Backend, rep, callers int) stressFile {
	f := stressFile{Protocol: ProtocolAmendment, Candidate: b.Name, Rep: rep, StartedUTC: time.Now().UTC()}
	fx, err := NewFixture(ctx, b, FixtureOptions{Leaves: p.Leaves})
	if err != nil {
		f.Errors = append(f.Errors, "fixture: "+err.Error())
		f.EndedUTC = time.Now().UTC()
		return f
	}
	defer closeFixture(fx, &f.Errors)
	f.FixtureSeq = fx.Seq
	f.HostBefore = ReadHost()
	f.Record = RunStress(ctx, fx, p, callers, StressHooks{})
	f.HostAfter = ReadHost()
	f.Summary = SummarizeStress(f.Record)
	f.Summary.Candidate, f.Summary.Rep = b.Name, rep
	f.Summary.Problems = append(f.Summary.Problems, checkFinal(fx, f.Record, p)...)
	f.EndedUTC = time.Now().UTC()
	return f
}

// checkFinal inspects the last causal checkpoint: every unrelated leaf, the
// provider and the consumer ACTIVE, and one active provision owned by the provider.
func checkFinal(fx *Fixture, rec StressRecord, p Plan) []string {
	last := rec.Checkpoints[len(rec.Checkpoints)-1]
	var problems []string
	if last.Error != "" {
		return []string{"final checkpoint: " + last.Error}
	}
	if last.LeafStates[string("ACTIVE")] != p.Leaves {
		problems = append(problems, fmt.Sprintf("after drain only %d of %d unrelated leaves ACTIVE: %v", last.LeafStates["ACTIVE"], p.Leaves, last.LeafStates))
	}
	for _, f := range last.Fibers {
		if f.State != "ACTIVE" {
			problems = append(problems, fmt.Sprintf("%s is %s after drain", f.Name, f.State))
		}
	}
	if len(last.Provisions) != 1 || !last.Provisions[0].Active || last.Provisions[0].Owner != fx.Provider.ID {
		problems = append(problems, fmt.Sprintf("after drain provisions are not the single active provider-owned entry: %+v", last.Provisions))
	}
	return problems
}
