// SPDX-License-Identifier: Apache-2.0

// Package aiops embeds the manifest, plugin.yaml, of every plugin of the release module
// table: the loader's and those under plugins/. This directory is the one above all of
// them, and go:embed reads no file outside the directory of its package. The module
// table that plugingen generates into dist/ from dist.yaml reads each plugin's manifest
// from here (ADR-048 decision 18; ADR-060 decision 20).
package aiops

import "embed"

// Manifests holds loader/plugin.yaml and plugins/<dir>/plugin.yaml.
//
//go:embed loader/plugin.yaml plugins/*/plugin.yaml
var Manifests embed.FS
