// SPDX-License-Identifier: Apache-2.0

package launch_test

import (
	"slices"
	"testing"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

func TestRequiredSetUnmet(t *testing.T) {
	provides := func(p string) []string {
		return map[string][]string{"aiops/sql": {"sql"}, "aiops/http": {"http"}}[p]
	}
	set := launch.RequiredSet{Rows: []string{"b", "a"}, Keys: []string{"sql", "http", "jobs"}}
	for _, tc := range []struct {
		name string
		rows []launch.RowState
		want []string
	}{
		{"nothing mounted", nil, []string{"key:http", "key:jobs", "key:sql", "row:a", "row:b"}},
		{"all active", []launch.RowState{
			{ID: "a", State: kernel.Active}, {ID: "b", State: kernel.Active},
			{ID: "s", Plugin: "aiops/sql", State: kernel.Active}, {ID: "h", Plugin: "aiops/http", State: kernel.Active},
			{ID: "j", Plugin: "aiops/jobs", State: kernel.Active}}, []string{"key:jobs"}},
		{"provider failed", []launch.RowState{
			{ID: "a", State: kernel.Active}, {ID: "b", State: kernel.Failed},
			{ID: "s", Plugin: "aiops/sql", State: kernel.Failed}, {ID: "h", Plugin: "aiops/http", State: kernel.Active}},
			[]string{"key:jobs", "key:sql", "row:b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := set.Unmet(tc.rows, provides); !slices.Equal(got, tc.want) {
				t.Errorf("Unmet = %v, want %v", got, tc.want)
			}
		})
	}
	if got := (launch.RequiredSet{}).Unmet(nil, provides); len(got) != 0 {
		t.Errorf("empty set: Unmet = %v", got)
	}
}
