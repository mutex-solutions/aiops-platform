// SPDX-License-Identifier: Apache-2.0

package launch

import (
	"context"
	"errors"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The toolbox of launch 0.2: what ProfileContext.Toolbox returns, the code beside the
// module table that the operations plugin reaches through profileContext (ADR-061
// decisions 21 and 33). This package holds every type the toolbox takes and returns,
// so boot implements and hands on the parts without importing what implements Tools,
// and a plugin uses them without importing boot. Every path in a request is absolute.

// ErrNotCarried is the error of a part of Tools that the binary does not link: every
// part in aiops, which links neither the builder nor what it links.
var ErrNotCarried = errors.New("launch: this binary does not carry the tool")

// Toolbox is the launcher's toolbox. Render, Gate, ReadManifest, Shipped and Release
// are boot's; Tools are the parts that only aiopsctl links and hands to boot.
type Toolbox interface {
	// Render composes a named profile from a home, bundles and patch files, admits it
	// with this binary's module table and the signed manifest that boot trusts, and
	// returns the dumps asked for, the diagnostics and the verdict. Given a home, it
	// first refuses a home whose current distribution is not the one this binary
	// embeds (ADR-061 decision 26).
	Render(ctx context.Context, r RenderRequest) (Rendered, error)
	// Gate evaluates the clauses of one entry point of the gate on an artifact, against
	// a profile of a home or a published signed manifest (ADR-060 decision 17).
	Gate(ctx context.Context, r GateRequest) (Verdict, error)
	// ReadManifest is boot's reader of plugin.yaml in strict mode (ADR-048 decision 15).
	ReadManifest(ctx context.Context, r ManifestRequest) (ManifestSummary, []Diagnostic, error)
	// Shipped returns the bundles and profiles that the binary embeds, by path.
	Shipped(ctx context.Context) (map[string][]byte, error)
	// Release returns the signed manifest that the binary embeds, or with against the
	// one at that path, with its instantiable plugins.
	Release(ctx context.Context, against string) (Release, error)
	Tools
}

// Tools are the parts that only aiopsctl links. In aiops each returns an error that
// matches ErrNotCarried.
type Tools interface {
	// Build is the builder, on the image path or the local path; for smoke it also boots
	// the test-only profile and reads its row states.
	Build(ctx context.Context, r BuildRequest) (Built, error)
	// Generate is plugingen.
	Generate(ctx context.Context, r GenerateRequest) ([]Diagnostic, error)
	// Vet is the import analyzer and plugin vet.
	Vet(ctx context.Context, dir string) ([]Diagnostic, error)
	// Suite runs a suite: an embedded test binary, or a module compiled with the
	// toolchain.
	Suite(ctx context.Context, r SuiteRequest) (SuiteReport, error)
	// Template returns the files of a template, by path.
	Template(ctx context.Context, r TemplateRequest) (map[string][]byte, error)
	// Scenario runs a named, versioned scenario on a pack's rows replayed from its
	// fixtures.
	Scenario(ctx context.Context, r ScenarioRequest) (ScenarioReport, error)
}

// NoTools is the Tools of a binary that links none of them: every part answers
// ErrNotCarried.
type NoTools struct{}

var _ Tools = NoTools{}

func (NoTools) Build(context.Context, BuildRequest) (Built, error) { return Built{}, ErrNotCarried }
func (NoTools) Generate(context.Context, GenerateRequest) ([]Diagnostic, error) {
	return nil, ErrNotCarried
}
func (NoTools) Vet(context.Context, string) ([]Diagnostic, error) { return nil, ErrNotCarried }
func (NoTools) Suite(context.Context, SuiteRequest) (SuiteReport, error) {
	return SuiteReport{}, ErrNotCarried
}
func (NoTools) Template(context.Context, TemplateRequest) (map[string][]byte, error) {
	return nil, ErrNotCarried
}
func (NoTools) Scenario(context.Context, ScenarioRequest) (ScenarioReport, error) {
	return ScenarioReport{}, ErrNotCarried
}

// Diagnostic is one finding. Check is a gate check, a step of plugin check or an
// obligation id; File and Line are where, when known.
type Diagnostic struct {
	Check   string
	File    string
	Line    int
	Text    string
	Warning bool
}

// The outcomes of a clause.
const (
	OutcomePass         = "pass"
	OutcomeDeny         = "deny"
	OutcomeNotEvaluated = "not-evaluated"
)

// Clause is the outcome of one clause of the gate: Check is 1 to 10, or 0 for the
// module table lookup that runs before them; Clause names the clause within the check,
// "" for the whole check.
type Clause struct {
	Check   int
	Clause  string
	Outcome string
	Reason  string
}

// Verdict is what one entry point of the gate decided. RebuildRequired is set where
// install of in-process code passed every clause it evaluates: the code takes a build.
type Verdict struct {
	Entry           string
	Clauses         []Clause
	RebuildRequired bool
}

// Bundle is the bundle of an installed artifact: its plugin id and bundle.yaml.
type Bundle struct {
	ID   string
	Data []byte
}

// RenderRequest is what Render composes. Home is "" for none, and then the shipped
// profiles are composed alone; Bundles is "" for the home's. Roles, Server and Dumps
// are the flags of profile render, each dump by its flag (--dump, --dump-hooks,
// --dump-config-schema). Append is the bundle of an artifact, appended to the profile's
// bundles as install appends it (ADR-047 decision 17); a zero ID appends none.
type RenderRequest struct {
	Home, Profile, Bundles string
	Patches                []string
	Roles                  []string
	Server                 bool
	Dumps                  []string
	Append                 Bundle
}

// KeyProviders is one key of a rendered profile: its classes ("open" or "bound") and
// the enabled rows that provide it, sorted.
type KeyProviders struct {
	Name               string
	Provider, Consumer string
	Rows               []string
}

// Rendered is what Render returns. Diagnostics holds the lines that profile render
// prints: an error is a diagnostic that is not a warning, and the profile is refused
// when it holds one. Departures are the departures from the release bindings, and
// Distribution is the digest of the signed manifest it admitted with.
type Rendered struct {
	Composition  Composition
	Admission    Admission
	Required     RequiredSet
	Keys         []KeyProviders
	Departures   []string
	Dumps        map[string]string
	Diagnostics  []Diagnostic
	Distribution string
}

// GateRequest is one artifact at one entry point: "plugin check", "plugin gate
// --against" or "plugin install". Signer is the key id that the caller verified,
// "unsigned", or "" where the entry point verifies none. Home and Profile name the
// target profile of install; Against holds the bytes of a published signed manifest.
type GateRequest struct {
	Entry                         string
	Manifest, Bundle, Attestation []byte
	Digest                        string
	Signer                        string
	Home, Profile                 string
	Against                       []byte
}

// ManifestRequest names the directory of a plugin.yaml, and the release it is read
// against.
type ManifestRequest struct {
	Dir, Against string
}

// ManifestSummary holds six fields of boot's own reading of a manifest, so that the
// reader stays boot's (ADR-048 decision 15). Definitions is requires.definitions.
type ManifestSummary struct {
	ID, Version                   string
	Runtimes, Provides, Registers []string
	Definitions                   map[string][]string
}

// Release is a signed distribution manifest: its digest, its bytes, and its
// instantiable plugins.
type Release struct {
	Digest   string
	Manifest []byte
	Plugins  []ReleasePlugin
}

// ReleasePlugin is an instantiable plugin of a release: its id and the first entry of
// the examples of its configSchema, nil when it has none (ADR-047 decision 12).
type ReleasePlugin struct {
	ID      string
	Example any
}

// BuildRequest is what the builder builds (ADR-044): dist.yaml, the module cache, the
// mirrors by prefix, the modules whose version may be raised and the builder, "image"
// or "local"; for smoke and verify the plugin directory, or the module path, version
// and sum, with the release it runs against.
type BuildRequest struct {
	DistYAML, ModCache            string
	Mirrors                       map[string]string
	AllowBump                     []string
	Builder                       string
	Plugin                        string
	Module, Version, Sum, Against string
}

// Built is what the builder wrote: the image, the signed manifest's bytes, the site's
// aiopsctl and the verdict, and for smoke each row with its state and what it waits
// for.
type Built struct {
	Image    string
	Manifest []byte
	Aiopsctl string
	Verdict  Verdict
	Rows     []RowWait
}

// RowWait is one row of a booted profile: its state and the keys it waits for.
type RowWait struct {
	ID       string
	State    kernel.State
	WaitsFor []string
}

// GenerateRequest is the directory that plugingen reads, and whether it writes.
type GenerateRequest struct {
	Dir   string
	Write bool
}

// SuiteRequest is the plugin directory, the definitions with their versions, the
// halves to run and the fixtures directory.
type SuiteRequest struct {
	Dir         string
	Definitions map[string]string
	Halves      []string
	Fixtures    string
}

// SuiteReport is per definition its suite revision, the halves that ran and each
// failed obligation by id.
type SuiteReport struct {
	Definitions []SuiteResult
}

// SuiteResult is the run of one definition's suite.
type SuiteResult struct {
	Name     string
	Revision int
	Halves   []string
	Failed   []string
}

// TemplateRequest names a template: a registry or "connector", the plugin id, the
// language and whether the plugin is under contrib/.
type TemplateRequest struct {
	Registry, ID, Lang string
	Contrib            bool
}

// ScenarioRequest is the pack directory and the scenario's name.
type ScenarioRequest struct {
	Dir, Name string
}

// ScenarioReport is the scenario's name and version, the halves that ran and each
// failure.
type ScenarioReport struct {
	Name, Version string
	Halves        []string
	Failures      []string
}
