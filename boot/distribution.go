package boot

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// MaxDistBytes bounds a signed distribution manifest, which holds the manifest of
// every plugin of its module table.
const MaxDistBytes = 16 << 20

// Distribution is a signed distribution manifest as read (ADR-060 decision 20): what
// the builder writes for a release or a site, the binary embeds, dist sign signs and
// dist use records by Digest, the SHA-256 of its bytes (ADR-020 decision 12). It
// gives way to the S1 stand-ins dist.yaml and modules.yaml (dist.go). One YAML
// document, every field required but site, each set written sorted:
//
//	distVersion: 1
//	plugins:                  the module table (07-c4-plugin-sdk.md:1344)
//	  <plugin id>:
//	    package: <the import path of the package that declares the plugin value>
//	    manifest: <its plugin.yaml, byte for byte>
//	    attestation: <its statement (ReadAttestation)>   optional (ADR-020 decision 9)
//	modules:                  each module the binary links, sorted by path
//	  - {path, version, sum, replace: {path, version, sum}, licence}
//	packages: [<import path>] the package graph of the binary (ADR-060 decision 58)
//	pins:                     the go.sum lines of the build, sorted by path and version
//	  - {path, version, sum, gomod, release, unchecked}
//	bindings: {providers: {<key>: [<plugin id>]}, consumers: {<key>: [<plugin id>]},
//	           sealedHooks: {<hook>: [<plugin id>]}, required: {keys: [<key>], rows: [<row id>]}}
//	site: <the entries a site added, in the shape of bindings>   optional
//	acceptLicenses: [<SPDX id>]
//	serves: {pluginApi: "<major>.<minor>", kernel: {keys: <classes>, hooks: <hooks>},
//	         definitions: {<name>: [<served major>]}}
//
// licence is the detected licence, an SPDX expression. A module records a version
// and a sum, or neither for the main module, or a replacement and neither for a
// replaced module (ADR-020 decision 28). A pin records sum, gomod or both: release is
// the release's pin of a version that --allow-bump raised, and unchecked marks a hash
// checked against nothing (ADR-044 decisions 8 and 10). The four fields of bindings
// are optional, and site lists what a site added to them (ADR-044 decisions 13 and
// 22). A served major, sorted by major, holds the fields of definition.yaml that the
// gate reads (ADR-060 decisions 7 and 9): {version, compatibleFrom, deprecated: {since,
// removeNotBefore, successor}, suite, classes, hooks}, the last optional; classes maps
// a key to {provider: open|bound, consumer: open|bound|signed} and hooks a hook to
// {stages, access: open|signed|sealed, remote: yes|no}.
type Distribution struct {
	Digest         string
	Plugins        map[string]DistPlugin
	Modules        []DistModule
	Packages       []string
	Pins           []DistPin
	Release, Site  DistManifest // bindings and site
	AcceptLicenses []string
	Serves         Serves
}

// DistPlugin is one entry of the module table. Attestation is nil when absent.
type DistPlugin struct {
	Package               string
	Manifest, Attestation []byte
}

// DistModule is one module the binary links. Replace is nil unless it is replaced.
type DistModule struct {
	GoModule
	Replace *GoModule
	Licence string
}

// DistPin is one pinned module version.
type DistPin struct {
	Path, Version, Sum, GoMod, Release string
	Unchecked                          bool
}

// Serves is serves: the author API the binary serves and, per definition, each major
// it links. Kernel holds the classes of the four built-in keys and the six hooks of
// the kernel (ADR-048 decision 12).
type Serves struct {
	PluginAPI   string
	Kernel      ServedDefinition
	Definitions map[string][]ServedDefinition
}

// ServedDefinition is one served major of a definition. Deprecated is nil unless the
// major is deprecated, and Hooks unless the definition owns a hook.
type ServedDefinition struct {
	Version, CompatibleFrom string
	Deprecated              map[string]string
	Suite                   int
	Classes                 map[string]KeyClass
	Hooks                   map[string]ServedHook
}

// KeyClass is the provider class and the consumer class of a key.
type KeyClass struct{ Provider, Consumer string }

// ServedHook is a hook: its stages in chain order, its access class and its remote
// mark.
type ServedHook struct {
	Stages         []string
	Access, Remote string
}

// ReadDistribution reads the signed distribution manifest data, the content of file.
func ReadDistribution(file string, data []byte) (Distribution, error) {
	r, root, err := newReader(file, data, RuleDist, MaxDistBytes, "a distribution manifest")
	if err != nil {
		return Distribution{}, err
	}
	f := r.fields(root, "a distribution manifest", []string{"distVersion", "plugins", "modules", "packages", "pins", "bindings",
		"site", "acceptLicenses", "serves"}, "distVersion", "plugins", "modules", "packages", "pins", "bindings", "acceptLicenses", "serves")
	if v := r.num(f["distVersion"], "distVersion"); r.err == nil && v != 1 {
		r.fail(f["distVersion"], "distVersion %d is unknown: this reader knows 1", v)
	}
	d := Distribution{Digest: digestOf(data), Plugins: map[string]DistPlugin{}}
	for _, pr := range r.mapping(f["plugins"], "plugins", okManifestID) {
		e := r.fields(pr.v, "plugin "+pr.key, []string{"package", "manifest", "attestation"}, "package", "manifest")
		p := DistPlugin{Package: r.word(e["package"], "package", okPath), Manifest: []byte(r.str(e["manifest"], "manifest"))}
		if e["attestation"] != nil {
			p.Attestation = []byte(r.str(e["attestation"], "attestation"))
		}
		d.Plugins[pr.key] = p
	}
	for _, c := range r.list(f["modules"], "modules") {
		e := r.fields(c, "an entry of modules", []string{"path", "version", "sum", "replace", "licence"}, "path", "licence")
		m := DistModule{GoModule: r.gomod(e, "modules"), Licence: r.word(e["licence"], "modules licence", okSPDX)}
		if n := e["replace"]; n != nil {
			rep := r.gomod(r.fields(n, "replace", []string{"path", "version", "sum"}, "path"), "replace")
			m.Replace = &rep
			if m.Version != "" {
				r.fail(n, "module %s records a replacement and no version or sum of its own", m.Path)
			}
		}
		d.Modules = append(d.Modules, m)
	}
	r.sorted(f["modules"], "modules", len(d.Modules), func(i int) bool { return d.Modules[i-1].Path < d.Modules[i].Path })
	d.Packages = r.set(f["packages"], "packages", okPath)
	for _, c := range r.list(f["pins"], "pins") {
		e := r.fields(c, "an entry of pins", []string{"path", "version", "sum", "gomod", "release", "unchecked"}, "path", "version")
		p := DistPin{Path: r.word(e["path"], "pins path", okPath), Version: r.word(e["version"], "pins version", okGoVersion),
			Sum: r.word(e["sum"], "pins sum", okSum), GoMod: r.word(e["gomod"], "pins gomod", okSum),
			Release: r.word(e["release"], "pins release", okGoVersion), Unchecked: r.flag(e["unchecked"], "pins unchecked")}
		if p.Sum == "" && p.GoMod == "" {
			r.fail(c, "pin %s %s records sum, gomod or both", p.Path, p.Version)
		}
		d.Pins = append(d.Pins, p)
	}
	r.sorted(f["pins"], "pins", len(d.Pins), func(i int) bool {
		a, b := d.Pins[i-1], d.Pins[i]
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Version, b.Version)) < 0
	})
	d.Release, d.Site = r.distBindings(f["bindings"], "bindings"), r.distBindings(f["site"], "site")
	d.AcceptLicenses = r.set(f["acceptLicenses"], "acceptLicenses", func(s string) bool { return okSPDX(s) && !strings.ContainsAny(s, " ()") })
	d.Serves = r.serves(f["serves"])
	if r.err != nil {
		return Distribution{}, r.err
	}
	return d, nil
}

// distBindings reads bindings or site; each field is optional.
func (r *mreader) distBindings(n *yaml.Node, what string) DistManifest {
	f := r.fields(n, what, []string{"providers", "consumers", "sealedHooks", "required"})
	var d DistManifest
	d.Providers = r.binding(f["providers"], what+".providers", okIdent)
	d.Consumers = r.binding(f["consumers"], what+".consumers", okIdent)
	for _, b := range r.binding(f["sealedHooks"], what+".sealedHooks", okHook) {
		d.SealedHooks = append(d.SealedHooks, HookListeners{Hook: b.Key, Plugins: b.Plugins})
	}
	q := r.fields(f["required"], what+".required", []string{"keys", "rows"})
	d.RequiredKeys = r.set(q["keys"], what+".required.keys", okIdent)
	d.RequiredRows = r.set(q["rows"], what+".required.rows", func(s string) bool { return okID(s, true) })
	return d
}

// binding reads {<key>: [<plugin id>]}, sorted by key.
func (r *mreader) binding(n *yaml.Node, what string, valid func(string) bool) []Binding {
	var out []Binding
	for _, pr := range r.mapping(n, what, valid) {
		out = append(out, Binding{Key: pr.key, Plugins: r.set(pr.v, what+" "+pr.key, okManifestID)})
	}
	slices.SortFunc(out, func(a, b Binding) int { return cmp.Compare(a.Key, b.Key) })
	return out
}

// serves reads serves.
func (r *mreader) serves(n *yaml.Node) Serves {
	f := r.fields(n, "serves", []string{"pluginApi", "kernel", "definitions"}, "pluginApi", "kernel", "definitions")
	k := r.fields(f["kernel"], "serves.kernel", []string{"keys", "hooks"}, "keys", "hooks")
	s := Serves{PluginAPI: r.word(f["pluginApi"], "serves.pluginApi", okMajorMinor), Definitions: map[string][]ServedDefinition{},
		Kernel: ServedDefinition{Classes: r.classes(k["keys"], "serves.kernel.keys"), Hooks: r.servedHooks(k["hooks"], "serves.kernel.hooks")}}
	for _, pr := range r.mapping(f["definitions"], "serves.definitions", okKebab) {
		what := "serves.definitions " + pr.key
		var majors []int
		for _, c := range r.list(pr.v, what) {
			sd := r.served(c, what)
			major, _, _ := majorMinor(sd.Version)
			majors = append(majors, major)
			s.Definitions[pr.key] = append(s.Definitions[pr.key], sd)
		}
		r.sorted(pr.v, what, len(majors), func(i int) bool { return majors[i-1] < majors[i] })
		if len(majors) == 0 {
			r.fail(pr.v, "%s serves no major", what)
		}
	}
	return s
}

// served reads one served major of a definition: compatibleFrom only at major 0, a
// minor up to the version's, and deprecated naming a later major as its successor.
func (r *mreader) served(n *yaml.Node, what string) ServedDefinition {
	e := r.fields(n, "a served major of "+what, []string{"version", "compatibleFrom", "deprecated", "suite", "classes", "hooks"},
		"version", "suite", "classes")
	sd := ServedDefinition{Version: r.word(e["version"], what+" version", okMajorMinor),
		CompatibleFrom: r.word(e["compatibleFrom"], what+" compatibleFrom", okMajorMinor), Suite: r.num(e["suite"], what+" suite"),
		Classes: r.classes(e["classes"], what+" classes"), Hooks: r.servedHooks(e["hooks"], what+" hooks")}
	major, minor, _ := majorMinor(sd.Version)
	if cMajor, cMinor, ok := majorMinor(sd.CompatibleFrom); ok && (major != 0 || cMajor != 0 || cMinor > minor) {
		r.fail(e["compatibleFrom"], "%s compatibleFrom %s: only at major 0, a minor up to %s", what, sd.CompatibleFrom, sd.Version)
	}
	if dn := e["deprecated"]; dn != nil {
		df := r.fields(dn, what+" deprecated", []string{"since", "removeNotBefore", "successor"}, "since", "removeNotBefore", "successor")
		sd.Deprecated = map[string]string{"since": r.word(df["since"], what+" deprecated since", func(s string) bool { return s != "" }),
			"removeNotBefore": r.word(df["removeNotBefore"], what+" deprecated removeNotBefore", func(s string) bool {
				_, err := time.Parse(time.DateOnly, s)
				return err == nil
			}),
			"successor": r.word(df["successor"], what+" deprecated successor", func(s string) bool {
				v, ok := strings.CutPrefix(s, "v")
				n, err := strconv.Atoi(v)
				return ok && err == nil && strconv.Itoa(n) == v && n > major
			})}
	}
	return sd
}

// classes reads {<key>: {provider, consumer}}.
func (r *mreader) classes(n *yaml.Node, what string) map[string]KeyClass {
	out := map[string]KeyClass{}
	for _, pr := range r.mapping(n, what, okIdent) {
		e := r.fields(pr.v, what+" "+pr.key, []string{"provider", "consumer"}, "provider", "consumer")
		out[pr.key] = KeyClass{Provider: r.word(e["provider"], what+" provider", oneOf("open", "bound")),
			Consumer: r.word(e["consumer"], what+" consumer", oneOf("open", "bound", "signed"))}
	}
	return out
}

// servedHooks reads {<hook>: {stages, access, remote}}, nil when n is absent.
func (r *mreader) servedHooks(n *yaml.Node, what string) map[string]ServedHook {
	var out map[string]ServedHook
	for _, pr := range r.mapping(n, what, okHook) {
		e := r.fields(pr.v, what+" "+pr.key, []string{"stages", "access", "remote"}, "stages", "access", "remote")
		h := ServedHook{Access: r.word(e["access"], what+" access", oneOf("open", "signed", "sealed")),
			Remote: r.word(e["remote"], what+" remote", oneOf("yes", "no"))}
		for _, c := range r.list(e["stages"], what+" stages") {
			h.Stages = append(h.Stages, r.word(c, what+" stage", okIdent))
		}
		if len(h.Stages) == 0 || !okList(h.Stages, okIdent) {
			r.fail(pr.v, "%s %s names each stage once, at least one", what, pr.key)
		}
		if out == nil {
			out = map[string]ServedHook{}
		}
		out[pr.key] = h
	}
	return out
}

// mapping returns the entries of the mapping n, whose keys are data that satisfy
// valid; nil when n is absent.
func (r *mreader) mapping(n *yaml.Node, what string, valid func(string) bool) []pair {
	if n == nil || r.err != nil {
		return nil
	}
	prs, err := r.pairs(n, "", what)
	r.keep(err)
	for _, pr := range prs {
		if !valid(pr.key) {
			r.fail(pr.k, "%s names %q, which is not valid", what, pr.key)
		}
	}
	return prs
}

// okSum reports whether s is a go.sum hash, h1: and the base64 of 32 bytes.
func okSum(s string) bool {
	h, ok := strings.CutPrefix(s, "h1:")
	return ok && okHash32(h)
}
