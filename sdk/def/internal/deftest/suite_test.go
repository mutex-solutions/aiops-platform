// SPDX-License-Identifier: Apache-2.0

package deftest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Ran records a suite that ran into the directory that RecordVar names, the
// record that the release build and step 5 of plugin check attest (ADR-060 decisions 18
// and 50): the directory of the package, the definition and the halves. With no
// directory it records nothing, and a suite that skipped is not recorded.
func TestRanRecordsWhatRan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(RecordVar, "")
	t.Run("no directory", func(t *testing.T) { Ran(t, "echoers", Half(true)) })
	t.Setenv(RecordVar, dir)
	t.Run("ran", func(t *testing.T) { Ran(t, "echoers", Half(false)) })
	t.Run("skipped", func(t *testing.T) { Ran(t, "echoers", Half(true)); t.Skip("a suite that skipped is not recorded") })
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("records %v, %v; want one", files, err)
	}
	wd, err := os.Getwd()
	b, rerr := os.ReadFile(files[0])
	var got []string
	if err != nil || rerr != nil || json.Unmarshal(b, &got) != nil || !slices.Equal(got, []string{wd, "echoers", "obligations"}) {
		t.Errorf("the record %s (%v, %v); want the package's directory %s, echoers and obligations", b, err, rerr, wd)
	}
}
