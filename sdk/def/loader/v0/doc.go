// SPDX-License-Identifier: Apache-2.0

// Package loader is the definition of the loader key: the entry tree, with
// create, update, remove, await and locate (03-c3-kernel.md:64;
// plugin-system-2026-11.md §5). The key is consumer-bound, class C
// (03-c3-kernel.md:64; 07-c4-plugin-sdk.md:556): it resolves only for the
// entries the distribution manifest lists (reload, readiness, tenant-trees).
//
// The package also declares the loader's three hooks, all sealed to the
// distribution (07-c4-plugin-sdk.md:463; plugin-system-2026-11.md §8):
// loader/patch-context (waterfall), loader/entry-init and loader/volatile-update
// (events). Only the fiber that provides the loader key dispatches them
// (07-c4-plugin-sdk.md:439).
//
// The package imports the standard library and sdk/kernel only.
package loader
