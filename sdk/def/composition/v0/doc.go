// SPDX-License-Identifier: Apache-2.0

// Package composition is the definition of the composition key: the committed
// generation as read-only data, rows, declared hooks and registrations, states,
// by scope (03-c3-kernel.md:64; plugin-system-2026-11.md §5). The loader
// provides it; the gate owners, http-server and plugin-inventory read it
// (03-c3-kernel.md:70; 07-c4-plugin-sdk.md:440 "Expected listeners").
//
// The package also holds the payload of the composition/applied event and
// NewApplied, which declares it for the owner (07-c4-plugin-sdk.md:461).
//
// The package imports the standard library and sdk/kernel only; it does not
// import boot, which converts its own types to these.
package composition
