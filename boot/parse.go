package boot

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Bounds on the input. They keep a hostile or broken file from costing
// unbounded memory or time.
const (
	// MaxFileBytes is the largest layer, profile, include or matrix file.
	MaxFileBytes = 1 << 20
	// MaxDepth is the deepest nesting of YAML collections and group rows.
	MaxDepth = 32
	// MaxRows is the largest number of rows in one composition (a shipped profile has about 100).
	MaxRows = 20000
	// MaxIncludeDepth is the deepest nesting of core:include rows.
	MaxIncludeDepth = 4
	// MaxComposeBytes bounds the bytes one composition parses: its layer files,
	// and an included file at each of its mounts (a shipped profile parses a few
	// tens of KiB).
	MaxComposeBytes = 16 << 20
)

// How YAML scalars are typed. A plain scalar is typed by the YAML core rules
// (null, true/false, decimal integers, floats, else string) with these
// restrictions, because a type that surprises must fail loudly rather than
// change a value:
//
//   - quoted and block scalars are always strings;
//   - an integer must be decimal with no leading zero, no "_", no sign other
//     than "+" or "-", and fit int64: 0755, 0x1F, 0o7, 1_000 and 2^63 are
//     errors, so no octal or hex reading can happen;
//   - a float must use only digits, ".", "e", "E", "+" and "-" and be finite:
//     .inf and .nan are errors; a plain integer too big for int64 is an
//     error and is never read as a float;
//   - the YAML 1.1 words y, n, yes, no, on and off in any case are errors
//     when plain (quote them to get a string, or use true/false);
//   - a timestamp-looking scalar such as 2001-12-14 stays the string as
//     written;
//   - explicit tags, anchors, aliases and merge keys are errors.

type parser struct {
	file   string
	server bool
	// literal reads ${ as text: a manifest holds no reference (ADR-048 decision 3).
	literal bool
}

func (p *parser) err(n *yaml.Node, rule Rule, row, format string, a ...any) *Error {
	return &Error{File: p.file, Line: n.Line, Row: row, Rule: rule, Msg: fmt.Sprintf(format, a...)}
}

// parseDocument decodes exactly one YAML document and returns its root node.
func parseDocument(file string, data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	err := dec.Decode(&doc)
	if err == io.EOF {
		return nil, &Error{File: file, Rule: RuleEmpty, Msg: "file is empty; write [] to disable a layer"}
	}
	if err != nil {
		return nil, &Error{File: file, Line: yamlLine(err), Rule: RuleSyntax, Msg: err.Error(), Err: err}
	}
	var next yaml.Node
	if err := dec.Decode(&next); err != io.EOF {
		return nil, &Error{File: file, Line: next.Line, Rule: RuleDocument, Msg: "a file holds one YAML document"}
	}
	return doc.Content[0], nil
}

// yamlLine extracts the line from a message of the form "... line N: ...".
func yamlLine(err error) int {
	msg := err.Error()
	i := strings.Index(msg, "line ")
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range msg[i+len("line "):] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		if n > math.MaxInt32 {
			return 0
		}
	}
	return n
}

func (p *parser) guard(n *yaml.Node, row string) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return p.err(n, RuleAlias, row, "anchors and aliases are not accepted")
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return p.err(n, RuleTag, row, "explicit tags are not accepted")
	}
	return nil
}

type pair struct {
	key string
	k   *yaml.Node
	v   *yaml.Node
}

// pairs returns the entries of a mapping in document order, rejecting
// duplicate and non-scalar keys.
func (p *parser) pairs(n *yaml.Node, row, what string) ([]pair, error) {
	if err := p.guard(n, row); err != nil {
		return nil, err
	}
	if n.Kind != yaml.MappingNode {
		return nil, p.err(n, RuleType, row, "%s must be a mapping", what)
	}
	out := make([]pair, 0, len(n.Content)/2)
	seen := make(map[string]bool, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if err := p.guard(k, row); err != nil {
			return nil, err
		}
		if k.Kind != yaml.ScalarNode {
			return nil, p.err(k, RuleType, row, "a key of %s must be a scalar", what)
		}
		if k.ShortTag() == "!!merge" {
			return nil, p.err(k, RuleAlias, row, "merge keys are not accepted")
		}
		if !p.literal && strings.Contains(k.Value, "${") {
			return nil, p.err(k, RuleReference, row, "a reference cannot be a key")
		}
		if seen[k.Value] {
			return nil, p.err(k, RuleDuplicateKey, row, "key %q appears twice in %s", k.Value, what)
		}
		seen[k.Value] = true
		out = append(out, pair{key: k.Value, k: k, v: v})
	}
	return out, nil
}

// value converts a node to a Value, typing scalars by the rules above.
func (p *parser) value(n *yaml.Node, row string, depth int) (Value, error) {
	if depth > MaxDepth {
		return Value{}, p.err(n, RuleLimit, row, "nesting deeper than %d", MaxDepth)
	}
	if err := p.guard(n, row); err != nil {
		return Value{}, err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return p.scalar(n, row)
	case yaml.SequenceNode:
		list := make([]Value, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := p.value(c, row, depth+1)
			if err != nil {
				return Value{}, err
			}
			list = append(list, v)
		}
		return Value{Kind: KindList, List: list}, nil
	}
	prs, err := p.pairs(n, row, "a config mapping")
	if err != nil {
		return Value{}, err
	}
	fields := make([]Field, 0, len(prs))
	for _, pr := range prs {
		v, err := p.value(pr.v, row, depth+1)
		if err != nil {
			return Value{}, err
		}
		fields = append(fields, Field{Key: pr.key, Value: v})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	return Value{Kind: KindMap, Map: fields}, nil
}

func (p *parser) scalar(n *yaml.Node, row string) (Value, error) {
	const quoted = yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle
	text := n.Value
	if n.Style&quoted != 0 {
		return p.str(n, row)
	}
	switch n.ShortTag() {
	case "!!null":
		return Value{}, nil
	case "!!bool":
		return Value{Kind: KindBool, Bool: strings.EqualFold(text, "true")}, nil
	case "!!int":
		i, err := strconv.ParseInt(text, 10, 64)
		if err != nil || !decimal(text) {
			return Value{}, p.err(n, RuleType, row, "integer %q is not plain decimal int64; quote it to keep a string", text)
		}
		return Value{Kind: KindInt, Int: i}, nil
	case "!!float":
		f, err := strconv.ParseFloat(text, 64)
		if err != nil || strings.Trim(text, "0123456789.eE+-") != "" || !strings.ContainsAny(text, ".eE") {
			return Value{}, p.err(n, RuleType, row, "number %q is not a finite decimal float or an int64; quote it to keep a string", text)
		}
		if f == 0 {
			f = 0 // folds negative zero
		}
		return Value{Kind: KindFloat, Float: f}, nil
	case "!!timestamp":
		return Value{Kind: KindString, Str: text}, nil
	case "!!str":
		if _, err := strconv.ParseFloat(text, 64); errors.Is(err, strconv.ErrRange) {
			return Value{}, p.err(n, RuleType, row, "number %q is out of range; quote it to keep a string", text)
		}
		switch strings.ToLower(text) {
		case "y", "n", "yes", "no", "on", "off":
			return Value{}, p.err(n, RuleType, row, "plain %q is ambiguous between a string and a boolean; quote it or use true/false", text)
		}
		return p.str(n, row)
	}
	return Value{}, p.err(n, RuleType, row, "scalar %q has an unsupported type %s", text, n.ShortTag())
}

// decimal reports whether s is an optional sign and a decimal integer with no
// leading zero.
func decimal(s string) bool {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		s = s[1:]
	}
	if s == "0" {
		return true
	}
	if s == "" || s[0] == '0' {
		return false
	}
	return strings.Trim(s, "0123456789") == ""
}

// str builds a string Value, or a reference node when the string holds one.
func (p *parser) str(n *yaml.Node, row string) (Value, error) {
	if p.literal || !strings.Contains(n.Value, "${") {
		return Value{Kind: KindString, Str: n.Value}, nil
	}
	ref, ok := parseRef(n.Value)
	if !ok {
		return Value{}, p.err(n, RuleReference, row, "malformed reference %q: write the whole value as ${env|file|secret|flag:name}", n.Value)
	}
	return Value{Kind: KindRef, Ref: ref}, nil
}

// parseRef parses a whole string of the form ${scheme:name}. The syntax is
// ADR-059 decision 6 and 08-profiles-roadmap-adrs.md:162: env names are
// [A-Za-z_][A-Za-z0-9_]*, secret and flag names are [A-Za-z0-9][A-Za-z0-9._-]*,
// and a file path is any text without control characters. Text around the
// reference is not accepted.
func parseRef(s string) (Ref, bool) {
	if len(s) < 4 || !strings.HasPrefix(s, "${") || s[len(s)-1] != '}' {
		return Ref{}, false
	}
	inner := s[2 : len(s)-1]
	if strings.ContainsAny(inner, "${}") {
		return Ref{}, false
	}
	scheme, name, ok := strings.Cut(inner, ":")
	if !ok || name == "" {
		return Ref{}, false
	}
	switch RefScheme(scheme) {
	case RefEnv:
		ok = identifier(name)
	case RefSecret, RefFlag:
		ok = okIdent(name)
	case RefFile:
		ok = !strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f })
	default:
		ok = false
	}
	return Ref{Scheme: RefScheme(scheme), Name: name}, ok
}

func identifier(s string) bool {
	for i, c := range s {
		letter := c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return false
		}
	}
	return s != ""
}

// okIdent reports whether s is [A-Za-z0-9][A-Za-z0-9._-]*, at most 128 bytes.
func okIdent(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i, c := range s {
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && (i == 0 || c != '.' && c != '_' && c != '-') {
			return false
		}
	}
	return true
}

// okID reports whether s is a row id: one identifier, or with composed set a
// ":"-separated chain of them (the id of a child of a group).
func okID(s string, composed bool) bool {
	segs := strings.Split(s, ":")
	if len(segs) > 1 && !composed {
		return false
	}
	for _, seg := range segs {
		if !okIdent(seg) {
			return false
		}
	}
	return true
}

// okPlugin reports whether s is "<owner>/<name>", core:group or core:include.
func okPlugin(s string) bool {
	if s == pluginGroup || s == pluginInclude {
		return true
	}
	owner, name, ok := strings.Cut(s, "/")
	return ok && okIdent(owner) && okIdent(name)
}

const (
	pluginGroup   = "core:group"
	pluginInclude = "core:include"
)

// text reads a string scalar: quoted, or plain with the string type.
func (p *parser) text(n *yaml.Node, row, what string) (string, error) {
	if err := p.guard(n, row); err != nil {
		return "", err
	}
	const quoted = yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle
	if n.Kind != yaml.ScalarNode || n.Style&quoted == 0 && n.ShortTag() != "!!str" {
		return "", p.err(n, RuleType, row, "%s must be a string", what)
	}
	return n.Value, nil
}

func (p *parser) boolean(n *yaml.Node, row, what string) (bool, error) {
	if err := p.guard(n, row); err != nil {
		return false, err
	}
	if n.Kind != yaml.ScalarNode || n.Style != 0 || n.ShortTag() != "!!bool" {
		return false, p.err(n, RuleType, row, "%s must be true or false", what)
	}
	return strings.EqualFold(n.Value, "true"), nil
}

// names reads a list of identifiers as a sorted set.
func (p *parser) names(n *yaml.Node, row, what string) ([]string, error) {
	if err := p.guard(n, row); err != nil {
		return nil, err
	}
	if n.Kind != yaml.SequenceNode {
		return nil, p.err(n, RuleType, row, "%s must be a list", what)
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		s, err := p.text(c, row, what+" item")
		if err != nil {
			return nil, err
		}
		if !okIdent(s) {
			return nil, p.err(c, RuleSyntaxID, row, "%s item %q is not a name", what, s)
		}
		out = append(out, s)
	}
	sort.Strings(out)
	for i := 1; i < len(out); i++ {
		if out[i] == out[i-1] {
			return nil, p.err(n, RuleSyntaxID, row, "%s lists %q twice", what, out[i])
		}
	}
	return out, nil
}

// fieldBag holds the ten row fields as written; nil means absent.
type fieldBag struct {
	id        *string
	plugin    *string
	config    *yaml.Node
	enabled   *bool
	required  *bool
	roles     *[]Role
	inject    *[]string
	intercept *[]Field
	isolate   *[]string
	group     *bool
	pluginAt  *yaml.Node
	rolesAt   *yaml.Node
}

func (p *parser) bag(prs []pair, row string, depth int) (fieldBag, error) {
	var b fieldBag
	for _, pr := range prs {
		var err error
		switch pr.key {
		case "id":
			var s string
			if s, err = p.text(pr.v, row, "id"); err == nil {
				b.id = &s
			}
		case "plugin":
			var s string
			if s, err = p.text(pr.v, row, "plugin"); err == nil {
				b.plugin, b.pluginAt = &s, pr.k
			}
		case "config":
			b.config = pr.v
		case "enabled":
			var v bool
			if v, err = p.boolean(pr.v, row, "enabled"); err == nil {
				b.enabled = &v
			}
		case "required":
			var v bool
			if v, err = p.boolean(pr.v, row, "required"); err == nil {
				b.required = &v
			}
		case "group":
			var v bool
			if v, err = p.boolean(pr.v, row, "group"); err == nil {
				b.group = &v
			}
		case "roles":
			err = p.roles(&b, pr, row)
		case "inject":
			var v []string
			if v, err = p.names(pr.v, row, "inject"); err == nil {
				b.inject = &v
			}
		case "isolate":
			var v []string
			if v, err = p.names(pr.v, row, "isolate"); err == nil {
				b.isolate = &v
			}
		case "intercept":
			err = p.intercept(&b, pr.v, row, depth)
		default:
			err = p.err(pr.k, RuleUnknownField, row, "unknown field %q", pr.key)
		}
		if err != nil {
			return b, err
		}
	}
	return b, nil
}

func (p *parser) roles(b *fieldBag, pr pair, row string) error {
	names, err := p.names(pr.v, row, "roles")
	if err != nil {
		return err
	}
	roles := make([]Role, len(names))
	for i, s := range names {
		switch Role(s) {
		case RoleEdge, RoleAPI, RoleWorker:
			roles[i] = Role(s)
		default:
			return p.err(pr.v, RuleRoles, row, "role %q is not one of edge, api, worker", s)
		}
	}
	b.roles, b.rolesAt = &roles, pr.k
	return nil
}

func (p *parser) intercept(b *fieldBag, n *yaml.Node, row string, depth int) error {
	prs, err := p.pairs(n, row, "intercept")
	if err != nil {
		return err
	}
	fields := make([]Field, 0, len(prs))
	for _, pr := range prs {
		if !okIdent(pr.key) {
			return p.err(pr.k, RuleSyntaxID, row, "intercept key %q is not a name", pr.key)
		}
		v, err := p.value(pr.v, row, depth+1)
		if err != nil {
			return err
		}
		// config-refs resolves references through internal/config, so in the config
		// of a row only (03-c3-kernel.md:68); one in an intercept would reach the
		// provider unresolved.
		if refs := collectRefs(v, "intercept."+pr.key, nil); len(refs) > 0 {
			return p.err(pr.v, RuleReference, row, "%s holds a reference: references are resolved in config only", refs[0].Path)
		}
		fields = append(fields, Field{Key: pr.key, Value: v})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Key < fields[j].Key })
	b.intercept = &fields
	return nil
}

// label returns the id written in a mapping, for error messages.
func label(prs []pair) string {
	for _, pr := range prs {
		if pr.key == "id" && pr.v.Kind == yaml.ScalarNode {
			return pr.v.Value
		}
	}
	return ""
}

// insertRow parses one row of an insert list or of a group.
func (p *parser) insertRow(n *yaml.Node, depth int) (ParsedRow, error) {
	if depth > MaxDepth {
		return ParsedRow{}, p.err(n, RuleLimit, "", "nesting deeper than %d", MaxDepth)
	}
	prs, err := p.pairs(n, "", "a row")
	if err != nil {
		return ParsedRow{}, err
	}
	row := label(prs)
	b, err := p.bag(prs, row, depth)
	if err != nil {
		return ParsedRow{}, err
	}
	if b.id == nil || b.plugin == nil {
		return ParsedRow{}, p.err(n, RuleType, row, "a row states id and plugin")
	}
	if !okID(*b.id, false) {
		return ParsedRow{}, p.err(n, RuleSyntaxID, row, "id %q must be one name, with no \":\"", *b.id)
	}
	if !okPlugin(*b.plugin) {
		return ParsedRow{}, p.err(b.pluginAt, RuleSyntaxID, row, "plugin %q is not <owner>/<name>, core:group or core:include", *b.plugin)
	}
	r := ParsedRow{Line: n.Line}
	r.ID, r.Plugin, r.Enabled = *b.id, *b.plugin, true
	if b.enabled != nil {
		r.Enabled = *b.enabled
	}
	if b.required != nil {
		r.Required = *b.required
	}
	if b.group != nil {
		r.Group = *b.group
	}
	if b.roles != nil {
		r.Roles = *b.roles
	}
	if p.server && len(r.Roles) == 0 {
		return ParsedRow{}, p.err(n, RuleRoles, row, "a row of a server bundle states roles (edge, api, worker)")
	}
	if b.inject != nil {
		r.Inject = *b.inject
	}
	if b.intercept != nil {
		r.Intercept = *b.intercept
	}
	if b.isolate != nil {
		r.Isolate = *b.isolate
	}
	if err := p.config(&r, b.config, n, depth); err != nil {
		return ParsedRow{}, err
	}
	return r, nil
}

// config reads the config of an inserted row: a list of child rows for
// core:group, a {path} mapping for core:include, a mapping otherwise.
func (p *parser) config(r *ParsedRow, c *yaml.Node, at *yaml.Node, depth int) error {
	if c == nil {
		if r.Plugin == pluginInclude {
			return p.err(at, RuleGroup, r.ID, "core:include needs config {path: <file>}")
		}
		c = &yaml.Node{Kind: yaml.MappingNode, Line: at.Line}
		if r.Plugin == pluginGroup {
			c.Kind = yaml.SequenceNode
		}
	}
	switch r.Plugin {
	case pluginGroup:
		if err := p.guard(c, r.ID); err != nil {
			return err
		}
		if c.Kind != yaml.SequenceNode {
			return p.err(c, RuleGroup, r.ID, "the config of core:group is the list of its child rows")
		}
		for _, cn := range c.Content {
			child, err := p.insertRow(cn, depth+1)
			if err != nil {
				return err
			}
			r.Children = append(r.Children, child)
		}
		r.Config = Value{Kind: KindList}
		return nil
	case pluginInclude:
		prs, err := p.pairs(c, r.ID, "the config of core:include")
		if err != nil {
			return err
		}
		if len(prs) != 1 || prs[0].key != "path" {
			return p.err(c, RuleGroup, r.ID, "the config of core:include is exactly {path: <file>}")
		}
		path, err := p.text(prs[0].v, r.ID, "path")
		if err != nil {
			return err
		}
		if path == "" {
			return p.err(prs[0].v, RuleGroup, r.ID, "include path is empty")
		}
		r.Config = Value{Kind: KindMap, Map: []Field{{Key: "path", Value: Value{Kind: KindString, Str: path}}}}
		return nil
	}
	v, err := p.value(c, r.ID, depth+1)
	if err != nil {
		return err
	}
	if v.Kind != KindMap {
		return p.err(c, RuleType, r.ID, "config must be a mapping")
	}
	r.Config = v
	return nil
}

// layerItems parses the root of a layer file: a list of patch and insert
// items (07-c4-plugin-sdk.md:1229).
func (p *parser) layerItems(root *yaml.Node) ([]Item, error) {
	if err := p.guard(root, ""); err != nil {
		return nil, err
	}
	if root.Kind != yaml.SequenceNode {
		return nil, p.err(root, RuleType, "", "a layer file is a list; write [] to disable a layer")
	}
	items := make([]Item, 0, len(root.Content))
	for _, n := range root.Content {
		it, err := p.item(n)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

func (p *parser) item(n *yaml.Node) (Item, error) {
	prs, err := p.pairs(n, "", "a layer item")
	if err != nil {
		return Item{}, err
	}
	for _, pr := range prs {
		if pr.key != "insert" {
			continue
		}
		if len(prs) != 1 {
			return Item{}, p.err(n, RuleUnknownField, "", "an insert item holds only insert")
		}
		if err := p.guard(pr.v, ""); err != nil {
			return Item{}, err
		}
		if pr.v.Kind != yaml.SequenceNode {
			return Item{}, p.err(pr.v, RuleType, "", "insert must be a list of rows")
		}
		it := Item{Line: n.Line}
		for _, rn := range pr.v.Content {
			r, err := p.insertRow(rn, 1)
			if err != nil {
				return Item{}, err
			}
			it.Insert = append(it.Insert, r)
		}
		return it, nil
	}
	row := label(prs)
	b, err := p.bag(prs, row, 0)
	if err != nil {
		return Item{}, err
	}
	switch {
	case b.id == nil:
		return Item{}, p.err(n, RuleType, "", "an item states id (a patch) or insert")
	case b.plugin != nil:
		return Item{}, p.err(b.pluginAt, RuleFixedField, row, "plugin is fixed at insert; disable this row and enable another to swap")
	case b.roles != nil:
		return Item{}, p.err(b.rolesAt, RuleFixedField, row, "roles are fixed at insert")
	case !okID(*b.id, true):
		return Item{}, p.err(n, RuleSyntaxID, row, "id %q is not a row id", *b.id)
	}
	pt := Patch{ID: *b.id, Enabled: b.enabled, Required: b.required, Inject: b.inject,
		Intercept: b.intercept, Isolate: b.isolate, Group: b.group}
	if b.config != nil {
		v, err := p.value(b.config, row, 1)
		if err != nil {
			return Item{}, err
		}
		pt.Config = &v
	}
	return Item{Line: n.Line, Patch: &pt}, nil
}

// ParseConfig reads data, the content of file, as the config of one row under the
// rules of a layer (ADR-059 decision 6): one YAML document, a mapping, with the
// layer parser's typing of plain scalars, its refusal of anchors, aliases, tags,
// merge keys and duplicate keys, its references and its bounds. It returns the tree
// the launcher hands the row (launch.Row.Config): nil, bool, int64, float64,
// string, launch.Ref, []any and map[string]any. plugingen reads the row config of a
// generated cycle test with it.
func ParseConfig(file string, data []byte) (any, error) {
	if len(data) > MaxFileBytes {
		return nil, &Error{File: file, Rule: RuleLimit, Msg: "a config is larger than " + strconv.Itoa(MaxFileBytes) + " bytes"}
	}
	root, err := parseDocument(file, data)
	if err != nil {
		return nil, err
	}
	p := &parser{file: file}
	v, err := p.value(root, "", 1)
	if err != nil {
		return nil, err
	}
	if v.Kind != KindMap {
		return nil, p.err(root, RuleType, "", "config must be a mapping")
	}
	return plain(v), nil
}

// parseProfile reads profile.yaml: {bundles: [name, ...]} in bundle order.
func parseProfile(file string, data []byte) ([]string, error) {
	root, err := parseDocument(file, data)
	if err != nil {
		return nil, err
	}
	p := &parser{file: file}
	prs, err := p.pairs(root, "", "profile.yaml")
	if err != nil {
		return nil, err
	}
	var bundles []string
	found := false
	for _, pr := range prs {
		if pr.key != "bundles" {
			return nil, p.err(pr.k, RuleUnknownField, "", "unknown field %q in profile.yaml", pr.key)
		}
		if err := p.guard(pr.v, ""); err != nil {
			return nil, err
		}
		if pr.v.Kind != yaml.SequenceNode {
			return nil, p.err(pr.v, RuleType, "", "bundles must be a list")
		}
		found = true
		seen := map[string]bool{}
		for _, c := range pr.v.Content {
			s, err := p.text(c, "", "bundle name")
			if err != nil {
				return nil, err
			}
			owner, name, two := strings.Cut(s, "/")
			if !okIdent(owner) || two && !okIdent(name) {
				return nil, p.err(c, RuleSyntaxID, "", "bundle name %q is not a name", s)
			}
			if seen[s] {
				return nil, p.err(c, RuleSyntaxID, "", "bundle %q is listed twice", s)
			}
			seen[s] = true
			bundles = append(bundles, s)
		}
	}
	if !found {
		return nil, p.err(root, RuleType, "", "profile.yaml states bundles")
	}
	return bundles, nil
}
