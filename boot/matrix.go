package boot

import (
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The failure matrix file, test/failure-matrix/matrix.yaml, mirrors the table
// of 08-profiles-roadmap-adrs.md section 1.6 cell by cell (ADR-059 decision 3).
// The schema lives here and not in the test package because ADR-059 decision
// 6 lets only boot/ and tools/ import the YAML library, so the test package
// reads the file through ParseMatrix.
//
// The file is a list of cells. One cell is one (failure, column) pair of the
// table, so a table of R rows and C columns has R*C cells:
//
//	failure:  the first column of the table, verbatim
//	column:   the header of the column, verbatim
//	outcome:  the text of the cell, verbatim (what the parity test compares)
//	fault:    the fault plugins to run, one run each: fail-apply, panic-apply,
//	          slow-apply, bad-config, rogue-provider, the installed artifacts
//	          unserved-version, unsigned-row and foreign-attestation,
//	          leaky-goroutine, slow-dispose, the stub remote images
//	          stub-remote-hang and stub-remote-ignore-deactivate, or none when
//	          the fault is in the files of the profile
//	expect:   exit (nonzero: the process ends non-zero; running: it does not
//	          end), ready (true or false), states (row id to fiber state in
//	          capitals) and diagnostic (a substring of the diagnostic output)
//	since:    the roadmap step from which the cell must pass, or n/a for a
//	          cell the table marks n/a
//
// The row ids of states are the fixture's: "fault" is the row of the fault
// plugin, "dependant" a row that injects what it provides, "required" a row
// named in the required set.

// Expect is what a cell asserts after the run.
type Expect struct {
	Exit       string
	Ready      string
	States     []StateExpect
	Diagnostic string
}

// StateExpect is the expected fiber state of one row.
type StateExpect struct {
	Row   string
	State string
}

// Cell is one cell of the failure matrix.
type Cell struct {
	Failure string
	Column  string
	Outcome string
	Fault   []string
	Expect  Expect
	Since   string
}

// Matrix is the parsed failure matrix, in file order.
type Matrix struct {
	Cells []Cell
}

// NotApplicable is the value of Since for a cell the table marks n/a.
const NotApplicable = "n/a"

// isNA reports whether the text of a table cell says it does not apply.
func isNA(outcome string) bool { return strings.HasPrefix(outcome, NotApplicable) }

func inSet(s string, set ...string) bool {
	for _, x := range set {
		if s == x {
			return true
		}
	}
	return false
}

// ParseMatrix reads and validates a failure matrix file.
func ParseMatrix(file string, data []byte) (Matrix, error) {
	root, err := parseDocument(file, data)
	if err != nil {
		return Matrix{}, err
	}
	p := &parser{file: file}
	if err := p.guard(root, ""); err != nil {
		return Matrix{}, err
	}
	if root.Kind != yaml.SequenceNode {
		return Matrix{}, p.err(root, RuleType, "", "the matrix is a list of cells")
	}
	var m Matrix
	seen := map[[2]string]bool{}
	for _, n := range root.Content {
		c, err := p.cell(n)
		if err != nil {
			return Matrix{}, err
		}
		key := [2]string{c.Failure, c.Column}
		if seen[key] {
			return Matrix{}, p.err(n, RuleMatrix, c.Failure, "cell (%q, %q) appears twice", c.Failure, c.Column)
		}
		seen[key] = true
		m.Cells = append(m.Cells, c)
	}
	return m, nil
}

// plain reads a scalar as its text, whatever its YAML type.
func (p *parser) plain(n *yaml.Node, row, what string) (string, error) {
	if err := p.guard(n, row); err != nil {
		return "", err
	}
	if n.Kind != yaml.ScalarNode {
		return "", p.err(n, RuleType, row, "%s must be a scalar", what)
	}
	return n.Value, nil
}

func (p *parser) cell(n *yaml.Node) (Cell, error) {
	prs, err := p.pairs(n, "", "a cell")
	if err != nil {
		return Cell{}, err
	}
	var c Cell
	got := map[string]bool{}
	for _, pr := range prs {
		got[pr.key] = true
		switch pr.key {
		case "failure":
			c.Failure, err = p.text(pr.v, "", "failure")
		case "column":
			c.Column, err = p.text(pr.v, "", "column")
		case "outcome":
			c.Outcome, err = p.text(pr.v, "", "outcome")
		case "since":
			c.Since, err = p.plain(pr.v, "", "since")
		case "fault":
			c.Fault, err = p.faults(pr.v)
		case "expect":
			c.Expect, err = p.expect(pr.v)
		default:
			err = p.err(pr.k, RuleUnknownField, "", "unknown field %q in a cell", pr.key)
		}
		if err != nil {
			return Cell{}, err
		}
	}
	for _, k := range []string{"failure", "column", "outcome", "since", "fault", "expect"} {
		if !got[k] {
			return Cell{}, p.err(n, RuleMatrix, c.Failure, "a cell states %s", k)
		}
	}
	return c, p.checkCell(n, c)
}

func (p *parser) checkCell(n *yaml.Node, c Cell) error {
	steps := []string{"S1", "S2a", "S2b", "S3", "S4a", "S4b", "S5", "S6a", "S6b", "S7", "S8a", "S8b", "S9a", "S9b", "S10", NotApplicable}
	switch {
	case c.Failure == "" || c.Column == "" || c.Outcome == "":
		return p.err(n, RuleMatrix, c.Failure, "failure, column and outcome are not empty")
	case !inSet(c.Since, steps...):
		return p.err(n, RuleMatrix, c.Failure, "since %q is not a roadmap step or n/a", c.Since)
	case len(c.Fault) == 0:
		return p.err(n, RuleMatrix, c.Failure, "fault lists at least one name; none when the fault is in the files")
	case c.Since == NotApplicable:
		if !isNA(c.Outcome) || len(c.Fault) != 1 || c.Fault[0] != "none" || c.Expect.Exit != "n/a" ||
			c.Expect.Ready != "n/a" || len(c.Expect.States) != 0 || c.Expect.Diagnostic != "" {
			return p.err(n, RuleMatrix, c.Failure, "a cell with since n/a has outcome n/a, fault none and an empty expect (exit n/a, ready n/a)")
		}
	case isNA(c.Outcome):
		return p.err(n, RuleMatrix, c.Failure, "a cell whose outcome is n/a has since n/a")
	case !inSet(c.Expect.Exit, "nonzero", "running") || !inSet(c.Expect.Ready, "true", "false") || c.Expect.Diagnostic == "":
		return p.err(n, RuleMatrix, c.Failure, "expect states exit (nonzero or running), ready (true or false) and a diagnostic")
	}
	return nil
}

func (p *parser) faults(n *yaml.Node) ([]string, error) {
	if err := p.guard(n, ""); err != nil {
		return nil, err
	}
	if n.Kind != yaml.SequenceNode {
		return nil, p.err(n, RuleType, "", "fault must be a list")
	}
	var out []string
	for _, c := range n.Content {
		s, err := p.text(c, "", "fault name")
		if err != nil {
			return nil, err
		}
		if !inSet(s, "none", "fail-apply", "panic-apply", "slow-apply", "bad-config", "rogue-provider", "unserved-version", "unsigned-row",
			"foreign-attestation", "leaky-goroutine", "slow-dispose", "stub-remote-hang", "stub-remote-ignore-deactivate") {
			return nil, p.err(c, RuleMatrix, "", "fault %q is not a fault plugin or stub image", s)
		}
		out = append(out, s)
	}
	return out, nil
}

func (p *parser) expect(n *yaml.Node) (Expect, error) {
	prs, err := p.pairs(n, "", "expect")
	if err != nil {
		return Expect{}, err
	}
	var e Expect
	for _, pr := range prs {
		switch pr.key {
		case "exit":
			e.Exit, err = p.plain(pr.v, "", "exit")
		case "ready":
			e.Ready, err = p.plain(pr.v, "", "ready")
		case "diagnostic":
			e.Diagnostic, err = p.text(pr.v, "", "diagnostic")
		case "states":
			e.States, err = p.states(pr.v)
		default:
			err = p.err(pr.k, RuleUnknownField, "", "unknown field %q in expect", pr.key)
		}
		if err != nil {
			return Expect{}, err
		}
	}
	return e, nil
}

func (p *parser) states(n *yaml.Node) ([]StateExpect, error) {
	prs, err := p.pairs(n, "", "states")
	if err != nil {
		return nil, err
	}
	var out []StateExpect
	for _, pr := range prs {
		s, err := p.text(pr.v, "", "a state")
		if err != nil {
			return nil, err
		}
		if !inSet(s, "PENDING", "LOADING", "ACTIVE", "FAILED", "UNLOADING", "DISPOSED") {
			return nil, p.err(pr.v, RuleMatrix, pr.key, "state %q is not a fiber state in capitals", s)
		}
		if !okID(pr.key, false) {
			return nil, p.err(pr.k, RuleSyntaxID, pr.key, "row id %q is not a name", pr.key)
		}
		out = append(out, StateExpect{Row: pr.key, State: s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Row < out[j].Row })
	return out, nil
}
