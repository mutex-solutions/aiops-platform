// SPDX-License-Identifier: Apache-2.0

// Package transport is the client of the socket of the row reload-socket, internal to
// the operations plugin (ADR-062 decision 16): from the read of <home>/run/status.json
// to the second submission. The library's Install, Enable, Disable and Remove call
// Submit while they hold the write lock and the snapshot of 08:374, and do with the
// files what Outcome.Restore says (ADR-062 decision 11). It speaks wire format
// version 1 of sdk/def/reload/v0 and dials with net.DialUnix and the network unix
// only, the named exception of the dial rule (decision 14).
package transport

import (
	"context"
	"time"

	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
)

// The defaults of --connect-wait and --wait (ADR-062 decisions 9 and 10), and how
// often a dial is tried again within the connect window. The default window is the
// load, drain and dispose deadlines of reload and reload-socket at the defaults of
// ADR-059 decision 30; a site whose --limits raises them raises the flag.
const (
	DefaultConnectWait = 60 * time.Second
	DefaultWait        = 10 * time.Minute
	retryEvery         = 100 * time.Millisecond
)

// Request is one submission of a command.
type Request struct {
	Home    string // $AIOPS_HOME, from which the paths derive; the row's dir is never read
	Profile string // the profile the command changed
	Command string // reload.CommandEnable, CommandDisable, CommandRemove or CommandInstall
	// ConnectWait bounds each connection, retried every 100 ms; Wait bounds the
	// whole wait for the final result. Zero takes the default.
	ConnectWait, Wait time.Duration
}

// Kind is the row of the outcome table of ADR-062 decision 11 that applies.
type Kind string

// The kinds.
const (
	// Final: a final result, in Result. With status abandoned it holds state-unknown,
	// since one without it is submitted once more.
	Final Kind = "final"
	// Refused: an error line before an id existed, in Error: peer, busy, request,
	// version, or a refusal of Submit.
	Refused Kind = "refused"
	// NotRunning: status.json is absent, says stopped or names another profile; the
	// change applies when the process starts.
	NotRunning Kind = "not-running"
	// RestartRequired: a process runs the profile and the socket did not answer
	// within the connect window; the change applies at the next start.
	RestartRequired Kind = "restart-required"
	// NoFinal: the second submission ended with no final result either.
	NoFinal Kind = "no-final"
	// Unknown: Wait elapsed, or the connection was lost beyond the connect window,
	// after a request was sent.
	Unknown Kind = "unknown"
)

// Outcome is what a submission came to.
type Outcome struct {
	Kind   Kind
	Result *reload.WireResult // Final
	Error  *reload.WireError  // Refused
	Socket string             // the socket dialled
	IDs    []reload.ID        // every operation id the row answered, oldest first
	Last   reload.Status      // the last status the row answered, "" for none
	// untouched: NoFinal where each operation was abandoned without state-unknown, so
	// no generation touched the tree.
	untouched bool
}

// Restore is a stub.
func (o Outcome) Restore() bool { return false }

// Submit is a stub that sends nothing.
func Submit(ctx context.Context, r Request) (Outcome, error) { return Outcome{}, nil }
