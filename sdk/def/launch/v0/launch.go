// SPDX-License-Identifier: Apache-2.0

package launch

import (
	"context"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The six launch keys. Each is provider-bound, so only the module-table entry
// that the distribution manifest lists, the launcher, may provide it
// (03-c3-kernel.md:63; 07-c4-plugin-sdk.md:555, :1359). The names and the major
// are fixed by 03-c3-kernel.md:63.
var (
	// ProfileContextKey: class P (03-c3-kernel.md:63).
	ProfileContextKey = kernel.NewKey[ProfileContext]("profileContext", 0, kernel.ProviderBound())
	// CatalogKey: class P (03-c3-kernel.md:63).
	CatalogKey = kernel.NewKey[Catalog]("catalog", 0, kernel.ProviderBound())
	// CmdlineKey: class P (03-c3-kernel.md:63).
	CmdlineKey = kernel.NewKey[Cmdline]("cmdline", 0, kernel.ProviderBound())
	// ReadyKey: class P (03-c3-kernel.md:63).
	ReadyKey = kernel.NewKey[Ready]("ready", 0, kernel.ProviderBound())
	// KillSwitchesKey: class P (03-c3-kernel.md:63).
	KillSwitchesKey = kernel.NewKey[KillSwitches]("killSwitches", 0, kernel.ProviderBound())
	// AppExitKey: class PC, provider-bound and consumer-bound
	// (03-c3-kernel.md:63; 07-c4-plugin-sdk.md:556).
	AppExitKey = kernel.NewKey[AppExit]("appExit", 0, kernel.ProviderBound(), kernel.ConsumerBound())
)

// Digest is the digest over a composed tree (08-profiles-roadmap-adrs.md:184),
// the same 32 bytes as boot.Digest.
type Digest [32]byte

// Ref is a closed reference ${scheme:name} left in a config tree, unresolved:
// resolving is the job of config-refs (03-c3-kernel.md:68). Scheme is "env",
// "file", "secret" or "flag" (08-profiles-roadmap-adrs.md:162).
type Ref struct{ Scheme, Name string }

// Row is one row of a composed tree, with the ten row fields minus the plugin
// config's YAML form (07-c4-plugin-sdk.md:1229; 00-decisions.md:475). Config is the
// parsed tree as plain Go values: nil, bool, int64, float64, string, Ref, []any
// and map[string]any; nothing else survives parsing (ADR-059 decision 6). Roles,
// Inject and Isolate are sorted sets, Intercept maps a key name to the intercept
// config of that key. A child of a group is a row of its own with the composed id
// "<group>:<id>" and Parent set (08-profiles-roadmap-adrs.md:159).
type Row struct {
	ID        string
	Plugin    string // "<owner>/<name>", "core:group" or "core:include"
	Parent    string
	Config    any
	Enabled   bool
	Required  bool
	Group     bool
	Roles     []string
	Inject    []string
	Isolate   []string
	Intercept map[string]any
}

// Composition is one immutable composed tree: rows in final order, parents before
// children, and the digest of the tree.
type Composition struct {
	Rows   []Row
	Digest Digest
}

// Layer says where one composed layer came from (08-profiles-roadmap-adrs.md:111).
type Layer struct {
	Number int    // 1 bundle, 2 profile, 3 site, 4 patch
	Source string // "bundle:<name>", "profile:<name>", "site" or "patch:<path>"
	File   string
}

// Denial is one row the gate refused (03-c3-kernel.md:358).
type Denial struct {
	Row    string
	Check  string // the unmet check of the ten
	Reason string
}

// Admission is the result of admitting a candidate: the denied rows. A denied
// optional row is detached as disabled; a denied required row is an error of
// Admit instead (03-c3-kernel.md:358, :1361).
type Admission struct{ Denied []Denial }

// Action is what a Plan does to one row.
type Action string

// The four actions of a plan (03-c3-kernel.md:378).
const (
	ActionCreate    Action = "create"
	ActionUpdate    Action = "update"
	ActionRemove    Action = "remove"
	ActionUnchanged Action = "unchanged"
)

// PlanEntry is the change of one row between two compositions. Changed names the
// row fields that differ and ConfigPaths the dotted config paths that differ, so
// a volatile-only update is told from a restart (07-c4-plugin-sdk.md:1374).
type PlanEntry struct {
	ID          string
	Action      Action
	Changed     []string
	ConfigPaths []string
}

// Plan is the diff between two compositions by row id, sorted by id, and the
// restart closure (03-c3-kernel.md:378).
type Plan struct {
	From, To Digest
	Entries  []PlanEntry
	Closure  []string
}

// ProfileContext is the profileContext service: the profile locations and
// layers (plugin-system-2026-11.md:178) and, by ADR-059 decision 7, the compose,
// admit and plan steps that reload calls (03-c3-kernel.md:84, :376-378). The
// methods other than Compose, Admit and Plan are docs-silent minimums.
type ProfileContext interface {
	// Profile is the name of the profile the process runs (07-c4-plugin-sdk.md:1353).
	Profile() string
	// Role is the role of the process; rows of other roles are never mounted
	// (07-c4-plugin-sdk.md:1360).
	Role() string
	// Layers lists the layers of the committed composition, in layer order.
	Layers() []Layer
	// Compose composes layers 0 to 4 into a candidate, pure
	// (03-c3-kernel.md:376). It touches no running fiber.
	Compose(ctx context.Context) (Composition, error)
	// Admit runs the gate, the bindings and the static graph over a candidate,
	// pure (03-c3-kernel.md:377). An error rejects the generation.
	Admit(ctx context.Context, c Composition) (Admission, error)
	// Plan diffs two compositions by row id and computes the restart closure,
	// pure (03-c3-kernel.md:378).
	Plan(ctx context.Context, from, to Composition) (Plan, error)
}

// RoleAll is the role of a process that mounts every row: "--role all mounts every
// row" (03-c3-kernel.md:547). Any other role mounts the rows whose roles list names
// it.
const RoleAll = "all"

// CatalogEntry is what the launcher knows of one plugin of the module table and
// its manifest (07-c4-plugin-sdk.md:1339; boot.ModuleEntry without the value).
type CatalogEntry struct {
	ID       string // "<owner>/<name>"
	Module   string
	Version  string
	Provides []string // key names
	Needs    []string // key names
	// Volatile lists the config paths of the plugin that a change commits in place,
	// with no restart (07-c4-plugin-sdk.md:1374; ADR-059 decision 10: listed in the
	// module table entry until S2a derives them from the config type). A path is
	// dotted: each segment names a map key, a segment "*" matches any key or list
	// index, and a path covers its whole subtree. A key that holds a dot or is "*"
	// cannot be named: only an ancestor path covers it.
	Volatile []string
}

// Catalog is the catalog service: the module table and manifests
// (plugin-system-2026-11.md:178). The docs give no methods; this is the minimum
// that plugin-manager needs to list and inspect (03-c3-kernel.md:71) and the
// loader to mount and to resolve the key names of a row.
type Catalog interface {
	// Entries lists every entry, sorted by id.
	Entries() []CatalogEntry
	// Lookup returns the entry of a plugin id.
	Lookup(id string) (CatalogEntry, bool)
	// Plugin returns the kernel.Plugin value of a plugin id, the one the module
	// table holds (07-c4-plugin-sdk.md:1339, :1355). The loader needs it to mount a
	// row (03-c3-kernel.md:82). Docs silent, open point: it hands the value to
	// every consumer of the open-class consumer side.
	Plugin(id string) (kernel.Plugin, bool)
	// Key returns the key that a row names by string in inject, isolate and
	// intercept (07-c4-plugin-sdk.md:1244, :1347): the kernel addresses keys by
	// value and has no lookup by name, so the launcher fills this read-only table
	// from the definitions of the module table. The name is the key's Name. ok is
	// false for a name the table does not hold.
	Key(name string) (kernel.AnyKey, bool)
}

// Cmdline is the cmdline service: argv (plugin-system-2026-11.md:178).
type Cmdline interface {
	// Args is the argument vector, program name excluded.
	Args() []string
	// Flag returns the value of a named flag, which is what the ${flag:name}
	// reference reads (03-c3-kernel.md:68).
	Flag(name string) (string, bool)
}

// Ready is the readiness latch (03-c3-kernel.md:67; 07-c4-plugin-sdk.md:1348).
// readiness sets it when the required set is ACTIVE and drops it while a provider
// restarts in place.
type Ready interface {
	Set()
	Drop()
	IsReady() bool
}

// Names of the kill switches (08-profiles-roadmap-adrs.md:126).
const (
	AgentsPaused      = "agents-paused"
	TelemetryDisabled = "telemetry-disabled"
)

// KillSwitches is the killSwitches service: named switches read from the
// environment, read-only (plugin-system-2026-11.md:178).
type KillSwitches interface {
	// On reports whether the named switch is on; an unknown name is off.
	On(name string) bool
}

// ExitReason says why a process exit is requested.
type ExitReason string

// The reasons: the three cases in which the process is replaced rather than
// restarted in place (03-c3-kernel.md:372).
const (
	ExitDisposeLeak ExitReason = "dispose-leak"   // a process-scope fiber leaked past its dispose deadline
	ExitRestoreFail ExitReason = "restore-fail"   // a restore failed
	ExitGaveUp      ExitReason = "restart-giveup" // restart-policy gave up on an infrastructure provider
)

// ExitRequest asks the launcher to end the process.
type ExitRequest struct {
	Reason ExitReason
	Detail string // free text for the log, for example the key or fiber
}

// AppExit is the appExit service: request a process exit
// (plugin-system-2026-11.md:178). The launcher owns signals and bounded shutdown
// (03-c3-kernel.md:63), so a request is not an exit: it never returns an exit code.
type AppExit interface {
	RequestExit(r ExitRequest)
}
