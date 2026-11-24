// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// The profile commands (ADR-061 decisions 16, 20, 27 and 41): profile render and profile
// verify hand their request to the launcher's toolbox, which composes and admits with
// boot's code and this binary's module table, and write nothing; profile init and
// profile sanitize write the home through Home.Write, under its lock and its snapshot.

// profileName refuses a profile that is not a name.
func profileName(p string) error {
	if p == "" || p == "." || p == ".." || strings.ContainsAny(p, `/\`) {
		return usage("%q is not the name of a profile", p)
	}
	return nil
}

// Render is profile render: the request as the launcher's toolbox renders it. With no
// home the toolbox composes the shipped profiles alone (ADR-061 decision 25).
func (l Library) Render(ctx context.Context, r def.RenderRequest) (launch.Rendered, error) {
	if r.Home != "" {
		if _, err := l.home(r.Home); err != nil {
			return launch.Rendered{}, err
		}
	}
	if err := profileName(r.Profile); err != nil {
		return launch.Rendered{}, err
	}
	return l.Box.Render(ctx, r.RenderRequest)
}

// VerifyProfile is profile verify: the render of the profile, cli when none is named,
// for its departures from the release bindings, with no dump
// (08-profiles-roadmap-adrs.md:366).
func (l Library) VerifyProfile(ctx context.Context, r def.RenderRequest) (launch.Rendered, error) {
	r.Profile, r.Dumps = cmp.Or(r.Profile, "cli"), nil
	return l.Render(ctx, r)
}

// InitProfile is profile init: it copies the shipped profiles/<p>/profile.yaml into the
// home, unless the home holds one, which it keeps (ADR-060 decision 27).
func (l Library) InitProfile(ctx context.Context, r def.ProfileRequest) (def.Change, error) {
	h, err := l.home(r.Home)
	if err != nil {
		return def.Change{}, err
	}
	if err := profileName(r.Profile); err != nil {
		return def.Change{}, err
	}
	shipped, err := l.Box.Shipped(ctx)
	if err != nil {
		return def.Change{}, err
	}
	rel := "profiles/" + r.Profile + "/profile.yaml"
	data, ok := shipped[rel]
	if !ok {
		return def.Change{}, refused("no shipped profile %s", r.Profile)
	}
	var ch def.Change
	note := "copied the shipped profile " + r.Profile + " to " + rel
	done, err := h.Write(ctx, Write{
		Command: "profile init " + r.Profile,
		Plan: func() ([]Change, error) {
			switch _, err := os.Lstat(h.path(rel)); {
			case err == nil:
				note = "the home holds " + rel + ", which is kept"
				return nil, nil
			case !errors.Is(err, fs.ErrNotExist):
				return nil, err
			}
			return []Change{{Path: rel, Data: data}}, nil
		},
		Submit: func(context.Context) error { ch.Outcome, ch.Reason = submission(h, r.Profile); return nil },
	})
	ch.Written, ch.Notes = done.Written, done.Notes
	if err == nil {
		ch.Notes = append(ch.Notes, note)
	}
	return ch, err
}

// SanitizeProfile is profile sanitize: under the lock it renders the profile, cli when
// none is named, and sets aside the layer file of the home that stops its composition,
// its profile layer or the site layer, then renders again with that file read as
// absent, as dsh's sanitizeProfile does (ADR-061 decision 41; dsh §3.4). Each file set
// aside is copied to <name>.bak-<time> and removed, and the profile is submitted.
func (l Library) SanitizeProfile(ctx context.Context, r def.ProfileRequest) (def.Change, error) {
	h, err := l.home(r.Home)
	if err != nil {
		return def.Change{}, err
	}
	p := cmp.Or(r.Profile, "cli")
	if err := profileName(p); err != nil {
		return def.Change{}, err
	}
	layers := []string{"profiles/" + p + "/aiops.patch.yml", "aiops.patch.yml"}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	var ch def.Change
	var notes []string
	done, err := h.Write(ctx, Write{
		Command: "profile sanitize " + p,
		Plan: func() ([]Change, error) {
			var aside []string
			for range layers {
				rd, err := l.Box.Render(ctx, launch.RenderRequest{Home: h.Dir, Profile: p, Aside: aside})
				if err != nil {
					return nil, err
				}
				i := slices.IndexFunc(rd.Diagnostics, func(d launch.Diagnostic) bool {
					return !d.Warning && d.Check == "compose" && slices.Contains(layers, d.File) && !slices.Contains(aside, d.File)
				})
				if i < 0 {
					break
				}
				d := rd.Diagnostics[i]
				aside = append(aside, d.File)
				notes = append(notes, fmt.Sprintf("%s: set aside as %s.bak-%s", d.Text, path.Base(d.File), stamp))
			}
			var changes []Change
			for _, f := range aside {
				data, err := os.ReadFile(h.path(f))
				if err != nil {
					return nil, err
				}
				changes = append(changes, Change{Path: f + ".bak-" + stamp, Data: data}, Change{Path: f, Remove: true})
			}
			if len(changes) == 0 {
				notes = append(notes, "no layer file of the home stops the composition of "+p)
			}
			return changes, nil
		},
		Submit: func(context.Context) error { ch.Outcome, ch.Reason = submission(h, p); return nil },
	})
	ch.Written, ch.Notes = done.Written, done.Notes
	if err == nil {
		ch.Notes = append(ch.Notes, notes...)
	}
	return ch, err
}
