package boot

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// File names a file inside a file system. The Path is also the name used in
// errors and provenance.
type File struct {
	FS   fs.FS
	Path string
}

// Sources are the inputs of a composition. Nothing else is read, so the same
// Sources and Options always give the same Composition.
//
//	layer 0  the empty root: the start of every composition, no rows
//	layer 1  Bundles: bundles/<name>/bundle.yaml for each name in profile.yaml,
//	         in profile order (required)
//	layer 2  Home: profiles/<Profile>/aiops.patch.yml (optional)
//	layer 3  Home: aiops.patch.yml (optional)
//	layer 4  Patches, in argument order (required)
//	layer 6  not a layer of rows: see ReadKillSwitches
//
// Home holds profiles/<Profile>/profile.yaml, which is required. Layer 5, the
// tenant overlay, arrives at S4b (08-profiles-roadmap-adrs.md:111). Append names
// bundles that follow those of profile.yaml in layer 1, as install appends the bundle
// of an artifact (ADR-047 decision 17).
type Sources struct {
	Profile string
	Home    fs.FS
	Bundles fs.FS
	Patches []File
	Append  []string
}

// Options tune a composition.
type Options struct {
	// Server makes every inserted row state roles
	// (08-profiles-roadmap-adrs.md:156).
	Server bool
	// Modules and Locks enter the digest only (08-profiles-roadmap-adrs.md:184).
	Modules ModuleTable
	Locks   []LockDigest
}

// Compose reads profile.yaml and the layers 1 to 4 and applies the per-layer
// rules of 08-profiles-roadmap-adrs.md:139-148 over the empty root:
//
//   - insert adds a new id only; an existing id fails in every layer;
//   - a patch targets a row by id; an unknown id is a warning;
//   - config in a patch replaces the whole config, and intercept the whole
//     intercept; there is no deep merge (the provenance keeps the origin of
//     each intercept key, see interceptField);
//   - plugin and roles are fixed at insert and no patch carries them;
//   - required may be raised, never lowered;
//   - a patch never sets inject, isolate or group on a required row (the
//     part of that rule about bound keys and the distribution's required set
//     belongs to admission, which reads those lists);
//   - the config of a core:group row is its children, with composed ids
//     "<group>:<id>"; a core:include row mounts the rows of a file, resolved
//     against the directory of the file that holds the include row.
//
// A missing required file, an empty file, a composition that parses more than
// MaxComposeBytes or any rule violation is an *Error and composition stops. The
// rule "the last enabled provider of a bound key cannot be disabled" needs the
// distribution manifest and is not checked here.
func Compose(src Sources, opt Options) (Composition, []Warning, error) {
	c := &composer{opt: opt, index: map[string]int{}, unread: map[string]string{}}
	if !okIdent(src.Profile) {
		return Composition{}, nil, &Error{File: src.Profile, Rule: RuleSyntaxID, Msg: "profile name is not a name"}
	}
	profile := File{FS: src.Home, Path: path.Join("profiles", src.Profile, "profile.yaml")}
	data, err := readFile(profile)
	if err != nil {
		return Composition{}, nil, fileError(profile.Path, err)
	}
	bundles, min, err := parseProfile(profile.Path, data)
	if err != nil {
		return Composition{}, nil, err
	}
	c.min = min
	for _, name := range slices.Concat(bundles, src.Append) {
		// The bundle of an installed artifact, named by its plugin id, is read from the
		// home only (ADR-047 decision 5).
		f := File{FS: src.Bundles, Path: path.Join("bundles", name, "bundle.yaml")}
		if strings.Contains(name, "/") {
			f.FS = src.Home
		}
		if err := c.layer(f, 1, "bundle:"+name, false); err != nil {
			return Composition{}, nil, err
		}
	}
	optional := []File{
		{FS: src.Home, Path: path.Join("profiles", src.Profile, "aiops.patch.yml")},
		{FS: src.Home, Path: "aiops.patch.yml"},
	}
	if err := c.layer(optional[0], 2, "profile:"+src.Profile, true); err != nil {
		return Composition{}, nil, err
	}
	if err := c.layer(optional[1], 3, "site", true); err != nil {
		return Composition{}, nil, err
	}
	for _, f := range src.Patches {
		if err := c.layer(f, 4, "patch:"+f.Path, false); err != nil {
			return Composition{}, nil, err
		}
	}
	return c.finish(), c.warns, nil
}

// readFile reads a whole file of at most MaxFileBytes. A larger file is an
// *Error already.
func readFile(f File) ([]byte, error) {
	if f.FS == nil {
		return nil, &fs.PathError{Op: "open", Path: f.Path, Err: fs.ErrNotExist}
	}
	h, err := f.FS.Open(f.Path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = h.Close() }()
	data, err := io.ReadAll(io.LimitReader(h, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, &Error{File: f.Path, Rule: RuleLimit, Msg: "file is larger than " + strconv.Itoa(MaxFileBytes) + " bytes"}
	}
	return data, nil
}

// fileError classifies an error from readFile.
func fileError(file string, err error) error {
	var ce *Error
	switch {
	case errors.As(err, &ce):
		return ce
	case errors.Is(err, fs.ErrNotExist):
		return &Error{File: file, Rule: RuleMissing, Msg: "file does not exist", Err: err}
	}
	return &Error{File: file, Rule: RuleRead, Msg: err.Error(), Err: err}
}

type composer struct {
	opt     Options
	rows    []*crow
	index   map[string]int
	warns   []Warning
	read    int               // bytes of the layer files and of each include mount, against MaxComposeBytes
	min     string            // minStability of profile.yaml
	clashes []string          // the installed artifacts whose group id was taken (artifact)
	unread  map[string]string // the installed artifacts whose kept bundle does not read (layer)
}

// The ten fields in canonical order, as indexes of crow.origin.
const (
	iID = iota
	iPlugin
	iConfig
	iEnabled
	iRequired
	iRoles
	iInject
	iIntercept
	iIsolate
	iGroup
	numFields
)

type crow struct {
	row    Row
	origin [numFields]Origin
	icpt   map[string]Origin // per intercept key, the item that last added, changed or removed it
}

// interceptField names the provenance of one key of an intercept. A patch
// replaces the whole intercept, but only bundle layers may set the intercept of a
// provider-bound key (08-profiles-roadmap-adrs.md:148), so gate check 8 needs to
// know which keys a later layer added, changed or dropped. Provenance lists one
// such field per key a row's intercept has held, after the ten row fields.
func interceptField(key string) FieldName { return FieldIntercept + "." + FieldName(key) }

// layer parses one layer file and applies it. An absent optional file is
// skipped.
func (c *composer) layer(f File, number int, source string, optional bool) error {
	data, err := readFile(f)
	if optional && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var items []Item
	if err == nil {
		items, err = c.parseItems(f, data)
	} else {
		err = fileError(f.Path, err)
	}
	id, ok := strings.CutPrefix(source, "bundle:")
	installed := ok && strings.Contains(id, "/")
	switch {
	case installed && err != nil:
		// The kept bundle of an installed artifact that does not read inserts nothing, and
		// the gate denies the artifact: a composition error would stop every profile that
		// holds it (ADR-047 decisions 6 and 18).
		c.unread[id] = err.Error()
		return nil
	case err != nil:
		return err
	case installed:
		items = c.artifact(id, items)
	}
	l := Layer{Number: number, Source: source, File: f.Path, Items: items}
	for _, it := range l.Items {
		for _, r := range it.Insert {
			if err := c.insert(r, "", l, f, 0); err != nil {
				return err
			}
		}
		if it.Patch != nil {
			if err := c.patch(*it.Patch, l, it.Line); err != nil {
				return err
			}
		}
	}
	return nil
}

// artifact mounts the rows that the bundle of the installed artifact id inserts under one
// core:group row named by the <name> segment of id, enabled, with every role that one of
// them states, or none when one states none, so it mounts wherever a row does (ADR-047
// decision 5). The bundle places no row outside the group, so its other items change
// nothing, and gate check 8 denies the artifact for them. When a row composed before it
// holds the group's id, or the id is the loader's, it inserts nothing and records the
// clash (ADR-047 decision 6).
func (c *composer) artifact(id string, items []Item) []Item {
	_, name, _ := strings.Cut(id, "/")
	if _, clash := c.index[name]; clash || name == LoaderRowID {
		c.clashes = append(c.clashes, id)
		return nil
	}
	g := ParsedRow{Row: Row{ID: name, Plugin: pluginGroup, Enabled: true, Config: Value{Kind: KindList}}}
	every := false
	for _, it := range items {
		for _, r := range it.Insert {
			g.Children, every = append(g.Children, r), every || len(r.Roles) == 0
			for _, role := range r.Roles {
				if !slices.Contains(g.Roles, role) {
					g.Roles = append(g.Roles, role)
				}
			}
		}
	}
	if slices.Sort(g.Roles); every {
		g.Roles = nil
	}
	return []Item{{Insert: []ParsedRow{g}}}
}

// parseItems parses a layer file or a mount of an included one. Each counts
// against MaxComposeBytes, so include rows that name the same file again and
// again cannot make a composition cost more than its bound.
func (c *composer) parseItems(f File, data []byte) ([]Item, error) {
	if c.read += len(data); c.read > MaxComposeBytes {
		return nil, &Error{File: f.Path, Rule: RuleLimit,
			Msg: "the layer files and included files of the composition, counted at each include, hold more than " + strconv.Itoa(MaxComposeBytes) + " bytes"}
	}
	root, err := parseDocument(f.Path, data)
	if err != nil {
		return nil, err
	}
	p := &parser{file: f.Path, server: c.opt.Server}
	return p.layerItems(root)
}

// insert adds one row, its group children and its included rows.
func (c *composer) insert(pr ParsedRow, parent string, l Layer, f File, depth int) error {
	id := pr.ID
	if parent != "" {
		id = parent + ":" + id
	}
	fail := func(rule Rule, format string, a ...any) error {
		return &Error{File: f.Path, Line: pr.Line, Row: id, Rule: rule, Msg: fmt.Sprintf(format, a...)}
	}
	if _, ok := c.index[id]; ok {
		return fail(RuleDuplicateID, "insert adds a new id only, and %q exists", id)
	}
	if len(c.rows) >= MaxRows {
		return fail(RuleLimit, "more than %d rows", MaxRows)
	}
	o := Origin{Layer: l.Number, Source: l.Source, File: f.Path, Line: pr.Line}
	cr := &crow{row: pr.Row, icpt: map[string]Origin{}}
	cr.row.ID, cr.row.Parent = id, parent
	for i := range cr.origin {
		cr.origin[i] = o
	}
	for _, f := range pr.Intercept {
		cr.icpt[f.Key] = o
	}
	c.index[id] = len(c.rows)
	c.rows = append(c.rows, cr)
	for _, child := range pr.Children {
		if err := c.insert(child, id, l, f, depth); err != nil {
			return err
		}
	}
	if pr.Plugin == pluginInclude {
		return c.include(pr, id, l, f, depth)
	}
	return nil
}

// include mounts the rows of the file named by a core:include row.
func (c *composer) include(pr ParsedRow, id string, l Layer, f File, depth int) error {
	fail := func(rule Rule, format string, a ...any) error {
		return &Error{File: f.Path, Line: pr.Line, Row: id, Rule: rule, Msg: fmt.Sprintf(format, a...)}
	}
	rel := pr.Config.Map[0].Value.Str
	if !fs.ValidPath(rel) || rel == "." {
		return fail(RuleInclude, "include path %q must be a clean relative path with no \"..\"", rel)
	}
	if depth >= MaxIncludeDepth {
		return fail(RuleInclude, "includes nested deeper than %d", MaxIncludeDepth)
	}
	inc := File{FS: f.FS, Path: path.Join(path.Dir(f.Path), rel)}
	data, err := readFile(inc)
	if err != nil {
		var ce *Error
		if errors.As(err, &ce) {
			return ce
		}
		return &Error{File: f.Path, Line: pr.Line, Row: id, Rule: RuleInclude,
			Msg: fmt.Sprintf("cannot read included file %s: %v", inc.Path, err), Err: err}
	}
	items, err := c.parseItems(inc, data)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Patch != nil {
			return &Error{File: inc.Path, Line: it.Line, Row: it.Patch.ID, Rule: RuleInclude, Msg: "an included file may only insert rows"}
		}
		for _, r := range it.Insert {
			if err := c.insert(r, id, l, inc, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// patch applies one patch item.
func (c *composer) patch(pt Patch, l Layer, line int) error {
	i, ok := c.index[pt.ID]
	if !ok {
		c.warns = append(c.warns, Warning{Kind: WarnUnknownID, File: l.File, Line: line, Row: pt.ID,
			Msg: fmt.Sprintf("patch targets unknown id %q and is skipped", pt.ID)})
		return nil
	}
	cr := c.rows[i]
	fail := func(rule Rule, format string, a ...any) error {
		return &Error{File: l.File, Line: line, Row: pt.ID, Rule: rule, Msg: fmt.Sprintf(format, a...)}
	}
	o := Origin{Layer: l.Number, Source: l.Source, File: l.File, Line: line}
	r := &cr.row
	if cr.row.Required && (pt.Inject != nil || pt.Isolate != nil || pt.Group != nil) {
		return fail(RuleRequiredRow, "inject, isolate and group are never patched on a required row")
	}
	if pt.Required != nil && !*pt.Required && r.Required {
		return fail(RuleRequiredLower, "required may be raised, never lowered")
	}
	if pt.Config != nil {
		switch {
		case r.Plugin == pluginGroup || r.Plugin == pluginInclude:
			return fail(RuleGroup, "the config of %s is its children; patch the child rows by id", r.Plugin)
		case pt.Config.Kind != KindMap:
			return fail(RuleType, "config must be a mapping")
		}
		r.Config = *pt.Config
		cr.origin[iConfig] = o
	}
	if pt.Enabled != nil {
		r.Enabled, cr.origin[iEnabled] = *pt.Enabled, o
	}
	if pt.Required != nil {
		r.Required, cr.origin[iRequired] = *pt.Required, o
	}
	if pt.Inject != nil {
		r.Inject, cr.origin[iInject] = *pt.Inject, o
	}
	if pt.Intercept != nil {
		old, nw := fieldMap(Value{Kind: KindMap, Map: r.Intercept}), fieldMap(Value{Kind: KindMap, Map: *pt.Intercept})
		for k := range old {
			if _, kept := nw[k]; !kept {
				cr.icpt[k] = o
			}
		}
		for k, v := range nw {
			if ov, had := old[k]; !had || !equalValue(ov, v) {
				cr.icpt[k] = o
			}
		}
		r.Intercept, cr.origin[iIntercept] = *pt.Intercept, o
	}
	if pt.Isolate != nil {
		r.Isolate, cr.origin[iIsolate] = *pt.Isolate, o
	}
	if pt.Group != nil {
		r.Group, cr.origin[iGroup] = *pt.Group, o
	}
	return nil
}

// finish freezes the composition: rows in order, provenance, references and
// digest.
func (c *composer) finish() Composition {
	rows := make([]ComposedRow, len(c.rows))
	plain := make([]Row, len(c.rows))
	names := FieldNames()
	for i, cr := range c.rows {
		prov := Provenance{Fields: make([]FieldOrigin, numFields, numFields+len(cr.icpt))}
		for j, n := range names {
			prov.Fields[j] = FieldOrigin{Field: n, Origin: cr.origin[j]}
		}
		for _, k := range slices.Sorted(maps.Keys(cr.icpt)) {
			prov.Fields = append(prov.Fields, FieldOrigin{Field: interceptField(k), Origin: cr.icpt[k]})
		}
		refs := collectRefs(cr.row.Config, "config", nil) // the parser refuses one in an intercept
		rows[i] = ComposedRow{Row: cr.row, Provenance: prov, Refs: refs}
		plain[i] = cr.row
	}
	return Composition{Rows: rows, Digest: EncodeDigest(plain, c.opt.Modules, c.opt.Locks), MinStability: c.min, Clashes: c.clashes, Unread: c.unread}
}

// collectRefs appends the references of a value, in document order for lists
// and key order for maps. A path is dotted: "config.auth.secret",
// "config.allow[0]".
func collectRefs(v Value, p string, out []RefUse) []RefUse {
	switch v.Kind {
	case KindRef:
		out = append(out, RefUse{Path: p, Ref: v.Ref})
	case KindList:
		for i, x := range v.List {
			out = collectRefs(x, p+"["+strconv.Itoa(i)+"]", out)
		}
	case KindMap:
		fs := append([]Field(nil), v.Map...)
		sort.Slice(fs, func(i, j int) bool { return fs[i].Key < fs[j].Key })
		for _, f := range fs {
			out = collectRefs(f.Value, p+"."+f.Key, out)
		}
	}
	return out
}
