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
// fail, on the call graph of vta; a mux of its own is no default mux. The default mux
// and the default registry count however init reaches them: through an interface
// value, a method value, a variable that holds them, a parameter, or a factory of
// promauto; the default mux used after init does not. The exceptions table, keyed by
// package and version, admits gRPC's proto codec at its pin and gob's own registration,
// which fail without it (ADR-044 decision 21; ADR-020 decision 27).
func TestInitDenyList(t *testing.T) {
	got := scan(t)
	const mux, reg = "a default-mux handler: net/http.DefaultServeMux in example.test/inits/cmd/", "a default-registry collector: github.com/prometheus/client_golang/prometheus.DefaultRegisterer in example.test/inits/cmd/"
	for main, want := range map[string]string{
		"sqldriver":   "a database/sql driver: database/sql.Register in example.test/inits/cmd/sqldriver.init#1",
		"mux":         "a default-mux handler: net/http.HandleFunc in example.test/inits/cmd/mux.init#1",
		"defaultmux":  mux + "defaultmux.init#1",
		"promauto":    "a default-registry collector: github.com/prometheus/client_golang/prometheus/promauto.NewCounter in example.test/inits/cmd/promauto.init",
		"iface":       "a database/sql driver: database/sql.Register in (example.test/inits/cmd/iface.viaMethod).do",
		"funcval":     "a database/sql driver: database/sql.Register in example.test/inits/cmd/funcval.viaValue",
		"ifacemux":    mux + "ifacemux.init#1",
		"methodvalue": mux + "methodvalue.init",
		"aliasmux":    mux + "aliasmux.init",
		"parammux":    mux + "parammux.init#1",
		"aliasreg":    reg + "aliasreg.init",
		"promwith":    reg + "promwith.init",
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
	// An exception admits the call at the version it was reviewed at only: at another,
	// it needs a new review (owner, 2026-11-25).
	reviewed, moved := exceptions, []exception(nil)
	defer func() { exceptions = reviewed }()
	for _, x := range reviewed {
		x.version += "-another"
		moved = append(moved, x)
	}
	for name, table := range map[string][]exception{"without its exception": nil, "with its exception at another version": moved} {
		exceptions = table
		got = scan(t)
		for _, from := range []string{"encoding/gob", "google.golang.org/grpc/encoding/proto"} {
			if len(got[from]) != 1 {
				t.Errorf("%s %s: %v", from, name, got[from])
			}
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
