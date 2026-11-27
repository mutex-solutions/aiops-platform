// SPDX-License-Identifier: Apache-2.0

package def_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	spine "github.com/mutex-solutions/aiops-platform/test/dist/def/spine/v0"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// The definition.yaml of each test definition agrees with its Go keys, as the test of
// every definition does (ADR-060 decision 7): the same keys, majors and classes. Each
// declares suite revision 0, no suite (ADR-060 decision 55).
func TestTestDefinitionsAgreeWithTheirKeys(t *testing.T) {
	const mod = "github.com/mutex-solutions/aiops-platform/test/dist/def/"
	for dir, keys := range map[string][]kernel.AnyKey{
		"spine/v0": {spine.Key},
		"toy/v0":   {toy.StoreKey, toy.PolicyKey, toy.FaultKey},
	} {
		src, err := os.ReadFile(filepath.Join(dir, "definition.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		d, err := plugingen.ParseDefinition(mod+dir, src)
		if err != nil {
			t.Fatal(err)
		}
		if d.Suite != 0 || len(d.Classes) != len(keys) {
			t.Errorf("%s: suite %d, classes %v", dir, d.Suite, d.Classes)
		}
		for _, k := range keys {
			pb, cb, _ := kernel.KeyClasses(k)
			want := plugingen.Class{Provider: class(pb), Consumer: class(cb)}
			if got, ok := d.Classes[k.Name()]; !ok || got != want || k.Major() != d.Major {
				t.Errorf("%s: key %s@%d is %+v in definition.yaml at major %d, want %+v", dir, k.Name(), k.Major(), got, d.Major, want)
			}
		}
	}
}

func class(bound bool) string {
	if bound {
		return "bound"
	}
	return "open"
}
