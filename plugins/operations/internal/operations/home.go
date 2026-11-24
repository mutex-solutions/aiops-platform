// SPDX-License-Identifier: Apache-2.0

// Package operations is the operations library of the plugin aiops/operations
// (ADR-061 decisions 14 to 19; ADR-060 decision 41), with the client of the socket of
// reload-socket (ADR-062 decision 16).
package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// Home is one $AIOPS_HOME as the library writes it.
type Home struct {
	Dir      string             // the absolute path of the home
	LockWait time.Duration      // how long a second writer waits for write.lock
	hook     func(stage string) // a test's view of the steps of a write
	euid     func() int         // the effective user; os.Geteuid when nil
}

// Change is one path that a write replaces, creates or removes: a file with Data, a
// directory written whole with Files, or with Remove a path removed.
type Change struct {
	Path   string            // the slash path under the home
	Data   []byte            // the content of a file
	Files  map[string][]byte // the files of a directory, by name
	Remove bool              // the path is removed
}

// Write is one write of the home.
type Write struct {
	Command string                          // the holder's command, recorded in write.lock
	Plan    func() ([]Change, error)        // reads the home under the lock: the changes, in rename order; none writes and submits nothing
	Submit  func(ctx context.Context) error // the submission once the renames are done; an error restores
}

// Done is what a write did.
type Done struct {
	Written []string // the absolute paths written, in rename order
	Notes   []string // what the operator is told
}

// The refusals of a write, which match the errors of the definition.
var (
	ErrLocked = fmt.Errorf("%w", def.ErrLocked)
	ErrOwner  = fmt.Errorf("%w: the home is owned by another user", def.ErrRefused)
)

const (
	lockFile    = "write.lock"
	snapshotDir = "snapshot"
	snapshotNew = "snapshot.new" // a snapshot being written, renamed to snapshotDir once synced
	snapshotOld = "snapshot.old" // a snapshot being removed, renamed from snapshotDir first
	// restoreBound is how long a restore runs after its write failed or its context
	// ended: half the drain deadline of ADR-059 decision 30 (ADR-061 decision 17).
	restoreBound = 5 * time.Second
	lockPoll     = 20 * time.Millisecond
)

// Write runs one write of the home (ADR-061 decisions 17 and 19). It makes sure of
// the home, takes write.lock, restores a snapshot that an interrupted write left,
// runs the plan, writes into snapshot/ a copy of each path the plan replaces and the
// list of those it creates, renames each change into place, submits, and removes the
// snapshot. A failure after the snapshot, a failed submission and the end of ctx
// restore the snapshot under a context of their own, bounded to five seconds; when
// the bound passes or the process dies, the snapshot stays for the next writer, which
// restores what is left of it under its own context before anything else.
func (h Home) Write(ctx context.Context, w Write) (Done, error) {
	if err := h.prepare(); err != nil {
		return Done{}, err
	}
	unlock, err := h.lock(ctx, w.Command)
	if err != nil {
		return Done{}, err
	}
	defer unlock()
	var done Done
	restored, err := h.recover(ctx)
	if err != nil {
		return done, fmt.Errorf("restore the snapshot of an interrupted write in %s: %w", h.path(snapshotDir), err)
	}
	if restored {
		done.Notes = append(done.Notes, "restored the snapshot that an interrupted write left in "+h.path(snapshotDir))
	}
	if err := ctx.Err(); err != nil {
		return done, err
	}
	changes, err := w.Plan()
	if err != nil || len(changes) == 0 {
		return done, err
	}
	if err := h.snapshot(changes); err != nil {
		return done, errors.Join(err, os.RemoveAll(h.path(snapshotNew)))
	}
	err = h.apply(ctx, changes, &done)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && w.Submit != nil {
		err = w.Submit(ctx)
	}
	if err != nil {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), restoreBound)
		defer cancel()
		if rerr := h.restore(rctx); rerr != nil {
			return Done{}, fmt.Errorf("%w; the restore stopped, and the next writer restores %s: %v", err, h.path(snapshotDir), rerr)
		}
		return Done{Notes: append(done.Notes, "restored the home from its snapshot")}, err
	}
	return done, h.drop()
}

// drop removes the snapshot: it renames it aside first, so a kill while its files are
// removed leaves no part of a snapshot for the next writer to read.
func (h Home) drop() error {
	err := os.RemoveAll(h.path(snapshotOld))
	if err == nil {
		err = os.Rename(h.path(snapshotDir), h.path(snapshotOld))
	}
	if err == nil {
		err = syncDir(h.Dir)
	}
	if err != nil {
		return err
	}
	h.step("trashed")
	return os.RemoveAll(h.path(snapshotOld))
}

func (h Home) path(rel string) string { return filepath.Join(h.Dir, filepath.FromSlash(rel)) }

func (h Home) step(stage string) {
	if h.hook != nil {
		h.hook(stage)
	}
}

// prepare creates the home and run/ with mode 0700 where they are missing, but not a
// missing parent, and refuses a home whose owner is not the effective user (ADR-047
// decision 14).
func (h Home) prepare() error {
	if err := os.Mkdir(h.Dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	st, err := os.Stat(h.Dir)
	if err != nil {
		return err
	}
	euid := os.Geteuid()
	if h.euid != nil {
		euid = h.euid()
	}
	if s, ok := st.Sys().(*syscall.Stat_t); ok && int(s.Uid) != euid {
		return fmt.Errorf("%w: %s belongs to uid %d, and this process runs as uid %d (ADR-047 decision 14)", ErrOwner, h.Dir, s.Uid, euid)
	}
	if !st.IsDir() {
		return fmt.Errorf("the home %s is not a directory", h.Dir)
	}
	if err := os.Mkdir(h.path("run"), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}

// lock takes the exclusive flock(2) of write.lock, the one file of the home that is
// created once with mode 0600 and never renamed or deleted, since the lock binds to one
// inode (ADR-061 decision 19). The system releases it when its holder exits. A second
// writer tries until LockWait has passed and then fails, naming the holder from the
// record that it reads without the lock. The holder writes its record, its process id
// and its command, in place through the locked descriptor.
func (h Home) lock(ctx context.Context, command string) (func(), error) {
	p := h.path(lockFile)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd())
	deadline := time.Now().Add(h.LockWait)
	tick := time.NewTicker(lockPoll)
	defer tick.Stop()
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(err, f.Close())
		}
		if !time.Now().Before(deadline) {
			return nil, errors.Join(fmt.Errorf("%w: %s, after %v, by %s (ADR-061 decision 19)", ErrLocked, p, h.LockWait, holder(p)), f.Close())
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), f.Close())
		case <-tick.C:
		}
	}
	unlock := func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = f.Close() }
	err = f.Truncate(0)
	if err == nil {
		_, err = f.WriteAt(fmt.Appendf(nil, "%d %s\n", os.Getpid(), command), 0)
	}
	if err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

// holder reads the record of write.lock at p: a short or empty read is an unknown
// holder.
func holder(p string) string {
	b, _ := os.ReadFile(p)
	s, whole := strings.CutSuffix(string(b), "\n")
	pid, cmd, ok := strings.Cut(s, " ")
	if !whole || !ok || pid == "" {
		return "an unknown holder"
	}
	return "process " + pid + " (" + cmd + ")"
}

// paths is the list of a snapshot: the paths a write replaces, whose copies are under
// files/, and the paths it creates.
type paths struct{ Replaced, Created []string }

// snapshot writes into snapshot/ a copy of each path that changes replace and the list
// of the paths they create, the highest missing directory for a path whose parent is
// missing, and syncs it before it renames it into place.
func (h Home) snapshot(changes []Change) error {
	tmp := h.path(snapshotNew)
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.Mkdir(tmp, 0o700); err != nil {
		return err
	}
	var l paths
	for _, c := range changes {
		_, err := os.Lstat(h.path(c.Path))
		switch {
		case err == nil:
			l.Replaced = append(l.Replaced, c.Path)
			if err := copyTree(h.path(c.Path), filepath.Join(tmp, "files", filepath.FromSlash(c.Path))); err != nil {
				return err
			}
		case !errors.Is(err, fs.ErrNotExist):
			return err
		default:
			top := c.Path
			for d := path.Dir(c.Path); d != "."; d = path.Dir(d) {
				if _, err := os.Lstat(h.path(d)); err == nil {
					break
				}
				top = d
			}
			if !slices.ContainsFunc(l.Created, func(p string) bool { return p == top || strings.HasPrefix(top, p+"/") }) {
				l.Created = append(l.Created, top)
			}
		}
	}
	b, err := json.Marshal(l)
	if err == nil {
		err = writeFile(filepath.Join(tmp, "paths.json"), b)
	}
	if err == nil {
		err = syncDir(tmp)
	}
	if err == nil {
		err = os.Rename(tmp, h.path(snapshotDir))
	}
	if err == nil {
		err = syncDir(h.Dir)
	}
	return err
}

// apply renames each change into place, in order: a file or a directory is written
// beside its target and renamed over it.
func (h Home) apply(ctx context.Context, changes []Change, done *Done) error {
	for _, c := range changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		dst := h.path(c.Path)
		dir := filepath.Dir(dst)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		tmp := filepath.Join(dir, "."+filepath.Base(dst)+".new")
		err := os.RemoveAll(tmp)
		if c.Remove {
			err = errors.Join(err, os.RemoveAll(dst))
		} else if err == nil && c.Files != nil {
			err = writeDir(tmp, c.Files)
			if err == nil {
				err = os.RemoveAll(dst)
			}
		} else if err == nil {
			err = writeFile(tmp, c.Data)
		}
		if err == nil && !c.Remove {
			err = os.Rename(tmp, dst)
		}
		if err == nil {
			err = syncDir(dir)
		}
		if err != nil {
			return err
		}
		done.Written = append(done.Written, dst)
		h.step("renamed")
	}
	return nil
}

// recover removes a snapshot that was never renamed into place, since no change was
// renamed before it was, and one that was being removed, and restores one that was
// renamed into place. A snapshot/ with no paths.json is part of a snapshot whose
// removal an earlier version did not rename aside: paths.json is in place before the
// snapshot is, so it is removed too. It reports whether it restored. The restore is not
// bounded: the writer holds the lock and does nothing else until it is done, and ctx
// stops it with its progress kept.
func (h Home) recover(ctx context.Context) (bool, error) {
	for _, p := range []string{snapshotNew, snapshotOld} {
		if err := os.RemoveAll(h.path(p)); err != nil {
			return false, err
		}
	}
	if _, err := os.Stat(h.path(snapshotDir + "/paths.json")); errors.Is(err, fs.ErrNotExist) {
		return false, os.RemoveAll(h.path(snapshotDir))
	} else if err != nil {
		return false, err
	}
	return true, h.restore(ctx)
}

// restore copies each replaced path back from the snapshot, removes each created path
// and then the snapshot. After each entry it writes paths.json without that entry, and
// each step can run again, so a restore that stopped is finished by the next one from
// where it stopped.
func (h Home) restore(ctx context.Context) error {
	snap := h.path(snapshotDir)
	b, err := os.ReadFile(filepath.Join(snap, "paths.json"))
	if err != nil {
		return err
	}
	var l paths
	if err := json.Unmarshal(b, &l); err != nil {
		return err
	}
	for _, rel := range slices.Concat(l.Replaced, l.Created) {
		if !fs.ValidPath(rel) || rel == "." {
			return fmt.Errorf("the snapshot names %q, which is not a path under the home", rel)
		}
	}
	for len(l.Replaced) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel := l.Replaced[0]
		dst := h.path(rel)
		tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".restore")
		err := os.RemoveAll(tmp)
		if err == nil {
			err = copyTree(filepath.Join(snap, "files", filepath.FromSlash(rel)), tmp)
		}
		if st, serr := os.Stat(tmp); err == nil && serr == nil && st.IsDir() {
			err = os.RemoveAll(dst)
		}
		if err == nil {
			err = os.Rename(tmp, dst)
		}
		if err == nil {
			l.Replaced = l.Replaced[1:]
			err = progress(snap, l)
		}
		if err != nil {
			return err
		}
		h.step("restored")
	}
	for len(l.Created) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := os.RemoveAll(h.path(l.Created[0]))
		if err == nil {
			l.Created = l.Created[1:]
			err = progress(snap, l)
		}
		if err != nil {
			return err
		}
		h.step("restored")
	}
	return h.drop()
}

// progress replaces paths.json of the snapshot snap with l, the entries a restore has
// left to restore.
func progress(snap string, l paths) error {
	b, err := json.Marshal(l)
	if err == nil {
		err = writeFile(filepath.Join(snap, "paths.json.new"), b)
	}
	if err == nil {
		err = os.Rename(filepath.Join(snap, "paths.json.new"), filepath.Join(snap, "paths.json"))
	}
	if err == nil {
		err = syncDir(snap)
	}
	return err
}

// copyTree copies the file or the directory src to dst, files with mode 0600 and
// directories with mode 0700.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		b, err := os.ReadFile(p)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(target), 0o700)
		}
		if err == nil {
			err = writeFile(target, b)
		}
		return err
	})
}

// writeDir writes files into the new directory dir.
func writeDir(dir string, files map[string][]byte) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	for name, data := range files {
		if name != filepath.Base(name) || name == "." || name == ".." {
			return fmt.Errorf("%q is not a file name", name)
		}
		if err := writeFile(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	return syncDir(dir)
}

// writeFile writes data to p with mode 0600 and syncs it.
func writeFile(p string, data []byte) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
