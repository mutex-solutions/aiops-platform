// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"context"
	"errors"
	"io"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
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
	Home string
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
}

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
// @sha256:<digest>, and optionally oci:// before it. The builder pushes the
// distribution manifest beside its image as a referrer of it, of artifactType
// DistributionType, whose one layer of DistributionLayer holds the manifest's bytes;
// dist sign pushes one more referrer of that type, with the manifest and its bundle, a
// layer of BundleType (ADR-020 decisions 2 and 12; ADR-044 decision 1). A request
// reads the credentials of a registry from config.json under DOCKER_CONFIG, or under
// .docker/ of HOME, of the caller's environment, and reaches a registry on the
// loopback interface over plain HTTP.
const (
	DistributionType  = "application/vnd.aiops.distribution.v1"
	DistributionLayer = "application/vnd.aiops.distribution.manifest.v1+yaml"
	BundleType        = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

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

// Written is the paths a method wrote outside the home.
type Written struct{ Paths []string }

// Change is what a method that writes the home did (ADR-061 decision 17).
type Change struct {
	Written []string // the home paths written, in rename order
	Notes   []string // what the operator is told: a snapshot restored
	Outcome Outcome  // the outcome of the submission
	Reason  string   // why, for RestartRequired
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
	// SignDist is dist sign. It writes no home, and Written names the referrer it
	// pushed by digest.
	SignDist(ctx context.Context, r SignDistRequest) (Written, error)
	// UseDist is dist use. It writes the record of the distribution, then
	// verified/current, then image.yaml, and answers RestartRequired with ReasonImage.
	UseDist(ctx context.Context, r UseRequest) (Change, error)
}
