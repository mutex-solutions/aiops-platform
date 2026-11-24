// SPDX-License-Identifier: Apache-2.0

// Package reload is the reload definition: the one queue that changes the tree
// (03-c3-kernel.md:65, :87). It holds the key, the operation and result types, the
// composition/applied event and the reload/operation event, and from 0.2 the wire
// format of the socket of the row reload-socket with the status type its client reads
// (wire.go; ADR-062 decision 8). The row that serves it is plugins/reload; the fake is
// in the fake package.
package reload
