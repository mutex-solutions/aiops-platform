// SPDX-License-Identifier: Apache-2.0

package teststate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	loaderdef "github.com/mutex-solutions/aiops-platform/sdk/def/loader/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// KernelFile is the file, beside the state file, in which the test binary records the
// kernel's own view of the tree: the oracle that the suites hold the loader's rows to,
// since the loader's records alone cannot show that they diverged from the kernel.
const KernelFile = "kernel.json"

// Fiber is one fiber of the kernel that is not DISPOSED, root excluded.
type Fiber struct {
	ID     uint64 `json:"id"`
	Plugin string `json:"plugin"`
	State  string `json:"state"`
}

// RecordKernel is the test binary's boot.RunOptions.Kernel: it starts a goroutine of
// the process, outside the tree, that reads root.Inspect every 10ms and writes the
// fibers to runDir/KernelFile when they change, until the kernel refuses the read.
func RecordKernel(root *kernel.Root, runDir string) {
	go func() {
		var last []byte
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for range tick.C {
			v, err := root.Inspect(context.Background())
			if err != nil {
				return
			}
			fibers := []Fiber{}
			for _, f := range v.Fibers {
				if f.ID != 0 && f.State != kernel.Disposed {
					fibers = append(fibers, Fiber{ID: f.ID, Plugin: f.Plugin, State: string(f.State)})
				}
			}
			data, _ := json.Marshal(fibers)
			if !bytes.Equal(data, last) && runfile.Write(runDir, KernelFile, data) == nil {
				last = data
			}
		}
	}()
}

// TreeProblems compares the rows the loader says are mounted with the kernel's fibers:
// for the plugin of every row the loader knows, and for the group plugins, the states
// of the kernel's fibers must be those of the mounted rows, one fiber per row. The
// launcher's own fibers run no row's plugin and are left out.
func TreeProblems(s State, fibers []Fiber) []string {
	rows, live := map[string][]string{}, map[string][]string{}
	for _, p := range []string{loaderdef.GroupPlugin, loaderdef.IncludePlugin} {
		rows[p] = nil
	}
	for _, r := range s.Rows {
		switch {
		case r.Mount == string(loaderdef.Mounted):
			rows[r.Plugin] = append(rows[r.Plugin], r.State)
		case r.Plugin != "":
			if _, ok := rows[r.Plugin]; !ok {
				rows[r.Plugin] = nil // a row's plugin with no row mounted: it has no fiber
			}
		}
	}
	for _, f := range fibers {
		if _, ok := rows[f.Plugin]; ok {
			live[f.Plugin] = append(live[f.Plugin], f.State)
		}
	}
	var bad []string
	for _, p := range slices.Sorted(maps.Keys(rows)) {
		want, got := slices.Sorted(slices.Values(rows[p])), slices.Sorted(slices.Values(live[p]))
		if !slices.Equal(want, got) {
			bad = append(bad, fmt.Sprintf("plugin %s: the loader's mounted rows are %v, the kernel's fibers %v", p, want, got))
		}
	}
	return bad
}
