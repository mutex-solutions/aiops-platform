// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

// registry is the value of the built-in registry service, "plugin records, the
// fiber index" (plugin record :55). The documents give it no interface, so it
// has no methods: the core already keeps the records and the fiber index, and what
// a plugin may read of them is an open point.
type registry struct{}
