package boot

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const cellOK = `- failure: f
  column: c
  outcome: Stop
  fault: [fail-apply]
  expect: {exit: nonzero, ready: false, states: {b: FAILED, a: ACTIVE}, diagnostic: d}
  since: S1
`

func TestParseMatrix(t *testing.T) {
	m, err := ParseMatrix("m.yaml", []byte(cellOK+`- failure: f
  column: c2
  outcome: n/a
  fault: [none]
  expect: {exit: n/a, ready: n/a}
  since: n/a
`))
	if err != nil || len(m.Cells) != 2 {
		t.Fatal(err, m)
	}
	c := m.Cells[0]
	if c.Since != "S1" || c.Expect.Exit != "nonzero" || c.Expect.States[0] != (StateExpect{"a", "ACTIVE"}) || c.Fault[0] != "fail-apply" {
		t.Errorf("%+v", c)
	}
	if m.Cells[1].Since != NotApplicable {
		t.Errorf("%+v", m.Cells[1])
	}
}

func TestParseMatrixShippedFile(t *testing.T) {
	data, err := os.ReadFile("../test/failure-matrix/matrix.yaml")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseMatrix("matrix.yaml", data)
	if err != nil || len(m.Cells) == 0 {
		t.Fatal(err)
	}
}

func TestParseMatrixErrors(t *testing.T) {
	rep := func(old, new string) string { return strings.Replace(cellOK, old, new, 1) }
	na := "- {failure: f, column: c, outcome: n/a, fault: [none], expect: {exit: n/a, ready: n/a}, since: n/a}\n"
	tests := []struct {
		name, in string
		rule     Rule
	}{
		{"empty", "", RuleEmpty},
		{"not list", "x: 1", RuleType},
		{"alias", "&a []", RuleAlias},
		{"cell not mapping", "- x", RuleType},
		{"duplicate cell", cellOK + cellOK, RuleMatrix},
		{"unknown field", rep("since: S1", "since: S1\n  extra: 1"), RuleUnknownField},
		{"missing field", rep("  since: S1\n", ""), RuleMatrix},
		{"failure not string", rep("failure: f", "failure: [a]"), RuleType},
		{"column not string", rep("column: c", "column: 1"), RuleType},
		{"outcome not string", rep("outcome: Stop", "outcome: 5"), RuleType},
		{"since anchor", rep("since: S1", "since: &x S1"), RuleAlias},
		{"fault anchor", rep("fault: [fail-apply]", "fault: &x [none]"), RuleAlias},
		{"since not scalar", rep("since: S1", "since: [S1]"), RuleType},
		{"since unknown", rep("since: S1", "since: S99"), RuleMatrix},
		{"empty failure", rep("failure: f", "failure: ''"), RuleMatrix},
		{"fault not list", rep("fault: [fail-apply]", "fault: x"), RuleType},
		{"fault empty", rep("fault: [fail-apply]", "fault: []"), RuleMatrix},
		{"fault unknown", rep("fault: [fail-apply]", "fault: [nope]"), RuleMatrix},
		{"fault item not string", rep("fault: [fail-apply]", "fault: [1]"), RuleType},
		{"expect not mapping", rep("expect: {exit: nonzero, ready: false, states: {b: FAILED, a: ACTIVE}, diagnostic: d}", "expect: [a]"), RuleType},
		{"expect unknown", rep("exit: nonzero,", "exit: nonzero, x: 1,"), RuleUnknownField},
		{"exit bad", rep("exit: nonzero", "exit: maybe"), RuleMatrix},
		{"exit not scalar", rep("exit: nonzero", "exit: [a]"), RuleType},
		{"ready bad", rep("ready: false", "ready: maybe"), RuleMatrix},
		{"ready not scalar", rep("ready: false", "ready: [a]"), RuleType},
		{"diagnostic missing", rep("diagnostic: d", "diagnostic: ''"), RuleMatrix},
		{"diagnostic not string", rep("diagnostic: d", "diagnostic: [d]"), RuleType},
		{"states not mapping", rep("states: {b: FAILED, a: ACTIVE}", "states: [a]"), RuleType},
		{"state not string", rep("b: FAILED", "b: 1"), RuleType},
		{"state lower case", rep("b: FAILED", "b: failed"), RuleMatrix},
		{"state row bad", rep("b: FAILED", "'b c': FAILED"), RuleSyntaxID},
		{"na with outcome", strings.Replace(na, "outcome: n/a", "outcome: Stop", 1), RuleMatrix},
		{"na with states", strings.Replace(na, "ready: n/a", "ready: n/a, states: {a: ACTIVE}", 1), RuleMatrix},
		{"na with fault", strings.Replace(na, "[none]", "[fail-apply]", 1), RuleMatrix},
		{"outcome n/a with step", strings.Replace(na, "since: n/a", "since: S1", 1), RuleMatrix},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseMatrix("m.yaml", []byte(tc.in))
			var ce *Error
			if !errors.As(err, &ce) || ce.Rule != tc.rule {
				t.Fatalf("got %v, want rule %s", err, tc.rule)
			}
		})
	}
}

func TestNAOutcomeText(t *testing.T) {
	in := "- {failure: f, column: c, outcome: 'n/a: no harness is in a required set', fault: [none], expect: {exit: n/a, ready: n/a}, since: n/a}\n"
	if _, err := ParseMatrix("m.yaml", []byte(in)); err != nil {
		t.Fatal(err)
	}
}
