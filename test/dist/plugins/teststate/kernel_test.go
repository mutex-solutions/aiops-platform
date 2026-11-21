// SPDX-License-Identifier: Apache-2.0

package teststate

import (
	"strings"
	"testing"
)

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
