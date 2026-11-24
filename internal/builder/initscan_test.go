// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
)

// fixtureEnv is the environment of the scan of testdata/inits, a module of its own that
// reaches nothing outside it.
func fixtureEnv() []string {
	return append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0")
}

// scan returns, by main package of testdata/inits/cmd, the findings in the functions of
// that package or of the module's fixture registries reached from its init.
func scan(t *testing.T) map[string][]Finding {
	t.Helper()
	fs, err := ScanInits(context.Background(), filepath.Join("testdata", "inits"), fixtureEnv(), "./cmd/...")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]Finding{}
	for _, f := range fs {
		main := strings.TrimPrefix(f.From, "example.test/inits/cmd/")
		out[main] = append(out[main], f)
	}
	return out
}

// The deny-list (ADR-044 decisions 11, 12, 24 and 25): per kind, a registration from
// init fails, in the main package or in the package that declares the registry; a
// registration reached through an interface method and one through a function value
// fail, on the call graph of vta; a mux of its own is no default mux. The exceptions
// table, keyed by package and version, admits gRPC's proto codec at its pin and gob's
// own registration, which fail without it (ADR-044 decision 21; ADR-020 decision 27).
func TestInitDenyList(t *testing.T) {
	got := scan(t)
	for main, want := range map[string]string{
		"sqldriver":  "a database/sql driver: database/sql.Register in example.test/inits/cmd/sqldriver.init#1",
		"mux":        "a default-mux handler: net/http.HandleFunc in example.test/inits/cmd/mux.init#1",
		"defaultmux": "a default-mux handler: (*net/http.ServeMux).Handle@net/http.DefaultServeMux in example.test/inits/cmd/defaultmux.init#1",
		"promauto":   "a default-registry collector: github.com/prometheus/client_golang/prometheus/promauto.NewCounter in example.test/inits/cmd/promauto.init",
		"iface":      "a database/sql driver: database/sql.Register in (example.test/inits/cmd/iface.viaMethod).do",
		"funcval":    "a database/sql driver: database/sql.Register in example.test/inits/cmd/funcval.viaValue",
	} {
		if !slices.ContainsFunc(got[main], func(f Finding) bool { return strings.HasPrefix(f.String(), want) }) {
			t.Errorf("%s: %v, want %q", main, got[main], want)
		}
	}
	if own := got["github.com/prometheus/client_golang/prometheus"]; len(own) != 1 || own[0].Call != "github.com/prometheus/client_golang/prometheus.MustRegister" {
		t.Errorf("the Prometheus client's own registration: %v", own)
	}
	for _, main := range []string{"clean", "gobown", "codec", "encoding/gob", "google.golang.org/grpc/encoding/proto"} {
		if len(got[main]) > 0 {
			t.Errorf("%s: %v", main, got[main])
		}
	}
	defer func(e []exception) { exceptions = e }(exceptions)
	exceptions = nil
	got = scan(t)
	for _, from := range []string{"encoding/gob", "google.golang.org/grpc/encoding/proto"} {
		if len(got[from]) != 1 {
			t.Errorf("%s without its exception: %v", from, got[from])
		}
	}
}

// A binary that links gRPC v1.82.1 fails on the default-mux kind without the tag
// grpcnotrace, through the init of golang.org/x/net/trace, and passes with it and the
// encoding/proto exception (ADR-044 decision 14; ADR-020 decision 27). It loads the
// whole program of cmd/aiops twice, so -short skips it.
func TestGRPCNeedsTheTag(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the program of cmd/aiops twice; the CI job builder runs it")
	}
	root, ctx := filepath.Join("..", ".."), context.Background()
	fs, err := ScanInits(ctx, root, append(os.Environ(), "GOFLAGS=-mod=readonly"), "./cmd/aiops")
	if err != nil || len(fs) > 0 {
		t.Fatalf("with the tag: %v, %v", err, fs)
	}
	defer func(f []string) { plugingen.BuildFlags = f }(plugingen.BuildFlags)
	plugingen.BuildFlags = nil
	fs, err = ScanInits(ctx, root, append(os.Environ(), "GOFLAGS=-mod=readonly"), "./cmd/aiops")
	if err != nil || !slices.ContainsFunc(fs, func(f Finding) bool { return f.From == tracePackage && f.Call == "net/http.HandleFunc" }) {
		t.Errorf("without the tag: %v, %v", err, fs)
	}
}
