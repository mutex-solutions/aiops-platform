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
	// RuleManifest: a plugin manifest breaks a rule of schema 1.0 or names a major
	// this reader does not know; gate check 1 denies it (ADR-048 decisions 3 and 24).
	RuleManifest Rule = "manifest"
	// RuleAttestation: an attestation statement breaks its format (ADR-020 decision 9).
	RuleAttestation Rule = "attestation"
	// RuleTrust: trust.yaml breaks its format or holds a field that waits for S6a
	// (ADR-020 decisions 5, 6 and 26).
	RuleTrust Rule = "trust"
	// RuleRecord: a record under verified/ breaks its format or vouches for another
	// digest (ADR-020 decision 10).
	RuleRecord Rule = "record"
	// RuleLock: plugins.lock breaks its format (ADR-047 decision 8).
	RuleLock Rule = "lock"
	// RuleKept: a file that install kept is not the one whose digest install verified;
	// check 5 denies every row of the artifact (ADR-047 decision 18).
	RuleKept Rule = "kept"
	// RuleDist: a signed distribution manifest breaks its format (ADR-060 decision 20).
	RuleDist Rule = "distribution"
	// RuleAuthorAPI: gate check 1, requires.pluginApi is not the author API the
	// distribution serves, or runtimes lists wasm (ADR-060 decisions 8 and 58).
	RuleAuthorAPI Rule = "author-api"
	// RuleUnserved: gate check 2, a definition version of requires is not served.
	RuleUnserved Rule = "unserved-definition"
	// RuleDeprecated: a warning of gate check 2, never a denial: an admitted row uses a
	// deprecated major (08-profiles-roadmap-adrs.md:510; ADR-060 decision 9).
	RuleDeprecated Rule = "deprecated-major"
	// RuleStability: gate check 4, a manifest below the profile's minStability.
	RuleStability Rule = "stability"
	// RuleModule: gate check 5, a Go module absent from a trusted distribution
	// manifest or not the one it lists (ADR-020 decision 12).
	RuleModule Rule = "module"
	// RuleSigner: gate check 5, no verified signer recorded beside the digest, one
	// that trust.yaml does not list for the id and runtimes (ADR-020 decisions 6 to 8),
	// or, for an artifact with no module under a reserved owner, one that is not the
	// release key the binary carries (ADR-048 decision 14).
	RuleSigner Rule = "signer"
	// RuleLicence: gate check 6, a module under a licence outside the L7 allow-list and
	// not accepted, or an acceptance in the release (08-profiles-roadmap-adrs.md:1346).
	RuleLicence Rule = "licence"
	// RuleSignedClass: gate check 7, a signed key or hook used by an untrusted signer
	// (ADR-020 decision 7).
	RuleSignedClass Rule = "signed-class"
	// RuleFake: gate check 7, a fake or sdk/testkit package in the package graph of a
	// server binary (ADR-060 decision 58).
	RuleFake Rule = "fake"
	// RuleInstalled: gate check 8, the bundle of an installed artifact breaks the
	// insert-only rule (ADR-047 decisions 11 and 19; ADR-060 decision 57).
	RuleInstalled Rule = "installed-artifact"
	// RuleGroupClash: gate check 8, the group of an installed artifact takes the id of a
	// row composed before it, so none of its rows is inserted (ADR-047 decision 6).
	RuleGroupClash Rule = "group-clash"
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
