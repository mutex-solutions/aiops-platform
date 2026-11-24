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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mutex-solutions/aiops-platform/plugins/reload-socket/internal/socket"
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

func apply(c *kernel.Context, cfg Config, b bounds) error {
	dir, where := cfg.Dir, cfg.Dir
	if dir == "" {
		home, ok := launch.CmdlineKey.Of(c).Flag("home")
		if !ok || home == "" {
			return errors.New("reload-socket: no dir in the config and no home (--home or $AIOPS_HOME)")
		}
		dir, where = filepath.Join(home, reload.RunDir), home
	}
	s := &server{rl: reload.Key.Of(c), b: b, uid: os.Geteuid(), slots: make(chan struct{}, b.conns), open: map[net.Conn]bool{}}
	var ln *net.UnixListener
	err := c.Effect("reload-socket", func() (kernel.Disposer, error) {
		var dispose kernel.Disposer
		var err error
		ln, dispose, err = socket.Listen(dir, where)
		return dispose, err
	})
	if err != nil {
		return err
	}
	c.Go("reload-socket-close", func(ctx context.Context) error {
		<-ctx.Done()
		s.shut(ln)
		return nil
	})
	c.Go("reload-socket-accept", func(ctx context.Context) error { return s.accept(c, ctx, ln) })
	return nil
}

// server is what one activation serves.
type server struct {
	rl    reload.Reload
	b     bounds
	uid   int
	slots chan struct{} // one per open connection

	mu     sync.Mutex
	open   map[net.Conn]bool
	closed bool
}

// accept serves each connection on a tracked goroutine of its own until the listener
// closes. A Context.Go that the kernel refuses because the row is unloading ends the
// loop, as plugins/reload does.
func (s *server) accept(c *kernel.Context, ctx context.Context, ln *net.UnixListener) error {
	for {
		conn, err := ln.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("reload-socket: accept: %w", err)
		}
		if s.admit(conn) && !s.start(c, conn) {
			return nil
		}
	}
}

// admit checks the peer and takes a place for conn, and refuses it otherwise.
func (s *server) admit(conn *net.UnixConn) bool {
	uid, err := socket.PeerUID(conn)
	switch {
	case err != nil:
		s.refuse(conn, reload.NewError("reload-socket: the peer's credentials do not read: "+err.Error(), reload.CausePeer))
		return false
	case !servedPeer(uid, s.uid):
		s.refuse(conn, reload.NewError(fmt.Sprintf("reload-socket: the peer runs as uid %d; only uid %d, the user of this process, submits", uid, s.uid), reload.CausePeer))
		return false
	}
	select {
	case s.slots <- struct{}{}:
	default:
		s.refuse(conn, reload.NewError(fmt.Sprintf("reload-socket: %d connections are open; try again", s.b.conns), reload.CauseBusy))
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		<-s.slots
		_ = conn.Close()
		return false
	}
	s.open[conn] = true
	return true
}

// servedPeer reports whether a peer of uid peer may submit to a process of effective
// uid own: only the same uid, so root is refused unless the process runs as root
// (ADR-062 decision 5).
func servedPeer(peer, own int) bool { return peer == own }

func (s *server) start(c *kernel.Context, conn *net.UnixConn) (ok bool) {
	defer func() {
		if p := recover(); p != nil {
			if err, isErr := p.(error); !isErr || !errors.Is(err, kernel.ErrInactive) {
				panic(p)
			}
			s.drop(conn) // Context.Go refuses an unloading row with a panic of an error
			ok = false
		}
	}()
	c.Go("reload-socket-conn", func(ctx context.Context) error {
		defer s.drop(conn)
		s.serve(ctx, conn)
		return nil // a client's error never fails the row
	})
	return true
}

func (s *server) drop(conn net.Conn) {
	_ = conn.Close()
	s.mu.Lock()
	delete(s.open, conn)
	s.mu.Unlock()
	<-s.slots
}

// shut closes the listener, which unlinks the socket file, and every open
// connection, so that each blocking read or write of the row ends.
func (s *server) shut(ln *net.UnixListener) {
	s.mu.Lock()
	s.closed = true
	for conn := range s.open {
		_ = conn.Close()
	}
	s.mu.Unlock()
	_ = ln.Close()
}

func (s *server) refuse(conn net.Conn, e *reload.WireError) {
	s.write(conn, reload.Reply{V: reload.WireVersion, Error: e})
	_ = conn.Close()
}

// serve answers the one request of conn.
func (s *server) serve(ctx context.Context, conn net.Conn) {
	req, bad := s.read(conn)
	if bad != nil {
		s.write(conn, reload.Reply{V: reload.WireVersion, Error: bad})
		return
	}
	id := req.ID
	if req.Op == reload.OpSubmit {
		var err error
		if id, err = s.rl.Submit(ctx, reload.Op{Source: reload.SourcePrefix + req.Command}); err != nil {
			s.write(conn, reload.Reply{V: reload.WireVersion, Error: reload.EncodeError(err)})
			return
		}
	}
	s.follow(ctx, conn, id)
}

// read reads and checks the request, within the read bound of the accept.
func (s *server) read(conn net.Conn) (reload.Request, *reload.WireError) {
	refused := func(format string, a ...any) (reload.Request, *reload.WireError) {
		return reload.Request{}, reload.NewError("reload-socket: "+fmt.Sprintf(format, a...), reload.CauseRequest)
	}
	_ = conn.SetReadDeadline(time.Now().Add(s.b.read))
	line, err := bufio.NewReaderSize(io.LimitReader(conn, reload.MaxRequest), reload.MaxRequest).ReadSlice('\n')
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		return refused("no request within %s of the accept", s.b.read)
	case errors.Is(err, bufio.ErrBufferFull):
		return refused("a request holds at most %d bytes with its newline", reload.MaxRequest)
	case err != nil:
		return refused("a request is one line ended by a newline")
	}
	req, err := reload.DecodeRequest(line)
	if err != nil {
		return refused("a request is one JSON object: %v", err)
	}
	if req.V != reload.WireVersion {
		return reload.Request{}, reload.NewError(fmt.Sprintf("reload-socket: version %d is not served; this row serves version %d", req.V, reload.WireVersion), reload.CauseVersion)
	}
	switch req.Op {
	case reload.OpSubmit:
		if !slices.Contains(reload.Commands(), req.Command) {
			return refused("unknown command %q; version 1 serves %s", req.Command, strings.Join(reload.Commands(), ", "))
		}
	case reload.OpAwait:
		if req.ID == 0 {
			return refused("await names the id that a submit answered")
		}
	default:
		return refused("unknown op %q; version 1 serves %s and %s", req.Op, reload.OpSubmit, reload.OpAwait)
	}
	return req, nil
}

// follow writes the result of id as it stands and, when that is not final, the final
// result. It stops for a client that closed its end, noticed within the notice bound
// (the operation goes on in reload), and when the row unloads, which closes conn.
func (s *server) follow(ctx context.Context, conn net.Conn, id reload.ID) {
	r, ok := s.rl.Result(id)
	if !ok {
		s.write(conn, reload.Reply{V: reload.WireVersion, Error: reload.EncodeError(reload.ErrUnknown)})
		return
	}
	if !s.result(conn, r) || r.Status.Done() {
		return
	}
	for {
		actx, cancel := context.WithTimeout(ctx, s.b.notice)
		r, err := s.rl.Await(actx, id)
		cancel()
		switch {
		case err == nil:
			s.result(conn, r)
			return
		case ctx.Err() != nil:
			return
		case errors.Is(err, context.DeadlineExceeded):
			if s.gone(conn) {
				return
			}
		default:
			s.write(conn, reload.Reply{V: reload.WireVersion, Error: reload.EncodeError(err)})
			return
		}
	}
}

func (s *server) result(conn net.Conn, r reload.Result) bool {
	w := reload.EncodeResult(r)
	return s.write(conn, reload.Reply{V: reload.WireVersion, Result: &w})
}

// gone reports whether the client closed its end: a read that does not time out at
// once found an end of file, an error or bytes no request may send.
func (s *server) gone(conn net.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(time.Millisecond))
	_, err := conn.Read(make([]byte, 1))
	var ne net.Error
	return !errors.As(err, &ne) || !ne.Timeout()
}

// write writes one reply line within the write bound and reports whether it went.
func (s *server) write(conn net.Conn, r reload.Reply) bool {
	_ = conn.SetWriteDeadline(time.Now().Add(s.b.write))
	_, err := conn.Write(reload.Line(r))
	return err == nil
}
