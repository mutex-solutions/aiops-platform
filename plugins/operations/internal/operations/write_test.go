// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// newHome returns a home in a fresh directory with trust.yaml "before\n", which the
// writes of these tests replace, and a short lock wait.
func newHome(t *testing.T) Home {
	t.Helper()
	h := Home{Dir: filepath.Join(t.TempDir(), "home"), LockWait: 50 * time.Millisecond}
	if err := os.Mkdir(h.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	put(t, h, "trust.yaml", "before\n")
	return h
}

func put(t *testing.T, h Home, rel, data string) {
	t.Helper()
	p := filepath.Join(h.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// tree returns every regular file under the home but write.lock, by slash path, with
// its content.
func tree(t *testing.T, h Home) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(h.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "write.lock" {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(h.Dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// record is the change of a write as dist use makes it: a file replaced, then a
// directory and a file created (ADR-061 decision 17).
func record(data string) []Change {
	return []Change{
		{Path: "trust.yaml", Data: []byte(data)},
		{Path: "verified/sha256-ab", Files: map[string][]byte{"record.yaml": []byte("kind: distribution\n"), "manifest": []byte(data)}},
		{Path: "verified/current", Data: []byte("sha256:ab\n")},
	}
}

func plan(cs []Change) func() ([]Change, error) { return func() ([]Change, error) { return cs, nil } }

// A write replaces and creates what its plan names, each file with mode 0600, and
// removes its snapshot; what it wrote is reported by absolute path.
func TestWriteReplacesAndCreates(t *testing.T) {
	h := newHome(t)
	done, err := h.Write(t.Context(), Write{Command: "test", Plan: plan(record("after\n"))})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"trust.yaml": "after\n", "verified/sha256-ab/record.yaml": "kind: distribution\n",
		"verified/sha256-ab/manifest": "after\n", "verified/current": "sha256:ab\n"}
	if got := tree(t, h); !equal(got, want) {
		t.Fatalf("home %v, want %v", got, want)
	}
	wrote := []string{filepath.Join(h.Dir, "trust.yaml"), filepath.Join(h.Dir, "verified", "sha256-ab"), filepath.Join(h.Dir, "verified", "current")}
	if !slices.Equal(done.Written, wrote) || len(done.Notes) != 0 {
		t.Fatalf("done %+v, want written %v and no note", done, wrote)
	}
	for _, rel := range []string{"trust.yaml", "verified/sha256-ab/record.yaml", "verified/current", "write.lock"} {
		if st, err := os.Stat(filepath.Join(h.Dir, rel)); err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, %v; want mode 0600", rel, st, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "snapshot")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the snapshot stays after the write: %v", err)
	}
}

// The home and run/ are created with mode 0700 where they are missing; a home whose
// parent is missing is not created; a home owned by another user is refused and
// nothing is written (ADR-047 decision 14).
func TestHomeModesAndOwner(t *testing.T) {
	h := Home{Dir: filepath.Join(t.TempDir(), "new"), LockWait: time.Second}
	if _, err := h.Write(t.Context(), Write{Command: "test", Plan: plan(record("x\n"))}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{h.Dir, filepath.Join(h.Dir, "run")} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v, %v; want a directory of mode 0700", d, st, err)
		}
	}
	orphan := Home{Dir: filepath.Join(t.TempDir(), "missing", "home"), LockWait: time.Second}
	if _, err := orphan.Write(t.Context(), Write{Command: "test", Plan: plan(record("x\n"))}); err == nil {
		t.Error("a home whose parent is missing was created")
	}
	other := newHome(t)
	before := tree(t, other)
	other.euid = func() int { return os.Geteuid() + 1 }
	if _, err := other.Write(t.Context(), Write{Command: "test", Plan: plan(record("x\n"))}); !errors.Is(err, ErrOwner) {
		t.Errorf("a home of another user: %v, want ErrOwner", err)
	}
	if got := tree(t, other); !equal(got, before) {
		t.Errorf("a refused home changed: %v", got)
	}
}

// A submission that fails restores the snapshot: the replaced file is back and every
// created path is gone (ADR-061 decision 17).
func TestRefusedSubmissionRestores(t *testing.T) {
	h := newHome(t)
	before := tree(t, h)
	refused := errors.New("refused")
	_, err := h.Write(t.Context(), Write{Command: "test", Plan: plan(record("after\n")), Submit: func(context.Context) error { return refused }})
	if !errors.Is(err, refused) {
		t.Fatalf("write: %v, want the refusal", err)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Fatalf("home %v, want it restored to %v", got, before)
	}
}

// The end of the context after the first rename restores the home under a context of
// its own, and the write returns the context's error (ADR-061 decision 17).
func TestEndedContextRestores(t *testing.T) {
	h := newHome(t)
	before := tree(t, h)
	ctx, cancel := context.WithCancel(t.Context())
	h.hook = func(stage string) {
		if stage == "renamed" {
			cancel()
		}
	}
	_, err := h.Write(ctx, Write{Command: "test", Plan: plan(record("after\n"))})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("write: %v, want context.Canceled", err)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Fatalf("home %v, want it restored to %v", got, before)
	}
}

// A second writer waits for the bound, then fails naming the holder from the record in
// write.lock, and writes nothing; the holder's record is its process id and command
// (ADR-061 decision 19).
func TestSecondWriterFailsNamingTheHolder(t *testing.T) {
	h := newHome(t)
	before := tree(t, h)
	f, err := os.OpenFile(filepath.Join(h.Dir, "write.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("4242 aiopsctl trust add\n"); err != nil {
		t.Fatal(err)
	}
	_, err = h.Write(t.Context(), Write{Command: "test", Plan: plan(record("after\n"))})
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "4242") || !strings.Contains(err.Error(), "aiopsctl trust add") {
		t.Fatalf("second writer: %v, want ErrLocked naming process 4242 and its command", err)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Fatalf("a writer that did not get the lock changed the home: %v", got)
	}
	// An empty record names an unknown holder.
	if err := f.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Write(t.Context(), Write{Command: "test", Plan: plan(record("after\n"))}); !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("second writer: %v, want ErrLocked naming an unknown holder", err)
	}
}

// Two writers of one home run one after the other: each plan reads what the other
// wrote, so neither change is lost; write.lock keeps its inode across writes and
// holds the record of the writer that holds it.
func TestWritersAreSerialised(t *testing.T) {
	h := newHome(t)
	h.LockWait = time.Minute
	appendLine := func(line string) Write {
		return Write{Command: "append " + line, Plan: func() ([]Change, error) {
			b, err := os.ReadFile(filepath.Join(h.Dir, "trust.yaml"))
			return []Change{{Path: "trust.yaml", Data: append(b, line+"\n"...)}}, err
		}}
	}
	inside, release := make(chan struct{}), make(chan struct{})
	first := h
	first.hook = func(stage string) {
		if stage == "renamed" {
			close(inside)
			<-release
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(1)
	go func() { defer wg.Done(); _, err := first.Write(t.Context(), appendLine("one")); errs <- err }()
	select {
	case <-inside:
	case err := <-errs:
		t.Fatalf("the first writer returned %v before its first rename", err)
	}
	lock, err := os.ReadFile(filepath.Join(h.Dir, "write.lock"))
	if err != nil || string(lock) != strconv.Itoa(os.Getpid())+" append one\n" {
		t.Errorf("write.lock while the first writer holds it: %q, %v", lock, err)
	}
	st1, err := os.Stat(filepath.Join(h.Dir, "write.lock"))
	if err != nil {
		t.Fatal(err)
	}
	wg.Add(1)
	go func() { defer wg.Done(); _, err := h.Write(t.Context(), appendLine("two")); errs <- err }()
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := tree(t, h)["trust.yaml"]; got != "before\none\ntwo\n" {
		t.Errorf("trust.yaml %q, want both lines in order", got)
	}
	st2, err := os.Stat(filepath.Join(h.Dir, "write.lock"))
	if err != nil || !os.SameFile(st1, st2) {
		t.Errorf("write.lock changed its inode across writes: %v", err)
	}
}

// helper runs a write in a child process that stops at a stage of the write, says so
// on its stdout, and waits there to be killed.
const helperEnv = "OPERATIONS_TEST_HELPER"

// stops is the stage at which the child of each mode stops: after its first rename;
// after the first step of the restore that its refused submission starts; and after
// the snapshot is renamed aside for its removal, at the end of the write or of the
// restore; and while its submission waits for the row.
var stops = map[string]string{"rename": "renamed", "restore": "restored", "remove": "trashed", "restore-remove": "trashed", "submit": "submitted"}

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("run by the kill tests in a child process")
	}
	h := Home{Dir: os.Getenv("OPERATIONS_TEST_HOME"), LockWait: time.Second}
	stop := stops[mode]
	var submit func(context.Context) error
	if strings.HasPrefix(mode, "restore") {
		submit = func(context.Context) error { return errors.New("refused") }
	}
	if mode == "submit" { // the request went out, and the row has not answered
		submit = func(context.Context) error { os.Stdout.WriteString("submitted\n"); select {} }
	}
	h.hook = func(stage string) {
		if stage == stop {
			os.Stdout.WriteString(stage + "\n")
			select {}
		}
	}
	_, err := h.Write(context.Background(), Write{Command: "child", Plan: plan(record("child\n")), Submit: submit})
	t.Fatalf("the child's write returned %v before it was killed", err)
}

// kill runs the helper in mode on h, waits until it reports its stage, and kills it.
func kill(t *testing.T, h Home, mode string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"="+mode, "OPERATIONS_TEST_HOME="+h.Dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err == nil && line != stops[mode]+"\n" {
		err = errors.New("not the stage " + stops[mode])
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("the child said %q: %v", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
}

// A write killed after its first rename leaves its snapshot; the next writer gets the
// lock at once, restores the home to its state before the killed write, says so, and
// then writes (ADR-061 decisions 17 and 19).
func TestKilledWriteIsRestoredByTheNextWriter(t *testing.T) {
	for _, mode := range []string{"rename", "restore"} {
		t.Run(mode, func(t *testing.T) {
			h := newHome(t)
			before := tree(t, h)
			kill(t, h, mode)
			if _, err := os.Stat(filepath.Join(h.Dir, "snapshot")); err != nil {
				t.Fatalf("the killed write left no snapshot: %v", err)
			}
			if got := tree(t, h); equal(got, before) {
				t.Fatalf("the child stopped before it changed the home: %v", got)
			}
			h.LockWait = 0 // the lock of a killed holder is free at once
			var seen map[string]string
			done, err := h.Write(t.Context(), Write{Command: "next", Plan: func() ([]Change, error) {
				seen = tree(t, h)
				return []Change{{Path: "aiops.patch.yml", Data: []byte("[]\n")}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !equal(seen, before) {
				t.Errorf("the next writer planned on %v, want the home restored to %v", seen, before)
			}
			if len(done.Notes) != 1 || !strings.Contains(done.Notes[0], "restored") {
				t.Errorf("notes %q, want one that says the snapshot was restored", done.Notes)
			}
		})
	}
}

// A write killed while its submission waits for the row keeps its files: the row may
// have taken the change and the process may run it, so the next writer drops the
// snapshot, restores nothing, and says that the verdict is unknown (ADR-062 decision
// 11).
func TestWriteKilledAfterItsSubmissionKeepsItsFiles(t *testing.T) {
	h := newHome(t)
	kill(t, h, "submit")
	h.LockWait = 0
	var seen map[string]string
	done, err := h.Write(t.Context(), Write{Command: "next", Plan: func() ([]Change, error) { seen = tree(t, h); return nil, nil }})
	want := map[string]string{"trust.yaml": "child\n", "verified/sha256-ab/record.yaml": "kind: distribution\n",
		"verified/sha256-ab/manifest": "child\n", "verified/current": "sha256:ab\n"}
	if err != nil || !equal(seen, want) {
		t.Errorf("the next writer: %v, planned on %v; want %v", err, seen, want)
	}
	if len(done.Notes) != 1 || !strings.Contains(done.Notes[0], "verdict") {
		t.Errorf("notes %q, want one that says the verdict of the killed submission is unknown", done.Notes)
	}
}

// A write killed while it removes its snapshot, at the end of the write or at the end
// of its restore, leaves a home that the next writer writes at once: the snapshot is
// renamed aside before it is removed, so no reader finds part of one, and the next
// writer restores nothing and keeps what the killed write left.
func TestWriteKilledWhileItRemovesItsSnapshot(t *testing.T) {
	for _, mode := range []string{"remove", "restore-remove"} {
		t.Run(mode, func(t *testing.T) {
			h := newHome(t)
			want := tree(t, h)
			if mode == "remove" {
				want = map[string]string{"trust.yaml": "child\n", "verified/sha256-ab/record.yaml": "kind: distribution\n",
					"verified/sha256-ab/manifest": "child\n", "verified/current": "sha256:ab\n"}
			}
			kill(t, h, mode)
			h.LockWait = 0
			done, err := h.Write(t.Context(), Write{Command: "next", Plan: func() ([]Change, error) {
				got := tree(t, h)
				for p := range got {
					if strings.HasPrefix(p, "snapshot") {
						delete(got, p)
					}
				}
				if !equal(got, want) {
					t.Errorf("the next writer planned on %v, want %v", got, want)
				}
				return nil, nil
			}})
			if err != nil || len(done.Notes) != 0 {
				t.Fatalf("the next write: %+v, %v; want no error and no note", done, err)
			}
			if got := tree(t, h); !equal(got, want) {
				t.Errorf("home %v, want %v and no snapshot", got, want)
			}
		})
	}
}

// A snapshot/ with no paths.json is what a removal killed by an earlier version left:
// paths.json is renamed into place before the snapshot is, so such a directory is never
// a snapshot to restore, and the next writer removes it and writes.
func TestASnapshotWithNoListIsRemoved(t *testing.T) {
	h := newHome(t)
	put(t, h, "snapshot/files/verified/sha256-ab/manifest", "partial\n")
	before := tree(t, h)
	delete(before, "snapshot/files/verified/sha256-ab/manifest")
	done, err := h.Write(t.Context(), Write{Command: "next", Plan: plan([]Change{{Path: "trust.yaml", Data: []byte("after\n")}})})
	if err != nil || len(done.Notes) != 0 {
		t.Fatalf("write: %+v, %v; want no error and no note", done, err)
	}
	if got := tree(t, h); !equal(got, map[string]string{"trust.yaml": "after\n"}) {
		t.Errorf("home %v, want trust.yaml written and no snapshot", got)
	}
}

// A restore that stops goes on where it stopped: each entry leaves paths.json once it is
// restored, so the next writer restores only what is left, and a restore that cannot
// finish within one bound still makes progress (ADR-061 decision 17).
func TestAStoppedRestoreResumes(t *testing.T) {
	h := newHome(t)
	before := tree(t, h)
	kill(t, h, "restore") // stopped after the first entry, trust.yaml, is restored
	b, err := os.ReadFile(filepath.Join(h.Dir, "snapshot", "paths.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l paths
	if err := json.Unmarshal(b, &l); err != nil || len(l.Replaced) != 0 || !slices.Equal(l.Created, []string{"verified"}) {
		t.Fatalf("paths.json %s, %v; want only the created path that is left", b, err)
	}
	if got := tree(t, h)["trust.yaml"]; got != before["trust.yaml"] {
		t.Fatalf("trust.yaml %q, want it restored to %q", got, before["trust.yaml"])
	}
	h.LockWait = 0
	if _, err := h.Write(t.Context(), Write{Command: "next", Plan: plan(nil)}); err != nil {
		t.Fatal(err)
	}
	if got := tree(t, h); !equal(got, before) {
		t.Errorf("home %v, want it restored to %v", got, before)
	}
}
