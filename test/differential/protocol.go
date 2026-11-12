// SPDX-License-Identifier: Apache-2.0

// Package differential defines the test-only, implementation-neutral S0 script
// and ordered trace protocol. It imports no kernel or candidate implementation.
package differential

const (
	ScriptSchema   = "s0-script/v1"
	TraceSchema    = "s0-trace/v1"
	CriteriaSHA256 = "628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c"
	SourceCommit   = "fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159"
)

// Script contains only data. Plugin bodies and cleanup are neutral API actions,
// not executable snippets or implementation-specific mock results.
type Script struct {
	Schema      string        `json:"schema"`
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Covers      []string      `json:"covers"`
	Contexts    []ContextSpec `json:"contexts,omitempty"`
	Plugins     []PluginSpec  `json:"plugins,omitempty"`
	Steps       []Action      `json:"steps"`
	Expect      []Assertion   `json:"expect,omitempty"`
	Diagnostic  string        `json:"diagnostic,omitempty"`
	Attribution []string      `json:"attribution,omitempty"`
}

type ContextSpec struct {
	ID        string         `json:"id"`
	Parent    string         `json:"parent"`
	Isolate   *IsolationSpec `json:"isolate,omitempty"`
	Intercept *InterceptSpec `json:"intercept,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type IsolationSpec struct {
	Key string `json:"key"`
	// Equal nonempty labels join; an omitted label allocates a fresh realm.
	Label string `json:"label,omitempty"`
}

type InterceptSpec struct {
	Key    string `json:"key"`
	Config any    `json:"config"`
}

type PluginSpec struct {
	ID      string         `json:"id"`
	Needs   map[string]any `json:"needs,omitempty"`
	Apply   []Action       `json:"apply"`
	Cleanup []Action       `json:"cleanup,omitempty"`
	// Update: empty/default restarts; retain vetoes restart; reject throws.
	Update string `json:"update,omitempty"`
	// ConfigTransform is a shallow merge performed by an internal/config hook.
	ConfigTransform map[string]any `json:"configTransform,omitempty"`
}

// Action is the union of documented operations in PROTOCOL.md. Await defaults
// true for externally started update/dispose/dispatch, false for mount and
// selfDispose. No sleep action exists: gate/waitMark/release are causal barriers.
type Action struct {
	Op      string         `json:"op"`
	Context string         `json:"context,omitempty"`
	Fiber   string         `json:"fiber,omitempty"`
	Plugin  string         `json:"plugin,omitempty"`
	Key     string         `json:"key,omitempty"`
	Label   string         `json:"label,omitempty"`
	To      string         `json:"to,omitempty"`
	Mode    string         `json:"mode,omitempty"`
	Task    string         `json:"task,omitempty"`
	Error   string         `json:"error,omitempty"`
	Value   any            `json:"value,omitempty"`
	Config  any            `json:"config,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
	Base    any            `json:"base,omitempty"`
	Head    any            `json:"head,omitempty"`
	Program []Action       `json:"program,omitempty"`
	Cleanup []Action       `json:"cleanup,omitempty"`
	Keys    []string       `json:"keys,omitempty"`
	Await   *bool          `json:"await,omitempty"`
	Strict  *bool          `json:"strict,omitempty"`
	Catch   bool           `json:"catch,omitempty"`
	Prepend bool           `json:"prepend,omitempty"`
	Global  bool           `json:"global,omitempty"`
	Symbol  bool           `json:"symbol,omitempty"`
	Count   int            `json:"count,omitempty"`
}

type Assertion struct {
	At string `json:"at"`
	// Path is an RFC 6901 JSON pointer into the checkpoint.
	Path   string `json:"path"`
	Equals any    `json:"equals"`
}

// Trace excludes run provenance so comparison never silently ignores metadata.
// A separate RunRecord binds this raw semantic record to executable/script hashes.
type Trace struct {
	Schema      string       `json:"schema"`
	Scenario    string       `json:"scenario"`
	Events      []Event      `json:"events"`
	Checkpoints []Checkpoint `json:"checkpoints"`
	Diagnostic  *Diagnostic  `json:"diagnostic,omitempty"`
}

// Event preserves every semantic event in arrival order. Data is deliberately
// explicit JSON rather than a string containing an unparseable debug log.
type Event struct {
	Seq  int            `json:"seq"`
	Kind string         `json:"kind"`
	Data map[string]any `json:"data"`
}

type Checkpoint struct {
	Label      string              `json:"label"`
	States     []FiberSnapshot     `json:"states"`
	Provisions []ProvisionSnapshot `json:"provisions"`
	Outputs    map[string]any      `json:"outputs"`
}

type FiberSnapshot struct {
	Fiber    string `json:"fiber"`
	State    string `json:"state"`
	FiberUID any    `json:"fiberUID"`
	Revision int    `json:"revision"`
	// Epoch is null when inactive; otherwise the revision and required provision
	// identities are recorded separately, avoiding a runtime-specific string.
	Epoch           *Epoch `json:"epoch"`
	RawConfig       any    `json:"rawConfig"`
	EffectiveConfig any    `json:"effectiveConfig"`
}

type Epoch struct {
	Revision int       `json:"revision"`
	Needs    []Binding `json:"needs"`
}

type Binding struct {
	Key          string `json:"key"`
	ProvisionUID any    `json:"provisionUID"`
}

type ProvisionSnapshot struct {
	Key          string `json:"key"`
	Realm        string `json:"realm"`
	Provider     string `json:"provider"`
	ProvisionUID any    `json:"provisionUID"`
	Value        any    `json:"value"`
	Active       bool   `json:"active"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	BoundMS int    `json:"boundMS"`
	Message string `json:"message"`
}

// RunRecord is execution evidence, not part of semantic equality.
type RunRecord struct {
	Schema               string            `json:"schema"`
	CriteriaSHA256       string            `json:"criteriaSHA256"`
	SourceCommit         string            `json:"sourceCommit"`
	StartedUTC           string            `json:"startedUTC"`
	EndedUTC             string            `json:"endedUTC"`
	NodeVersion          string            `json:"nodeVersion"`
	ExecutableSHA256     string            `json:"executableSHA256"`
	OracleSHA256         string            `json:"oracleSHA256"`
	VendorManifestSHA256 string            `json:"vendorManifestSHA256"`
	Scripts              map[string]string `json:"scripts"`
	ExitCode             int               `json:"exitCode"`
	ResolverSHA256       string            `json:"resolverSHA256,omitempty"`
	BaselineCommit       string            `json:"baselineCommit,omitempty"`
	Command              []string          `json:"command,omitempty"`
	Result               string            `json:"result,omitempty"`
	AssertionError       map[string]any    `json:"assertionError,omitempty"`
	NativeAttempts       int               `json:"nativeAttempts"`
	ResolvedPackages     []string          `json:"resolvedPackages"`
	TraceSHA256          string            `json:"traceSHA256"`
}
