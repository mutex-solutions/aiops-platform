// SPDX-License-Identifier: Apache-2.0

package plugingen

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/boot"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
)

// The signed distribution manifest that Distribution writes for the test binary is the
// one it embeds, and boot reads it (ADR-060 decisions 20 and 28): its module table is
// the binary's, each plugin with its manifest byte for byte and a statement whose
// subject is that manifest; a test plugin names its test definition at suite revision
// 0, with no half (ADR-060 decision 55); and every module of the repository is listed
// by its path, with no version and no sum (ADR-020 decision 28).
func TestDistributionOfTheTestBinary(t *testing.T) {
	root := filepath.Join("..", "..")
	input, err := os.ReadFile(filepath.Join(root, "test", "dist", "dist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	register, err := os.ReadFile(filepath.Join(root, "tools", "upstream-components.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Distribution(root, "./test/dist/cmd/aiops-test", input, register)
	if err != nil || !bytes.Equal(out, testdist.Signed) {
		t.Fatalf("%v: test/dist/distribution.yaml is not what Distribution writes", err)
	}
	d, err := boot.ReadDistribution("distribution.yaml", out)
	if err != nil {
		t.Fatal(err)
	}
	mods := testdist.Modules()
	if len(d.Plugins) != len(mods) {
		t.Fatalf("%d plugins in the manifest, %d in the module table", len(d.Plugins), len(mods))
	}
	for _, m := range mods {
		p := d.Plugins[m.Plugin.ID]
		st, err := boot.ReadAttestation(m.Plugin.ID, p.Attestation)
		if err != nil || !bytes.Equal(p.Manifest, m.Manifest) || st.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256(p.Manifest)) || !strings.Contains(p.Package, "/") {
			t.Fatalf("%s: %v, statement %+v", m.Plugin.ID, err, st)
		}
	}
	toy := mustStatement(t, d, "aiops-test/toy-provider").Definitions["toy"]
	if toy.Suite == nil || *toy.Suite != 0 || toy.Halves != nil {
		t.Errorf("toy-provider attests %+v", toy)
	}
	for _, m := range d.Modules {
		if strings.HasPrefix(m.Path, modPath) && (m.Version != "" || m.Sum != "" || m.Licence != "Apache-2.0") {
			t.Errorf("module %+v", m)
		}
	}
	for _, bad := range []struct{ input, register, main string }{
		{"bogus: 1", string(register), "./test/dist/cmd/aiops-test"},
		{string(input), "entries: 1", "./test/dist/cmd/aiops-test"},
		{string(input), string(register), "./no/such/package"},
	} {
		if _, err := Distribution(root, bad.main, []byte(bad.input), []byte(bad.register)); err == nil {
			t.Errorf("Distribution took %q", bad)
		}
	}
}

func mustStatement(t *testing.T, d boot.Distribution, id string) boot.Attestation {
	t.Helper()
	st, err := boot.ReadAttestation(id, d.Plugins[id].Attestation)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
