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
// in step"), and each is a cap fixed at the S2a exit, none provisional: ADR-060 decision
// 35 makes each the count measured on the commit that closes S2a plus ten per cent,
// rounded up to the next hundred. The row package of the operations library has a cap
// of its own beside the library's, which measures internal/operations only.
func TestLineCapsInStep(t *testing.T) {
	mk := capsOf(t, "../../Makefile", "$(KC)")
	ci := capsOf(t, "../../.github/workflows/ci.yml", "go run ./tools/kernelcheck")
	if !slices.Equal(mk, ci) {
		t.Errorf("the line caps differ:\nMakefile: %q\nci.yml:   %q", mk, ci)
	}
	for _, want := range []string{"sdk/kernel 8200", "loader 1300", "boot 10900", "plugins/operations 200 internal",
		"plugins/operations/internal/operations 5100", "cmd/aiopsctl:internal/aiopsctl 300", "internal/builder 4100",
		"sdk/def/operations/v0/fake 1200", "sdk/def/operations/v0/conformance 1300", "internal/vet 1300"} {
		if !slices.Contains(mk, want) || !slices.Contains(ci, want) {
			t.Errorf("no cap %q in the Makefile and ci.yml", want)
		}
	}
	for _, c := range mk {
		if strings.Contains(c, "provisional") {
			t.Errorf("a provisional cap after the S2a exit: %q", c)
		}
	}
	// The launcher of internal/aiopsctl came out of cmd/aiopsctl, whose row of ADR-060
	// decision 35 is the only one: the two share its budget, and neither has a cap of its
	// own.
	for _, c := range slices.Concat(mk, ci) {
		if strings.HasPrefix(c, "cmd/aiopsctl ") || strings.HasPrefix(c, "internal/aiopsctl ") {
			t.Errorf("a cap of its own: %q", c)
		}
	}
}
