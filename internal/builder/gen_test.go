// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"strings"
	"testing"
)

// Both binaries that dist build writes, a release's, a site's and the throwaway one of
// plugin smoke, embed the release's shipped bundles and profiles, as cmd/aiops and
// cmd/aiopsctl do (ADR-060 decision 27). The builder job boots the server of plugin
// smoke on a home that holds no aiops-core (cmd/aiopsctl, TestDistBuildSignAndUse).
func TestGeneratedMainsShipTheReleaseFiles(t *testing.T) {
	for name, src := range map[string]string{"cmd/aiops": aiopsMain, "cmd/aiopsctl": aiopsctlMain} {
		if !strings.Contains(src, "Shipped: dist.Shipped()") {
			t.Errorf("the %s that dist build writes embeds no shipped file of the release", name)
		}
	}
}
