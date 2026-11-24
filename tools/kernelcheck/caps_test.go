package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// capsOf returns the arguments of each `kernelcheck lines` call of a file, with the
// shell's line ends removed and make's $$ read as $.
func capsOf(t *testing.T, file, call string) []string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(regexp.QuoteMeta(call) + ` lines (.+)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		s := strings.ReplaceAll(m[1], "$$", "$")
		for _, end := range []string{`\`, "|| exit 1;", "&&"} {
			s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), end))
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// make check and the test job of CI run the same line caps (the Makefile: "Keep the two
// in step"), and the row package of the operations library has a cap of its own beside
// the library's: ADR-060 decision 35 and ADR-061, Consequences, cap "the operations
// library, plugins/operations" at 4,500 lines since the second note of 2026-11-24 to
// decision 35, and the library's cap measures internal/operations only.
func TestLineCapsInStep(t *testing.T) {
	mk := capsOf(t, "../../Makefile", "$(KC)")
	ci := capsOf(t, "../../.github/workflows/ci.yml", "go run ./tools/kernelcheck")
	if !slices.Equal(mk, ci) {
		t.Errorf("the line caps differ:\nMakefile: %q\nci.yml:   %q", mk, ci)
	}
	for _, want := range []string{"plugins/operations 4500 provisional internal", "plugins/operations/internal/operations 4500 provisional"} {
		if !slices.Contains(mk, want) || !slices.Contains(ci, want) {
			t.Errorf("no cap %q in the Makefile and ci.yml", want)
		}
	}
}
