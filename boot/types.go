// Package boot is the launcher library. It has two halves.
//
// The pure half reads a profile and its layers and builds one immutable
// composition, admits it and plans changes (compose.go, gate.go, graph.go,
// required.go, plan.go and the dumps). It reads no environment, no clock and no
// process, and does not import the kernel.
//
// The run half (run.go, launch.go, convert.go, limits.go) runs a composition on
// the kernel: the five boot steps, the six launch services, signals, bounded
// shutdown and exit codes. It takes every input as a value (RunOptions) and is the
// only part that imports sdk/kernel and the launch, loader and composition
// definitions. The package imports nothing from plugins/ or loader/: the loader
// arrives as a kernel.Plugin value in the module table (Registry).
//
// The contract is ADR-059 (docs/adr/ADR-059-s1-scope.md) and, in order of
// authority, docs/architecture/00-decisions.md section 6.1,
// docs/architecture/07-c4-plugin-sdk.md section 7.1 and
// docs/architecture/08-profiles-roadmap-adrs.md sections 1.2, 1.3 and 1.6.
//
// Every value type in this file is immutable by convention: a producer builds
// it once and nobody mutates it, so slices and maps are shared, never copied.
package boot

// Role is one value of the closed role vocabulary of a row
// (08-profiles-roadmap-adrs.md:156; 07-c4-plugin-sdk.md:1307).
type Role string

// The closed role vocabulary.
const (
	RoleEdge   Role = "edge"
	RoleAPI    Role = "api"
	RoleWorker Role = "worker"
)

// RefScheme is the scheme of a closed reference ${scheme:value}
// (08-profiles-roadmap-adrs.md:162).
type RefScheme string

// The four reference schemes. Resolution is the job of a plugin, not of boot.
const (
	RefEnv    RefScheme = "env"
	RefFile   RefScheme = "file"
	RefSecret RefScheme = "secret"
	RefFlag   RefScheme = "flag"
)

// Ref is a parsed reference. It is syntax only: boot never resolves one.
type Ref struct {
	Scheme RefScheme
	Name   string // the text after "scheme:"; a path for RefFile
}

// Kind is the type of a Value.
type Kind uint8

// The kinds of a Value. KindNull is the zero kind.
const (
	KindNull Kind = iota
	KindBool
	KindInt
	KindFloat
	KindString
	KindList
	KindMap
	KindRef
)

// Value is a node of a parsed config tree. Only the field that belongs to
// Kind is meaningful. A map is a slice of Field sorted by key with no
// duplicate keys, so iteration order is deterministic. Raw YAML never
// survives parsing.
type Value struct {
	Kind  Kind
	Bool  bool
	Int   int64
	Float float64 // never NaN or infinite; negative zero is stored as zero
	Str   string
	List  []Value
	Map   []Field
	Ref   Ref
}

// Field is one key and value of a map Value.
type Field struct {
	Key   string
	Value Value
}

// Row is one entry of the composed tree: the ten fields of the row format
// {id, plugin, config, enabled, required, roles, inject, intercept, isolate,
// group} (07-c4-plugin-sdk.md:1229; 00-decisions.md:475). Parent is the
// composed id of the enclosing core:group or core:include row, or "".
//
// In a Composition the children of a group are rows of their own, with the
// composed id "<group>:<id>" (08-profiles-roadmap-adrs.md:159) and Parent set,
// and the Config of a core:group row is an empty list. Roles, Inject and
// Isolate are sorted sets without duplicates. Intercept maps a key name to
// the intercept config of that key (ADR-059 decision 2).
type Row struct {
	ID        string
	Plugin    string // "<owner>/<name>", "core:group" or "core:include"
	Parent    string
	Config    Value
	Enabled   bool
	Required  bool
	Roles     []Role
	Inject    []string
	Intercept []Field
	Isolate   []string
	Group     bool
}

// FieldName names one of the ten row fields.
type FieldName string

// The ten row fields, in canonical order.
const (
	FieldID        FieldName = "id"
	FieldPlugin    FieldName = "plugin"
	FieldConfig    FieldName = "config"
	FieldEnabled   FieldName = "enabled"
	FieldRequired  FieldName = "required"
	FieldRoles     FieldName = "roles"
	FieldInject    FieldName = "inject"
	FieldIntercept FieldName = "intercept"
	FieldIsolate   FieldName = "isolate"
	FieldGroup     FieldName = "group"
)

// FieldNames returns the ten row fields in canonical order.
func FieldNames() []FieldName {
	return []FieldName{FieldID, FieldPlugin, FieldConfig, FieldEnabled, FieldRequired,
		FieldRoles, FieldInject, FieldIntercept, FieldIsolate, FieldGroup}
}

// Patch is a layer item that targets a row by id
// (08-profiles-roadmap-adrs.md:157). A nil pointer means the field is absent;
// a patch that omits config leaves it untouched. plugin and roles cannot
// appear in a patch (07-c4-plugin-sdk.md:1307-1308), so they are not here.
type Patch struct {
	ID        string
	Config    *Value
	Enabled   *bool
	Required  *bool
	Inject    *[]string
	Intercept *[]Field
	Isolate   *[]string
	Group     *bool
}

// ParsedRow is a row as written in a layer file, with its source line. A
// core:group row carries its children in Children and an empty Config.
type ParsedRow struct {
	Row
	Line     int
	Children []ParsedRow
}

// Item is one item of a layer file: exactly one of Insert and Patch is set.
type Item struct {
	Line   int
	Insert []ParsedRow
	Patch  *Patch
}

// Layer is one layer file after parsing and before composition.
type Layer struct {
	Number int    // 1 bundle, 2 profile, 3 site, 4 --patch (layer 0 is the empty root)
	Source string // "bundle:<name>", "profile:<name>", "site" or "patch:<path>"
	File   string
	Items  []Item
}

// Origin says which layer and file set a field of a row.
type Origin struct {
	Layer  int
	Source string
	File   string
	Line   int
}

// FieldOrigin is the origin of one field.
type FieldOrigin struct {
	Field  FieldName
	Origin Origin
}

// Provenance holds, for each of the ten fields in canonical order, the
// origin of its current value: the insert, or the latest patch that set it.
type Provenance struct {
	Fields []FieldOrigin
}

// Of returns the origin of a field, and false for a name that is not a row
// field.
func (p Provenance) Of(f FieldName) (Origin, bool) {
	for _, fo := range p.Fields {
		if fo.Field == f {
			return fo.Origin, true
		}
	}
	return Origin{}, false
}

// RefUse is one reference found in a row, with the path of the config value
// that holds it, for example "auth.secret" or "allow[0]".
type RefUse struct {
	Path string
	Ref  Ref
}

// ComposedRow is a Row with its provenance and the references it holds.
type ComposedRow struct {
	Row
	Provenance Provenance
	Refs       []RefUse
}

// Digest is a SHA-256 over the canonical encoding of a composition.
type Digest [32]byte

// Composition is one immutable composed tree: the rows of layers 0 to 4 in
// final order (parents before their children, inserts in layer order) and the
// digest of 08-profiles-roadmap-adrs.md:184. The encoding behind Digest is
// described at EncodeDigest.
type Composition struct {
	Rows   []ComposedRow
	Digest Digest
}

// ModuleEntry is what the launcher knows of one plugin of the module table.
// Plugin holds the kernel.Plugin value that kernel.Define returned, which only
// the run half reads (Run checks its declarations against this entry); it takes
// no part in the digest and the pure half never looks inside it.
type ModuleEntry struct {
	ID       string // manifest id "<owner>/<name>"
	Module   string // Go module path
	Version  string
	Sum      string
	Plugin   any
	Provides []string // key names the plugin provides
	Needs    []string // key names the plugin needs
	// Wants lists the keys the plugin reads optionally (a When body). A want
	// is no edge of the graph, and a provider-bound key refuses it
	// (07-c4-plugin-sdk.md:553). Added at S1 for gate check 7.
	Wants []string
	// Hooks lists the hooks the plugin listens on, with their declared stage
	// (07-c4-plugin-sdk.md:438). Added at S1 for --dump-hooks and the sealed
	// hook part of gate check 7.
	Hooks []HookDecl
	// Volatile lists the config field paths that can change without a
	// restart, until S2a derives them (ADR-059 decision 10). A path is
	// dotted, as in RefUse.Path.
	Volatile []string
}

// HookDecl is one hook a plugin listens on. Stage is the ordinal of the
// declared stage in the definition that owns the hook: a chain runs by Stage
// ascending, then plugin id (07-c4-plugin-sdk.md:438). StageName is for the
// dump only. Added at S1.
type HookDecl struct {
	Hook      string
	Stage     int
	StageName string
}

// ModuleTable maps a plugin id to its entry.
type ModuleTable map[string]ModuleEntry

// Binding lists the plugin ids allowed to hold one side of a bound key.
type Binding struct {
	Key     string
	Plugins []string
}

// HookListeners lists the plugin ids that may listen on one sealed hook.
type HookListeners struct {
	Hook    string
	Plugins []string
}

// DistManifest is the part of the signed distribution manifest that the
// launcher reads (08-profiles-roadmap-adrs.md:451; 07-c4-plugin-sdk.md:1340).
// No layer can change it. RequiredKeys must have an ACTIVE provider;
// RequiredRows are named by id.
type DistManifest struct {
	Providers    []Binding
	Consumers    []Binding
	SealedHooks  []HookListeners
	RequiredKeys []string
	RequiredRows []string
}

// LockDigest is the lock digest of one remote plugin or pack with no code
// (08-profiles-roadmap-adrs.md:184). None exist before S2a.
type LockDigest struct {
	Name   string
	Digest string
}

// Action is what a Plan does to one row.
type Action string

// The four actions of a plan.
const (
	ActionCreate    Action = "create"
	ActionUpdate    Action = "update"
	ActionRemove    Action = "remove"
	ActionUnchanged Action = "unchanged"
)

// Effect is what the loader does for one row of a plan: see the constants in
// plan.go. Added at S1.
type Effect string

// PlanEntry is the change of one row between two compositions. Changed lists
// the row fields that differ and ConfigPaths the config paths that differ, so
// that a later package can tell a volatile-only update from a restart.
type PlanEntry struct {
	ID          string
	Action      Action
	Changed     []FieldName
	ConfigPaths []string
	// Effect is what the loader does for the row, derived from the change and
	// from whether the row is mounted before and after (see Effect). Added at
	// S1 so that a volatile update is told from a restart.
	Effect Effect
}

// Plan is the diff between two compositions by row id
// (08-profiles-roadmap-adrs.md:201). Entries are sorted by id. Closure is the
// restart closure, left empty here and filled by the graph package.
type Plan struct {
	From    Digest
	To      Digest
	Entries []PlanEntry
	Closure []string
}

// Outcome is the judged result of one row (08-profiles-roadmap-adrs.md:203).
type Outcome string

// The outcomes of a verdict.
const (
	OutcomePass    Outcome = "pass"
	OutcomeFail    Outcome = "fail"
	OutcomeWarning Outcome = "warning" // inactive before and untouched
)

// RowVerdict is the judged result of one row. State is the fiber state in
// capitals, as a string, so that boot does not import the kernel.
type RowVerdict struct {
	ID      string
	Outcome Outcome
	State   string
	Detail  string
}

// Verdict is the judged result of a generation. Pass is true when no row
// failed. Rows are sorted by id.
type Verdict struct {
	Plan Digest // the To digest of the plan that was judged
	Pass bool
	Rows []RowVerdict
}

// KillSwitches is layer 6: two switches read from the environment by the
// launcher, which no layer and no row can reach
// (08-profiles-roadmap-adrs.md:111, 126).
type KillSwitches struct {
	AgentsPaused      bool
	TelemetryDisabled bool
}

// Names of the layer 6 environment variables.
const (
	EnvAgentsPaused      = "AIOPS_AGENTS_PAUSED"
	EnvTelemetryDisabled = "AIOPS_TELEMETRY_DISABLED"
)

// ReadKillSwitches builds layer 6 from a lookup function supplied by the
// launcher (for example os.Getenv), so that this package never reads the
// environment. A non-empty value means on.
func ReadKillSwitches(lookup func(name string) string) KillSwitches {
	return KillSwitches{
		AgentsPaused:      lookup(EnvAgentsPaused) != "",
		TelemetryDisabled: lookup(EnvTelemetryDisabled) != "",
	}
}
