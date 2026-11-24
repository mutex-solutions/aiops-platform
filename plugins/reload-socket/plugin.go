// SPDX-License-Identifier: Apache-2.0

// Package reloadsocket is the row reload-socket of aiops-core (ADR-062): it carries
// the submissions of aiopsctl plugin enable, disable, remove and install to the reload
// queue of the running process, from S2a until plugin-manager takes them at S6a. It
// serves wire format version 1 of sdk/def/reload/v0 on the stream socket
// <home>/run/reload.sock and provides nothing; it is a bound consumer of reload, in
// no required set, and the aiops-cli bundle disables it, so aiopsctl never listens.
//
// At apply, in one effect, the row checks that its directory is mode 0700 and owned
// by the process's effective uid, takes an exclusive flock of reload.lock, removes a
// socket a process left behind, listens and sets the socket to mode 0600
// (internal/socket).
// It serves only a peer whose uid, read with SO_PEERCRED before the request, equals
// the process's effective uid, and only on Linux. Every blocking call of a connection
// ends when the activation's context is cancelled, so an unload waits for no client.
package reloadsocket

import (
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// ID is the plugin id.
const ID = "aiops/reload-socket"

// Config is the config of the row. Dir is the directory of the socket and its lock,
// <home>/run when empty; it exists so that the generated cycle test can mount the
// row (ADR-062 decisions 1 and 18). aiopsctl derives the paths from the home and
// never reads it.
type Config struct {
	Dir string
}

// The fixed bounds of ADR-062 decisions 4 and 10.
const (
	maxConns  = 8               // open connections; the next one gets busy
	readWait  = 5 * time.Second // the request, from the accept
	writeWait = 5 * time.Second // each reply line
	notice    = time.Second     // a client that closed its end is noticed within this
)

// bounds are the bounds a row runs with: those above, shorter in a test.
type bounds struct {
	conns               int
	read, write, notice time.Duration
}

// New returns the row.
func New() kernel.Plugin { return newPlugin(bounds{maxConns, readWait, writeWait, notice}) }

func newPlugin(b bounds) kernel.Plugin {
	return plugin.Define(plugin.Spec[Config]{
		ID:     ID,
		Inject: []kernel.Dep{kernel.Need(reload.Key), kernel.Need(launch.CmdlineKey)},
		Apply:  func(c *kernel.Context, cfg *Config) error { return apply(c, *cfg, b) },
	})
}

// apply is a stub: the row listens nowhere yet.
func apply(*kernel.Context, Config, bounds) error { return nil }

// servedPeer is a stub that serves no peer.
func servedPeer(peer, own int) bool { return false }
