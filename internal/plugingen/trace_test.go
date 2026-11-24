// SPDX-License-Identifier: Apache-2.0

package plugingen

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// No binary of the repository links golang.org/x/net/trace, whose init registers
// /debug/requests and /debug/events on http.DefaultServeMux: gRPC reaches every binary
// through the signing library, and each build has the tag grpcnotrace (ADR-020
// decisions 6 and 27; ADR-044 decision 21). The graphs of cmd/aiops and cmd/aiopsctl
// under BuildFlags hold no such package; make check and CI build with the tag in
// GOFLAGS; and Distribution refuses a graph that holds the package, so the signed
// manifest cannot record it.
func TestReleaseBuildsLinkNoTrace(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, main := range []string{"./cmd/aiops", "./cmd/aiopsctl"} {
		out, err := goCmd(root, slices.Concat([]string{"list", "-deps"}, BuildFlags, []string{main})...)
		if err != nil {
			t.Fatal(err)
		}
		if pkgs := strings.Fields(string(out)); slices.Contains(pkgs, tracePackage) {
			t.Errorf("%s with %q links %s", main, BuildFlags, tracePackage)
		}
	}
	tags := regexp.MustCompile(`GOFLAGS\s*(\?=|:)\s*.*-tags=grpcnotrace`)
	for _, f := range []string{"Makefile", ".github/workflows/ci.yml"} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		if !tags.Match(b) {
			t.Errorf("%s sets no GOFLAGS with -tags=grpcnotrace", f)
		}
	}
	if err := traceFree("./cmd/x", []string{"golang.org/x/net/http2", tracePackage}); err == nil || !strings.Contains(err.Error(), "grpcnotrace") {
		t.Errorf("a graph with %s: %v, want a refusal that names the tag", tracePackage, err)
	}
	if err := traceFree("./cmd/x", []string{"golang.org/x/net/http2"}); err != nil {
		t.Error(err)
	}
}
