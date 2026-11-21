// Package failurematrix holds the failure matrix file and the test that keeps
// it equal to the table of 08-profiles-roadmap-adrs.md section 1.6
// (ADR-059 decision 3). The harness that runs the cells is a later package.
package failurematrix

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

// TestDesignRecordIsCitedBySection: the design record changes by amendment, and removing
// its copy of the table moved every line after it, so a reference into it names a section
// ("plugin-system-2026-11.md §9"), never a line, and the section exists. It reads what
// people write: Go, Markdown, YAML, shell and the Makefile.
func TestDesignRecordIsCitedBySection(t *testing.T) {
	const root = "../.."
	data, err := os.ReadFile(root + "/docs/design/plugin-system-2026-11.md")
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		if n, _, ok := strings.Cut(l, ". "); ok && strings.HasPrefix(n, "## ") {
			sections[strings.TrimPrefix(n, "## ")] = true
		}
	}
	byLine := regexp.MustCompile(`plugin-system-2026-11\.md:\d`)
	bySection := regexp.MustCompile(`plugin-system-2026-11\.md §(\d+)`)
	read := map[string]bool{".go": true, ".md": true, ".yml": true, ".yaml": true, ".sh": true}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == ".git" || d.Name() == ".claude" || d.Name() == "node_modules"):
			return filepath.SkipDir
		case d.IsDir() || !read[filepath.Ext(p)] && d.Name() != "Makefile":
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		for i, l := range strings.Split(string(b), "\n") {
			if byLine.MatchString(l) {
				t.Errorf("%s:%d cites the design record by line; cite its section, plugin-system-2026-11.md §N", rel, i+1)
			}
			for _, m := range bySection.FindAllStringSubmatch(l, -1) {
				if !sections[m[1]] {
					t.Errorf("%s:%d cites section %s of the design record, which has no such section", rel, i+1, m[1])
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) == 0 {
		t.Fatal("no section found in the design record; the parser is out of date")
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
