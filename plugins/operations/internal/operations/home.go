// SPDX-License-Identifier: Apache-2.0

// Package operations is the operations library of the plugin aiops/operations
// (ADR-061 decisions 14 to 19; ADR-060 decision 41).
package operations

import (
	"context"
	"errors"
	"time"
)

// Home is one $AIOPS_HOME as the library writes it.
type Home struct {
	Dir      string             // the absolute path of the home
	LockWait time.Duration      // how long a second writer waits for write.lock
	hook     func(stage string) // a test's view of the steps of a write
	euid     func() int         // the effective user; os.Geteuid when nil
}

// Change is one path that a write replaces or creates: a file with Data, or a
// directory written whole with Files.
type Change struct {
	Path  string            // the slash path under the home
	Data  []byte            // the content of a file
	Files map[string][]byte // the files of a directory, by name
}

// Write is one write of the home.
type Write struct {
	Command string                          // the holder's command, recorded in write.lock
	Plan    func() ([]Change, error)        // reads the home under the lock: the changes, in rename order
	Submit  func(ctx context.Context) error // the submission once the renames are done; an error restores
}

// Done is what a write did.
type Done struct {
	Written []string // the absolute paths written, in rename order
	Notes   []string // what the operator is told
}

// The refusals of a write.
var (
	ErrLocked = errors.New("write.lock is held by another writer")
	ErrOwner  = errors.New("the home is owned by another user")
)

// Write runs one write of the home.
func (h Home) Write(ctx context.Context, w Write) (Done, error) {
	return Done{}, errors.New("not implemented")
}
