package boot

import (
	"fmt"
	"strings"
)

// Admission is the result of steps 2 and 3 of boot on one composition: the
// gate, the static graph and the static half of the required audit
// (03-c3-kernel.md:357-359). It is data from the composition, the module table
// and the distribution manifest, so `aiopsctl profile render` produces it with
// no kernel.
type Admission struct {
	Comp     Composition // as composed
	Admitted Composition // the launcher's copy: denied rows disabled
	Modules  ModuleTable
	Dist     DistManifest
	Roles    []Role
	Gate     GateResult
	Graph    Graph
	Required RequiredSet
	Errors   []StaticError
	Warnings []StaticWarning
	// Prints are the lines that boot prints at every boot (bootInput), set by Render.
	Prints []string
}

// Render composes and admits a profile as boot does, for aiopsctl profile render
// (08-profiles-roadmap-adrs.md:357): on the module table that the signed distribution
// manifest d lists (Distribution.Table) and on what the home of src holds, trust.yaml,
// the records of dist use, plugins.lock and what install kept (bootInput), at
// EntryRender. It returns the admission and the warnings of Compose.
func Render(src Sources, server bool, d Distribution, roles []Role) (Admission, []Warning, error) {
	mods := d.Table()
	c, warns, err := Compose(src, Options{Server: server, Modules: mods, Locks: lockDigests(src.Home, src.Profile)})
	if err == nil {
		err = CheckReserved(c)
	}
	if err != nil {
		return Admission{}, warns, err
	}
	w := WithLoader(c)
	in, prints := bootInput(src.Home, src.Profile, w, mods, d, nil, nil)
	in.Entry = EntryRender
	a := AdmitWith(w, in, roles)
	a.Prints = prints
	return a, warns, nil
}

// AdmitWith runs the gate on the composition at the entry point of in, builds the
// graph of the admitted copy for the roles and audits the static part of the required
// set. Rows the gate denies are disabled in the copy before the graph is built, so one
// fault is reported once, by the gate.
func AdmitWith(c Composition, in GateInput, roles []Role) Admission {
	mods, dist := in.Mods, in.Bindings
	a := Admission{Comp: c, Modules: mods, Dist: dist, Roles: append([]Role(nil), roles...)}
	a.Gate = GateWith(c, in)
	a.Admitted = a.Gate.Admitted(c)
	a.Graph, a.Errors, a.Warnings = BuildGraph(a.Admitted, mods, dist, roles)
	a.Required = RequiredFor(a.Admitted, mods, dist, roles)
	a.Errors = append(a.Errors, AuditStatic(a.Admitted, mods, dist, a.Graph, roles)...)
	return a
}

// CheckReserved refuses a composition that holds a row with the id of the
// loader's row: boot mounts the loader itself and adds that row to the copy it
// admits (WithLoader), so a bundle or a patch may not hold it. Every caller that
// admits as boot does calls it first: boot.Run and `aiopsctl profile render`.
func CheckReserved(c Composition) error {
	if _, clash := findRow(c, LoaderRowID); clash {
		return fmt.Errorf("the row id %q is reserved: boot mounts the loader and no bundle holds it (03-c3-kernel.md:64)", LoaderRowID)
	}
	return nil
}

// Refused reports whether boot must stop: the gate refuses, or the graph or
// the required set has an error.
func (a Admission) Refused() bool { return a.Gate.Refused || len(a.Errors) > 0 }

// Diagnostics returns one line per finding, in a fixed order: the module
// table lookups, the failures of each gate slot, the gate's warnings of a deprecated
// major, the graph and required-set errors, then the grouped warnings. Each line starts with "error:" or
// "warning:". The words the failure matrix looks for are in the text: "not in
// the module table", "detached", "denied", "static graph", "waits for".
func (a Admission) Diagnostics() []string {
	var out []string
	for _, f := range a.Gate.Unresolved {
		if f.Stop {
			out = append(out, fmt.Sprintf("error: denied: required row %q: %s", f.Row, f.Reason))
		} else {
			out = append(out, fmt.Sprintf("warning: row %q is inactive: %s", f.Row, f.Reason))
		}
	}
	for _, s := range a.Gate.Slots {
		for _, f := range s.Fails {
			switch {
			case f.Rule == RuleIsolateBound:
				// Static graph errors stop boot whatever the row (08:469).
				out = append(out, fmt.Sprintf("error: static graph: %s, rule %s: %s", gateCheck(s.N), f.Rule, f.Reason))
			case f.Stop:
				out = append(out, fmt.Sprintf("error: denied: %s, rule %s: %s", gateCheck(s.N), f.Rule, f.Reason))
			case strings.Contains(f.Row, "/"): // a plugin id: an artifact none of whose rows was inserted
				out = append(out, fmt.Sprintf("warning: the installed artifact %s is left out: %s, rule %s: %s", f.Row, gateCheck(s.N), f.Rule, f.Reason))
			default:
				out = append(out, fmt.Sprintf("warning: row %q detached as disabled: %s, rule %s: %s", f.Row, gateCheck(s.N), f.Rule, f.Reason))
			}
		}
	}
	for _, w := range a.Gate.Warnings {
		out = append(out, fmt.Sprintf("warning: row %q uses a deprecated major: %s, rule %s: %s", w.Row, gateCheck(w.Check), w.Rule, w.Reason))
	}
	for _, e := range a.Errors {
		out = append(out, "error: "+e.Error())
	}
	for _, w := range a.Warnings {
		out = append(out, "warning: "+w.String())
	}
	return out
}
