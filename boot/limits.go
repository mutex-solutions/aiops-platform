package boot

import (
	"fmt"
	"time"
)

// Limits are the numbers the launcher hands the kernel and the bounds it puts
// on its own waits. The kernel holds no default (ADR-034 owner decisions 3 and
// 4), so every number lives here. The launcher owns policy of this kind
// (plugin-system-2026-11.md §3; 03-c3-kernel.md:63 "bounded shutdown").
//
// The three deadlines of the kernel are the only tunables the documents name
// (plugin-system-2026-11.md §3, item 3); they are configurable here, none is
// fixed in code. The documents give no value for any of them, so DefaultLimits
// is this package's reading, adopted as the release defaults for now (ADR-059
// decision 30).
//
// A distribution carries its own numbers in limits.yaml, next to dist.yaml
// (ParseLimits). The file may set any subset; a field it leaves out keeps the
// default. A value of zero or less is refused.
type Limits struct {
	LoadDeadline    time.Duration // kernel: Apply and an availability check
	DrainDeadline   time.Duration // kernel: leases and tracked work of a quiesced fiber
	DisposeDeadline time.Duration // kernel: the effect walk of one fiber
	ArchiveLimit    int
	EventWindow     int
	ObserverQueue   int
	OwnerInbox      int
	TerminalLimit   int

	// SettleBound bounds the wait for the tree to settle at boot. The design note
	// says a settle deadline is at least the load plus drain plus dispose deadline
	// (plugin-system-2026-11.md §9, step 5), which is what a zero value means.
	SettleBound time.Duration
	// ShutdownBound bounds Root.Close on a signal or an exit request
	// (03-c3-kernel.md:63 "bounded shutdown"). The tree comes down one fiber at a
	// time, so it is not derived from the three deadlines: the default is 30s.
	ShutdownBound time.Duration
	// LeakGrace is how long the process stays up after a fiber leaked, before it
	// is replaced: "readiness ends after the drain window and the process is
	// replaced" (08-profiles-roadmap-adrs.md:476). A zero value means the drain
	// deadline.
	LeakGrace time.Duration
}

// DefaultLimits returns the launcher's defaults: 10s for each of the three
// deadlines, a bounded history, a shutdown bound of 30s. The observer queue is the
// 65,536 events that ADR-034 sized it at ("2026-11-19 bounded observer queue and
// overflow rule"): a burst of state events of a few hundred rows passes a smaller one.
func DefaultLimits() Limits {
	return Limits{
		LoadDeadline: 10 * time.Second, DrainDeadline: 10 * time.Second, DisposeDeadline: 10 * time.Second,
		ArchiveLimit: 256, EventWindow: 1024, ObserverQueue: 65536, OwnerInbox: 256, TerminalLimit: 256,
		ShutdownBound: 30 * time.Second,
	}
}

func (l Limits) settleBound() time.Duration {
	if l.SettleBound > 0 {
		return l.SettleBound
	}
	return l.LoadDeadline + l.DrainDeadline + l.DisposeDeadline
}

func (l Limits) leakGrace() time.Duration {
	if l.LeakGrace > 0 {
		return l.LeakGrace
	}
	return l.DrainDeadline
}

// ParseLimits reads limits.yaml over DefaultLimits:
//
//	loadDeadline: 10s      drainDeadline: 10s     disposeDeadline: 10s
//	settleBound: 30s       shutdownBound: 30s     leakGrace: 10s
//	archiveLimit: 256      eventWindow: 1024      observerQueue: 65536
//	ownerInbox: 256        terminalLimit: 256
//
// Durations are strings in the form of time.ParseDuration.
func ParseLimits(file string, data []byte) (Limits, error) {
	l := DefaultLimits()
	root, err := parseDocument(file, data)
	if err != nil {
		return Limits{}, err
	}
	p := &parser{file: file}
	prs, err := p.pairs(root, "", "limits.yaml")
	if err != nil {
		return Limits{}, err
	}
	dur := map[string]*time.Duration{"loadDeadline": &l.LoadDeadline, "drainDeadline": &l.DrainDeadline,
		"disposeDeadline": &l.DisposeDeadline, "settleBound": &l.SettleBound, "shutdownBound": &l.ShutdownBound,
		"leakGrace": &l.LeakGrace}
	num := map[string]*int{"archiveLimit": &l.ArchiveLimit, "eventWindow": &l.EventWindow, "observerQueue": &l.ObserverQueue,
		"ownerInbox": &l.OwnerInbox, "terminalLimit": &l.TerminalLimit}
	for _, pr := range prs {
		switch {
		case dur[pr.key] != nil:
			s, err := p.text(pr.v, "", pr.key)
			if err != nil {
				return Limits{}, err
			}
			d, err := time.ParseDuration(s)
			if err != nil || d <= 0 {
				return Limits{}, p.err(pr.v, RuleType, "", "%s must be a positive duration such as 5s, got %q", pr.key, s)
			}
			*dur[pr.key] = d
		case num[pr.key] != nil:
			v, err := p.value(pr.v, "", 0)
			if err != nil {
				return Limits{}, err
			}
			if v.Kind != KindInt || v.Int <= 0 || v.Int > 1<<30 {
				return Limits{}, p.err(pr.v, RuleType, "", "%s must be a positive integer", pr.key)
			}
			*num[pr.key] = int(v.Int)
		default:
			return Limits{}, p.err(pr.k, RuleUnknownField, "", "unknown field %q in %s", pr.key, file)
		}
	}
	return l, nil
}

// check refuses a Limits the kernel would refuse, with the field named.
func (l Limits) check() error {
	for _, f := range []struct {
		name string
		ok   bool
	}{
		{"LoadDeadline", l.LoadDeadline > 0}, {"DrainDeadline", l.DrainDeadline > 0}, {"DisposeDeadline", l.DisposeDeadline > 0},
		{"ArchiveLimit", l.ArchiveLimit > 0}, {"EventWindow", l.EventWindow > 0}, {"ObserverQueue", l.ObserverQueue > 0},
		{"OwnerInbox", l.OwnerInbox > 0}, {"TerminalLimit", l.TerminalLimit > 0}, {"ShutdownBound", l.ShutdownBound > 0},
	} {
		if !f.ok {
			return fmt.Errorf("limits: %s must be positive", f.name)
		}
	}
	return nil
}
