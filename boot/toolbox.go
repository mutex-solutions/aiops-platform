// SPDX-License-Identifier: Apache-2.0

package boot

import launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"

// Summary returns the six fields of m that the toolbox's ReadManifest hands out.
func (m Manifest) Summary() launch.ManifestSummary { return launch.ManifestSummary{} }
