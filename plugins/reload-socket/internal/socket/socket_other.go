// SPDX-License-Identifier: Apache-2.0

//go:build !linux

// Package socket is what reload-socket asks of the operating system (ADR-062 decisions
// 3 to 5). It serves on Linux only.
package socket

import (
	"errors"
	"net"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// errLinuxOnly is why the row fails at apply on any system but Linux, rather than
// serve with fewer checks: the server runs on Linux, and SO_PEERCRED is Linux's
// (ADR-062 decision 5). The row still compiles here, since aiopsctl links the same
// module table.
var errLinuxOnly = errors.New("reload-socket: serves on Linux only, where SO_PEERCRED names the peer's uid")

// Listen fails: the row serves on Linux only.
func Listen(string, string) (*net.UnixListener, kernel.Disposer, error) {
	return nil, nil, errLinuxOnly
}

// PeerUID fails: the row serves on Linux only.
func PeerUID(*net.UnixConn) (int, error) { return 0, errLinuxOnly }
