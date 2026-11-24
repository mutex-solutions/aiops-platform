// SPDX-License-Identifier: Apache-2.0

// Package authoring is the part of the toolbox that aiopsctl links and hands to boot:
// plugingen, the import analyzer and plugin vet, the suites, the templates and the
// scenarios (ADR-061 decision 21), and the data of the lints of step 1.
package authoring

import (
	"github.com/mutex-solutions/aiops-platform/boot"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// SPDXVersion is the version of the SPDX license list that aiopsctl embeds.
const SPDXVersion = ""

// Tools are the parts of the toolbox that aiopsctl links.
type Tools struct {
	launch.NoTools
	SDK string
}

// Lint returns the data of the lints of step 1.
func Lint() boot.ManifestLint { return boot.ManifestLint{} }
