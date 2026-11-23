// SPDX-License-Identifier: Apache-2.0

// Package echoers is the definition of the echoers registry, one of the two toy
// registries on which S2a proves the authoring path (08-profiles-roadmap-adrs.md:694;
// plugin-system-2026-11.md §5; ADR-060 decisions 5 and 39). It needs no policy key and
// carries no tenant data. The key is plural: an echoer is a contribution, registered
// through the registry and provided under no key, so only the registry's dispatcher
// calls it (00-decisions.md:206). The dispatcher is the toy plugin example-dispatch
// under examples/; the recording fake is in the fake package and the suite, with its
// obligations half and its guard half, in the conformance package.
//
// The package imports the standard library and sdk/kernel only.
package echoers
