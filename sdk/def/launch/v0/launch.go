// SPDX-License-Identifier: Apache-2.0

package launch

import (
	"context"
	"errors"
	"slices"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// ErrRefsUnchecked is why Admit rejects a candidate that mounts a row whose config holds
// a reference while no config-refs row can check it: none is ACTIVE in the root realm,
// or the candidate disables, removes or mounts config-refs again (ADR-059 decision 16).
var ErrRefsUnchecked = errors.New("launch: the references of the candidate cannot be checked")

// The six launch keys. Each is provider-bound, so only the module-table entry
// that the distribution manifest lists, the launcher, may provide it
// (03-c3-kernel.md:63; 07-c4-plugin-sdk.md:555, :1359). The names and the major
// are fixed by 03-c3-kernel.md:63.
var (
	// ProfileContextKey: class PC, provider-bound and consumer-bound
	// (03-c3-kernel.md:63; 07-c4-plugin-sdk.md:557): Commit sets what the tree runs,
	// so only the entries that the distribution manifest lists resolve the key.
	ProfileContextKey = kernel.NewKey[ProfileContext]("profileContext", 0, kernel.ProviderBound(), kernel.ConsumerBound())
	// CatalogKey: class P (03-c3-kernel.md:63).
	CatalogKey = kernel.NewKey[Catalog]("catalog", 0, kernel.ProviderBound())
	// CmdlineKey: class P (03-c3-kernel.md:63).
	CmdlineKey = kernel.NewKey[Cmdline]("cmdline", 0, kernel.ProviderBound())
	// ReadyKey: class P (03-c3-kernel.md:63).
	ReadyKey = kernel.NewKey[Ready]("ready", 0, kernel.ProviderBound())
	// KillSwitchesKey: class P (03-c3-kernel.md:63).
	KillSwitchesKey = kernel.NewKey[KillSwitches]("killSwitches", 0, kernel.ProviderBound())
	// AppExitKey: class PC, provider-bound and consumer-bound
	// (03-c3-kernel.md:63; 07-c4-plugin-sdk.md:557).
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

// Admission is the result of admitting a candidate: the denied rows. At boot a denied
// optional row is detached as disabled; reload rejects a generation that has one
// (ADR-059 decision 15). A denied required row is listed in Denied too, and Admit
// also returns an error for it (03-c3-kernel.md:358, :1361).
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

// RequiredSet is the required set of the process: the ids of the rows that must be
// ACTIVE and the key names that must each have an ACTIVE provider, both sorted
// (00-decisions.md:492; 08-profiles-roadmap-adrs.md:451-459). It is boot.RequiredSet
// with the role filter applied.
type RequiredSet struct {
	Rows []string
	Keys []string
}

// RowState is the state of one mounted row, as RequiredSet.Unmet reads it.
type RowState struct {
	ID     string
	Plugin string // "<owner>/<name>"
	State  kernel.State
}

// Unmet lists what of the set is not ACTIVE, sorted: "row:<id>" for a required row
// that is absent or not ACTIVE, and "key:<name>" for a required key that no ACTIVE
// row provides. provides gives the key names a plugin provides (Catalog.Lookup). It
// is the one reading of "the required set is ACTIVE" (03-c3-kernel.md:67, :392) that
// readiness and reload share.
func (s RequiredSet) Unmet(rows []RowState, provides func(plugin string) []string) []string {
	var out []string
	active := map[string]bool{}
	served := map[string]bool{}
	for _, r := range rows {
		if r.State != kernel.Active {
			continue
		}
		active[r.ID] = true
		for _, k := range provides(r.Plugin) {
			served[k] = true
		}
	}
	for _, id := range s.Rows {
		if !active[id] {
			out = append(out, "row:"+id)
		}
	}
	for _, k := range s.Keys {
		if !served[k] {
			out = append(out, "key:"+k)
		}
	}
	slices.Sort(out)
	return out
}

// ProfileContext is the profileContext service: the profile locations and
// layers (plugin-system-2026-11.md §5) and, by ADR-059 decision 7, the compose,
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
	// Admit runs the gate, the bindings and the static graph over a candidate
	// (03-c3-kernel.md:377), and checks the config of every row that the candidate
	// mounts with a new row (created, enabled, changed, or held by a changed group):
	// with the plugin's Decode, or, for a config that holds a reference, with the dry
	// run of config-refs, which returns no value (ADR-059 decision 16). It touches no
	// running fiber. An error rejects the generation; a reference that does not
	// resolve, or any reference while config-refs is not ACTIVE (ErrRefsUnchecked), is
	// one.
	Admit(ctx context.Context, c Composition) (Admission, error)
	// Plan diffs two compositions by row id and computes the restart closure,
	// pure (03-c3-kernel.md:378).
	Plan(ctx context.Context, from, to Composition) (Plan, error)
	// Committed is the composition the process runs: the one boot composed until
	// Commit says another. reload plans from it.
	Committed() Composition
	// Commit records c as the composition the tree runs: Committed returns it and
	// Required is its required set from then on. c is one that Compose returned.
	// reload calls it after a generation passed its verdict, and after a rollback
	// with the composition it restored (ADR-059 decision 15); the committer of S4a
	// replaces that caller. It returns an error for a composition that this launcher
	// did not compose, or has forgotten, and then changes nothing.
	Commit(c Composition) error
	// Required is the required set of the committed composition for the process role:
	// the distribution's rows and keys plus the rows that a layer marked required
	// (00-decisions.md:492). readiness and reload judge by it.
	Required() RequiredSet
	// RequiredOf is the required set of c for the process role, by the rule of
	// Required, which is RequiredOf(Committed()). reload judges a candidate by it
	// before it commits the candidate. c is one that Commit would take; for another it
	// returns an error.
	RequiredOf(c Composition) (RequiredSet, error)
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
// (plugin-system-2026-11.md §5). The docs give no methods; this is the minimum
// that plugin-manager needs to list and inspect (03-c3-kernel.md:71) and the
// loader to resolve the key names of a row. It holds no kernel.Plugin value: a
// holder of one runs code under that entry's bindings (03-c3-kernel.md:80), so the
// launcher gives the module table's values to the loader alone, in its config.
type Catalog interface {
	// Entries lists every entry, sorted by id.
	Entries() []CatalogEntry
	// Lookup returns the entry of a plugin id.
	Lookup(id string) (CatalogEntry, bool)
	// Key returns the key that a row names by string in inject, isolate and
	// intercept (07-c4-plugin-sdk.md:1244, :1347): the kernel addresses keys by
	// value and has no lookup by name, so the launcher fills this read-only table
	// from the definitions of the module table. The name is the key's Name. ok is
	// false for a name the table does not hold.
	Key(name string) (kernel.AnyKey, bool)
}

// Cmdline is the cmdline service: argv (plugin-system-2026-11.md §5).
type Cmdline interface {
	// Args is the argument vector, program name excluded.
	Args() []string
	// Flag returns the value of a named flag, which is what the ${flag:name}
	// reference reads (03-c3-kernel.md:68).
	Flag(name string) (string, bool)
	// Env returns the value of an environment variable of the process, which is what
	// the ${env:name} reference reads (03-c3-kernel.md:68). It filters nothing: the
	// AIOPS_REF_ prefix rule (08-profiles-roadmap-adrs.md:162) is config-refs's.
	// Added in S1: the launch definition had no environment reader (open point,
	// settled before S2a: ADR-059 decision 37).
	Env(name string) (string, bool)
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
// environment, read-only (plugin-system-2026-11.md §5).
type KillSwitches interface {
	// On reports whether the named switch is on; an unknown name is off.
	On(name string) bool
}

// ExitReason says why a process exit is requested.
type ExitReason string

// The reasons: the three cases in which the process is replaced rather than
// restarted in place (03-c3-kernel.md:372).
const (
	ExitDisposeLeak ExitReason = "dispose-leak"   // a process-scope fiber leaked past its drain or dispose deadline
	ExitRestoreFail ExitReason = "restore-fail"   // a restore failed
	ExitGaveUp      ExitReason = "restart-giveup" // restart-policy gave up on an infrastructure provider
)

// ExitRequest asks the launcher to end the process.
type ExitRequest struct {
	Reason ExitReason
	Detail string // free text for the log, for example the key or fiber
}

// AppExit is the appExit service: request a process exit
// (plugin-system-2026-11.md §5). The launcher owns signals and bounded shutdown
// (03-c3-kernel.md:63), so a request is not an exit: it never returns an exit code.
type AppExit interface {
	RequestExit(r ExitRequest)
}
