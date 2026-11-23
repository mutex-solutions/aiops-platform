package boot

import "fmt"

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
}

// Admit admits as the S1 form of profile render does, on the stand-ins dist.yaml and
// modules.yaml (EntryRender).
func Admit(c Composition, mods ModuleTable, dist DistManifest, roles []Role) Admission {
	return AdmitWith(c, GateInput{Entry: EntryRender, Mods: mods, Bindings: dist}, roles)
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
// table lookups, the failures of each gate slot, the graph and required-set
// errors, then the grouped warnings. Each line starts with "error:" or
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
				out = append(out, fmt.Sprintf("error: static graph: gate check %d (%s), rule %s: %s", s.N, s.Name, f.Rule, f.Reason))
			case f.Stop:
				out = append(out, fmt.Sprintf("error: denied: gate check %d (%s), rule %s: %s", s.N, s.Name, f.Rule, f.Reason))
			default:
				out = append(out, fmt.Sprintf("warning: row %q detached as disabled: gate check %d (%s), rule %s: %s", f.Row, s.N, s.Name, f.Rule, f.Reason))
			}
		}
	}
	for _, e := range a.Errors {
		out = append(out, "error: "+e.Error())
	}
	for _, w := range a.Warnings {
		out = append(out, "warning: "+w.String())
	}
	return out
}
