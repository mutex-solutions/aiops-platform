// SPDX-License-Identifier: Apache-2.0

//go:build linux

// Package socket is what reload-socket asks of the operating system (ADR-062 decisions
// 3 to 5): the directory check, the lock, the listener and the peer's credentials. It
// serves on Linux only.
package socket

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"

	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Listen is the setup of the row's effect (ADR-062 decision 4): it checks dir, takes
// the lock, removes a socket left behind, listens and sets the socket to mode 0600.
// where names the home in the refusal of a lock held elsewhere. The disposer closes
// the listener, which unlinks the socket file, and then the lock file, so the lock is
// free only once the socket file is gone. On a failure it returns what to undo.
func Listen(dir, where string) (*net.UnixListener, kernel.Disposer, error) {
	sock, lockPath := filepath.Join(dir, reload.SocketFile), filepath.Join(dir, reload.LockFile)
	if len(sock) > reload.MaxSocketPath {
		return nil, nil, fmt.Errorf("reload-socket: the socket path %s is %d bytes; Linux holds at most %d", sock, len(sock), reload.MaxSocketPath)
	}
	if err := checkDir(dir); err != nil {
		return nil, nil, err
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("reload-socket: %w", err)
	}
	unlock := func(context.Context) error { return lock.Close() }
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, unlock, fmt.Errorf("reload-socket: another process serves %s: %s is locked", where, lockPath)
	} else if err != nil {
		return nil, unlock, fmt.Errorf("reload-socket: lock %s: %w", lockPath, err)
	}
	// Only the holder of the lock removes a socket, so it never removes a live one.
	if fi, err := os.Lstat(sock); err == nil && fi.Mode()&fs.ModeSocket != 0 {
		if err := os.Remove(sock); err != nil {
			return nil, unlock, fmt.Errorf("reload-socket: remove the socket left behind: %w", err)
		}
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		return nil, unlock, fmt.Errorf("reload-socket: %w", err)
	}
	dispose := func(ctx context.Context) error {
		err := ln.Close()
		if errors.Is(err, net.ErrClosed) { // the closing goroutine closed it first
			err = nil
		}
		return errors.Join(err, unlock(ctx))
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		return nil, dispose, fmt.Errorf("reload-socket: %w", err)
	}
	return ln, dispose, nil
}

// checkDir holds dir to a directory, not a symlink, owned by the process's effective
// uid, with no group or other permission bit (ADR-047 decision 14).
func checkDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("reload-socket: %w", err)
	}
	euid := os.Geteuid()
	switch st, _ := fi.Sys().(*syscall.Stat_t); {
	case fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("reload-socket: %s is a symlink; it must be a directory of mode 0700 owned by uid %d", dir, euid)
	case !fi.IsDir():
		return fmt.Errorf("reload-socket: %s is not a directory", dir)
	case st == nil || int(st.Uid) != euid || fi.Mode().Perm()&0o077 != 0:
		owner := -1
		if st != nil {
			owner = int(st.Uid)
		}
		return fmt.Errorf("reload-socket: %s must be owned by uid %d with mode 0700; it is owned by uid %d with mode %04o", dir, euid, owner, fi.Mode().Perm())
	}
	return nil
}

// PeerUID reads the uid of the peer of conn with SO_PEERCRED, mapped into the user
// namespace of this process.
func PeerUID(conn *net.UnixConn) (int, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if cerr != nil {
		return 0, cerr
	}
	return int(cred.Uid), nil
}
