// SPDX-License-Identifier: Apache-2.0

// Package aiops embeds the manifest, plugin.yaml, of every plugin of the release module
// table: the loader's and those under plugins/. This directory is the one above all of
// them, and go:embed reads no file outside the directory of its package. dist reads
// them into the table (ADR-048 decision 18) until the distribution builder generates
// the table into dist/ with the manifest of each plugin (ADR-060 decision 20).
package aiops

import "embed"

// Manifests holds loader/plugin.yaml and plugins/<dir>/plugin.yaml.
//
//go:embed loader/plugin.yaml plugins/*/plugin.yaml
var Manifests embed.FS
