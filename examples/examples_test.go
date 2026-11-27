// SPDX-License-Identifier: Apache-2.0

package examples_test

import (
	"slices"
	"testing"
	"time"

	"github.com/mutex-solutions/aiops-platform/examples"
	clockfake "github.com/mutex-solutions/aiops-platform/sdk/def/clock/v0/fake"
	launchfake "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/fake"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The toy plugins of the test release mount together, with fakes for what the release
// table provides, and each has an id of its own.
func TestPluginsMountTogether(t *testing.T) {
	root := kerneltest.New(t)
	l, lp := launchfake.New()
	l.Flags = map[string]string{"home": t.TempDir()}
	_, cp := clockfake.NewAt(time.Unix(0, 0))
	kerneltest.Mount(root, append([]kernel.Plugin{lp, cp}, examples.Plugins()...)...)
	var ids []string
	for _, p := range examples.Plugins() {
		ids = append(ids, p.ID)
	}
	slices.Sort(ids)
	if want := []string{"aiops/example-caller", "aiops/example-dispatch", "aiops/example-echoer", "aiops/example-stamper"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}
