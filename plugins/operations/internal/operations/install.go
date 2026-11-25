// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.yaml.in/yaml/v3"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
)

// The plugins of a profile (ADR-047 decisions 5 to 10 and 17; ADR-061 decision 17;
// ADR-062 decision 11): install, enable, disable and remove write the home under its
// lock and its snapshot and submit the profile over the socket of reload-socket; plugin
// gate --against writes nothing.

// Gate is plugin gate --against: the clauses of that entry point on the artifact of
// the spec against a published signed manifest, with no signer verified (08:493).
func (l Library) Gate(ctx context.Context, r def.GateRequest) (launch.Verdict, error) {
	if err := absolute("the signed manifest of --against", r.Against); err != nil {
		return launch.Verdict{}, err
	}
	against, err := os.ReadFile(r.Against)
	if err != nil {
		return launch.Verdict{}, refused("%v", err)
	}
	src, d, err := open(ctx, r.Caller, r.Spec)
	if err != nil {
		return launch.Verdict{}, err
	}
	q := launch.GateRequest{Entry: "plugin gate --against", Digest: d.Digest.String(), Against: against}
	m, err := manifest(ctx, src, d)
	for _, f := range []struct {
		mediaType string
		out       *[]byte
	}{{layerManifest, &q.Manifest}, {layerBundle, &q.Bundle}} {
		if l := layer(m, f.mediaType); err == nil && l != nil {
			*f.out, err = fetch(ctx, src, *l)
		}
	}
	if err == nil {
		q.Attestation, err = recorded(ctx, src, d)
	}
	if err != nil {
		return launch.Verdict{}, err
	}
	return l.Box.Gate(ctx, q)
}

// recorded returns the statement of the attestation of d as it stands, unverified: the
// unsigned statement that pack recorded, or the payload of the bundle that sign wrote.
func recorded(ctx context.Context, src source, d ocispec.Descriptor) ([]byte, error) {
	ms, err := referrers(ctx, src, d, statementType)
	if err == nil && len(ms) > 0 && len(ms[0].Layers) == 1 {
		return fetch(ctx, src, ms[0].Layers[0])
	}
	bs, err := bundles(ctx, src, d, attestationURI)
	if err != nil || len(bs) == 0 {
		return nil, err
	}
	b, err := load(bs[0])
	if err != nil {
		return nil, err
	}
	return b.GetDsseEnvelope().GetPayload(), nil
}

// Install is plugin install: under the lock it verifies the signature and the
// attestation as Verify does, refuses remote code until S2b and a pack or remote code
// into the profile cli (ADR-061 decision 40), refuses an id that another profile names
// at another digest, since a site holds one (ADR-047 decision 7), renders the profile
// with the artifact's bundle appended, which refuses a group that clashes, refuses an
// artifact of the name of one the profile holds, of whatever owner, and runs every
// clause of the entry point plugin install. A denial and rebuild-required write
// nothing. Then it writes the kept files, which it reuses where another profile names
// them and which replace, with a note, a leftover that no profile names (decision 10),
// the record, plugins.lock for a pack and the bundle list, and submits.
func (l Library) Install(ctx context.Context, r def.InstallRequest) (def.Change, error) {
	h, err := l.home(r.Home)
	if err == nil {
		r.Profile, err = profileOf(h, r.Profile)
	}
	if err != nil {
		return def.Change{}, err
	}
	src, d, err := open(ctx, r.Caller, r.Spec)
	if err != nil {
		return def.Change{}, err
	}
	var ch def.Change
	var res error
	var a trusted
	done, err := h.Write(ctx, Write{Command: "plugin install " + r.Spec,
		Plan: func() ([]Change, error) {
			if a, err = verified(ctx, h, src, d); err != nil {
				return nil, err
			}
			switch code := len(a.Runtimes) > 0; {
			case slices.Contains(a.Runtimes, "remote"):
				return nil, refused("remote code installs from S2b, with the remote runtime (ADR-060 decision 29)")
			case r.Profile == "cli" && !code:
				return nil, refused("into the profile cli only in-process code installs, since a pack's rows contribute no command (ADR-061 decision 40)")
			}
			keptAt, held, err := holders(h, r.Profile, a.ID)
			if err != nil {
				return nil, err
			}
			var other []string
			for _, p := range slices.Sorted(maps.Keys(held)) {
				if held[p] != d.Digest.String() {
					other = append(other, fmt.Sprintf("%s (%s)", p, cmp.Or(held[p], "no kept files")))
				}
			}
			if len(other) > 0 {
				return nil, refused("the profiles %s name %s at another digest, and a site holds one digest of a plugin id (ADR-047 decision 7): plugin remove it from them before an install of %s",
					strings.Join(other, ", "), a.ID, d.Digest)
			}
			profile, _, err := l.profile(ctx, h, r.Profile, &ch)
			if err != nil {
				return nil, err
			}
			bundle, err := fetchLayer(ctx, src, a.m, layerBundle)
			if err == nil {
				err = l.admit(ctx, h, r.Profile, src, d, a, bundle, &ch)
			}
			if err != nil {
				return nil, err
			}
			list, err := withBundles(profile, func(list []string) ([]string, error) {
				for _, b := range list {
					if def.Group(b) == def.Group(a.ID) {
						return nil, refused("%s holds %s, and two artifacts of the name %s never compose together (ADR-047 decision 17); plugin remove it first", r.Profile, b, def.Group(a.ID))
					}
				}
				return append(list, a.ID), nil
			})
			if err != nil {
				return nil, err
			}
			changes, entry, err := kept(ctx, src, d, a, bundle)
			if err != nil {
				return nil, err
			}
			for _, dir := range []string{"bundles/" + a.ID, "content/" + a.ID} {
				_, serr := os.Stat(h.path(dir))
				switch {
				case len(held) > 0 && keptAt == d.Digest.String() && serr == nil: // another profile names these kept files
					changes = slices.DeleteFunc(changes, func(c Change) bool { return c.Path == dir })
				case len(held) == 0 && serr == nil:
					ch.Notes = append(ch.Notes, fmt.Sprintf("removed the leftover %s, which no profile names (ADR-047 decision 10)", h.path(dir)))
					if !slices.ContainsFunc(changes, func(c Change) bool { return c.Path == dir }) {
						changes = append([]Change{{Path: dir, Remove: true}}, changes...)
					}
				}
			}
			if len(a.Runtimes) == 0 {
				var lock []byte
				lock, err = withLock(h, r.Profile, entry.ID, &entry)
				changes = append(changes, Change{Path: "profiles/" + r.Profile + "/plugins.lock", Data: lock})
			}
			return append(changes, Change{Path: "profiles/" + r.Profile + "/profile.yaml", Data: list}), err
		},
		Submit: submitted(h, r.Profile, reload.CommandInstall, "", [2]time.Duration{r.ConnectWait, r.Wait}, &ch, &res),
	})
	return changed(done, ch, err, res)
}

// holders returns the digest of the kept artifact.json of id, "" for none, and each
// profile of the home but p that names id, in its bundle list or its lock, with the
// digest it holds: that of its lock entry or, for in-process code, which has none, the
// kept one (ADR-047 decisions 7 and 16).
func holders(h Home, p, id string) (string, map[string]string, error) {
	kept := ""
	if b, err := os.ReadFile(h.path("bundles/" + id + "/artifact.json")); err == nil {
		kept = fmt.Sprintf("sha256:%x", sha256.Sum256(b))
	}
	held := map[string]string{}
	files, _ := filepath.Glob(h.path("profiles/*/profile.yaml"))
	for _, f := range files {
		q := filepath.Base(filepath.Dir(f))
		if q == p {
			continue
		}
		var list struct{ Bundles []string }
		locked, err := readLock(h, q)
		if err == nil {
			err = readYAML(f, &list)
		}
		if err != nil {
			return "", nil, err
		}
		if i := slices.IndexFunc(locked, func(e def.Locked) bool { return e.ID == id }); i >= 0 {
			held[q] = locked[i].Digest
		} else if slices.Contains(list.Bundles, id) {
			held[q] = kept
		}
	}
	return kept, held, nil
}

// profileOf is the profile of a command: p, or with none the profile that
// run/status.json of the home names (ADR-061 decision 30).
func profileOf(h Home, p string) (string, error) {
	if p == "" {
		var st reload.ProcessStatus
		if b, err := os.ReadFile(h.path("run/status.json")); err == nil && json.Unmarshal(b, &st) == nil {
			p = st.Profile
		}
		if p == "" {
			return "", usage("no --profile, and no run/status.json of %s names a profile", h.Dir)
		}
	}
	return p, def.ProfileName(p)
}

// profile returns profiles/<p>/profile.yaml of the home, or the shipped template,
// which the write then copies, and says so (ADR-061 decision 17).
func (l Library) profile(ctx context.Context, h Home, p string, ch *def.Change) ([]byte, bool, error) {
	rel := "profiles/" + p + "/profile.yaml"
	b, err := os.ReadFile(h.path(rel))
	if !errors.Is(err, fs.ErrNotExist) {
		return b, false, err
	}
	shipped, err := l.Box.Shipped(ctx)
	if err != nil {
		return nil, false, err
	}
	b, ok := shipped[rel]
	if !ok {
		return nil, false, refused("neither the home nor this binary holds the profile %s", p)
	}
	ch.Notes = append(ch.Notes, "copied the shipped profile "+p+" to "+rel)
	return b, true, nil
}

// admit renders the profile with the bundle appended as boot composes it, and runs the
// clauses of the entry point plugin install on the artifact with its verified signer
// (ADR-060 decision 17; ADR-047 decision 17).
func (l Library) admit(ctx context.Context, h Home, p string, src source, d ocispec.Descriptor, a trusted, bundle []byte, ch *def.Change) error {
	rd, err := l.Box.Render(ctx, launch.RenderRequest{Home: h.Dir, Profile: p, Append: launch.Bundle{ID: a.ID, Data: bundle}})
	if err != nil {
		return err
	}
	if e := errorsOf(rd.Diagnostics); e != "" {
		return refused("%s does not compose with the bundle of %s: %s", p, a.ID, e)
	}
	ch.Verdict, err = l.Box.Gate(ctx, launch.GateRequest{Entry: "plugin install", Manifest: a.file, Bundle: bundle,
		Attestation: a.statement, Digest: d.Digest.String(), Signer: a.signer, Home: h.Dir, Profile: p})
	if err != nil {
		return err
	}
	var denied []string
	for _, c := range ch.Verdict.Clauses {
		if c.Outcome == launch.OutcomeDeny {
			denied = append(denied, fmt.Sprintf("check %d %s: %s", c.Check, c.Clause, c.Reason))
		}
	}
	switch {
	case len(denied) > 0:
		return refused("the gate at plugin install denies %s: %s", a.ID, strings.Join(denied, "; "))
	case ch.Verdict.RebuildRequired:
		var mod struct{ Path, Version string }
		b, err := fetchLayer(ctx, src, a.m, layerModule)
		if err == nil {
			_ = json.Unmarshal(b, &mod)
		}
		ch.DistLines = []string{"modules:", fmt.Sprintf("  - {path: %s, version: %s}", mod.Path, mod.Version)}
		return fmt.Errorf("%w: %s is in-process code that the distribution of this home does not link: build one with dist.yaml naming its module, dist build, dist sign and dist use it, then install again (08:397)",
			def.ErrRebuildRequired, a.ID)
	}
	return nil
}

func errorsOf(ds []launch.Diagnostic) string {
	var out []string
	for _, d := range ds {
		if !d.Warning {
			out = append(out, d.Text)
		}
	}
	return strings.Join(out, "; ")
}

// fetchLayer returns the layer of mediaType of m, nil for none.
func fetchLayer(ctx context.Context, src source, m ocispec.Manifest, mediaType string) ([]byte, error) {
	if l := layer(m, mediaType); l != nil {
		return fetch(ctx, src, *l)
	}
	return nil, nil
}

// kept returns what install keeps of the artifact (ADR-047 decisions 5, 8 and 18;
// ADR-020 decision 10): bundles/<id>/ with the image manifest, plugin.yaml and
// bundle.yaml, content/<id>/ unpacked from the content layer, and the record under
// verified/; and its lock entry.
func kept(ctx context.Context, src source, d ocispec.Descriptor, a trusted, bundle []byte) ([]Change, def.Locked, error) {
	entry := def.Locked{ID: a.ID, Digest: d.Digest.String()}
	out := []Change{{Path: "bundles/" + a.ID, Files: map[string][]byte{"artifact.json": a.raw, "plugin.yaml": a.file, "bundle.yaml": bundle}}}
	if c := layer(a.m, layerContent); c != nil {
		b, err := fetch(ctx, src, *c)
		files := map[string][]byte{}
		if err == nil {
			err = untar(b, func(name string, data []byte) error { files[name] = data; return nil })
		}
		if err != nil {
			return nil, entry, err
		}
		entry.Content = c.Digest.String()
		out = append(out, Change{Path: "content/" + a.ID, Files: files})
	}
	rec := fmt.Sprintf("kind: artifact\ndigest: %s\nsigner: %s\nid: %s\nversion: %s\nruntimes: [%s]\n", d.Digest, a.signer, a.ID, a.Version, strings.Join(a.Runtimes, ", "))
	if slices.Contains(a.Runtimes, "inproc") {
		var mod struct{ Path, Version, Sum string }
		b, err := fetchLayer(ctx, src, a.m, layerModule)
		if err == nil {
			err = json.Unmarshal(b, &mod)
		}
		if err != nil {
			return nil, entry, refused("the module layer of %s: %v", a.ID, err)
		}
		rec += fmt.Sprintf("module: {path: %q, version: %q, sum: %q}\n", mod.Path, mod.Version, mod.Sum)
	}
	files := map[string][]byte{"record.yaml": []byte(rec), "attestation.json": a.statement}
	if a.sig != nil {
		files["signature.sigstore.json"], files["attestation.sigstore.json"] = a.sig, a.att
	}
	return append(out, Change{Path: "verified/" + strings.Replace(d.Digest.String(), ":", "-", 1), Files: files}), entry, nil
}

// withBundles returns the profile file b with its bundle list edited.
func withBundles(b []byte, edit func([]string) ([]string, error)) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, refused("profile.yaml is not a mapping: %v", err)
	}
	m := doc.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != "bundles" {
			continue
		}
		var list []string
		if err := m.Content[i+1].Decode(&list); err != nil {
			return nil, refused("bundles of profile.yaml: %v", err)
		}
		list, err := edit(list)
		if err == nil {
			err = m.Content[i+1].Encode(list)
		}
		m.Content[i+1].Style = yaml.FlowStyle
		if err != nil {
			return nil, err
		}
		return yaml.Marshal(&doc)
	}
	return nil, refused("profile.yaml lists no bundles")
}

// withLock returns plugins.lock of the profile with the entry of id replaced by e, or
// removed when e is nil, in the form boot reads (ADR-047 decision 8).
func withLock(h Home, p, id string, e *def.Locked) ([]byte, error) {
	locked, err := readLock(h, p)
	if err != nil {
		return nil, err
	}
	locked = slices.DeleteFunc(locked, func(l def.Locked) bool { return l.ID == id })
	if e != nil {
		locked = append(locked, *e)
	}
	slices.SortFunc(locked, func(a, b def.Locked) int { return strings.Compare(a.ID, b.ID) })
	var b bytes.Buffer
	b.WriteString("lockVersion: 1\nartifacts:")
	if len(locked) == 0 {
		b.WriteString(" []")
	}
	b.WriteString("\n")
	for _, l := range locked {
		fmt.Fprintf(&b, "  - {id: %s, digest: %s", l.ID, l.Digest)
		if l.Content != "" {
			fmt.Fprintf(&b, ", content: %s", l.Content)
		}
		b.WriteString("}\n")
	}
	return b.Bytes(), nil
}

func readLock(h Home, p string) ([]def.Locked, error) {
	var lock struct{ Artifacts []def.Locked }
	err := readYAML(h.path("profiles/"+p+"/plugins.lock"), &lock)
	return lock.Artifacts, err
}

// submitted is the submission of a write of Install, Enable, Disable or Remove over
// the socket of reload-socket (ADR-062 decisions 9 to 11), which runs under the lock
// once the renames are done. It records the outcome in ch and its error, by the table
// of ADR-061 decision 17, in *res, and returns that error where the table restores the
// files, so the write restores its snapshot.
func submitted(h Home, profile, command, target string, waits [2]time.Duration, ch *def.Change, res *error) func(context.Context) error {
	return func(ctx context.Context) error {
		o, err := Submit(ctx, Request{Home: h.Dir, Profile: profile, Command: command, ConnectWait: waits[0], Wait: waits[1]})
		if err != nil {
			return err
		}
		ch.Result, ch.Error, ch.IDs, ch.Last = o.Result, o.Error, o.IDs, o.Last
		restart := "; restart the process, which then boots the restored files (08:244)"
		ids := fmt.Sprintf("operation %v, last status %q", o.IDs, o.Last)
		switch o.Kind {
		case NotRunning:
			ch.Outcome = def.NotRunning
			ch.Notes = append(ch.Notes, fmt.Sprintf("no process runs %s on %s; the change applies when it starts", profile, h.Dir))
		case RestartRequired:
			ch.Outcome, ch.Reason = def.RestartRequired, def.ReasonNoAnswer
			*res = fmt.Errorf("%w: restart-required: %s did not answer; the change applies at the next start", def.ErrSubmission, o.Socket)
		case Final:
			switch r := o.Result; {
			case r.Status != reload.Passed && r.Error.Has(reload.CauseStateUnknown):
				*res = fmt.Errorf("%w: %s: %s%s", def.ErrSubmission, r.Status, r.Error.Text, restart)
			case r.Status != reload.Passed:
				*res = fmt.Errorf("%w: %s at generation %d: %s", def.ErrSubmission, r.Status, r.Generation, describe(r))
			case !def.Shows(r, command, target):
				*res = fmt.Errorf("%w: passed at generation %d, and %s %s does not show: a later layer, the site layer %s or a --patch file of the process, overrides it",
					def.ErrSubmission, r.Generation, command, target, h.path("aiops.patch.yml"))
			default:
				ch.Notes = append(ch.Notes, fmt.Sprintf("passed at generation %d, digest %s", r.Generation, r.Digest))
			}
		case Refused:
			*res = fmt.Errorf("%w: %s: %s", def.ErrSubmission, def.SubmitRefused, o.Error.Text)
			if o.Error.Has(reload.CauseStateUnknown) {
				*res = fmt.Errorf("%w%s", *res, restart)
			}
		default: // NoFinal, Unknown
			*res = fmt.Errorf("%w: %s: %s", def.ErrSubmission, o.Kind, ids)
		}
		if o.Restore() {
			return *res
		}
		return nil
	}
}

// describe names what a final result that did not pass says: each denial with its
// check, each failed row, and the rollback.
func describe(r *reload.WireResult) string {
	var out []string
	for _, d := range r.Admission.Denied {
		out = append(out, fmt.Sprintf("row %s denied by %s: %s", d.Row, d.Check, d.Reason))
	}
	for _, row := range r.Report.Rows {
		if row.Outcome != "pass" {
			out = append(out, fmt.Sprintf("row %s %s: %s", row.ID, row.Outcome, row.Detail))
		}
	}
	if r.Error != nil {
		out = append(out, r.Error.Text)
	}
	if r.Rollback != nil {
		out = append(out, fmt.Sprintf("rolled back to generation %d", r.Rollback.Generation))
	}
	return strings.Join(out, "; ")
}

// changed is the Change of a write that submitted: the paths written and the notes of
// the write, and the error of the write or, after one that kept the files, that of its
// submission.
func changed(done Done, ch def.Change, err, res error) (def.Change, error) {
	ch.Written, ch.Notes = done.Written, append(done.Notes, ch.Notes...)
	if err == nil {
		err = res
	}
	return ch, err
}

func (l Library) Enable(ctx context.Context, r def.RowRequest) (def.Change, error) {
	return l.row(ctx, r, reload.CommandEnable)
}

func (l Library) Disable(ctx context.Context, r def.RowRequest) (def.Change, error) {
	return l.row(ctx, r, reload.CommandDisable)
}

// row is plugin enable and plugin disable (08:375; ADR-061 decision 17): under the
// lock it renders the profile, refuses a row it does not compose and a disable that
// CannotDisable refuses, renders the changed profile in a view of the home, and only
// when boot would admit it writes the departure into the profile layer, copying the
// shipped profile first where the home holds none; then it submits.
func (l Library) row(ctx context.Context, r def.RowRequest, command string) (def.Change, error) {
	h, err := l.home(r.Home)
	if err == nil {
		r.Profile, err = profileOf(h, r.Profile)
	}
	if err != nil {
		return def.Change{}, err
	}
	var ch def.Change
	var res error
	layer := "profiles/" + r.Profile + "/aiops.patch.yml"
	done, err := h.Write(ctx, Write{Command: command + " " + r.Row,
		Plan: func() ([]Change, error) {
			profile, copied, err := l.profile(ctx, h, r.Profile, &ch)
			if err != nil {
				return nil, err
			}
			rd, err := l.composes(ctx, h, r.Profile)
			switch {
			case err != nil:
			case command == reload.CommandDisable:
				err = def.CannotDisable(rd, r.Profile, r.Row)
			case !slices.ContainsFunc(rd.Composition.Rows, func(x launch.Row) bool { return x.ID == r.Row }):
				err = refused("%s composes no row %q", r.Profile, r.Row)
			}
			if err != nil {
				return nil, err
			}
			patch, err := withEnabled(h.path(layer), r.Row, command == reload.CommandEnable)
			changes := []Change{{Path: layer, Data: patch}}
			if copied {
				changes = append([]Change{{Path: "profiles/" + r.Profile + "/profile.yaml", Data: profile}}, changes...)
			}
			if err == nil { // nothing is renamed before boot would admit the change
				var dir string
				if dir, err = view(h, changes); err == nil {
					_, err = l.composes(ctx, Home{Dir: dir}, r.Profile)
				}
				err = errors.Join(err, os.RemoveAll(dir))
			}
			return changes, err
		},
		Submit: submitted(h, r.Profile, command, r.Row, [2]time.Duration{r.ConnectWait, r.Wait}, &ch, &res),
	})
	return changed(done, ch, err, res)
}

// view returns a directory that reads as the home h once the files of cs hold their
// Data, for a render before any rename (ADR-061 decision 17): each directory on the
// path of a change is a directory of the view, whose other entries are symbolic links
// into the home. The caller removes it.
func view(h Home, cs []Change) (string, error) {
	dir, err := os.MkdirTemp("", "aiops-view-")
	if err != nil {
		return "", err
	}
	made := map[string]bool{}
	var open func(rel string) error
	open = func(rel string) error {
		at := filepath.Join(dir, filepath.FromSlash(rel))
		switch {
		case made[rel]:
			return nil
		case rel != ".":
			err := open(path.Dir(rel))
			if err == nil {
				err = os.Remove(at) // the link to the home's directory
			}
			if err == nil || errors.Is(err, fs.ErrNotExist) {
				err = os.Mkdir(at, 0o700)
			}
			if err != nil {
				return err
			}
		}
		made[rel] = true
		es, err := os.ReadDir(h.path(rel))
		for _, e := range es {
			if err == nil {
				err = os.Symlink(filepath.Join(h.path(rel), e.Name()), filepath.Join(at, e.Name()))
			}
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, c := range cs {
		p := filepath.Join(dir, filepath.FromSlash(c.Path))
		err = open(path.Dir(c.Path))
		if err == nil {
			err = os.Remove(p)
		}
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			err = os.WriteFile(p, c.Data, 0o600)
		}
		if err != nil {
			return dir, err
		}
	}
	return dir, nil
}

// composes renders the profile p of the home as boot would, and refuses it when it does
// not compose (ADR-060 decision 17).
func (l Library) composes(ctx context.Context, h Home, p string) (launch.Rendered, error) {
	rd, err := l.Box.Render(ctx, launch.RenderRequest{Home: h.Dir, Profile: p})
	if e := errorsOf(rd.Diagnostics); err == nil && e != "" {
		err = refused("%s does not compose: %s", p, e)
	}
	return rd, err
}

// withEnabled returns the profile layer at p with the departure {id, enabled: on}: the
// enabled of an item of the row is set, or such an item is appended.
func withEnabled(p, id string, on bool) ([]byte, error) {
	doc, err := itemsOf(p)
	if err != nil {
		return nil, err
	}
	seq := doc.Content[0]
	value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(on)}
	for _, it := range seq.Content {
		if v := field(it, "enabled"); v != nil && field(it, "id") != nil && field(it, "id").Value == id {
			*v = *value
			return yaml.Marshal(doc)
		}
	}
	seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "id"}, {Kind: yaml.ScalarNode, Value: id}, {Kind: yaml.ScalarNode, Value: "enabled"}, value}})
	return yaml.Marshal(doc)
}

// withoutEnabled returns the profile layer at p with the enabled field of every item
// under group dropped, and an item left with its id alone, nil where nothing changes;
// and, as JSON, each item under group that stays (ADR-047 decision 10).
func withoutEnabled(p, group string) ([]byte, []string, error) {
	doc, err := itemsOf(p)
	if err != nil {
		return nil, nil, err
	}
	changed, kept := false, []string(nil)
	doc.Content[0].Content = slices.DeleteFunc(doc.Content[0].Content, func(it *yaml.Node) bool {
		if id := field(it, "id"); id == nil || !strings.HasPrefix(id.Value, group+":") {
			return false
		}
		for i := 0; i+1 < len(it.Content); i += 2 {
			if it.Content[i].Value == "enabled" {
				it.Content, changed = slices.Delete(it.Content, i, i+2), true
				break
			}
		}
		if len(it.Content) == 2 {
			changed = true
			return true
		}
		var m map[string]any
		_ = it.Decode(&m)
		y, _ := json.Marshal(m)
		kept = append(kept, string(y))
		return false
	})
	if !changed {
		return nil, kept, nil
	}
	b, err := yaml.Marshal(doc)
	return b, kept, err
}

// itemsOf reads the profile layer at p as a list of items, an empty one where it is
// absent.
func itemsOf(p string) (*yaml.Node, error) {
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, refused("%s: %v", p, err)
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.SequenceNode}}}
	}
	if doc.Content[0].Kind != yaml.SequenceNode {
		return nil, refused("%s is not a list of items", p)
	}
	return &doc, nil
}

// field returns the value of key in the mapping n, nil for none.
func field(n *yaml.Node, key string) *yaml.Node {
	for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// Remove is plugin remove (ADR-047 decision 10): under the lock it refuses an id the
// profile does not install; an id that is no installed artifact, a plugin id with its
// kept files or its lock entry, which keeps every first-party bundle, aiops-cli and the
// transport's among them, out of its reach (ADR-062 decision 12); and, before it
// writes, a removal that takes away a row that CannotDisable refuses or the last
// enabled providers of a bound or a required key (ADR-061 decision 17). It detaches the
// bundle, then the lock entry, drops the enabled field of every item of the profile
// layer under the artifact's group and an item left with its id alone, so a later
// install leaves those rows disabled, keeps and names the items with other fields,
// keeps the change only when the changed profile renders
// as boot would admit it, and submits. After a final result that shows the change, or
// with no process on the profile, once the snapshot is dropped and when no profile
// names the id, it deletes bundles/<id>/ and content/<id>/, so a restore always finds
// them, and names one that stays (ADR-062 decision 11).
func (l Library) Remove(ctx context.Context, r def.RemoveRequest) (def.Change, error) {
	h, err := l.home(r.Home)
	if err == nil {
		r.Profile, err = profileOf(h, r.Profile)
	}
	if err != nil {
		return def.Change{}, err
	}
	var ch def.Change
	var res error
	list, layer := "profiles/"+r.Profile+"/profile.yaml", "profiles/"+r.Profile+"/aiops.patch.yml"
	submit := submitted(h, r.Profile, reload.CommandRemove, r.ID, [2]time.Duration{r.ConnectWait, r.Wait}, &ch, &res)
	done, err := h.Write(ctx, Write{Command: "plugin remove " + r.ID,
		Plan: func() ([]Change, error) {
			b, err := os.ReadFile(h.path(list))
			if err == nil {
				b, err = withBundles(b, func(bs []string) ([]string, error) {
					if i := slices.Index(bs, r.ID); i >= 0 {
						return slices.Delete(bs, i, i+1), nil
					}
					return nil, refused("%s does not install %s", r.Profile, r.ID)
				})
			}
			if err != nil {
				return nil, err
			}
			locked, err := readLock(h, r.Profile)
			if err != nil {
				return nil, err
			}
			_, kerr := os.Stat(h.path("bundles/" + r.ID))
			if strings.Count(r.ID, "/") != 1 || !fs.ValidPath(r.ID) || kerr != nil && !slices.ContainsFunc(locked, func(e def.Locked) bool { return e.ID == r.ID }) {
				return nil, refused("%s is no installed artifact of %s: plugin remove takes the plugin id of one, with its kept files or its lock entry (ADR-047 decision 10; ADR-062 decision 12); edit profiles/%s/profile.yaml to drop another bundle",
					r.ID, r.Profile, r.Profile)
			}
			rd, err := l.Box.Render(ctx, launch.RenderRequest{Home: h.Dir, Profile: r.Profile})
			if err == nil {
				err = def.CannotRemove(rd, r.Profile, r.ID)
			}
			lock, werr := withLock(h, r.Profile, r.ID, nil)
			patch, kept, perr := withoutEnabled(h.path(layer), def.Group(r.ID))
			if err = errors.Join(err, werr, perr); err != nil {
				return nil, err
			}
			for _, y := range kept {
				ch.Notes = append(ch.Notes, fmt.Sprintf("kept in %s: %s", layer, y))
			}
			changes := []Change{{Path: list, Data: b}}
			if slices.ContainsFunc(locked, func(e def.Locked) bool { return e.ID == r.ID }) {
				changes = append(changes, Change{Path: "profiles/" + r.Profile + "/plugins.lock", Data: lock})
			}
			if patch != nil {
				changes = append(changes, Change{Path: layer, Data: patch})
			}
			return changes, nil
		},
		Submit: func(ctx context.Context) error {
			if _, err := l.composes(ctx, h, r.Profile); err != nil {
				return err
			}
			return submit(ctx)
		},
		// The directories go after the snapshot, so neither a failed deletion nor a kill
		// restores a lock entry without its files; one that stays names no profile, and a
		// later install removes it (ADR-047 decision 10).
		After: func(d *Done) {
			profiles, _ := filepath.Glob(h.path("profiles/*/profile.yaml"))
			for _, p := range profiles {
				var f struct{ Bundles []string }
				if readYAML(p, &f) != nil || slices.Contains(f.Bundles, r.ID) {
					return
				}
			}
			if res != nil { // the verdict stays unknown, or the change waits for a start
				return
			}
			for _, dir := range []string{h.path("bundles/" + r.ID), h.path("content/" + r.ID)} {
				if err := os.RemoveAll(dir); err != nil {
					d.Notes = append(d.Notes, fmt.Sprintf("%s stays, and no profile names %s; a later install removes it: %v", dir, r.ID, err))
				}
			}
		},
	})
	return changed(done, ch, err, res)
}
