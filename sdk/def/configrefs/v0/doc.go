// SPDX-License-Identifier: Apache-2.0

// Package configrefs is the definition of the dry run of config references: given a
// config tree, it says whether every closed reference ${env|file|secret|flag:value}
// in it resolves, and never returns a resolved value (ADR-059 decision 16). The
// config-refs row provides it, so references are still resolved by that row
// (08-profiles-roadmap-adrs.md §1.3); the launcher resolves the key to reject, at
// admission, a live generation that creates, enables or changes a row whose reference
// does not resolve (08-profiles-roadmap-adrs.md §1.6, "Rejected; the row keeps its
// instance"). The fake is in the fake package.
//
// The package imports the standard library and sdk/kernel only.
package configrefs
