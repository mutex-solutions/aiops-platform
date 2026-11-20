// SPDX-License-Identifier: Apache-2.0

package composition

import "github.com/mutex-solutions/aiops-platform/sdk/kernel"

// Key is the composition key, class `-` (open) at major 0
// (03-c3-kernel.md:64; plugin-system-2026-11.md:190).
var Key = kernel.NewKey[Reader]("composition", 0)

// Scope is the scope of a generation. Layer 5 and the tenant scope arrive at S4b
// (03-c3-kernel.md:386), so at S1 only ScopeProcess has a generation.
type Scope string

// The two scopes of a generation (03-c3-kernel.md:384, :386).
const (
	ScopeProcess Scope = "process"
	ScopeTenant  Scope = "tenant"
)

// Digest is the digest over a composed tree (08-profiles-roadmap-adrs.md:184),
// the same 32 bytes as boot.Digest.
type Digest [32]byte

// Row is one row of a committed generation, read-only. Docs list what it holds
// (03-c3-kernel.md:64): the row, the hooks it declares, its registrations and its
// state. The field names are the definition's own, a minimal reading (open point).
type Row struct {
	ID            string
	Plugin        string       // "<owner>/<name>"
	Parent        string       // composed id of the enclosing group, or ""
	State         kernel.State // the row's fiber state now
	Hooks         []string     // hook names the row declares in Plugin.Hooks
	Registrations []string     // what the row registered, by label, as effects
}

// Generation is one committed generation of one scope.
type Generation struct {
	Scope  Scope
	Seq    uint64 // the generation number
	Digest Digest
	Rows   []Row
}

// Reader is what the composition key serves. It has no mutating method and every
// value it returns is a copy that the caller may keep.
type Reader interface {
	// Committed returns the committed generation of scope, and false when the
	// scope has none.
	Committed(scope Scope) (Generation, bool)
	// Declaring returns the rows of the committed generation of scope that declare
	// hook, which is how the owner of a gate knows its expected listeners
	// (07-c4-plugin-sdk.md:440).
	Declaring(scope Scope, hook string) []Row
}

// RowReport is the judged result of one row in a Report.
type RowReport struct {
	ID      string
	Outcome string // "pass", "fail" or "warning" (08-profiles-roadmap-adrs.md:203)
	State   kernel.State
	Detail  string
}

// Report is the verdict of a settled generation (03-c3-kernel.md:380).
type Report struct {
	Pass bool
	Rows []RowReport
}

// Applied is the payload of composition/applied: "the digest, the generation and
// the report" (03-c3-kernel.md:382; 07-c4-plugin-sdk.md:1387).
type Applied struct {
	Digest     Digest
	Generation uint64
	Report     Report
}

// NewApplied declares the composition/applied event, kind event, access open
// (07-c4-plugin-sdk.md:461), owned by the key of the fiber that publishes it:
// the reload key (03-c3-kernel.md:65), whose definition is not one of the four
// of this S1 task. That definition calls NewApplied(its key) once, beside its key
// (07-c4-plugin-sdk.md:381). Until then a test or a fake passes a stand-in
// owner. The event is not Scoped and not sealed.
func NewApplied(owner kernel.AnyKey) *kernel.Event[Applied] {
	return kernel.NewEvent[Applied]("composition/applied", kernel.OwnedBy(owner))
}
