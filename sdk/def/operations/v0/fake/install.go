// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
)

// The plugins of a profile: what install, enable, disable and remove change, on the
// verdict, the Rendered and the Result of the config.

// layer is what the fake holds of one profile of a home.
type layer struct {
	bundles []string
	enabled map[string]bool
	locked  []operations.Locked
}

func (s served) Gate(_ context.Context, r operations.GateRequest) (launch.Verdict, error) {
	return do(s, "Gate", func() (launch.Verdict, error) {
		if !filepath.IsAbs(r.Against) {
			return launch.Verdict{}, usage("the signed manifest %q is not an absolute path", r.Against)
		}
		_, err := s.f.artifactAt(r.Spec)
		return s.f.cfg.Verdict, err
	})
}

func (s served) Install(ctx context.Context, r operations.InstallRequest) (operations.Change, error) {
	return do(s, "Install", func() (operations.Change, error) {
		ch := operations.Change{Verdict: s.f.cfg.Verdict}
		a, signer, err := s.f.signer(r.Home, r.Spec)
		if err == nil {
			err = operations.ProfileName(r.Profile)
		}
		for _, c := range ch.Verdict.Clauses {
			if c.Outcome == launch.OutcomeDeny && err == nil {
				err = refused("check %d denies: %s", c.Check, c.Reason)
			}
		}
		switch {
		case err != nil:
			return ch, err
		case ch.Verdict.RebuildRequired:
			ch.DistLines = []string{"plugins:", fmt.Sprintf("  - {module: %s, version: %s, sum: %s}", a.module[0], a.module[1], a.module[2])}
			return ch, operations.ErrRebuildRequired
		case r.Profile == "cli" && !slices.Contains(a.in.Runtimes, "inproc"):
			return ch, refused("into the profile cli only in-process code installs (ADR-061 decision 40)")
		}
		return s.f.change(ctx, r.Home, r.Profile, reload.CommandInstall, a.in.ID, &ch, func(l *layer, st *state) error {
			held, kept := st.holders(r.Profile, a.in.ID), st.kept[a.in.ID]
			switch {
			case len(held) > 0 && kept != a.in.Digest:
				return refused("the profiles %s name %s at %s, and a site holds one digest of a plugin id (ADR-047 decision 7)",
					strings.Join(held, ", "), a.in.ID, cmp.Or(kept, "no kept files"))
			case slices.ContainsFunc(l.bundles, func(b string) bool { return operations.Group(b) == operations.Group(a.in.ID) }):
				return refused("%s holds an artifact of the name %s", r.Profile, operations.Group(a.in.ID))
			case kept != "" && len(held) == 0:
				ch.Notes = append(ch.Notes, "removed the leftover bundles/"+a.in.ID+", which no profile names (ADR-047 decision 10)")
			}
			st.kept[a.in.ID], l.bundles = a.in.Digest, append(l.bundles, a.in.ID)
			if len(a.in.Runtimes) == 0 {
				l.locked = append(l.locked, operations.Locked{ID: a.in.ID, Digest: a.in.Digest})
			}
			st.records = append(slices.DeleteFunc(st.records, func(x operations.Record) bool { return x.Digest == a.in.Digest }),
				operations.Record{Digest: a.in.Digest, Kind: "artifact", Signer: signer, ID: a.in.ID})
			return nil
		})
	})
}

// holders returns the profiles of the home but p whose bundle list names id. Each holds
// the digest of the kept files, its lock entry's for a pack, since an install of
// another digest is refused while one does (ADR-047 decisions 7 and 16).
func (st *state) holders(p, id string) []string {
	return slices.DeleteFunc(slices.Clone(st.profiles), func(q string) bool { return q == p || !slices.Contains(st.layers[q].bundles, id) })
}

func (s served) Enable(ctx context.Context, r operations.RowRequest) (operations.Change, error) {
	return do(s, "Enable", func() (operations.Change, error) { return s.f.row(ctx, r, true) })
}

func (s served) Disable(ctx context.Context, r operations.RowRequest) (operations.Change, error) {
	return do(s, "Disable", func() (operations.Change, error) { return s.f.row(ctx, r, false) })
}

// row writes the enabled departure of r.Row after the rules of the Rendered of the
// config.
func (f *Fake) row(ctx context.Context, r operations.RowRequest, on bool) (operations.Change, error) {
	rd, command := f.cfg.Rendered[r.Profile], reload.CommandEnable
	err := cmp.Or(homeOf(r.Home), f.composes(r.Profile))
	switch {
	case err != nil:
	case !on:
		command, err = reload.CommandDisable, operations.CannotDisable(rd, r.Profile, r.Row)
	case !slices.ContainsFunc(rd.Composition.Rows, func(x launch.Row) bool { return x.ID == r.Row }):
		err = refused("%s composes no row %q", r.Profile, r.Row)
	}
	if err != nil {
		return operations.Change{}, err
	}
	var ch operations.Change
	return f.change(ctx, r.Home, r.Profile, command, r.Row, &ch, func(l *layer, _ *state) error {
		l.enabled[r.Row] = on
		return nil
	})
}

// Remove takes an installed artifact only (ADR-047 decision 10; ADR-062 decision 12),
// from a profile that composes, drops the enabled departures of its rows, so a later
// install leaves them disabled, and the kept files once the change is kept and no
// profile names the id.
func (s served) Remove(ctx context.Context, r operations.RemoveRequest) (operations.Change, error) {
	return do(s, "Remove", func() (operations.Change, error) {
		var ch operations.Change
		return s.f.change(ctx, r.Home, r.Profile, reload.CommandRemove, r.ID, &ch, func(l *layer, st *state) error {
			i, err := slices.Index(l.bundles, r.ID), cmp.Or(operations.CannotRemove(s.f.cfg.Rendered[r.Profile], r.Profile, r.ID), s.f.composes(r.Profile))
			switch _, kept := st.kept[r.ID]; {
			case i < 0:
				return refused("%s does not install %s", r.Profile, r.ID)
			case !kept:
				return refused("%s is no installed artifact of %s: plugin remove takes the plugin id of one, with its kept files", r.ID, r.Profile)
			case err != nil:
				return err
			}
			l.bundles = slices.Delete(l.bundles, i, i+1)
			l.locked = slices.DeleteFunc(l.locked, func(e operations.Locked) bool { return e.ID == r.ID })
			maps.DeleteFunc(l.enabled, func(id string, _ bool) bool { return strings.HasPrefix(id, operations.Group(r.ID)+":") })
			if len(st.holders(r.Profile, r.ID)) == 0 && (s.f.cfg.Result == nil || operations.Shows(s.f.cfg.Result, reload.CommandRemove, r.ID)) {
				delete(st.kept, r.ID)
			}
			return nil
		})
	})
}

// composes refuses a change of profile whose Rendered in the config holds an error, as
// the row keeps a change only when the changed profile renders.
func (f *Fake) composes(profile string) error {
	for _, d := range f.cfg.Rendered[profile].Diagnostics {
		if !d.Warning {
			return refused("%s does not compose: %s", profile, d.Text)
		}
	}
	return nil
}

// change runs edit on the layer of profile under the lock of the home, after a copy of
// the shipped profile where the home holds none, and answers the submission of
// command on target by the config: with no Result no process runs; a Result that
// does not pass restores the home; a pass that does not show the change keeps it, and
// both fail with ErrSubmission.
func (f *Fake) change(ctx context.Context, home, profile, command, target string, ch *operations.Change, edit func(*layer, *state) error) (operations.Change, error) {
	if err := homeOf(home); err != nil {
		return *ch, err
	}
	if err := operations.ProfileName(profile); err != nil {
		return *ch, err
	}
	var notes []string
	written, err := f.write(ctx, home, func(st *state) error {
		if !slices.Contains(st.profiles, profile) {
			b, ok := f.cfg.Shipped["profiles/"+profile+"/profile.yaml"]
			if !ok {
				return refused("neither the home nor the binary holds the profile %s", profile)
			}
			st.profiles = append(st.profiles, profile)
			slices.Sort(st.profiles)
			list := strings.Trim(strings.TrimSpace(strings.TrimPrefix(string(b), "bundles:")), "[]")
			st.layers[profile] = layer{bundles: strings.Split(list, ", ")}
			notes = append(notes, "copied the shipped profile "+profile)
		}
		l := st.layers[profile]
		l.bundles, l.locked, l.enabled = slices.Clone(l.bundles), slices.Clone(l.locked), maps.Clone(l.enabled)
		if l.enabled == nil {
			l.enabled = map[string]bool{}
		}
		if err := edit(&l, st); err != nil {
			return err
		}
		st.layers[profile] = l
		ch.Result = f.cfg.Result
		if ch.Result == nil {
			ch.Outcome = operations.NotRunning
		} else if ch.Result.Status != reload.Passed {
			return fmt.Errorf("%w: %s", operations.ErrSubmission, ch.Result.Status)
		}
		return nil
	})
	ch.Notes = append(append(written, notes...), ch.Notes...)
	if err == nil && ch.Result != nil && !operations.Shows(ch.Result, command, target) {
		err = fmt.Errorf("%w: passed, and a later layer overrides %s %s", operations.ErrSubmission, command, target)
	}
	return *ch, err
}
