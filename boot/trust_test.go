package boot

import (
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

const hexA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// statement is a conformance attestation of a pack with every field of ADR-020
// decision 9, a field of a later version of the predicate and a second digest.
const statement = `{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [{"name": "acme/connector-x@0.1.0", "digest": {"sha256": "` + hexA + `", "sha512": "ff"}}],
  "predicateType": "https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1",
  "predicate": {
    "definitions": {
      "echoers": {"version": "0.1", "suite": 1, "halves": ["obligations", "guard"]},
      "toy": {"version": "0.1", "suite": 0},
      "clock": {"version": "0.2"}
    },
    "scenario": {"name": "echo", "version": 1, "halves": ["kernel"]},
    "notRun": ["TestTenants/two-tenant"],
    "level": "later"
  }
}`

func TestReadAttestation(t *testing.T) {
	one, zero := 1, 0
	want := Attestation{Name: "acme/connector-x@0.1.0", Digest: "sha256:" + hexA,
		Definitions: map[string]AttestedDefinition{
			"echoers": {Version: "0.1", Suite: &one, Halves: []string{"obligations", "guard"}},
			"toy":     {Version: "0.1", Suite: &zero},
			"clock":   {Version: "0.2"},
		},
		Scenario: &AttestedScenario{Name: "echo", Version: 1, Halves: []string{"kernel"}},
		NotRun:   []string{"TestTenants/two-tenant"}}
	got, err := ReadAttestation("attestation.json", []byte(statement))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, got, want)
	}
	// A module of in-process code consumes only and runs no scenario.
	min := `{"_type": "https://in-toto.io/Statement/v1", "subject": [{"name": "aiops/clock@1.0.0", "digest": {"sha256": "` + hexA + `"}}],
	  "predicateType": "https://github.com/mutex-solutions/aiops-platform/attestation/conformance/v1", "predicate": {"definitions": {}}}`
	got, err = ReadAttestation("a.json", []byte(min))
	if err != nil || got.Scenario != nil || got.NotRun != nil || len(got.Definitions) != 0 {
		t.Fatalf("%v: %+v", err, got)
	}
	for _, c := range []struct {
		name  string
		edits []string
		want  string
	}{
		{"not JSON", []string{"{\n", "["}, "not a statement"},
		{"Statement v0.1", []string{"Statement/v1", "Statement/v0.1"}, "_type"},
		{"another predicate", []string{"conformance/v1", "conformance/v2"}, "predicateType"},
		{"two subjects", []string{`"subject": [`, `"subject": [{"name": "a/b@1.0.0", "digest": {"sha256": "` + hexA + `"}}, `}, "one subject, not 2"},
		{"no definitions", []string{`"definitions": {`, `"defs": {`}, "states definitions"},
		{"subject name", []string{"acme/connector-x@0.1.0", "acme/connector-x"}, "subject"},
		{"subject version", []string{"@0.1.0", "@v0.1.0"}, "subject"},
		{"no sha256", []string{`"sha256": "` + hexA + `", `, ""}, "subject"},
		{"short digest", []string{hexA, "aa"}, "subject"},
		{"definition name", []string{`"clock"`, `"Clock"`}, "definitions Clock"},
		{"definition version", []string{`"0.2"`, `"0.2.0"`}, "definitions clock"},
		{"negative suite", []string{`"suite": 0`, `"suite": -1`}, "definitions toy"},
		{"halves without suite", []string{`{"version": "0.2"}`, `{"version": "0.2", "halves": []}`}, "definitions clock"},
		{"unknown half", []string{`"guard"]`, `"guard", "fuzz"]`}, "definitions echoers"},
		{"half twice", []string{`"guard"]`, `"guard", "guard"]`}, "definitions echoers"},
		{"scenario name", []string{`"echo"`, `"Echo"`}, "scenario"},
		{"scenario version", []string{`"version": 1, "halves": ["kernel"]`, `"version": 0, "halves": ["kernel"]`}, "scenario"},
		{"scenario without halves", []string{`"halves": ["kernel"]`, `"halves": []`}, "scenario"},
		{"scenario half twice", []string{`["kernel"]`, `["kernel", "kernel"]`}, "scenario"},
		{"empty notRun entry", []string{`"TestTenants/two-tenant"`, `""`}, "notRun"},
		{"notRun twice", []string{`["TestTenants/two-tenant"]`, `["x", "x"]`}, "notRun"},
		{"a suite that is no integer", []string{`"suite": 1`, `"suite": 1.5`}, "not a statement"},
	} {
		src := strings.NewReplacer(c.edits...).Replace(statement)
		if src == statement {
			t.Fatalf("%s: the edit changed nothing", c.name)
		}
		_, err := ReadAttestation("attestation.json", []byte(src))
		var e *Error
		if !errors.As(err, &e) || e.Rule != RuleAttestation || e.File != "attestation.json" || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := ReadAttestation("a.json", make([]byte, MaxFileBytes+1)); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("too large: %v", err)
	}
}

// keyP256 is an ECDSA P-256 public key as cosign writes it, and keyP256ID its id, the
// base64 of the SHA-256 of its DER form (ADR-020 decision 4). keyP384 is of another
// curve.
const (
	keyP256 = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEDPUA/g3IPqH6QTcfQ0jRwcMFM0tN
7yQPBey7pje6bz/JcApeDVyUU/8pRp2KZLPoQlYINAMey7QYvuqN7/5CSg==
-----END PUBLIC KEY-----
`
	keyP256ID = "pgDZjw/2WEDWgSL8kX/2FuBZeWTq7P62DET/4n/nGbE="
	keyP384   = `-----BEGIN PUBLIC KEY-----
MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAEvgx3g3kSL57BXegTfZX3CHz17py+rOuo
fi+Kvq1KpdaIClL7k1hdGreR7gVdxI0hHhuy/9jXExvEl5K3UTBmnu/ZptricGi2
+ldwK4u6ZZVGnKXjcObmHBckWhmlXAma
-----END PUBLIC KEY-----
`
)

// indent puts each line of s under a YAML literal block at depth spaces.
func indent(s string, depth int) string {
	pad := strings.Repeat(" ", depth)
	return pad + strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\n"+pad)
}

// trustFile is a trust.yaml with every field of S2a.
func trustFile() string {
	return "signers:\n  - key: |\n" + indent(keyP256, 6) + "\n    ids: [acme/*, beta/echoer]\n    runtimes: [inproc, remote]\n" +
		"    signed: [http/request, identities]\n  - key: |\n" + indent(keyP256, 6) + "\n    ids: [acme/connector-x]\n" +
		"distributions:\n  - key: release\n  - key: |\n" + indent(keyP256, 6) + "\nallowUnsigned: [acme/connector-y]\n"
}

func TestReadTrust(t *testing.T) {
	home := fstest.MapFS{"trust.yaml": {Data: []byte(trustFile())}}
	got, err := ReadTrust(home)
	want := Trust{
		Signers: []TrustSigner{
			{KeyID: keyP256ID, IDs: []string{"acme/*", "beta/echoer"}, Runtimes: []string{"inproc", "remote"},
				Signed: []string{"http/request", "identities"}},
			{KeyID: keyP256ID, IDs: []string{"acme/connector-x"}},
		},
		Distributions: []string{ReleaseRoot, keyP256ID},
		AllowUnsigned: []string{"acme/connector-y"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, got, want)
	}
	// With no file, or a file with no distributions, the release root alone is
	// trusted; an empty list of distributions trusts none (ADR-020 decision 13).
	for src, roots := range map[string][]string{"": {ReleaseRoot}, "allowUnsigned: []\n": {ReleaseRoot}, "distributions: []\n": {}} {
		home := fstest.MapFS{}
		if src != "" {
			home["trust.yaml"] = &fstest.MapFile{Data: []byte(src)}
		}
		if got, err := ReadTrust(home); err != nil || !reflect.DeepEqual(got.Distributions, roots) || got.Signers != nil {
			t.Errorf("%q: %v %+v", src, err, got)
		}
	}
	for _, c := range []struct {
		name  string
		edits []string
		rule  Rule
		want  string
	}{
		{"unknown field", []string{"allowUnsigned:", "allowSigned:"}, RuleUnknownField, `"allowSigned"`},
		{"indexes", []string{"allowUnsigned:", "indexes: []\nallowUnsigned:"}, RuleTrust, "indexes waits for S6a"},
		{"requireAttestations of a signer", []string{"    ids: [acme/connector-x]", "    ids: [acme/connector-x]\n    requireAttestations: []"},
			RuleTrust, "requireAttestations waits for S6a"},
		{"no ids", []string{"    ids: [acme/connector-x]\n", ""}, RuleTrust, "a signer states ids"},
		{"empty ids", []string{"[acme/connector-x]", "[]"}, RuleTrust, "at least one id pattern"},
		{"reserved owner", []string{"[acme/connector-x]", "[aiops/*]"}, RuleTrust, `ids entry "aiops/*"`},
		{"contrib", []string{"[acme/connector-x]", "[contrib/x]"}, RuleTrust, `ids entry "contrib/x"`},
		{"a wildcard owner", []string{"[acme/connector-x]", "[\"*/x\"]"}, RuleTrust, "ids entry"},
		{"unsorted ids", []string{"[acme/*, beta/echoer]", "[beta/echoer, acme/*]"}, RuleTrust, "ids is a set"},
		{"wasm", []string{"[inproc, remote]", "[inproc, wasm]"}, RuleTrust, `runtimes entry "wasm"`},
		{"a signed name", []string{"[http/request, identities]", "[\"a b\"]"}, RuleTrust, "signed entry"},
		{"release for a signer", []string{"signers:\n  - key: |\n" + indent(keyP256, 6), "signers:\n  - key: release"}, RuleTrust, "one PEM block"},
		{"text before the block", []string{"  - key: |\n      -----BEGIN", "  - key: |\n      x\n      -----BEGIN"}, RuleTrust, "one PEM block"},
		{"text after the block", []string{"END PUBLIC KEY-----\n    ids: [acme/connector-x]", "END PUBLIC KEY-----\n      x\n    ids: [acme/connector-x]"},
			RuleTrust, "one PEM block"},
		{"another block type", []string{"PUBLIC KEY-----", "CERTIFICATE-----"}, RuleTrust, "one PEM block"},
		{"P-384", []string{"distributions:\n  - key: release\n  - key: |\n" + indent(keyP256, 6), "distributions:\n  - key: |\n" + indent(keyP384, 6)},
			RuleTrust, "not an ECDSA P-256 public key"},
		{"not DER", []string{"distributions:\n  - key: release\n  - key: |\n" + indent(keyP256, 6),
			"distributions:\n  - key: |\n      -----BEGIN PUBLIC KEY-----\n      AAAA\n      -----END PUBLIC KEY-----"}, RuleTrust, "not an ECDSA P-256 public key"},
		{"key twice", []string{"  - key: release\n", "  - key: release\n  - key: release\n"}, RuleTrust, "one key twice"},
		{"a distribution with no key", []string{"  - key: release\n", "  - {}\n"}, RuleTrust, "a distribution states key"},
		{"allowUnsigned pattern", []string{"[acme/connector-y]", "[acme/*]"}, RuleTrust, "allowUnsigned entry"},
		{"a key that is no string", []string{"  - key: release\n", "  - key: [release]\n"}, RuleType, "key must be a string"},
	} {
		src := strings.NewReplacer(c.edits...).Replace(trustFile())
		if src == trustFile() {
			t.Fatalf("%s: the edit changed nothing", c.name)
		}
		_, err := ReadTrust(fstest.MapFS{"trust.yaml": {Data: []byte(src)}})
		var e *Error
		if !errors.As(err, &e) || e.Rule != c.rule || e.File != "trust.yaml" || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	for _, c := range []struct {
		data []byte
		rule Rule
	}{{nil, RuleEmpty}, {make([]byte, MaxFileBytes+1), RuleLimit}, {[]byte("[]"), RuleType}} {
		var e *Error
		if _, err := ReadTrust(fstest.MapFS{"trust.yaml": {Data: c.data}}); !errors.As(err, &e) || e.Rule != c.rule {
			t.Errorf("rule %s: %v", c.rule, err)
		}
	}
}

const (
	hexB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	hexC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	sumA = "h1:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
)

// records holds a record of each form of ADR-020 decision 10: in-process code with
// its attestation, a pack that allowUnsigned admitted, and a distribution manifest.
func records() fstest.MapFS {
	return fstest.MapFS{
		"verified/sha256-" + hexA + "/record.yaml": {Data: []byte("kind: artifact\ndigest: sha256:" + hexA + "\nsigner: " + keyP256ID +
			"\nid: acme/echoer\nversion: 0.1.0\nruntimes: [inproc]\nmodule: {path: example.com/acme/echoer, version: v0.1.0, sum: \"" + sumA + "\"}\n")},
		"verified/sha256-" + hexA + "/attestation.json": {Data: []byte(statement)},
		"verified/sha256-" + hexB + "/record.yaml": {Data: []byte("kind: artifact\ndigest: sha256:" + hexB +
			"\nsigner: unsigned\nid: acme/connector-y\nversion: 0.2.0\nruntimes: []\n")},
		"verified/sha256-" + hexC + "/record.yaml": {Data: []byte("kind: distribution\ndigest: sha256:" + hexC + "\nsigner: " + keyP256ID + "\n")},
	}
}

func TestReadRecord(t *testing.T) {
	home := records()
	for digest, want := range map[string]Record{
		"sha256:" + hexA: {Kind: "artifact", Digest: "sha256:" + hexA, Signer: keyP256ID, ID: "acme/echoer", Version: "0.1.0",
			Runtimes: []string{"inproc"}, Module: &GoModule{"example.com/acme/echoer", "v0.1.0", sumA}, Attestation: []byte(statement)},
		"sha256:" + hexB: {Kind: "artifact", Digest: "sha256:" + hexB, Signer: Unsigned, ID: "acme/connector-y", Version: "0.2.0",
			Runtimes: []string{}},
		"sha256:" + hexC: {Kind: "distribution", Digest: "sha256:" + hexC, Signer: keyP256ID},
	} {
		if got, err := ReadRecord(home, digest); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v\n got %+v\nwant %+v", digest, err, got, want)
		}
	}
	recA, recC := "verified/sha256-"+hexA+"/record.yaml", "verified/sha256-"+hexC+"/record.yaml"
	// A module of a build inside the repository is pinned by its path alone, as the signed
	// manifest lists it (ADR-020 decision 28).
	inRepo := maps.Clone(home)
	inRepo[recA] = &fstest.MapFile{Data: []byte(strings.Replace(string(home[recA].Data), `, version: v0.1.0, sum: "`+sumA+`"`, "", 1))}
	if got, err := ReadRecord(inRepo, "sha256:"+hexA); err != nil || got.Module == nil || *got.Module != (GoModule{Path: "example.com/acme/echoer"}) {
		t.Errorf("a module by its path: %+v %v", got, err)
	}
	for _, c := range []struct {
		name, file string
		edits      []string
		rule       Rule
		want       string
	}{
		{"unknown field", recC, []string{"signer:", "by:"}, RuleUnknownField, `"by"`},
		{"no signer", recC, []string{"signer: " + keyP256ID + "\n", ""}, RuleRecord, "record.yaml states signer"},
		{"another kind", recC, []string{"kind: distribution", "kind: image"}, RuleRecord, `kind "image"`},
		{"another digest", recC, []string{"digest: sha256:" + hexC, "digest: sha256:" + hexB}, RuleRecord, "vouches for sha256:" + hexB},
		{"a digest of another form", recC, []string{"digest: sha256:" + hexC, "digest: " + hexC}, RuleRecord, "digest"},
		{"a signer that is no key id", recC, []string{"signer: " + keyP256ID, "signer: abc"}, RuleRecord, `signer "abc"`},
		{"an unsigned distribution", recC, []string{"signer: " + keyP256ID, "signer: unsigned"}, RuleRecord, "names the key"},
		{"a distribution with an id", recC, []string{"\nsigner:", "\nid: a/b\nsigner:"}, RuleRecord, "exactly when its kind is artifact"},
		{"an artifact with no runtimes", recA, []string{"runtimes: [inproc]\n", ""}, RuleRecord, "exactly when its kind is artifact"},
		{"wasm", recA, []string{"[inproc]", "[inproc, wasm]"}, RuleRecord, `runtimes entry "wasm"`},
		{"code with no module", recA, []string{"runtimes: [inproc]", "runtimes: [remote]"}, RuleRecord, "module exactly when"},
		{"a module with no sum", recA, []string{`, sum: "` + sumA + `"`, ""}, RuleRecord, "both or neither"},
		{"a module with no version", recA, []string{"version: v0.1.0, ", ""}, RuleRecord, "both or neither"},
		{"a module version", recA, []string{"version: v0.1.0", "version: 0.1.0"}, RuleRecord, `module version "0.1.0"`},
		{"a module sum", recA, []string{sumA, "h1:abc"}, RuleRecord, "module sum"},
		{"a module path", recA, []string{"path: example.com/acme/echoer", `path: "a b"`}, RuleRecord, "module path"},
		{"a version", recA, []string{"version: 0.1.0\n", "version: v0.1.0\n"}, RuleRecord, `version "v0.1.0"`},
		{"an id", recA, []string{"id: acme/echoer", "id: echoer"}, RuleRecord, `id "echoer"`},
		{"not YAML", recC, []string{"kind: distribution", "kind: ["}, RuleSyntax, ""},
	} {
		home := records()
		src := strings.NewReplacer(c.edits...).Replace(string(home[c.file].Data))
		if src == string(home[c.file].Data) {
			t.Fatalf("%s: the edit changed nothing", c.name)
		}
		home[c.file] = &fstest.MapFile{Data: []byte(src)}
		_, err := ReadRecord(home, "sha256:"+strings.Split(c.file, "-")[1][:64])
		var e *Error
		if !errors.As(err, &e) || e.Rule != c.rule || e.File != c.file || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	big := records()
	big["verified/sha256-"+hexA+"/attestation.json"] = &fstest.MapFile{Data: make([]byte, MaxFileBytes+1)}
	for _, c := range []struct {
		home   fstest.MapFS
		digest string
		rule   Rule
	}{{home, "sha256:" + hexA[1:], RuleRecord}, {home, "sha256:" + strings.Repeat("d", 64), RuleMissing}, {big, "sha256:" + hexA, RuleLimit}} {
		var e *Error
		if _, err := ReadRecord(c.home, c.digest); !errors.As(err, &e) || e.Rule != c.rule {
			t.Errorf("%s: %v, want rule %s", c.digest, err, c.rule)
		}
	}
}

func TestReadCurrent(t *testing.T) {
	for data, want := range map[string]string{"sha256:" + hexC + "\n": "sha256:" + hexC, "sha256:" + hexC: "sha256:" + hexC} {
		if got, err := ReadCurrent(fstest.MapFS{"verified/current": {Data: []byte(data)}}); err != nil || got != want {
			t.Errorf("%q: %q, %v", data, got, err)
		}
	}
	if got, err := ReadCurrent(fstest.MapFS{}); got != "" || err != nil {
		t.Errorf("none: %q, %v", got, err)
	}
	for data, rule := range map[string]Rule{"sha256:" + hexC + "\n\n": RuleRecord, "": RuleRecord, string(make([]byte, MaxFileBytes+1)): RuleLimit} {
		var e *Error
		if _, err := ReadCurrent(fstest.MapFS{"verified/current": {Data: []byte(data)}}); !errors.As(err, &e) || e.Rule != rule {
			t.Errorf("%.20q: %v", data, err)
		}
	}
}
