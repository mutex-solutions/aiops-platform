// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"fmt"
	"regexp"
	"strings"
)

// The rules of a request that every provider keeps alike, so the row and the fake
// state each once (ADR-061 decisions 17 and 18). Each returns an error that matches
// ErrUsage or ErrRefused, or nil.

var (
	kebab = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	hook  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`) // a key or a hook, as boot reads signed
)

func usage(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, a...))
}

func refused(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, a...))
}

// ProfileName refuses a profile that is not a name.
func ProfileName(p string) error {
	if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `/\`) {
		return usage("%q is not the name of a profile", p)
	}
	return nil
}

// TrustForm refuses a trust add that ADR-020 decisions 5 to 7 do not allow: both forms
// or neither, an id pattern that is neither <owner>/<name> nor <owner>/* or whose owner
// is aiops or contrib, which only the release table admits, a runtime other than inproc
// and remote, and a signed entry that names no key or hook.
func TrustForm(r TrustRequest) error {
	switch {
	case r.Distribution && len(r.IDs)+len(r.Runtimes)+len(r.Signed) > 0:
		return refused("a distribution root takes no ids, runtimes or signed entries (ADR-020 decision 5)")
	case !r.Distribution && len(r.IDs) == 0:
		return refused("trust add takes the id patterns of a signer, or a distribution root (ADR-020 decision 5)")
	}
	for _, p := range r.IDs {
		owner, name, ok := strings.Cut(p, "/")
		if !ok || !kebab.MatchString(owner) || name != "*" && !kebab.MatchString(name) || owner == "aiops" || owner == "contrib" {
			return refused("%q is not an id pattern: <owner>/<name> or <owner>/*, of an owner other than aiops and contrib (ADR-020 decision 6)", p)
		}
	}
	for _, rt := range r.Runtimes {
		switch rt {
		case "inproc", "remote":
		case "wasm":
			return refused("the runtime wasm is refused, since gate check 1 denies it (ADR-020 decision 6)")
		default:
			return refused("%q is not a runtime: inproc or remote, and none for packs only (ADR-020 decision 6)", rt)
		}
	}
	for _, s := range r.Signed {
		if !hook.MatchString(s) {
			return refused("%q names no signed key or hook (ADR-020 decision 7)", s)
		}
	}
	return nil
}
