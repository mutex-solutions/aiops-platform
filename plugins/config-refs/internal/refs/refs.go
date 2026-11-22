// SPDX-License-Identifier: Apache-2.0

// Package refs resolves the closed references of a config tree. A reference is a
// launch.Ref node, which the launcher puts in the tree in place of the text
// ${scheme:name} (sdk/def/launch/v0 Ref; boot/parse.go parseRef). Text around a
// reference is not accepted by the launcher, so a reference is always a whole value
// (08-profiles-roadmap-adrs.md:162).
package refs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	refsdef "github.com/mutex-solutions/aiops-platform/sdk/def/configrefs/v0"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
)

// EnvPrefix is the prefix of the only environment names a reference may read:
// "`env:` reads only names with the prefix `AIOPS_REF_`"
// (08-profiles-roadmap-adrs.md:162; plugin-system-2026-11.md §9). The launch
// definition holds it, and Cmdline.Env serves no other name; the check here names the
// rule in the error.
const EnvPrefix = launch.EnvPrefix

// Error says which reference failed and why. The reason is fixed text: it never holds
// a value that was read, so an error can be logged whatever the reference names.
type Error struct{ Scheme, Name, Reason string }

func (e *Error) Error() string {
	return fmt.Sprintf("invalid config: reference ${%s:%s} does not resolve: %s", e.Scheme, e.Name, e.Reason)
}

// Is makes errors.Is(err, configrefs.ErrUnresolved) of the definition true: the
// failure "a reference does not resolve" of the failure matrix
// (08-profiles-roadmap-adrs.md:468), whose diagnostic is "invalid config", for the
// listener and the dry run alike.
func (e *Error) Is(target error) bool { return target == refsdef.ErrUnresolved }

// Resolver turns references into values through what the launcher serves.
type Resolver struct {
	Env      func(name string) (string, bool) // launch.Cmdline.Env
	Flag     func(name string) (string, bool) // launch.Cmdline.Flag
	RootFlag string                           // the flag that names the one directory ${file:} may read; "" for none
	MaxBytes int64                            // the largest file that is read
}

// Resolve returns v with every reference replaced by its value, and whether any was.
// It never changes v: a map or a list that holds a reference is copied. A value that
// is not a map, a list or a reference is returned as it is, which is how a config of
// another type (a struct of the loader's) passes through untouched.
func (r *Resolver) Resolve(v any) (any, bool, error) {
	switch t := v.(type) {
	case launch.Ref:
		s, err := r.one(t)
		return s, true, err
	case map[string]any:
		var out map[string]any
		for k, e := range t {
			ne, changed, err := r.Resolve(e)
			if err != nil {
				return nil, false, err
			}
			if changed && out == nil {
				out = make(map[string]any, len(t))
				for k2, e2 := range t {
					out[k2] = e2
				}
			}
			if changed {
				out[k] = ne
			}
		}
		if out == nil {
			return v, false, nil
		}
		return out, true, nil
	case []any:
		var out []any
		for i, e := range t {
			ne, changed, err := r.Resolve(e)
			if err != nil {
				return nil, false, err
			}
			if changed && out == nil {
				out = append([]any(nil), t...)
			}
			if changed {
				out[i] = ne
			}
		}
		if out == nil {
			return v, false, nil
		}
		return out, true, nil
	}
	return v, false, nil
}

func (r *Resolver) one(ref launch.Ref) (string, error) {
	fail := func(reason string) (string, error) {
		return "", &Error{Scheme: ref.Scheme, Name: ref.Name, Reason: reason}
	}
	switch ref.Scheme {
	case "env":
		if !strings.HasPrefix(ref.Name, EnvPrefix) {
			return fail("only names with the prefix " + EnvPrefix + " can be read")
		}
		if v, ok := r.Env(ref.Name); ok {
			return v, nil
		}
		return fail("the variable is not set")
	case "flag":
		if v, ok := r.Flag(ref.Name); ok {
			return v, nil
		}
		return fail("the flag is not set")
	case "file":
		v, reason := r.file(ref.Name)
		if reason != "" {
			return fail(reason)
		}
		return v, nil
	case "secret":
		// ADR-059 decision 10: "A `secret:` reference fails with 'does not resolve'
		// until S3", which builds the secrets definition.
		return fail("secrets are not available before S3")
	}
	return fail("unknown scheme")
}

// file reads name from the one directory the root flag names, which must be absolute
// so that it does not depend on the working directory; the server's flag for it is
// --refs-dir. os.Root refuses a name that leaves the directory, by ".." or by a link,
// and an absolute name. The docs say only "one directory named by flag"
// (08-profiles-roadmap-adrs.md:162).
func (r *Resolver) file(name string) (string, string) {
	if r.RootFlag == "" {
		return "", "no file root is configured"
	}
	dir, ok := r.Flag(r.RootFlag)
	if !ok || dir == "" {
		return "", "the flag " + r.RootFlag + " that names the file root is not set"
	}
	if !filepath.IsAbs(dir) {
		return "", "the file root is not an absolute path"
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", "the file root cannot be opened"
	}
	defer root.Close()
	// A FIFO or a device can block an open or a read without bound, so only a regular
	// file is read, and it is opened without blocking to find out what it is.
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", "the file does not exist"
	case err != nil:
		return "", "the path is outside the file root or cannot be opened"
	}
	defer f.Close()
	fi, err := f.Stat()
	var b []byte
	if err == nil && fi.Mode().IsRegular() {
		// One byte past the limit shows a larger file; min keeps MaxInt64 from wrapping.
		b, err = io.ReadAll(io.LimitReader(f, min(r.MaxBytes, math.MaxInt64-1)+1))
	}
	switch {
	case err != nil || !fi.Mode().IsRegular():
		return "", "the path is not a regular file or cannot be read"
	case int64(len(b)) > r.MaxBytes:
		return "", fmt.Sprintf("the file is larger than the limit of %d bytes", r.MaxBytes)
	}
	return string(b), ""
}
