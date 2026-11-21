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
	writtenFiles(t, root, func(rel string, lines []string) {
		for i, l := range lines {
			if byLine.MatchString(l) {
				t.Errorf("%s:%d cites the design record by line; cite its section, plugin-system-2026-11.md §N", rel, i+1)
			}
			for _, m := range bySection.FindAllStringSubmatch(l, -1) {
				if !sections[m[1]] {
					t.Errorf("%s:%d cites section %s of the design record, which has no such section", rel, i+1, m[1])
				}
			}
		}
	})
	if len(sections) == 0 {
		t.Fatal("no section found in the design record; the parser is out of date")
	}
}

// writtenFiles calls fn with the lines of each file people write under root: Go,
// Markdown, YAML, shell and the Makefile.
func writtenFiles(t *testing.T, root string, fn func(rel string, lines []string)) {
	read := map[string]bool{".go": true, ".md": true, ".yml": true, ".yaml": true, ".sh": true}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
		fn(rel, strings.Split(string(b), "\n"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestConfigRefsRuleByKeyCitesItsDecision: decision 18 pulls config-refs into the
// required set where a required row of the role holds a reference, and decision 24 has
// an active provider of a required key in the root realm, a row required by key, count
// as such a row; under both, disabling config-refs is valid only while no row required
// by row or by key holds a reference. So a paragraph that cites decision 18 for the
// rule by key cites decision 24 too, and a sentence beside that citation which calls
// disabling it valid or optional says while what. The entry of decision 18 in
// the build notes of ADR-059 is such a citation, and the entry of decision 24 there is
// the decision itself. The rule by key is also put as a row "of" or "inside" the
// required set, since that set holds a provider of a required key.
func TestConfigRefsRuleByKeyCitesItsDecision(t *testing.T) {
	cites := regexp.MustCompile(`\bdecision 18\b`)
	entry := regexp.MustCompile(`^- \*\*18\.\*\* `)
	decided := regexp.MustCompile(`\bdecision 24\b`)
	byKey := regexp.MustCompile(`\bby key\b|\ba required key\b|\bof the required set\b|\binside the (?:required )?set\b`)
	optional := regexp.MustCompile(`disabl[^.]*config-refs[^.]*\b(?:valid|optional)\b[^.]*`)
	n := 0
	writtenFiles(t, "../..", func(rel string, lines []string) {
		adr := filepath.Base(rel) == "ADR-059-s1-scope.md"
		for _, p := range paragraphs(rel, lines) {
			text := strings.ToLower(p.text)
			if !cites.MatchString(text) && (!adr || !entry.MatchString(text)) {
				continue
			}
			n++
			if byKey.MatchString(text) && !decided.MatchString(text) && (!adr || !strings.HasPrefix(text, "24. ")) {
				t.Errorf("%s:%d cites decision 18 for the rule by key and not decision 24, which decided it", rel, p.line)
			}
			for _, s := range optional.FindAllString(text, -1) {
				if !strings.Contains(s, "while no") {
					t.Errorf("%s:%d calls disabling config-refs valid with no condition: %q", rel, p.line, s)
				}
			}
		}
	})
	if n == 0 {
		t.Fatal("no paragraph cites decision 18; the parser is out of date")
	}
}

// TestDecision37LeavesItsPointsOpen: decision 37 has the definition points that S1
// recorded in its own code settled before S2a, and settles none of them itself. So a
// comment that cites it still calls its point an open point, and the decision accepts
// nothing as built.
func TestDecision37LeavesItsPointsOpen(t *testing.T) {
	cites := regexp.MustCompile(`\bdecision 37\b`)
	accepted := regexp.MustCompile(`\baccepted as built\b|\bstands at s1\b`)
	n := 0
	writtenFiles(t, "../..", func(rel string, lines []string) {
		adr := filepath.Base(rel) == "ADR-059-s1-scope.md"
		for _, p := range paragraphs(rel, lines) {
			text := strings.ToLower(p.text)
			switch {
			case adr && strings.HasPrefix(text, "37. "):
				n++
				if s := accepted.FindString(text); s != "" {
					t.Errorf("%s:%d decision 37 says %q, which the owner did not decide", rel, p.line, s)
				}
			case filepath.Ext(rel) != ".md" && cites.MatchString(text):
				n++
				if !strings.Contains(text, "open point") {
					t.Errorf("%s:%d cites decision 37 and no longer calls its point open", rel, p.line)
				}
			}
		}
	})
	if n == 0 {
		t.Fatal("found neither decision 37 nor a citation of it; the parser is out of date")
	}
}

// TestLimitsFilesCiteTheirDecisions: decision 30 makes the release limits the defaults
// for now and decision 39 confirms the test limits, so the comment of each limits file
// cites its decision and calls its values an open point no longer.
func TestLimitsFilesCiteTheirDecisions(t *testing.T) {
	for file, d := range map[string]string{"dist/limits.yaml": "ADR-059 decision 30", "test/dist/limits.yaml": "ADR-059 decision 39"} {
		data, err := os.ReadFile(filepath.Join("../..", file))
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for _, p := range paragraphs(file, strings.Split(string(data), "\n")) {
			text += " " + p.text
		}
		if !strings.Contains(text, d) || strings.Contains(text, "open point") {
			t.Errorf("%s: its comment must cite %s and call nothing an open point: %q", file, d, text)
		}
	}
}

type paragraph struct {
	line int
	text string
}

// paragraphs splits a file into the units a reader takes as one statement: a paragraph
// or list item in Markdown, a run of comment lines ("//" or "#") elsewhere.
func paragraphs(rel string, lines []string) []paragraph {
	md := filepath.Ext(rel) == ".md"
	item := regexp.MustCompile(`^([-*]|\d+\.) `)
	var out []paragraph
	open := false
	for i, l := range lines {
		s := strings.TrimSpace(l)
		text, ok := s, s != "" && !strings.HasPrefix(s, "#")
		if !md {
			if text, ok = strings.CutPrefix(s, "//"); !ok {
				text, ok = strings.CutPrefix(s, "#")
			}
			text = strings.TrimSpace(text)
			ok = ok && text != ""
		}
		switch {
		case !ok:
			open = false
		case !open || md && item.MatchString(s):
			out, open = append(out, paragraph{i + 1, text}), true
		default:
			out[len(out)-1].text += " " + text
		}
	}
	return out
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
