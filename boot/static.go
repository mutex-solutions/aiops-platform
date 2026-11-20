package boot

import (
	"fmt"
	"strings"
)

// Rules of admission and static-graph findings. They extend the rules of
// errors.go. Each cites the document line it enforces.
const (
	// RuleModuleMissing: a row's plugin is not in the module table
	// (03-c3-kernel.md:357; 08-profiles-roadmap-adrs.md:466).
	RuleModuleMissing Rule = "module-missing"
	// RuleProviderBinding: a plugin provides a provider-bound key and is not
	// in its binding (07-c4-plugin-sdk.md:553; gate check 7).
	RuleProviderBinding Rule = "provider-binding"
	// RuleConsumerBinding: a plugin needs a consumer-bound key and is not in
	// its binding (07-c4-plugin-sdk.md:554; gate check 7).
	RuleConsumerBinding Rule = "consumer-binding"
	// RuleWantBound: Want on a provider-bound key (07-c4-plugin-sdk.md:553).
	RuleWantBound Rule = "want-bound"
	// RuleIsolateBound: isolate of a provider-bound key by a plugin that is
	// not its bound provider, or on a required row
	// (07-c4-plugin-sdk.md:553; 08-profiles-roadmap-adrs.md:147).
	RuleIsolateBound Rule = "isolate-bound"
	// RuleSealedHook: a plugin listens on a sealed hook and is not listed
	// (07-c4-plugin-sdk.md:1340; gate check 7).
	RuleSealedHook Rule = "sealed-hook"
	// RuleBoundProvider: inject, isolate or group patched on a provider of a
	// bound key (08-profiles-roadmap-adrs.md:147; gate check 8).
	RuleBoundProvider Rule = "bound-provider-field"
	// RuleInterceptBound: intercept of a provider-bound key set outside the
	// bundle layer (08-profiles-roadmap-adrs.md:148; gate check 8).
	RuleInterceptBound Rule = "intercept-bound"
	// RuleLastProvider: the last enabled provider of a provider-bound key is
	// disabled (08-profiles-roadmap-adrs.md:145; compose leaves it to
	// admission).
	RuleLastProvider Rule = "last-provider"
	// RuleCycle: a dependency cycle (03-c3-kernel.md:359).
	RuleCycle Rule = "cycle"
	// RuleTwoProviders: two providers of one key in one realm
	// (03-c3-kernel.md:359).
	RuleTwoProviders Rule = "two-providers"
	// RuleBindingBroken: a binding of the distribution manifest names a
	// plugin that is not in the module table (03-c3-kernel.md:359).
	RuleBindingBroken Rule = "binding-broken"
	// RuleNoProvider: a need with no provider (03-c3-kernel.md:359, 361).
	RuleNoProvider Rule = "no-provider"
	// RuleRequiredMissing: a required row that is absent, disabled, detached or
	// not active in the selected role (08-profiles-roadmap-adrs.md:459).
	RuleRequiredMissing Rule = "required-missing"
	// RuleRequiredKey: a required key with no provider among the active rows.
	RuleRequiredKey Rule = "required-key"
	// RuleRequiredClosure: the static need closure of a required key or row
	// holds a provider outside the required set (gate check 7;
	// 08-profiles-roadmap-adrs.md:491).
	RuleRequiredClosure Rule = "required-closure"
)

// StaticError is an admission or static-graph error. It names the rows and
// keys it is about and the rule. Stage says which stage found it: "gate",
// "static graph" or "required set"; the failure matrix looks for the text
// "static graph" (test/failure-matrix/matrix.yaml).
type StaticError struct {
	Stage string
	Rule  Rule
	Rows  []string
	Keys  []string
	Msg   string
}

func (e *StaticError) Error() string { return fmt.Sprintf("%s: %s: %s", e.Stage, e.Rule, e.Msg) }

// StaticWarning is a grouped warning of the static graph or of the required
// audit: one finding that names several rows and keys.
type StaticWarning struct {
	Stage string
	Rule  Rule
	Rows  []string
	Keys  []string
	Msg   string
}

func (w StaticWarning) String() string { return fmt.Sprintf("%s: %s: %s", w.Stage, w.Rule, w.Msg) }

func quoted(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}
