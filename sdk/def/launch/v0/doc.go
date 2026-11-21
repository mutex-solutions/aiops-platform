// SPDX-License-Identifier: Apache-2.0

// Package launch is the definition of the six launch keys: profileContext,
// catalog, cmdline, ready, killSwitches and appExit (03-c3-kernel.md:63;
// 07-c4-plugin-sdk.md:1345; plugin-system-2026-11.md §5). The launcher provides
// them in its prepare step, before any row mounts (03-c3-kernel.md:81,
// 03-c3-kernel.md:360), so every key is provider-bound to the launcher
// (07-c4-plugin-sdk.md:555) and appExit is also consumer-bound
// (03-c3-kernel.md:63; 07-c4-plugin-sdk.md:556).
//
// Reload reaches composition through profileContext (ADR-059 decision 7): it
// holds Compose, Admit and Plan, the three pure steps of 03-c3-kernel.md:376-378.
// The docs call the service "a launch service that boot implements" without
// naming the key. profileContext is chosen because it is the key that holds the
// profile locations and layers a candidate is composed from
// (plugin-system-2026-11.md §5); a seventh key would contradict "six launch
// keys". Admission reads the module table, which boot holds, so Admit needs no
// second key. The project owner confirmed this choice (ADR-059 decision 35).
//
// The types here are plain, read-only data. The package does not import boot:
// boot imports this package and converts its own types to these.
package launch
