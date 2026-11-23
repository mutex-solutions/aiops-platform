package boot

import (
	"cmp"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The reader of plugin manifests, schema 1.x, plugin.yaml (ADR-048). boot may not
// import the plugin package (08-profiles-roadmap-adrs.md:1254), so it reads into
// types of its own, and a test of internal/plugingen holds them equal to
// plugin.Manifest, field by field and on every manifest of the repository (ADR-060
// decision 14). One reader has two modes (ADR-048 decision 15): the gate reads with
// ManifestLenient; plugin check, plugin check --write and the parity check read with
// ManifestStrict.
//
// Both modes hold every rule of ADR-048 decision 3: the YAML rules of the layer
// parser, except that ${ is text, since a manifest holds no reference; one type per
// field, and only its values for a closed set; every required field present, and an
// empty optional string read as absent; every set written sorted with no entry twice;
// and the rules between fields, those of the reserved owners aiops and contrib
// included (ADR-048 decision 14). A manifest that breaks one is malformed, and gate
// check 1 denies it (ADR-048 decision 24).

// ManifestMode says what ReadManifest does with a field that the schema does not name.
type ManifestMode uint8

const (
	// ManifestLenient is the reading of the gate: an unknown field makes the manifest
	// malformed at a minor the reader knows and is ignored, at any depth, at a higher
	// minor (07-c4-plugin-sdk.md:120; ADR-048 decision 24).
	ManifestLenient ManifestMode = iota
	// ManifestStrict is the reading of plugin check, plugin check --write and the
	// parity check: an unknown field fails at any minor and any depth
	// (07-c4-plugin-sdk.md:1567; ADR-048 decision 15).
	ManifestStrict
)

// Manifest is a plugin manifest as read: each field is the field of schema 1.0 with
// that name (ADR-048 decisions 4 to 13). A list or a mapping that is absent is nil,
// and a set is sorted. ConfigSchema is the parsed tree of the schema, integers as
// int64.
type Manifest struct {
	ManifestVersion string // "1.<minor>"
	ID              string // "<owner>/<name>"
	Version         string // Semantic Versioning 2.0.0
	Title           string
	Description     string
	License         string // an SPDX expression
	Publisher       ManifestPublisher
	Source          string
	Support         *ManifestSupport
	Stability       string   // experimental, preview or ga
	Scope           []string // a set of process and tenant
	Runtimes        []string // a set of inproc, remote and wasm; empty for a pack with no code
	Capabilities    *ManifestCapabilities
	Integrates      []ManifestIntegration
	Instantiable    bool
	// The generated half. A pack with no code carries Requires alone.
	Uses         []ManifestUse
	Provides     []ManifestKey
	Registers    []ManifestKey
	Hooks        []ManifestHook
	ConfigSchema map[string]any
	Durable      []any // empty until S3 fixes the shape of an entry
	Requires     ManifestRequires
}

// ManifestPublisher is publisher: ID equals the owner segment of the id.
type ManifestPublisher struct{ ID, Name, URL string }

// ManifestSupport is support, with at least one of the two set.
type ManifestSupport struct{ URL, Email string }

// ManifestCapabilities is capabilities. A Network entry is a host name, optionally
// with :<port>, or config: and a JSON Pointer (RFC 6901) into the config.
type ManifestCapabilities struct{ Network, Secrets []string }

// ManifestIntegration is one entry of integrates (ADR-048 decision 5).
type ManifestIntegration struct {
	System, Versions string
	Tested           []string
	Credential       string // read-only or broader
	License          string
}

// ManifestUse is one entry of uses: As is need or want.
type ManifestUse struct {
	Key   string
	Major int
	As    string
}

// ManifestKey is one entry of provides or registers.
type ManifestKey struct {
	Key   string
	Major int
}

// ManifestHook is one entry of hooks: a hook and the name of a stage.
type ManifestHook struct{ Hook, Stage string }

// ManifestRequires is requires (ADR-048 decision 12). Definitions maps a definition
// name to the "<major>.<minor>" it needs, one per major, sorted by major.
type ManifestRequires struct {
	PluginAPI      string
	RemoteProtocol []int
	Definitions    map[string][]string
}

// manifestFields returns the fields of schema 1.0 in order: the hand-written half,
// then the seven generated fields with requires last (ADR-048 decision 1).
func manifestFields() []string {
	return []string{"manifestVersion", "id", "version", "title", "description", "license",
		"publisher", "source", "support", "stability", "scope", "runtimes", "capabilities", "integrates",
		"instantiable", "uses", "provides", "registers", "hooks", "configSchema", "durable", "requires"}
}

const jsonSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

// ReadManifest reads the plugin manifest data, the content of file, in mode. Both
// modes fail on an absent manifestVersion and on a major other than 1
// (08-profiles-roadmap-adrs.md:497), and on every rule of the schema.
func ReadManifest(file string, data []byte, mode ManifestMode) (Manifest, error) {
	if len(data) > MaxFileBytes {
		return Manifest{}, &Error{File: file, Rule: RuleLimit, Msg: "a manifest is larger than " + strconv.Itoa(MaxFileBytes) + " bytes"}
	}
	root, err := parseDocument(file, data)
	if err != nil {
		return Manifest{}, err
	}
	// manifestVersion says whether an unknown field is ignored, so it is read first.
	r := &mreader{parser: parser{file: file, literal: true}, ignore: true}
	var m Manifest
	vn := r.fields(root, "a manifest", []string{"manifestVersion"}, "manifestVersion")["manifestVersion"]
	m.ManifestVersion = r.str(vn, "manifestVersion")
	major, minor, ok := majorMinor(m.ManifestVersion)
	if !ok {
		r.fail(vn, "manifestVersion %q is not \"<major>.<minor>\"", m.ManifestVersion)
	} else if major != 1 {
		r.fail(vn, "manifest major %d is unknown: this reader knows major 1", major)
	}
	r.ignore = mode == ManifestLenient && minor > 0
	f := r.fields(root, "a manifest", manifestFields(),
		"id", "version", "title", "license", "publisher", "stability", "scope", "runtimes", "requires")

	m.ID = r.word(f["id"], "id", okManifestID)
	owner, _, _ := strings.Cut(m.ID, "/")
	m.Version = r.word(f["version"], "version", okSemver)
	m.Title = r.word(f["title"], "title", func(s string) bool { return s != "" && !strings.ContainsAny(s, "\r\n") })
	m.Description = r.str(f["description"], "description")
	m.License = r.word(f["license"], "license", okSPDX)
	if _, ref := spdx(m.License); ref && (owner == "aiops" || owner == "contrib") {
		r.fail(f["license"], "license: under the reserved owner %s it names SPDX list ids only, no LicenseRef- or DocumentRef- id", owner)
	}
	pub := r.fields(f["publisher"], "publisher", []string{"id", "name", "url"}, "id")
	m.Publisher = ManifestPublisher{ID: r.str(pub["id"], "publisher.id"), Name: r.str(pub["name"], "publisher.name"),
		URL: r.str(pub["url"], "publisher.url")}
	if m.Publisher.ID != owner {
		r.fail(pub["id"], "publisher.id %q is not %q, the owner segment of the id", m.Publisher.ID, owner)
	}
	m.Source = r.word(f["source"], "source", func(s string) bool {
		u, err := url.Parse(s)
		return s == "" || err == nil && u.Scheme != "" && u.Host != ""
	})
	if n := f["support"]; n != nil {
		sf := r.fields(n, "support", []string{"url", "email"})
		m.Support = &ManifestSupport{URL: r.str(sf["url"], "support.url"), Email: r.str(sf["email"], "support.email")}
		if *m.Support == (ManifestSupport{}) {
			r.fail(n, "support states url or email")
		}
	}
	m.Stability = r.word(f["stability"], "stability", oneOf("experimental", "preview", "ga"))
	if m.Scope = r.set(f["scope"], "scope", oneOf("process", "tenant")); len(m.Scope) == 0 {
		r.fail(f["scope"], "scope holds process, tenant or both")
	}
	m.Runtimes = r.set(f["runtimes"], "runtimes", oneOf("inproc", "remote", "wasm"))
	if n := f["capabilities"]; n != nil {
		cf := r.fields(n, "capabilities", []string{"network", "secrets"})
		m.Capabilities = &ManifestCapabilities{Network: r.set(cf["network"], "capabilities.network", okNetwork),
			Secrets: r.set(cf["secrets"], "capabilities.secrets", okIdent)}
	}
	if n := f["integrates"]; n != nil {
		m.Integrates = []ManifestIntegration{}
		for _, c := range r.list(n, "integrates") {
			e := r.integration(c, owner)
			if slices.ContainsFunc(m.Integrates, func(x ManifestIntegration) bool { return x.System == e.System }) {
				r.fail(c, "integrates lists the system %q twice", e.System)
			}
			m.Integrates = append(m.Integrates, e)
		}
	}
	m.Instantiable = r.flag(f["instantiable"], "instantiable")

	// A pack with no code carries requires alone; code carries all seven (ADR-048
	// decision 13).
	pack := len(m.Runtimes) == 0
	for _, g := range manifestFields()[15:21] {
		if pack && f[g] != nil {
			r.fail(f[g], "a pack with no code carries no %s", g)
		} else if !pack && f[g] == nil {
			r.fail(root, "a manifest of code states %s", g)
		}
	}
	m.Uses = entries(r, f["uses"], "uses", []string{"key", "major", "as"}, func(e map[string]*yaml.Node) ManifestUse {
		return ManifestUse{Key: r.word(e["key"], "uses key", okIdent), Major: r.num(e["major"], "uses major"),
			As: r.word(e["as"], "uses as", oneOf("need", "want"))}
	}, func(a, b ManifestUse) int { return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.Major, b.Major)) })
	m.Provides = r.keys(f["provides"], "provides")
	m.Registers = r.keys(f["registers"], "registers")
	m.Hooks = entries(r, f["hooks"], "hooks", []string{"hook", "stage"}, func(e map[string]*yaml.Node) ManifestHook {
		return ManifestHook{Hook: r.word(e["hook"], "hooks hook", okHook), Stage: r.word(e["stage"], "hooks stage", okIdent)}
	}, func(a, b ManifestHook) int { return cmp.Or(cmp.Compare(a.Hook, b.Hook), cmp.Compare(a.Stage, b.Stage)) })
	if n := f["configSchema"]; n != nil {
		m.ConfigSchema = r.schema(n)
	}
	if n := f["durable"]; n != nil {
		m.Durable = []any{}
		if len(r.list(n, "durable")) > 0 {
			r.fail(n, "durable holds no entry until S3 fixes its shape (ADR-048 decision 11)")
		}
	}
	m.Requires = r.requires(f["requires"], m.Runtimes)

	if m.Instantiable && (owner != "aiops" || len(m.Registers) == 0 || len(m.Provides)+len(m.Hooks)+len(m.Durable) > 0) {
		r.fail(f["instantiable"], "instantiable: true marks a plugin of the owner aiops that registers contributions and nothing else")
	}
	if m.Capabilities != nil && !pack {
		for _, e := range m.Capabilities.Network {
			if p, ok := strings.CutPrefix(e, "config:"); ok && !schemaHas(m.ConfigSchema, p) {
				r.fail(f["capabilities"], "capabilities.network %q names no property of configSchema", e)
			}
		}
	}
	if r.err != nil {
		return Manifest{}, r.err
	}
	return m, nil
}

// Entry returns the module entry that m declares (ADR-048 decision 18): Provides from
// provides, Needs and Wants from uses by as, Hooks from hooks, one per hook and stage,
// and Volatile, the path of each subschema of configSchema marked x-aiops-volatile,
// written from the row as "config.<key>" with "*" for a list item or a map value
// (ADR-048 decision 9). The ordinal of a stage is served data, which boot reads with
// the signed distribution manifest, so Stage is 0 until then. Module, Version, Sum
// and Plugin are the module table's.
func (m Manifest) Entry() ModuleEntry {
	e := ModuleEntry{ID: m.ID}
	for _, k := range m.Provides {
		e.Provides = append(e.Provides, k.Key)
	}
	for _, u := range m.Uses {
		if u.As == "want" {
			e.Wants = append(e.Wants, u.Key)
		} else {
			e.Needs = append(e.Needs, u.Key)
		}
	}
	for _, h := range m.Hooks {
		e.Hooks = append(e.Hooks, HookDecl{Hook: h.Hook, StageName: h.Stage})
	}
	e.Volatile = volatileOf(m.ConfigSchema, "config")
	slices.Sort(e.Volatile)
	return e
}

// volatileOf lists the path of s, when it is marked volatile, and of each subschema
// below it that is.
func volatileOf(s map[string]any, path string) []string {
	var out []string
	if s["x-aiops-volatile"] == true {
		out = append(out, path)
	}
	props, _ := s["properties"].(map[string]any)
	for k, sub := range props {
		sub, _ := sub.(map[string]any)
		out = append(out, volatileOf(sub, path+"."+k)...)
	}
	for _, kw := range []string{"items", "additionalProperties"} {
		if sub, ok := s[kw].(map[string]any); ok {
			out = append(out, volatileOf(sub, path+".*")...)
		}
	}
	return out
}

// mreader reads one manifest. It keeps the first error, after which every read is a
// no-op that returns a zero value.
type mreader struct {
	parser
	ignore bool // an unknown field is ignored: the lenient mode at a higher minor
	err    error
}

func (r *mreader) keep(err error) bool {
	if r.err == nil {
		r.err = err
	}
	return r.err == nil
}

func (r *mreader) fail(n *yaml.Node, format string, a ...any) {
	if r.err == nil {
		r.err = r.parser.err(n, RuleManifest, "", format, a...)
	}
}

// fields returns the values of the mapping n by key. A key that is not in known
// fails unless r.ignore, and a key of required that is absent fails.
func (r *mreader) fields(n *yaml.Node, what string, known []string, required ...string) map[string]*yaml.Node {
	if n == nil || r.err != nil {
		return nil
	}
	prs, err := r.pairs(n, "", what)
	if !r.keep(err) {
		return nil
	}
	out := make(map[string]*yaml.Node, len(prs))
	for _, pr := range prs {
		if slices.Contains(known, pr.key) {
			out[pr.key] = pr.v
		} else if !r.ignore {
			r.keep(r.parser.err(pr.k, RuleUnknownField, "", "unknown field %q in %s", pr.key, what))
		}
	}
	for _, k := range required {
		if out[k] == nil {
			r.fail(n, "%s states %s", what, k)
		}
	}
	return out
}

func (r *mreader) str(n *yaml.Node, what string) string {
	if n == nil || r.err != nil {
		return ""
	}
	s, err := r.text(n, "", what)
	r.keep(err)
	return s
}

// word reads a string that satisfies valid, or "" when n is absent.
func (r *mreader) word(n *yaml.Node, what string, valid func(string) bool) string {
	s := r.str(n, what)
	if r.err == nil && n != nil && !valid(s) {
		r.fail(n, "%s %q is not valid", what, s)
	}
	return s
}

func (r *mreader) flag(n *yaml.Node, what string) bool {
	if n == nil || r.err != nil {
		return false
	}
	b, err := r.boolean(n, "", what)
	r.keep(err)
	return b
}

// num reads a non-negative integer that fits an int32.
func (r *mreader) num(n *yaml.Node, what string) int {
	if n == nil || r.err != nil {
		return 0
	}
	v, err := r.value(n, "", 0)
	if r.keep(err) && (v.Kind != KindInt || v.Int < 0 || v.Int > math.MaxInt32) {
		r.keep(r.parser.err(n, RuleType, "", "%s must be a non-negative integer", what))
	}
	return int(v.Int)
}

// list returns the entries of the sequence n, which is present.
func (r *mreader) list(n *yaml.Node, what string) []*yaml.Node {
	if r.err == nil && r.keep(r.guard(n, "")) && n.Kind != yaml.SequenceNode {
		r.keep(r.parser.err(n, RuleType, "", "%s must be a list", what))
	}
	if r.err != nil {
		return nil
	}
	return n.Content
}

// sorted fails unless each of the count entries follows the one before it, as less
// says: a set is written sorted and holds no entry twice.
func (r *mreader) sorted(n *yaml.Node, what string, count int, less func(i int) bool) {
	for i := 1; i < count; i++ {
		if !less(i) {
			r.fail(n, "%s is a set: each entry once, sorted", what)
		}
	}
}

// set reads a set of strings that satisfy valid, or nil when n is absent.
func (r *mreader) set(n *yaml.Node, what string, valid func(string) bool) []string {
	if n == nil {
		return nil
	}
	out := []string{}
	for _, c := range r.list(n, what) {
		out = append(out, r.word(c, what+" entry", valid))
	}
	r.sorted(n, what, len(out), func(i int) bool { return out[i-1] < out[i] })
	return out
}

// entries reads a set of mappings whose fields are the known ones, all required,
// sorted by order; nil when n is absent.
func entries[T any](r *mreader, n *yaml.Node, what string, known []string, read func(map[string]*yaml.Node) T, order func(a, b T) int) []T {
	if n == nil {
		return nil
	}
	out := []T{}
	for _, c := range r.list(n, what) {
		out = append(out, read(r.fields(c, "an entry of "+what, known, known...)))
	}
	r.sorted(n, what, len(out), func(i int) bool { return order(out[i-1], out[i]) < 0 })
	return out
}

func (r *mreader) keys(n *yaml.Node, what string) []ManifestKey {
	return entries(r, n, what, []string{"key", "major"}, func(e map[string]*yaml.Node) ManifestKey {
		return ManifestKey{Key: r.word(e["key"], what+" key", okIdent), Major: r.num(e["major"], what+" major")}
	}, func(a, b ManifestKey) int { return cmp.Or(cmp.Compare(a.Key, b.Key), cmp.Compare(a.Major, b.Major)) })
}

func (r *mreader) integration(n *yaml.Node, owner string) ManifestIntegration {
	f := r.fields(n, "an entry of integrates", []string{"system", "versions", "tested", "credential", "license"},
		"system", "versions", "tested", "credential")
	e := ManifestIntegration{System: r.word(f["system"], "integrates system", okKebab),
		Versions:   r.word(f["versions"], "integrates versions", func(s string) bool { return s != "" }),
		Tested:     r.set(f["tested"], "integrates tested", func(s string) bool { return s != "" }),
		Credential: r.word(f["credential"], "integrates credential", oneOf("read-only", "broader")),
		License:    r.word(f["license"], "integrates license", func(s string) bool { return s == "" || okSPDX(s) })}
	if len(e.Tested) == 0 {
		r.fail(n, "an entry of integrates names at least one tested version")
	}
	if owner == "contrib" && e.Credential == "broader" {
		r.fail(f["credential"], "the owner contrib declares no credential: broader")
	}
	return e
}

// schema reads configSchema: a mapping that is empty, the schema every value
// satisfies, or names the dialect of JSON Schema 2020-12 (ADR-048 decision 9).
func (r *mreader) schema(n *yaml.Node) map[string]any {
	if r.err != nil {
		return nil
	}
	v, err := r.value(n, "", 1)
	if !r.keep(err) {
		return nil
	}
	s, ok := plain(v).(map[string]any)
	if !ok {
		r.fail(n, "configSchema must be a mapping")
	} else if len(s) > 0 && s["$schema"] != jsonSchemaDialect {
		r.fail(n, "configSchema names $schema %s", jsonSchemaDialect)
	}
	return s
}

// requires reads requires: pluginApi exactly when runtimes holds inproc,
// remoteProtocol exactly when it holds remote (ADR-048 decision 12).
func (r *mreader) requires(n *yaml.Node, runtimes []string) ManifestRequires {
	f := r.fields(n, "requires", []string{"pluginApi", "remoteProtocol", "definitions"}, "definitions")
	q := ManifestRequires{PluginAPI: r.word(f["pluginApi"], "requires.pluginApi", func(s string) bool {
		_, _, ok := majorMinor(s)
		return ok || s == ""
	})}
	if pn := f["remoteProtocol"]; pn != nil {
		q.RemoteProtocol = []int{}
		for _, c := range r.list(pn, "requires.remoteProtocol") {
			q.RemoteProtocol = append(q.RemoteProtocol, r.num(c, "requires.remoteProtocol entry"))
		}
		r.sorted(pn, "requires.remoteProtocol", len(q.RemoteProtocol), func(i int) bool { return q.RemoteProtocol[i-1] < q.RemoteProtocol[i] })
	}
	q.Definitions = r.definitions(f["definitions"])
	if (q.PluginAPI != "") != slices.Contains(runtimes, "inproc") {
		r.fail(n, "requires states pluginApi exactly when runtimes holds inproc")
	}
	if (len(q.RemoteProtocol) > 0) != slices.Contains(runtimes, "remote") {
		r.fail(n, "requires states remoteProtocol exactly when runtimes holds remote")
	}
	return q
}

// definitions reads requires.definitions: its keys are data, definition names.
func (r *mreader) definitions(n *yaml.Node) map[string][]string {
	if n == nil || r.err != nil {
		return nil
	}
	prs, err := r.pairs(n, "", "requires.definitions")
	if !r.keep(err) {
		return nil
	}
	out := make(map[string][]string, len(prs))
	for _, pr := range prs {
		what := "requires.definitions " + pr.key
		if !okKebab(pr.key) {
			r.fail(pr.k, "requires.definitions names %q, which is not a definition name", pr.key)
		}
		vs, majors := []string{}, []int{}
		for _, c := range r.list(pr.v, what) {
			s := r.word(c, what+" version", func(s string) bool { _, _, ok := majorMinor(s); return ok })
			major, _, _ := majorMinor(s)
			vs, majors = append(vs, s), append(majors, major)
		}
		r.sorted(pr.v, what, len(majors), func(i int) bool { return majors[i-1] < majors[i] })
		if len(vs) == 0 {
			r.fail(pr.v, "%s names no version", what)
		}
		out[pr.key] = vs
	}
	return out
}

func oneOf(values ...string) func(string) bool {
	return func(s string) bool { return slices.Contains(values, s) }
}

// majorMinor reads "<major>.<minor>", two decimal numbers with no leading zero. The
// expressions of this file are compiled where they are used: boot holds no
// package-level variable (plugin vet), and a manifest is read once.
func majorMinor(s string) (major, minor int, ok bool) {
	m := regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`).FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, true
}

// okSemver reports whether s is a Semantic Versioning 2.0.0 version, by the regular
// expression that semver.org gives.
func okSemver(s string) bool {
	return regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-(0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*)?(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$`).MatchString(s)
}

// okKebab reports whether s is kebab-case: lowercase letters and digits in words
// joined by single hyphens, starting with a letter (ADR-048 decision 4).
func okKebab(s string) bool {
	return regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).MatchString(s)
}

// okManifestID reports whether s is "<owner>/<name>", each segment kebab-case.
func okManifestID(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && okKebab(owner) && okKebab(name)
}

// okNetwork reports whether s is a network capability: config: and a JSON Pointer,
// or a host name, optionally with :<port>.
func okNetwork(s string) bool {
	if p, ok := strings.CutPrefix(s, "config:"); ok {
		_, ok = pointer(p)
		return ok
	}
	host, port, hasPort := strings.Cut(s, ":")
	if n, err := strconv.ParseUint(port, 10, 16); hasPort && (err != nil || n == 0 || port[0] == '0') {
		return false
	}
	for _, l := range strings.Split(host, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' ||
			strings.Trim(l, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") != "" {
			return false
		}
	}
	return len(host) <= 253
}

// pointer decodes a JSON Pointer (RFC 6901) that names a member: one or more keys,
// each after a "/", in which ~1 stands for "/" and ~0 for "~".
func pointer(p string) ([]string, bool) {
	if !strings.HasPrefix(p, "/") {
		return nil, false
	}
	keys := strings.Split(p[1:], "/")
	for i, k := range keys {
		if strings.Count(k, "~") != strings.Count(k, "~0")+strings.Count(k, "~1") {
			return nil, false
		}
		keys[i] = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
	}
	return keys, true
}

// schemaHas reports whether the JSON Pointer p names a property of schema: each key a
// property of the subschema that the keys before it name.
func schemaHas(schema map[string]any, p string) bool {
	keys, _ := pointer(p)
	for _, k := range keys {
		props, _ := schema["properties"].(map[string]any)
		if schema, _ = props[k].(map[string]any); schema == nil {
			return false
		}
	}
	return true
}

func okSPDX(s string) bool {
	ok, _ := spdx(s)
	return ok
}

// spdx reports whether s is an SPDX license expression by the grammar of the SPDX
// specification (annex "SPDX license expressions"), and whether it holds a
// LicenseRef- or DocumentRef- id. The operators AND, OR and WITH are upper case. An id
// is not looked up: the SPDX list lookup is step 1 of plugin check (ADR-048 decision
// 15).
func spdx(s string) (ok, ref bool) {
	e := &spdxExpr{toks: strings.Fields(strings.NewReplacer("(", " ( ", ")", " ) ").Replace(s))}
	ok = e.compound() && e.i == len(e.toks)
	return ok, e.ref
}

type spdxExpr struct {
	toks []string
	i    int
	ref  bool
}

func (e *spdxExpr) next() string {
	if e.i == len(e.toks) {
		return ""
	}
	e.i++
	return e.toks[e.i-1]
}

func (e *spdxExpr) at(op string) bool {
	if e.i < len(e.toks) && e.toks[e.i] == op {
		e.i++
		return true
	}
	return false
}

// compound reads terms joined by AND or OR.
func (e *spdxExpr) compound() bool {
	for e.term() {
		if !e.at("AND") && !e.at("OR") {
			return true
		}
	}
	return false
}

// term reads "(" compound ")", or a license id with an optional "+", or a
// [DocumentRef-<id>:]LicenseRef-<id>, each with an optional WITH and an exception id.
func (e *spdxExpr) term() bool {
	t := e.next()
	if t == "(" {
		return e.compound() && e.next() == ")"
	}
	if doc, rest, ok := strings.Cut(t, ":"); ok {
		d, isDoc := strings.CutPrefix(doc, "DocumentRef-")
		if !isDoc || !spdxID(d) || !strings.HasPrefix(rest, "LicenseRef-") {
			return false
		}
		t = rest
	}
	simple := false
	if id, isRef := strings.CutPrefix(t, "LicenseRef-"); isRef {
		e.ref, simple = true, spdxID(id)
	} else {
		simple = spdxID(strings.TrimSuffix(t, "+"))
	}
	if simple && e.at("WITH") {
		return spdxID(e.next())
	}
	return simple
}

// spdxID reports whether s is an idstring, letters, digits, "-" and ".", and no operator.
func spdxID(s string) bool {
	return s != "" && s != "AND" && s != "OR" && s != "WITH" &&
		strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-.") == ""
}
