// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
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

// CannotDisable refuses a change that disables or removes the row id of the profile
// that rd renders, which the manager cannot make (ADR-061 decision 17; ADR-062 decision
// 12): a row of aiops-cli in the profile cli, the transport's rows reload-socket and
// reload, a required row, and the last enabled provider of a provider-bound or
// consumer-bound key, whose consumers would lose it. Each refusal says to edit the
// profile layer and restart the process.
func CannotDisable(rd launch.Rendered, profile, id string) error {
	i := slices.IndexFunc(rd.Composition.Rows, func(r launch.Row) bool { return r.ID == id })
	if i < 0 {
		return refused("%s composes no row %q", profile, id)
	}
	r := rd.Composition.Rows[i]
	why := ""
	switch {
	case profile == "cli" && (r.Plugin == "aiops/cli" || r.Plugin == "aiops/operations" || strings.HasPrefix(r.Plugin, "aiops/command-")):
		why = "it is a row of aiops-cli, which runs this command"
	case r.Plugin == "aiops/reload-socket" || r.Plugin == "aiops/reload":
		why = "it carries the submission of this command (ADR-062 decision 12)"
	case r.Required || slices.Contains(rd.Required.Rows, id):
		why = "it is a required row"
	}
	for _, k := range rd.Keys {
		if why == "" && slices.Equal(k.Rows, []string{id}) && (k.Provider == "bound" || k.Consumer == "bound") {
			why = "it is the last enabled provider of the bound key " + k.Name
		}
	}
	if why != "" {
		return refused("the manager cannot disable or remove %s of %s: %s; edit profiles/%s/aiops.patch.yml and restart the process", id, profile, why, profile)
	}
	return nil
}

// CannotRemove refuses the removal of the installed artifact id from the profile that
// rd renders, which takes away the rows whose origin is id, when one of them is a row
// that CannotDisable refuses, or when they are every enabled provider of a bound or a
// required key (ADR-061 decision 17).
func CannotRemove(rd launch.Rendered, profile, id string) error {
	lost := map[string]bool{}
	for _, row := range rd.Composition.Rows {
		if row.Origin != id {
			continue
		}
		if err := CannotDisable(rd, profile, row.ID); err != nil {
			return err
		}
		lost[row.ID] = true
	}
	for _, k := range rd.Keys {
		bound := k.Provider == "bound" || k.Consumer == "bound" || slices.Contains(rd.Required.Keys, k.Name)
		if bound && len(k.Rows) > 0 && !slices.ContainsFunc(k.Rows, func(row string) bool { return !lost[row] }) {
			return refused("the manager cannot remove %s from %s: its rows are the last enabled providers of the key %s; edit profiles/%s/profile.yaml and restart the process",
				id, profile, k.Name, profile)
		}
	}
	return nil
}

// Shows reports whether a final result passed and shows the change of command on
// target (ADR-062 decision 11): its report holds the row of enable as ACTIVE, and holds
// neither the row of disable nor a row of the group of the artifact that remove
// removed; a pass of install shows its change.
func Shows(r *reload.WireResult, command, target string) bool {
	if r == nil || r.Status != reload.Passed {
		return false
	}
	for _, row := range r.Report.Rows {
		switch {
		case command == reload.CommandEnable && row.ID == target:
			return row.State == string(kernel.Active)
		case command == reload.CommandDisable && row.ID == target,
			command == reload.CommandRemove && strings.HasPrefix(row.ID, Group(target)+":"):
			return false
		}
	}
	return command != reload.CommandEnable
}

// Group is the group under which the rows of the artifact id mount: its name
// (08:395, "a row id is <pack name>:<row>").
func Group(id string) string {
	_, name, _ := strings.Cut(id, "/")
	return name
}
