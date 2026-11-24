// SPDX-License-Identifier: Apache-2.0

package reload

import "errors"

// The wire format of the socket <home>/run/reload.sock (ADR-062 decisions 6 to 8):
// JSON lines on a stream socket, version 1. The client sends one request; the row
// reload-socket answers with one or more reply lines and closes. Each line is one
// JSON object in UTF-8 ended by a newline. After a submit or an await the row writes
// the result as it stands and, when that is not final, the final result; an error
// line is always the last line.
//
// Within version 1 a field, an op value or a command value is added and never changed
// or removed, and a reader ignores a field it does not know; testdata/wire-v1.golden
// holds every form that version 1 has had. The row and its client, the operations
// plugin, cannot import each other, so both import these types.

// The files of the home that the transport reads, under RunDir.
const (
	RunDir     = "run"
	SocketFile = "reload.sock"
	LockFile   = "reload.lock"
	StatusFile = "status.json"
	// MaxSocketPath is the longest socket path that Linux holds: 108 bytes with the
	// terminating NUL.
	MaxSocketPath = 107
)

// WireVersion is the version of the wire format; MaxRequest bounds a request line,
// its newline included.
const (
	WireVersion = 1
	MaxRequest  = 4096
)

// The op values of a request.
const (
	OpSubmit = "submit"
	OpAwait  = "await"
)

// The commands a submit carries. The row submits Op{Source: SourcePrefix + command}.
const (
	CommandEnable  = "plugin enable"
	CommandDisable = "plugin disable"
	CommandRemove  = "plugin remove"
	CommandInstall = "plugin install"
	SourcePrefix   = "aiopsctl "
)

// Commands returns the commands of version 1.
func Commands() []string { return nil }

// The causes of an error, a closed list. The first nine are the errors of this
// definition, one for each that the error matches; the last four are the transport's.
const (
	CauseQueueFull    = "queue-full"
	CauseClosed       = "closed"
	CauseNested       = "nested"
	CauseUnknown      = "unknown"
	CauseAbandoned    = "abandoned"
	CauseVerdict      = "verdict"
	CauseDenied       = "denied"
	CauseRollback     = "rollback"
	CauseStateUnknown = "state-unknown"
	// CausePeer: the peer's uid is not the process's effective uid.
	CausePeer = "peer"
	// CauseBusy: the row serves its most connections already.
	CauseBusy = "busy"
	// CauseRequest: a malformed or oversized request, an op or a command the row does
	// not serve, or no request within its deadline.
	CauseRequest = "request"
	// CauseVersion: a version the row does not serve.
	CauseVersion = "version"
)

// Request is the one request of a connection.
type Request struct {
	V       int    `json:"v"`
	Op      string `json:"op"`
	Command string `json:"command,omitempty"` // submit only
	ID      ID     `json:"id,omitempty"`      // await only
}

// Reply is one reply line: a result or an error.
type Reply struct {
	V      int         `json:"v"`
	Result *WireResult `json:"result,omitempty"`
	Error  *WireError  `json:"error,omitempty"`
}

// WireError is an error with its causes. A client decides on the causes and prints
// the text.
type WireError struct {
	Text   string   `json:"text"`
	Causes []string `json:"causes"`
}

// Has reports whether cause is among e's causes.
func (e *WireError) Has(cause string) bool { return false }

// WireResult is Result field for field. Digests are lowercase hex, and Previous is
// the digest of the composition only: its rows hold row configs, which the client
// has in its files.
type WireResult struct {
	ID         ID            `json:"id"`
	Source     string        `json:"source"`
	Status     Status        `json:"status"`
	Stage      Stage         `json:"stage"`
	Error      *WireError    `json:"error,omitempty"`
	Generation uint64        `json:"generation"`
	Digest     string        `json:"digest"`
	Previous   string        `json:"previous"`
	Admission  WireAdmission `json:"admission"`
	Plan       WirePlan      `json:"plan"`
	Applied    []WireApplied `json:"applied"`
	Report     WireReport    `json:"report"`
	Rollback   *WireRollback `json:"rollback,omitempty"`
}

// WireAdmission is launch.Admission.
type WireAdmission struct {
	Denied []WireDenial `json:"denied"`
}

// WireDenial is launch.Denial.
type WireDenial struct {
	Row    string `json:"row"`
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

// WirePlan is launch.Plan.
type WirePlan struct {
	From    string          `json:"from"`
	To      string          `json:"to"`
	Entries []WirePlanEntry `json:"entries"`
	Closure []string        `json:"closure"`
}

// WirePlanEntry is launch.PlanEntry.
type WirePlanEntry struct {
	ID          string   `json:"id"`
	Action      string   `json:"action"`
	Changed     []string `json:"changed"`
	ConfigPaths []string `json:"configPaths"`
}

// WireApplied is loader.Result.
type WireApplied struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
}

// WireReport is composition.Report.
type WireReport struct {
	Pass bool      `json:"pass"`
	Rows []WireRow `json:"rows"`
}

// WireRow is composition.RowReport.
type WireRow struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	State   string `json:"state"`
	Detail  string `json:"detail"`
}

// WireRollback is Rollback.
type WireRollback struct {
	Plan       WirePlan      `json:"plan"`
	Applied    []WireApplied `json:"applied"`
	Report     WireReport    `json:"report"`
	Generation uint64        `json:"generation"`
	Error      *WireError    `json:"error,omitempty"`
}

// ProcessStatus is what a client reads of <home>/run/status.json, which
// internal/serve writes: the launcher's phase ("starting", "running", "stopping" or
// "stopped") and the profile it runs.
type ProcessStatus struct {
	Phase   string `json:"phase"`
	Profile string `json:"profile"`
}

// PhaseStopped is the phase of a process that ended.
const PhaseStopped = "stopped"

// NewError returns an error of the transport with its causes.
func NewError(text string, causes ...string) *WireError { return nil }

// EncodeError returns err with the causes it matches, nil for nil.
func EncodeError(err error) *WireError { return nil }

// EncodeResult returns r as the wire carries it.
func EncodeResult(r Result) WireResult { return WireResult{} }

// Line returns v as one line of JSON ended by a newline.
func Line(v any) []byte { return nil }

// DecodeRequest reads one request line.
func DecodeRequest(line []byte) (Request, error) { return Request{}, errNotYet }

// DecodeReply reads one reply line, which holds a result or an error.
func DecodeReply(line []byte) (Reply, error) { return Reply{}, errNotYet }

var errNotYet = errors.New("reload: the wire format is not built yet")
