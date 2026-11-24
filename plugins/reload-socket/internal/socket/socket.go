// SPDX-License-Identifier: Apache-2.0

// Package socket is what reload-socket asks of the operating system. It is a stub.
package socket

import (
	"errors"
	"net"
)

// PeerUID is a stub.
func PeerUID(*net.UnixConn) (int, error) { return 0, errors.New("socket: not built yet") }
