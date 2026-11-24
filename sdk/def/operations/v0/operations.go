// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	reload "github.com/mutex-solutions/aiops-platform/sdk/def/reload/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the operations key: consumer-bound, class C, at major 0, so the release
// manifest lists the rows that may call it, the command rows and from S6a the plugin
// manager, and no contributed tool can wrap it (ADR-061 decision 15).
var Key = kernel.NewKey[Operations]("operations", 0, kernel.ConsumerBound())

// The errors of every provider, matched with errors.Is. Once the provider is
// released, every method returns an error that matches kernel.ErrInactive
// (07-c4-plugin-sdk.md:520).
var (
	// ErrUsage is a request that cannot run as asked: a method that needs a home
	// without one ($AIOPS_HOME unset or empty), a path that is not absolute, a name that
	// is not that of a profile, or a key password that the environment does not set. Its text names what is missing, and
	// aiopsctl answers it as a usage error (ADR-061 decisions 10 and 17).
	ErrUsage = errors.New("operations: usage")
	// ErrRefused is a request that the rules refuse: a form of trust add that ADR-020
	// decisions 5 and 6 do not allow, a key file of another kind, a key pair that
	// exists, a signature that no trusted key verifies, a profile that the binary does
	// not ship. A refused method writes nothing.
	ErrRefused = errors.New("operations: refused")
	// ErrLocked is a write that did not get the write lock of the home within its
	// bound; its text names the holder from the record in write.lock (ADR-061
	// decision 19).
	ErrLocked = errors.New("operations: write.lock is held by another writer")
	// ErrRebuildRequired is an install of in-process code that every clause of its
	// entry point passed, into a home whose distribution lacks its module: nothing is
	// written, and the Change holds the dist.yaml lines (08:397; ADR-060 decision 23).
	ErrRebuildRequired = errors.New("operations: rebuild-required")
	// ErrSubmission is a write whose submission did not show the change: a final result
	// other than passed, a pass that a later layer overrides, an error line, no final
	// result, or a process that did not answer. The Change says what is kept or
	// restored, and aiopsctl exits 1 (ADR-061 decision 17).
	ErrSubmission = errors.New("operations: the submission does not show the change")
)

// The names of the environment that the library reads through Caller.Env only.
const (
	HomeVar        = "AIOPS_HOME"         // the home, which a command row passes as a request's Home
	KeyPasswordVar = "AIOPS_KEY_PASSWORD" // the password of a private key, which may be empty (ADR-020 decision 4)
)

// Release in Home.Distributions is the default release root, whose key the release
// binary carries (ADR-020 decision 13).
const Release = "release"

// Caller is embedded in every request.
type Caller struct {
	// Env looks up a variable of the invocation's environment, the only way the
	// library reads it (ADR-061 decision 17); nil reads nothing.
	Env func(name string) (string, bool)
	// Progress takes the progress lines, stderr in aiopsctl; nil drops them.
	Progress io.Writer
}

// HomeRequest asks what a home holds. Every path of a request is absolute.
type HomeRequest struct {
	Caller
	Home    string
	Profile string // the profile whose bundles, departures and lock Home reports; "" for none
}

// Home is what a home holds, as its files are read without the lock (ADR-061
// decision 19). A home that does not exist reads empty, with the default root.
type Home struct {
	Profiles      []string // the profiles of the home, each a profiles/<p>/profile.yaml, sorted
	Signers       []Signer // signers of trust.yaml, in file order
	Distributions []string // key ids of distributions, Release among them; [Release] when the field is absent
	AllowUnsigned []string
	Records       []Record // the records under verified/, by digest
	Current       string   // verified/current: the digest of the distribution that dist use verified last
	Image         string   // image.yaml: the image that dist use verified last, by digest
	Snapshot      bool     // whether an interrupted write left its snapshot (ADR-061 decision 17)
	// For the profile of the request: its bundle list, the enabled departures of its
	// profile layer by row id, and the entries of its plugins.lock (ADR-047 decision 8).
	Bundles []string
	Enabled map[string]bool
	Locked  []Locked
}

// Locked is one entry of plugins.lock: a plugin id, the digest of its artifact and
// the digest of its content layer, "" for none. In-process code has no entry.
type Locked struct{ ID, Digest, Content string }

// Signer is one entry of signers of trust.yaml, its key by id (ADR-020 decisions 4
// to 7).
type Signer struct {
	KeyID                 string
	IDs, Runtimes, Signed []string
}

// Record is one record under verified/ (ADR-020 decision 10). ID is empty for a
// distribution.
type Record struct {
	Digest, Kind, Signer, ID string
}

// RenderRequest is profile render and profile verify: a request of the launcher's
// toolbox, with the caller. Its Home is "" for none, and then the shipped profiles are
// composed alone (ADR-061 decision 25).
type RenderRequest struct {
	Caller
	launch.RenderRequest
}

// ProfileRequest names a profile of a home: profile init and profile sanitize. A
// profile is a name, not a path.
type ProfileRequest struct {
	Caller
	Home, Profile string
}

// TrustRequest is trust add (ADR-020 decisions 5 and 6). With IDs it appends a
// signer limited to those id patterns, each <owner>/<name> or <owner>/*, of an owner
// other than aiops and contrib, and to Runtimes, inproc or remote, none covering packs
// only, trusted for the keys and hooks of Signed. With Distribution it appends a
// distribution root, and writes the default root first when it creates the field
// (ADR-020 decision 13). One of the two forms, not both.
type TrustRequest struct {
	Caller
	Home, KeyFile         string
	IDs, Runtimes, Signed []string
	Distribution          bool
}

// KeyRequest is key generate: the key pair <Prefix>.key and <Prefix>.pub in cosign's
// format, the private key encrypted with the password of KeyPasswordVar (ADR-020
// decisions 4 and 21). It refuses a prefix whose files exist.
type KeyRequest struct {
	Caller
	Prefix string
}

// An image is an OCI image reference, <registry>/<repository> with :<tag> or
// @sha256:<digest>, and optionally oci:// before it, or, since 0.3, the absolute path of
// the OCI image layout of one image, which dist build writes. The builder writes the
// distribution manifest beside its image as a referrer of it, of artifactType
// DistributionType, whose one layer of DistributionLayer holds the manifest's bytes;
// dist sign pushes one more referrer of that type, with the manifest and a layer of
// the bundle media type of ADR-020 decision 2, the manifest's bundle (ADR-020
// decision 12; ADR-044 decision 1). A request reads the credentials of a registry from
// config.json under DOCKER_CONFIG, or under .docker/ of HOME, of the caller's
// environment, and reaches a registry on the loopback interface over plain HTTP.
const (
	DistributionType  = "application/vnd.aiops.distribution.v1"
	DistributionLayer = "application/vnd.aiops.distribution.manifest.v1+yaml"
)

// BuildRequest is dist build (added at 0.3): the builder's request of the toolbox
// (ADR-044; ADR-060 decision 22), whose dist.yaml path is absolute. It writes no home.
type BuildRequest struct {
	Caller
	launch.BuildRequest
}

// SignDistRequest is dist sign: the distribution manifest of Image signed with the
// private key file Key and pushed with its bundle as one referrer of the image. It
// refuses an image whose referrers hold no distribution manifest, or two.
type SignDistRequest struct {
	Caller
	Image, Key string
}

// UseRequest is dist use: the bundle of each distribution referrer of Image verified
// with each key of distributions, then the record of the manifest's digest,
// verified/current and image.yaml, which names the image by digest, written, or
// nothing (ADR-020 decisions 10 and 12; ADR-047 decision 13).
type UseRequest struct {
	Caller
	Home, Image string
}

// PackRequest is plugin pack: the plugin directory Dir, which holds plugin.yaml and
// bundle.yaml, go.mod for in-process code and content/ and fixtures/ for a pack,
// written as one artifact into the OCI image layout Out, a directory that does not
// exist or is empty, with its attestation as a referrer, unsigned (ADR-047 decisions 1
// to 3; ADR-020 decisions 9 and 24).
type PackRequest struct {
	Caller
	Dir, Out string
}

// SignRequest is plugin sign: the artifact of the layout Out and its attestation
// signed with the private key file Key, each signature a referrer of the artifact
// (ADR-020 decisions 2 and 9).
type SignRequest struct {
	Caller
	Out, Key string
}

// PushRequest is plugin push: the artifact of the layout Out with its referrers copied
// to Registry, <registry>/<repository>, and tagged with its version.
type PushRequest struct {
	Caller
	Out, Registry string
}

// Pushed is the spec by digest of what plugin push pushed:
// oci://<registry>/<repository>@sha256:<hex> (ADR-047 decision 1).
type Pushed struct{ Spec string }

// SpecRequest names an artifact by its spec: oci:// and a reference, or the absolute
// path of an OCI image layout. Home is the home whose trust.yaml plugin verify reads,
// and Builder the builder of its suites, image or local.
type SpecRequest struct {
	Caller
	Spec, Home, Builder string
}

// Inspected is what plugin inspect read of an artifact, its manifest and its bundle
// only (ADR-060 decision 58, reading 2): the digest of its OCI manifest, its id,
// version and runtimes, each row of its bundle and the paths of its content. Signer is
// "": inspect verifies no signer, and install verifies the signature over Digest
// (ADR-047 decision 2).
type Inspected struct {
	Digest, ID, Version string
	Runtimes            []string
	Rows                []Row
	Content             []string
	Signer              string
}

// Row is one row that a bundle inserts: its id, plugin and config.
type Row struct {
	ID, Plugin string
	Config     map[string]any
}

// Verified is what plugin verify found (ADR-060 decisions 31 and 46): the digest, the
// signer whose signature verified, the steps in order, signature, attestation and
// suites, and whether every step passed.
type Verified struct {
	Digest, Signer string
	Steps          []VerifyStep
	Passed         bool
}

// VerifyStep is one step of plugin verify with its outcome, pass, fail or not-built,
// and why.
type VerifyStep struct{ Name, Outcome, Reason string }

// NewRequest is plugin new (ADR-060 decision 24): the template of Registry, a registry
// of the release or Connector, for the plugin ID in Lang, written into Dir, which does
// not exist or is empty. Contrib writes the owner contrib. For Connector, Against is the
// signed manifest of the release whose instantiable plugins the pack's rows take, ""
// for the one this binary embeds, and Plugins, when set, names the plugins whose rows
// it writes (ADR-047 decision 12).
type NewRequest struct {
	Caller
	Registry, ID, Lang string
	Contrib            bool
	Against            string
	Plugins            []string
	Dir                string
}

// Connector is the Registry of NewRequest that writes a connector pack.
const Connector = "connector"

// CheckRequest is plugin check on the plugin in Dir (07-c4-plugin-sdk.md:1565-1574).
// Write writes the generated half of plugin.yaml and the generated tests first, as
// plugingen does; Against is the signed manifest of the release that step 7 gates
// against, "" for the one this binary embeds.
type CheckRequest struct {
	Caller
	Dir     string
	Write   bool
	Against string
}

// The outcomes of a step of plugin check.
const (
	StepPass          = "pass"
	StepFail          = "fail"
	StepNotApplicable = "not applicable"
	StepSkipped       = "skipped" // a step after a failed one: the check stops at the first
)

// Steps are the names of the steps of plugin check, step n at n-1
// (07-c4-plugin-sdk.md:1565-1574).
var Steps = [8]string{"manifest", "parity", "static", "alone", "conformance", "effects", "pack and verify", "scenario"}

// Step is one step of plugin check: its number, 1 to 8, its name, its outcome and its
// findings. A failed obligation is a diagnostic whose Check is the obligation id.
type Step struct {
	N           int
	Name        string
	Outcome     string
	Diagnostics []launch.Diagnostic
}

// CheckReport is what plugin check found: the eight steps in order, and the
// attestation it would sign, an in-toto Statement v1 of the conformance predicate
// (ADR-020 decision 9), which is nil unless every step passed or does not apply. plugin
// pack records the same statement, with the subject of the artifact it packs.
type CheckReport struct {
	Steps       []Step
	Attestation []byte
}

// AttestationType is the predicate type of the conformance attestation (ADR-020
// decisions 9 and 25).
const AttestationType = "https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1"

// Attested is what the attestation says of one definition: its version, and for a
// definition whose suite ran the suite revision and the halves that ran (ADR-060
// decisions 18 and 56).
type Attested struct {
	Version string   `json:"version"`
	Suite   *int     `json:"suite,omitempty"`
	Halves  []string `json:"halves,omitempty"`
}

// Scenario is the scenario a pack ran: its name, its version and its halves (ADR-060
// decision 49).
type Scenario struct {
	Name    string   `json:"name"`
	Version int      `json:"version"`
	Halves  []string `json:"halves"`
}

// Statement returns the conformance attestation of the plugin id at version whose
// subject has the sha256 digest hex: the definitions, the scenario of a pack, nil for
// code, and the generated tests that did not run (ADR-020 decision 9).
func Statement(id, version, hex string, defs map[string]Attested, scenario *Scenario, notRun []string) []byte {
	pred := map[string]any{"definitions": defs}
	if scenario != nil {
		pred["scenario"] = scenario
	}
	if len(notRun) > 0 {
		pred["notRun"] = notRun
	}
	b, _ := json.Marshal(map[string]any{"_type": "https://in-toto.io/Statement/v1", "predicateType": AttestationType,
		"subject":   []any{map[string]any{"name": id + "@" + version, "digest": map[string]string{"sha256": hex}}},
		"predicate": pred})
	return b
}

// Passed reports whether no step failed.
func (r CheckReport) Passed() bool {
	for _, s := range r.Steps {
		if s.Outcome == StepFail {
			return false
		}
	}
	return len(r.Steps) > 0
}

// InstallRequest is plugin install (ADR-047 decisions 5 to 10 and 17): the artifact of
// Spec, as SpecRequest names one, into Profile of Home. ConnectWait and Wait bound the
// submission (ADR-062 decisions 9 and 10); zero takes 60 seconds and 10 minutes.
type InstallRequest struct {
	Caller
	Home, Profile, Spec string
	ConnectWait, Wait   time.Duration
}

// RowRequest is plugin enable and plugin disable: Row, a row id of the composition of
// Profile, an installed row as <group>:<row>.
type RowRequest struct {
	Caller
	Home, Profile, Row string
	ConnectWait, Wait  time.Duration
}

// RemoveRequest is plugin remove: ID, the plugin id of an artifact installed into
// Profile.
type RemoveRequest struct {
	Caller
	Home, Profile, ID string
	ConnectWait, Wait time.Duration
}

// GateRequest is plugin gate <spec> --against <dist.yaml>: the artifact of Spec gated
// against the published signed manifest at the absolute path Against (08:493).
type GateRequest struct {
	Caller
	Spec, Against string
}

// Written is the paths a method wrote outside the home.
type Written struct{ Paths []string }

// Change is what a method that writes the home did (ADR-061 decision 17).
type Change struct {
	Written []string // the home paths written, in rename order
	Notes   []string // what the operator is told: a snapshot restored, an item kept
	Outcome Outcome  // NotRunning or RestartRequired, "" otherwise
	Reason  string   // why, for RestartRequired
	// The submission over the socket of ADR-062 by Install, Enable, Disable and Remove
	// (ADR-061 decision 44): the final result as the wire carries it; or, with none, the
	// error line before an id, or each operation id and the last status.
	Result *reload.WireResult
	Error  *reload.WireError
	IDs    []reload.ID
	Last   reload.Status
	// Install: the clauses of the gate, and with rebuild-required the dist.yaml lines
	// of the module (08:397).
	Verdict   launch.Verdict
	DistLines []string
}

// Outcome is the outcome of the submission that ends every write of the home
// (ADR-061 decision 17).
type Outcome string

// The outcomes of a write that submits nothing and the reasons of RestartRequired
// (ADR-061 decision 17). Version 1 of the wire of ADR-062 carries plugin install,
// enable, disable and remove only, whose methods return the outcome of its decision
// 11 when they join; the writes of 0.1 submit nothing.
const (
	NotRunning      Outcome = "not-running"      // no run/status.json names a running process: the change applies at its next start
	RestartRequired Outcome = "restart-required" // kept; the running process takes it at its next start

	ReasonNotSubmitted = "not-submitted" // a write the wire does not carry: the running process reads it at its next start
	ReasonImage        = "image"         // a new image is a new process (07-c4-plugin-sdk.md:1869)
	ReasonNoAnswer     = "no-answer"     // a process runs the profile and its socket did not answer within ConnectWait
)

// The names of the rows of the table of ADR-062 decision 11 that end with no final
// result (ADR-061 decision 44). The text of ErrSubmission begins with one, or with
// the status of a final result that does not show the change.
const (
	SubmitRefused  = "refused"         // an error line before an id: Error; the files are restored
	NoFinalResult  = "no-final-result" // no final result on the second submission either: IDs and Last
	VerdictUnknown = "verdict-unknown" // Wait elapsed, or the connection was lost, after the operation was accepted: IDs and Last; kept
)

// Operations is what the key serves (ADR-061 decision 16). Each method takes the
// caller's context first. A method that writes the home holds its write lock from
// before the checks that read the home to the end of its submission, restores a
// snapshot that an interrupted write left before it writes and says so, and on a
// failure or the end of ctx restores what it changed (ADR-061 decisions 17 and 19).
type Operations interface {
	// Home reads what a home holds; it takes no lock.
	Home(ctx context.Context, r HomeRequest) (Home, error)
	// Render is profile render: it composes and admits a profile as the server would,
	// with the toolbox of the launcher, and writes nothing.
	Render(ctx context.Context, r RenderRequest) (launch.Rendered, error)
	// VerifyProfile is profile verify: it renders a profile, cli when none is named,
	// for its departures from the release bindings, with no dump, and writes nothing.
	VerifyProfile(ctx context.Context, r RenderRequest) (launch.Rendered, error)
	// InitProfile is profile init: it copies the shipped template of the profile into
	// the home, unless the home holds the profile, which it keeps and says so, and
	// submits the profile when it wrote it (ADR-060 decision 27).
	InitProfile(ctx context.Context, r ProfileRequest) (Change, error)
	// SanitizeProfile is profile sanitize: it renames each layer file of the home that
	// stops the composition of the profile, cli when none is named, to
	// <name>.bak-<time>, and submits the profile when it set one aside (ADR-061
	// decision 41).
	SanitizeProfile(ctx context.Context, r ProfileRequest) (Change, error)
	// AddTrust is trust add. It writes trust.yaml and submits the profile that
	// run/status.json names.
	AddTrust(ctx context.Context, r TrustRequest) (Change, error)
	// GenerateKey is key generate. It writes no home.
	GenerateKey(ctx context.Context, r KeyRequest) (Written, error)
	// Build is dist build (added at 0.3): what the toolbox's builder built, the image,
	// the signed manifest and the site's aiopsctl. It writes no home; it refuses a
	// dist.yaml path or a module cache that is not absolute.
	Build(ctx context.Context, r BuildRequest) (launch.Built, error)
	// SignDist is dist sign. It writes no home, and Written names the referrer it
	// pushed by digest.
	SignDist(ctx context.Context, r SignDistRequest) (Written, error)
	// UseDist is dist use. It writes the record of the distribution, then
	// verified/current, then image.yaml, and answers RestartRequired with ReasonImage.
	UseDist(ctx context.Context, r UseRequest) (Change, error)
	// Pack is plugin pack. It writes no home; Written names the layout.
	Pack(ctx context.Context, r PackRequest) (Written, error)
	// Sign is plugin sign. It writes no home; Written names the layout.
	Sign(ctx context.Context, r SignRequest) (Written, error)
	// Push is plugin push. It writes no home.
	Push(ctx context.Context, r PushRequest) (Pushed, error)
	// Inspect is plugin inspect: it fetches the manifest and the bundle of an artifact
	// only, and installs and runs nothing.
	Inspect(ctx context.Context, r SpecRequest) (Inspected, error)
	// Verify is plugin verify: it verifies the signature with the signers of trust.yaml
	// that cover the artifact, then the attestation with the same key, before it uses
	// any other blob, then runs the suites of plugin check on what the artifact
	// carries. A signature or an attestation that does not verify is a refusal; a suite
	// step not built is a step that did not pass.
	Verify(ctx context.Context, r SpecRequest) (Verified, error)
	// Gate is plugin gate --against: the clauses of that entry point on the artifact,
	// with no signer verified. It writes nothing.
	Gate(ctx context.Context, r GateRequest) (launch.Verdict, error)
	// Install is plugin install. It verifies the signature and the attestation as
	// Verify does, runs every clause of the entry point plugin install and the group
	// rule of ADR-047 decision 17, and only then writes the kept files under
	// bundles/<id>/ and content/<id>/, the record, plugins.lock for a pack and the
	// bundle list, and submits. A denial writes nothing; so does rebuild-required.
	Install(ctx context.Context, r InstallRequest) (Change, error)
	// Enable and Disable write the row's enabled departure into the profile layer,
	// keep it only when the changed profile renders, and submit. Disable refuses what
	// CannotDisable refuses.
	Enable(ctx context.Context, r RowRequest) (Change, error)
	Disable(ctx context.Context, r RowRequest) (Change, error)
	// Remove detaches the artifact's bundle and its lock entry, keeps the departures of
	// its rows in the profile layer and names them, submits, and deletes bundles/<id>/
	// and content/<id>/ when the change was kept and no other profile names the id.
	Remove(ctx context.Context, r RemoveRequest) (Change, error)
	// New is plugin new (added at 0.2). It writes the files of the template into Dir
	// and returns their paths, sorted; it refuses a Dir that holds anything, a template
	// that the toolbox has not and a language other than go, and writes nothing then.
	// It writes no home.
	New(ctx context.Context, r NewRequest) (Written, error)
	// Check is plugin check (added at 0.2). It runs the eight steps through the
	// toolbox, stops at the first failed step and reports the rest as skipped; a failed
	// step is a report, not an error. It writes no home, and the author's directory only
	// with Write.
	Check(ctx context.Context, r CheckRequest) (CheckReport, error)
}
