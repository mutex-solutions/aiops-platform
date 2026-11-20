// Package failurematrix holds the failure matrix file and the test that keeps
// it equal to the table of 08-profiles-roadmap-adrs.md section 1.6
// (ADR-059 decision 3). The harness that runs the cells is a later package.
package failurematrix

import (
	"os"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
)

const (
	docPath    = "../../docs/architecture/08-profiles-roadmap-adrs.md"
	matrixPath = "matrix.yaml"
	tableHead  = "| Failure | Optional row at boot |"
)

// table is the failure policy table: its column headers and, for each row,
// the failure and the five cell texts.
type table struct {
	columns []string
	rows    [][]string
}

func splitRow(line string) []string {
	var cells []string
	for _, c := range strings.Split(strings.Trim(strings.TrimSpace(line), "|"), " | ") {
		cells = append(cells, strings.TrimSpace(c))
	}
	return cells
}

func readTable(t *testing.T) table {
	t.Helper()
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	var tb table
	for i, l := range lines {
		if !strings.HasPrefix(l, tableHead) {
			continue
		}
		tb.columns = splitRow(l)[1:]
		for _, r := range lines[i+2:] { // skip the separator line
			if !strings.HasPrefix(r, "|") {
				break
			}
			tb.rows = append(tb.rows, splitRow(r))
		}
		return tb
	}
	t.Fatalf("table %q not found in %s", tableHead, docPath)
	return tb
}

func TestMatrixMatchesTable(t *testing.T) {
	tb := readTable(t)
	data, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := boot.ParseMatrix(matrixPath, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(tb.columns) != 5 || len(tb.rows) == 0 {
		t.Fatalf("table has %d columns and %d rows; the parser is out of date", len(tb.columns), len(tb.rows))
	}
	if want := len(tb.rows) * len(tb.columns); len(m.Cells) != want {
		t.Fatalf("matrix has %d cells, the table has %d rows x %d columns = %d", len(m.Cells), len(tb.rows), len(tb.columns), want)
	}
	i := 0
	for r, row := range tb.rows {
		if len(row) != len(tb.columns)+1 {
			t.Fatalf("table row %d has %d cells, want %d", r+1, len(row), len(tb.columns)+1)
		}
		for c, col := range tb.columns {
			cell := m.Cells[i]
			i++
			where := "row " + row[0] + ", column " + col
			if cell.Failure != row[0] {
				t.Errorf("cell %d: failure %q, table has %q", i, cell.Failure, row[0])
			}
			if cell.Column != col {
				t.Errorf("%s: column %q, table has %q", where, cell.Column, col)
			}
			if cell.Outcome != row[c+1] {
				t.Errorf("%s: outcome %q, table has %q", where, cell.Outcome, row[c+1])
			}
		}
	}
}

// TestTableHasNoCopy: 08 section 1.6 is the only failure matrix, so the design record
// links to it instead of holding a copy that drifts from it.
func TestTableHasNoCopy(t *testing.T) {
	const record = "../../docs/design/plugin-system-2026-11.md"
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), tableHead) {
		t.Errorf("%s holds a copy of the failure policy table; link to 08 section 1.6 instead", record)
	}
}

// TestS1CellsAreRunnable keeps the S1 cells honest: each states an exit, a
// ready value and a diagnostic, and the tenant tree column is never S1.
func TestS1CellsAreRunnable(t *testing.T) {
	data, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := boot.ParseMatrix(matrixPath, data)
	if err != nil {
		t.Fatal(err)
	}
	s1 := 0
	for _, c := range m.Cells {
		if c.Column == "Tenant tree" && c.Since == "S1" {
			t.Errorf("%q: the tenant tree column joins at S4b", c.Failure)
		}
		if c.Since == "S1" {
			s1++
		}
	}
	if s1 == 0 {
		t.Error("no cell is marked S1")
	}
}
