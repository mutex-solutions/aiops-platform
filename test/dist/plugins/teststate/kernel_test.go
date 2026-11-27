// SPDX-License-Identifier: Apache-2.0

package teststate

import (
	"os"
	"strings"
	"testing"

	clockfake "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	compositionfake "github.com/mutex-solutions/aiops-platform/sdk/def/composition/v0/fake"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	loaderfake "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0/fake"
	reloadfake "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/cycle"
)

// With no dir in the config and no --home, as in the generated cycle test, the row
// writes its state file into a directory of its own under the temporary directory
// and removes it when it unloads, so a run leaves no file behind, in the source tree
// or anywhere else, and two runs never share a file.
func TestNoDirLeavesNoFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	var fakes []cycle.Fake
	clock, clockPlugin := clockfake.New()
	composition, compositionPlugin := compositionfake.New()
	launch, launchPlugin := launchfake.New()
	loader, loaderPlugin := loaderfake.New()
	reload, reloadPlugin := reloadfake.New()
	fakes = append(fakes, cycle.Fake{Plugin: clockPlugin, Inventory: clock.Inventory},
		cycle.Fake{Plugin: compositionPlugin, Inventory: composition.Inventory},
		cycle.Fake{Plugin: launchPlugin, Inventory: launch.Inventory},
		cycle.Fake{Plugin: loaderPlugin, Inventory: loader.Inventory},
		cycle.Fake{Plugin: reloadPlugin, Inventory: reload.Inventory})
	cycle.Run(t, New(), nil, nil, fakes...)
	if left, err := os.ReadDir(tmp); err != nil || len(left) != 0 {
		t.Fatalf("the row left %v in the temporary directory (%v)", left, err)
	}
}

// TreeProblems holds the loader's mounted rows to the kernel's fibers, plugin by
// plugin, and leaves the launcher's own fibers out.
func TestTreeProblems(t *testing.T) {
	s := State{Rows: []Row{
		{ID: "a", Plugin: "t/p", Mount: "mounted", State: "ACTIVE"},
		{ID: "b", Plugin: "t/p", Mount: "mounted", State: "PENDING"},
		{ID: "off", Plugin: "t/q", Mount: "disabled"},
		{ID: "g", Plugin: "core:group", Mount: "mounted", State: "ACTIVE"},
	}}
	agree := []Fiber{{1, "aiops/loader", "ACTIVE"}, {2, "t/p", "PENDING"}, {3, "t/p", "ACTIVE"}, {4, "core:group", "ACTIVE"}}
	if bad := TreeProblems(s, agree); len(bad) != 0 {
		t.Fatalf("problems of an agreeing tree: %v", bad)
	}
	for name, tc := range map[string]struct {
		fibers []Fiber
		want   string
	}{
		"a second fiber of a row":   {append(agree, Fiber{5, "t/p", "ACTIVE"}), "plugin t/p"},
		"a row with no fiber":       {agree[1:3], "plugin core:group"},
		"another state":             {[]Fiber{{2, "t/p", "FAILED"}, {3, "t/p", "ACTIVE"}, {4, "core:group", "ACTIVE"}}, "plugin t/p"},
		"a group fiber, no row":     {append(agree, Fiber{6, "core:include", "PENDING"}), "plugin core:include"},
		"a fiber of a disabled row": {append(agree, Fiber{7, "t/q", "ACTIVE"}), "plugin t/q"},
		// A fiber left running for a row the loader removed or forgot, whose plugin no
		// listed row runs.
		"a fiber of a forgotten row": {append(agree, Fiber{8, "t/c", "ACTIVE"}), "plugin t/c"},
		"a fiber of a removed row":   {append(agree, Fiber{9, "aiops-test/panic-apply", "FAILED"}), "plugin aiops-test/panic-apply"},
	} {
		bad := TreeProblems(s, tc.fibers)
		if got := strings.Join(bad, "; "); tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: problems %q, want %q", name, got, tc.want)
		}
	}
}
