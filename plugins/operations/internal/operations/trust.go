// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// trust.yaml, one file per site under the home (ADR-020 decision 5): what trust add
// appends to it and what Home and dist use read of it. boot reads the same file with
// its own reader (boot.ReadTrust); what trust add writes keeps that reader's rules:
// sets sorted and unique, a key one PEM block of an ECDSA P-256 public key, and the
// default root first in a distributions field that trust add creates.

// entry is the entry that trust add appends: a signer, or with dist a distribution
// root.
type entry struct {
	pem, id               string
	ids, runtimes, signed []string
	dist                  bool
}

func kebab(s string) bool { return regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`).MatchString(s) }

// okHook reports whether s names a key or a hook, as boot reads the entries of signed.
func okHook(s string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`).MatchString(s)
}

// okPattern reports whether s is an id pattern, <owner>/<name> or <owner>/*, of an
// owner other than aiops and contrib, which only the release table admits (ADR-020
// decision 6).
func okPattern(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && kebab(owner) && (name == "*" || kebab(name)) && owner != "aiops" && owner != "contrib"
}

// trustEntry checks the form of a trust add and reads its key file. A refusal writes
// nothing.
func trustEntry(r def.TrustRequest) (entry, error) {
	switch {
	case r.Distribution && len(r.IDs)+len(r.Runtimes)+len(r.Signed) > 0:
		return entry{}, refused("a distribution root takes no ids, runtimes or signed entries (ADR-020 decision 5)")
	case !r.Distribution && len(r.IDs) == 0:
		return entry{}, refused("trust add takes the id patterns of a signer, or a distribution root (ADR-020 decision 5)")
	}
	for _, p := range r.IDs {
		if !okPattern(p) {
			return entry{}, refused("%q is not an id pattern: <owner>/<name> or <owner>/*, of an owner other than aiops and contrib (ADR-020 decision 6)", p)
		}
	}
	for _, rt := range r.Runtimes {
		switch rt {
		case "inproc", "remote":
		case "wasm":
			return entry{}, refused("the runtime wasm is refused, since gate check 1 denies it (ADR-020 decision 6)")
		default:
			return entry{}, refused("%q is not a runtime: inproc or remote, and none for packs only (ADR-020 decision 6)", rt)
		}
	}
	for _, s := range r.Signed {
		if !okHook(s) {
			return entry{}, refused("%q names no signed key or hook (ADR-020 decision 7)", s)
		}
	}
	b, err := os.ReadFile(r.KeyFile)
	if err != nil {
		return entry{}, refused("read the public key: %v", err)
	}
	_, id, err := LoadPublicKey(b)
	if err != nil {
		return entry{}, refused("%s: %v", r.KeyFile, err)
	}
	set := func(s []string) []string { return slices.Compact(slices.Sorted(slices.Values(s))) }
	return entry{pem: strings.TrimSpace(string(b)) + "\n", id: id, ids: set(r.IDs), runtimes: set(r.Runtimes), signed: set(r.Signed), dist: r.Distribution}, nil
}

// appendTrust returns trust.yaml at file with e appended, the rest of the file kept,
// comments included. It refuses a distribution key that the file lists already.
func appendTrust(file string, e entry) ([]byte, error) {
	b, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, refused("%s does not read: %v", file, err)
	}
	if len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, refused("%s is not a mapping", file)
	}
	str := func(s string, style yaml.Style) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: s, Style: style}
	}
	list := func(name string) (*yaml.Node, bool, error) {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == name {
				if n := root.Content[i+1]; n.Kind == yaml.SequenceNode {
					return n, false, nil
				}
				return nil, false, refused("%s: %s is not a list", file, name)
			}
		}
		n := &yaml.Node{Kind: yaml.SequenceNode}
		root.Content = append(root.Content, str(name, 0), n)
		return n, true, nil
	}
	m := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{str("key", 0), str(e.pem, yaml.LiteralStyle)}}
	for _, f := range []struct {
		name string
		vals []string
	}{{"ids", e.ids}, {"runtimes", e.runtimes}, {"signed", e.signed}} {
		if len(f.vals) > 0 {
			s := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
			for _, v := range f.vals {
				s.Content = append(s.Content, str(v, 0))
			}
			m.Content = append(m.Content, str(f.name, 0), s)
		}
	}
	name := "signers"
	if e.dist {
		name = "distributions"
	}
	seq, created, err := list(name)
	if err != nil {
		return nil, err
	}
	if e.dist && created {
		// Adding a site key never drops the default root (ADR-020 decision 13).
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{str("key", 0), str(def.Release, 0)}})
	}
	for _, c := range seq.Content {
		var d struct{ Key string }
		if e.dist && c.Decode(&d) == nil && d.Key != def.Release {
			if _, id, err := LoadPublicKey([]byte(d.Key)); err == nil && id == e.id {
				return nil, refused("distributions of %s lists the key %s already", file, e.id)
			}
		}
	}
	seq.Content = append(seq.Content, m)
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	return out.Bytes(), enc.Close()
}

// trust is what Home and dist use read of trust.yaml.
type trust struct {
	signers    []def.Signer
	signerKeys []*ecdsa.PublicKey // the key of each signer
	dists      []string           // key ids, def.Release among them; nil when the field is absent
	distKeys   []trustKey
	allow      []string
}

type trustKey struct {
	id  string
	pub *ecdsa.PublicKey
}

// readTrust reads trust.yaml at file; a missing file reads empty.
func readTrust(file string) (trust, error) {
	var f struct {
		Signers []struct {
			Key                   string
			IDs, Runtimes, Signed []string `yaml:",flow"`
		}
		Distributions *[]struct{ Key string }
		AllowUnsigned []string `yaml:"allowUnsigned"`
	}
	if err := readYAML(file, &f); err != nil {
		return trust{}, err
	}
	t := trust{allow: f.AllowUnsigned}
	for _, s := range f.Signers {
		pub, id, err := LoadPublicKey([]byte(s.Key))
		if err != nil {
			return trust{}, fmt.Errorf("%s: a signer: %w", file, err)
		}
		t.signers = append(t.signers, def.Signer{KeyID: id, IDs: s.IDs, Runtimes: s.Runtimes, Signed: s.Signed})
		t.signerKeys = append(t.signerKeys, pub)
	}
	if f.Distributions != nil {
		t.dists = []string{}
		for _, d := range *f.Distributions {
			if d.Key == def.Release {
				t.dists = append(t.dists, def.Release)
				continue
			}
			pub, id, err := LoadPublicKey([]byte(d.Key))
			if err != nil {
				return trust{}, fmt.Errorf("%s: a distribution: %w", file, err)
			}
			t.dists, t.distKeys = append(t.dists, id), append(t.distKeys, trustKey{id: id, pub: pub})
		}
	}
	return t, nil
}
