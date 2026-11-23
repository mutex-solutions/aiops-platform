// SPDX-License-Identifier: Apache-2.0

package failurematrix

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mutex-solutions/aiops-platform/dist"
	testdist "github.com/mutex-solutions/aiops-platform/test/dist"
	"github.com/mutex-solutions/aiops-platform/test/trusted"
)

// faultDenial is what the denial of each installed fault says: the clause its one
// defect breaks. The harness asserts it beside the cell's diagnostic.
var faultDenial = map[string]string{"unserved-version": "requires toy 0.2, and the distribution serves [0.1]",
	"unsigned-row": "no verified signer is recorded", "foreign-attestation": "the attestation names another subject"}

// installFault writes into a fresh home the fault plugin of the test distribution as
// plugin install keeps an in-process artifact, with the one defect of the fault in its
// generated manifest, signature or attestation (ADR-060 decision 31), and makes the
// profile smoke name its bundle last, as install appends it (ADR-047 decision 5). The
// bundle inserts the row "fault" disabled, which boot mounts under the artifact's group
// as "<fault>:fault", and a patch of the cell, <fault>.optional.yml or
// <fault>.required.yml, enables it:
//   - unserved-version: the artifact was built against toy 0.2, which the test
//     distribution does not serve (check 2);
//   - unsigned-row: no verified signer is recorded beside its digest, and allowUnsigned
//     does not list it (check 5);
//   - foreign-attestation: its statement names the digest of the manifest, the subject
//     of the plugin's module-table entry, and not the artifact's (check 10).
func installFault(home, fault string) error {
	id := "aiops-test/" + fault
	mods := testdist.Modules()
	i := slices.IndexFunc(mods, func(m dist.Module) bool { return m.Plugin.ID == id })
	if i < 0 {
		return fmt.Errorf("the test distribution has no fault plugin %s", id)
	}
	man := mods[i].Manifest
	a := trusted.Artifact{Manifest: man, Definitions: `{"toy": {"version": "0.1"}}`,
		Bundle: []byte("- insert:\n    - {id: fault, plugin: " + id + ", roles: [api], enabled: false}\n")}
	switch fault {
	case "unserved-version":
		a.Manifest = bytes.Replace(man, []byte(`toy: ["0.1"]`), []byte(`toy: ["0.2"]`), 1)
		a.Definitions = `{"toy": {"version": "0.2"}}`
	case "unsigned-row":
		a.Unsigned = true
	case "foreign-attestation":
		sum := sha256.Sum256(man)
		a.Subject = hex.EncodeToString(sum[:])
	}
	if err := trusted.Install(home, a); err != nil {
		return err
	}
	profile := filepath.Join(home, "profiles", "smoke", "profile.yaml")
	if err := os.MkdirAll(filepath.Dir(profile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(profile, []byte("bundles: [smoke, "+id+"]\n"), 0o644)
}
