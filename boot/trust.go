package boot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The readers of the trust inputs of the gate (ADR-020): the attestation statement,
// trust.yaml and the records that aiopsctl wrote under $AIOPS_HOME/verified/. Boot
// reads what aiopsctl verified and recorded, and verifies no signature itself
// (08-profiles-roadmap-adrs.md:404; ADR-020 decision 12).

// AttestationType is the predicate type of the conformance attestation (ADR-020
// decision 25), carried by an in-toto Statement v1 (ADR-020 decision 9).
const (
	AttestationType = "https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1"
	statementType   = "https://in-toto.io/Statement/v1"
)

// Attestation is a conformance attestation as read (ADR-020 decision 9; ADR-060
// decision 18):
//
//	{"_type": "https://in-toto.io/Statement/v1",
//	 "subject": [{"name": "<plugin id>@<version>", "digest": {"sha256": "<hex>"}}],
//	 "predicateType": AttestationType,
//	 "predicate": {
//	   "definitions": {"<name>": {"version": "<major>.<minor>", "suite": <n>, "halves": ["obligations", "guard"]}},
//	   "scenario": {"name": "<name>", "version": <n>, "halves": ["<half>"]},
//	   "notRun": ["<generated test>"]}}
//
// One subject, whose digest is that of what the gate admits; definitions names every
// definition of requires, with suite and halves for each that a suite ran on (ADR-060
// decision 56); scenario is a pack's (ADR-060 decision 49). The predicate is
// frozen-additive, so a field the reader does not know is ignored.
type Attestation struct {
	Name, Digest string // the subject: "<plugin id>@<version>" and "sha256:<hex>"
	Definitions  map[string]AttestedDefinition
	Scenario     *AttestedScenario
	NotRun       []string
}

// AttestedDefinition is one entry of definitions. Suite is nil for a definition the
// plugin only consumes, and Halves lists obligations, guard or both.
type AttestedDefinition struct {
	Version string
	Suite   *int
	Halves  []string
}

// AttestedScenario is the scenario a pack ran, with the halves that ran.
type AttestedScenario struct {
	Name    string
	Version int
	Halves  []string
}

// ReadAttestation reads the statement data, the content of file.
func ReadAttestation(file string, data []byte) (Attestation, error) {
	fail := func(format string, a ...any) (Attestation, error) {
		return Attestation{}, &Error{File: file, Rule: RuleAttestation, Msg: fmt.Sprintf(format, a...)}
	}
	if len(data) > MaxFileBytes {
		return fail("a statement is larger than %d bytes", MaxFileBytes)
	}
	var st struct {
		Type    string `json:"_type"`
		Subject []struct {
			Name   string
			Digest map[string]string
		}
		PredicateType string
		Predicate     struct {
			Definitions map[string]AttestedDefinition
			Scenario    *AttestedScenario
			NotRun      []string
		}
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return fail("not a statement: %v", err)
	}
	switch {
	case st.Type != statementType:
		return fail("_type %q is not %s", st.Type, statementType)
	case st.PredicateType != AttestationType:
		return fail("predicateType %q is not %s", st.PredicateType, AttestationType)
	case len(st.Subject) != 1:
		return fail("a statement names one subject, not %d", len(st.Subject))
	case st.Predicate.Definitions == nil:
		return fail("the predicate states definitions")
	}
	sub, p := st.Subject[0], st.Predicate
	id, version, _ := strings.Cut(sub.Name, "@")
	digest := "sha256:" + sub.Digest["sha256"]
	if !okManifestID(id) || !okSemver(version) || !okDigest(digest) {
		return fail("the subject %q with digest %v is not <plugin id>@<version> with a sha256 digest", sub.Name, sub.Digest)
	}
	for _, name := range slices.Sorted(maps.Keys(p.Definitions)) {
		d := p.Definitions[name]
		if !okKebab(name) || !okMajorMinor(d.Version) || d.Suite != nil && *d.Suite < 0 || d.Suite == nil && d.Halves != nil ||
			!okList(d.Halves, oneOf("obligations", "guard")) {
			return fail("definitions %s is not {version, suite, halves}, with halves of obligations and guard only beside suite", name)
		}
	}
	if s := p.Scenario; s != nil && (!okKebab(s.Name) || s.Version < 1 || len(s.Halves) == 0 || !okList(s.Halves, okKebab)) {
		return fail("the scenario is not {name, version, halves}, with a version of 1 or more and at least one half")
	}
	if !okList(p.NotRun, func(s string) bool { return s != "" }) {
		return fail("notRun lists a test once, by name")
	}
	return Attestation{Name: sub.Name, Digest: digest, Definitions: p.Definitions, Scenario: p.Scenario, NotRun: p.NotRun}, nil
}

// ReleaseRoot in Trust.Distributions stands for the default release root, whose key
// the release binary carries (ADR-020 decision 13).
const ReleaseRoot = "release"

// Trust is trust.yaml, one file per site under $AIOPS_HOME (ADR-020 decisions 5 to 8):
//
//	signers:                            optional
//	  - key: <PEM PUBLIC KEY>           an ECDSA P-256 key (ADR-020 decision 2)
//	    ids: [<owner>/<name> | <owner>/*]   neither owner aiops nor contrib
//	    runtimes: [inproc, remote]      optional: without, the entry covers packs only
//	    signed: [<key or hook>]         optional: the signed keys and hooks it is trusted for
//	distributions:                      optional: absent, the release root alone
//	  - key: release | <PEM PUBLIC KEY>
//	allowUnsigned: [<plugin id>]        optional
//
// A key is held by its id, the base64 of the SHA-256 of its DER form (ADR-020 decision
// 4). Sets are written sorted. indexes and requireAttestations wait for S6a (ADR-060
// decision 26; ADR-020 decision 26), and the reader refuses both.
type Trust struct {
	Signers       []TrustSigner
	Distributions []string // key ids, ReleaseRoot among them by default
	AllowUnsigned []string
}

// TrustSigner is one entry of signers.
type TrustSigner struct {
	KeyID                 string
	IDs, Runtimes, Signed []string
}

// ReadTrust reads trust.yaml of home. With no such file the release root alone is
// trusted.
func ReadTrust(home fs.FS) (Trust, error) {
	t := Trust{Distributions: []string{ReleaseRoot}}
	r, root, err := readHome(home, "trust.yaml", RuleTrust)
	if r == nil {
		return t, err
	}
	f := r.fields(root, "trust.yaml", []string{"signers", "distributions", "allowUnsigned", "indexes", "requireAttestations"})
	r.s6a(f)
	for _, c := range r.list(f["signers"], "signers") {
		e := r.fields(c, "a signer", []string{"key", "ids", "runtimes", "signed", "requireAttestations"}, "key", "ids")
		r.s6a(e)
		s := TrustSigner{KeyID: r.key(e["key"], false), IDs: r.set(e["ids"], "ids", okPattern),
			Runtimes: r.set(e["runtimes"], "runtimes", oneOf("inproc", "remote")), Signed: r.set(e["signed"], "signed", okHook)}
		if len(s.IDs) == 0 {
			r.fail(c, "a signer lists at least one id pattern")
		}
		t.Signers = append(t.Signers, s)
	}
	if n := f["distributions"]; n != nil {
		t.Distributions = []string{}
		for _, c := range r.list(n, "distributions") {
			id := r.key(r.fields(c, "a distribution", []string{"key"}, "key")["key"], true)
			if slices.Contains(t.Distributions, id) {
				r.fail(c, "distributions lists one key twice")
			}
			t.Distributions = append(t.Distributions, id)
		}
	}
	t.AllowUnsigned = r.set(f["allowUnsigned"], "allowUnsigned", okManifestID)
	if r.err != nil {
		return Trust{}, r.err
	}
	return t, nil
}

// s6a refuses the fields of trust.yaml that wait for S6a.
func (r *mreader) s6a(f map[string]*yaml.Node) {
	for _, k := range []string{"indexes", "requireAttestations"} {
		if f[k] != nil {
			r.fail(f[k], "%s waits for S6a (ADR-060 decision 26; ADR-020 decision 26)", k)
		}
	}
}

// key reads a PEM PUBLIC KEY block of an ECDSA P-256 key, or with release the word
// release, and returns its id.
func (r *mreader) key(n *yaml.Node, release bool) string {
	s := r.str(n, "key")
	if r.err != nil || release && s == ReleaseRoot {
		return s
	}
	b, rest := pem.Decode([]byte(s))
	if b == nil || !strings.HasPrefix(strings.TrimSpace(s), "-----BEGIN ") || b.Type != "PUBLIC KEY" || len(b.Headers) > 0 ||
		strings.TrimSpace(string(rest)) != "" {
		r.fail(n, "key holds one PEM block PUBLIC KEY and nothing else")
		return ""
	}
	k, err := x509.ParsePKIXPublicKey(b.Bytes)
	if ec, ok := k.(*ecdsa.PublicKey); err != nil || !ok || ec.Curve != elliptic.P256() {
		r.fail(n, "key is not an ECDSA P-256 public key (ADR-020 decision 2)")
	}
	sum := sha256.Sum256(b.Bytes)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// okPattern reports whether s is an id pattern, <owner>/<name> or <owner>/*, of an
// owner other than aiops and contrib, which only the release table admits (ADR-020
// decision 6).
func okPattern(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && okKebab(owner) && (name == "*" || okKebab(name)) && owner != "aiops" && owner != "contrib"
}

// readHome returns a reader of file of home under rule, or three nils when the file
// does not exist.
func readHome(home fs.FS, file string, rule Rule) (*mreader, *yaml.Node, error) {
	data, err := readFile(File{FS: home, Path: file})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fileError(file, err)
	}
	return newReader(file, data, rule, MaxFileBytes, file)
}

// okList reports whether every entry of list satisfies valid and none is there twice.
func okList(list []string, valid func(string) bool) bool {
	for i, s := range list {
		if !valid(s) || slices.Contains(list[:i], s) {
			return false
		}
	}
	return true
}

// okDigest reports whether s is "sha256:" and 64 lowercase hex digits.
func okDigest(s string) bool {
	h, ok := strings.CutPrefix(s, "sha256:")
	return ok && len(h) == 64 && strings.Trim(h, "0123456789abcdef") == ""
}
