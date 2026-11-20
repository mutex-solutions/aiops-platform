package boot

import (
	"fmt"
	"strings"
)

// Rule names the rule that a composition error or warning is about.
type Rule string

// Rules of composition errors. Each cites the document line it enforces.
const (
	// RuleMissing: profile.yaml, a named bundle or a --patch file must exist
	// (08-profiles-roadmap-adrs.md:160).
	RuleMissing Rule = "file-missing"
	// RuleRead: a file exists but cannot be read.
	RuleRead Rule = "file-read"
	// RuleEmpty: a layer file that exists but is empty fails; disable a layer
	// with [] (08-profiles-roadmap-adrs.md:160).
	RuleEmpty Rule = "empty-file"
	// RuleLimit: a size, depth or row-count bound was passed.
	RuleLimit Rule = "limit"
	// RuleSyntax: the file is not valid YAML.
	RuleSyntax Rule = "syntax"
	// RuleDocument: more than one YAML document in a file.
	RuleDocument Rule = "multi-document"
	// RuleAlias: an anchor, alias or merge key; they are not accepted.
	RuleAlias Rule = "alias"
	// RuleTag: an explicit YAML tag; they are not accepted.
	RuleTag Rule = "tag"
	// RuleDuplicateKey: a key twice in one mapping.
	RuleDuplicateKey Rule = "duplicate-key"
	// RuleUnknownField: a field that is not in the schema of the item.
	RuleUnknownField Rule = "unknown-field"
	// RuleType: a value of the wrong YAML type, or a scalar that is ambiguous.
	RuleType Rule = "type"
	// RuleSyntaxID: an id, name or plugin id that is not well formed.
	RuleSyntaxID Rule = "id-syntax"
	// RuleDuplicateID: insert adds a new id only; an existing id fails in
	// every layer (08-profiles-roadmap-adrs.md:143, 158).
	RuleDuplicateID Rule = "insert-existing-id"
	// RuleFixedField: plugin and roles are fixed at insert and no patch
	// changes them (08-profiles-roadmap-adrs.md:143, 158).
	RuleFixedField Rule = "fixed-field"
	// RuleRequiredLower: required may be raised, never lowered
	// (08-profiles-roadmap-adrs.md:146).
	RuleRequiredLower Rule = "required-lower"
	// RuleRequiredRow: inject, isolate and group are never patched on a
	// required row (08-profiles-roadmap-adrs.md:147).
	RuleRequiredRow Rule = "required-row-field"
	// RuleRoles: every row of a server bundle states roles from the closed
	// vocabulary (08-profiles-roadmap-adrs.md:156).
	RuleRoles Rule = "roles"
	// RuleReference: a malformed ${scheme:value} reference
	// (08-profiles-roadmap-adrs.md:162).
	RuleReference Rule = "reference"
	// RuleGroup: a core:group or core:include row that is malformed, or a
	// patch of the config of a row whose config is its children
	// (08-profiles-roadmap-adrs.md:159).
	RuleGroup Rule = "group"
	// RuleInclude: a core:include that cannot be resolved.
	RuleInclude Rule = "include"
	// RuleMatrix: the failure matrix file breaks its schema.
	RuleMatrix Rule = "matrix"
)

// Error is a composition error. It names the file, the line when the YAML
// library gives one (0 otherwise), the row id and the rule.
type Error struct {
	File string
	Line int
	Row  string
	Rule Rule
	Msg  string
	Err  error // underlying cause, may be nil
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(e.File)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	if e.Row != "" {
		fmt.Fprintf(&b, ": row %q", e.Row)
	}
	fmt.Fprintf(&b, ": %s: %s", e.Rule, e.Msg)
	return b.String()
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// WarningKind classifies a warning.
type WarningKind string

// WarnUnknownID: a patch targets an id that no row has; it is skipped
// (08-profiles-roadmap-adrs.md:160).
const WarnUnknownID WarningKind = "unknown-id"

// Warning is a composition warning. Composition goes on after one.
type Warning struct {
	Kind WarningKind
	File string
	Line int
	Row  string
	Msg  string
}

func (w Warning) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", w.File, w.Line, w.Kind, w.Msg)
}
