// SPDX-License-Identifier: Apache-2.0

// Package ocitest is a registry of the OCI distribution protocol that runs inside a
// test, over net/http/httptest on the loopback interface, with its content in memory:
// blobs pushed in one or two requests, manifests by tag and by digest, deleted by
// digest, and the referrers API, which a registry can leave out so that a client falls
// back to the referrers tag schema. With a user it asks for Basic credentials, as a
// registry that a Docker config.json logs into does. Only test code imports it.
package ocitest

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The media types this package writes.
const (
	ManifestType = "application/vnd.oci.image.manifest.v1+json"
	IndexType    = "application/vnd.oci.image.index.v1+json"
	EmptyType    = "application/vnd.oci.empty.v1+json"
)

// Options are the forms of a registry.
type Options struct {
	User, Password string // with a user, every request needs these Basic credentials
	NoReferrers    bool   // no referrers API, as Distribution v3.1.2 has none
}

// Registry is a running registry.
type Registry struct {
	Host  string // host:port, on the loopback interface
	opts  Options
	mu    sync.Mutex
	repos map[string]*repo
	log   []string
}

// Requests returns each request served, "<method> <path>", in order.
func (r *Registry) Requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

type repo struct {
	blobs     map[string][]byte
	manifests map[string]stored // by digest
	tags      map[string]string // tag to digest
}

type stored struct {
	mediaType string
	data      []byte
}

// Descriptor is an OCI content descriptor.
type Descriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

// New starts a registry that the end of tb stops.
func New(tb testing.TB, o Options) *Registry {
	tb.Helper()
	r := &Registry{opts: o, repos: map[string]*repo{}}
	srv := httptest.NewServer(http.HandlerFunc(r.serve))
	tb.Cleanup(srv.Close)
	r.Host = strings.TrimPrefix(srv.URL, "http://")
	return r
}

// Digest returns "sha256:" and the hex of the SHA-256 of b.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *Registry) repo(name string) *repo {
	p := r.repos[name]
	if p == nil {
		p = &repo{blobs: map[string][]byte{}, manifests: map[string]stored{}, tags: map[string]string{}}
		r.repos[name] = p
	}
	return p
}

// Blob stores data as a blob of the repository name.
func (r *Registry) Blob(name, mediaType string, data []byte) Descriptor {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := Digest(data)
	r.repo(name).blobs[d] = data
	return Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

// Manifest stores data as a manifest of the repository name, under tag too unless it
// is "", and returns its descriptor.
func (r *Registry) Manifest(name, tag, mediaType string, data []byte) Descriptor {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := Digest(data)
	p := r.repo(name)
	p.manifests[d] = stored{mediaType, data}
	if tag != "" {
		p.tags[tag] = d
	}
	return Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

// Layer is one layer of an artifact.
type Layer struct {
	MediaType   string
	Data        []byte
	Annotations map[string]string
}

// Artifact stores an OCI image manifest with artifactType, the empty config, and
// layers, a referrer of subject when it is not nil, under tag too unless it is "".
func (r *Registry) Artifact(name, tag, artifactType string, subject *Descriptor, layers ...Layer) Descriptor {
	empty := r.Blob(name, EmptyType, []byte("{}"))
	m := struct {
		SchemaVersion int          `json:"schemaVersion"`
		MediaType     string       `json:"mediaType"`
		ArtifactType  string       `json:"artifactType,omitempty"`
		Config        Descriptor   `json:"config"`
		Layers        []Descriptor `json:"layers"`
		Subject       *Descriptor  `json:"subject,omitempty"`
	}{2, ManifestType, artifactType, empty, []Descriptor{}, subject}
	for _, l := range layers {
		d := r.Blob(name, l.MediaType, l.Data)
		d.Annotations = l.Annotations
		m.Layers = append(m.Layers, d)
	}
	b, _ := json.Marshal(m)
	d := r.Manifest(name, tag, ManifestType, b)
	d.ArtifactType = artifactType
	return d
}

// Fetch returns the manifest or the blob of the repository name at ref, a tag or a
// digest.
func (r *Registry) Fetch(name, ref string) ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.repo(name)
	if d, ok := p.tags[ref]; ok {
		ref = d
	}
	if m, ok := p.manifests[ref]; ok {
		return m.data, true
	}
	b, ok := p.blobs[ref]
	return b, ok
}

// Referrers returns the manifests of the repository name whose subject is digest.
func (r *Registry) Referrers(name, digest string) []Descriptor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.referrers(r.repo(name), digest)
}

func (r *Registry) referrers(p *repo, digest string) []Descriptor {
	out := []Descriptor{}
	for d, m := range p.manifests {
		var v struct {
			ArtifactType string
			Config       Descriptor
			Subject      *Descriptor
			Annotations  map[string]string
		}
		if json.Unmarshal(m.data, &v) == nil && v.Subject != nil && v.Subject.Digest == digest {
			out = append(out, Descriptor{MediaType: m.mediaType, Digest: d, Size: int64(len(m.data)),
				ArtifactType: v.ArtifactType, Annotations: v.Annotations})
		}
	}
	return out
}

// serve answers the routes of the distribution protocol under /v2/.
func (r *Registry) serve(w http.ResponseWriter, q *http.Request) {
	if u, pw, ok := q.BasicAuth(); r.opts.User != "" && (!ok || u != r.opts.User || pw != r.opts.Password) {
		w.Header().Set("WWW-Authenticate", `Basic realm="ocitest"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	path := strings.TrimPrefix(q.URL.Path, "/v2/")
	if path == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, q.Method+" "+q.URL.Path)
	for _, kind := range []string{"/blobs/uploads/", "/blobs/", "/manifests/", "/referrers/"} {
		if name, ref, ok := strings.Cut(path, kind); ok {
			r.route(w, q, kind, r.repo(name), ref)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func (r *Registry) route(w http.ResponseWriter, q *http.Request, kind string, p *repo, ref string) {
	body, _ := io.ReadAll(q.Body)
	switch {
	case kind == "/blobs/uploads/" && q.Method == http.MethodPost && q.URL.Query().Get("digest") == "":
		w.Header().Set("Location", q.URL.Path+rand.Text())
		w.WriteHeader(http.StatusAccepted)
	case kind == "/blobs/uploads/" && (q.Method == http.MethodPut || q.Method == http.MethodPost):
		if d := q.URL.Query().Get("digest"); d != Digest(body) {
			http.Error(w, `{"errors": [{"code": "DIGEST_INVALID"}]}`, http.StatusBadRequest)
			return
		}
		p.blobs[Digest(body)] = body
		w.Header().Set("Docker-Content-Digest", Digest(body))
		w.WriteHeader(http.StatusCreated)
	case kind == "/blobs/" && (q.Method == http.MethodGet || q.Method == http.MethodHead):
		b, ok := p.blobs[ref]
		r.content(w, q, ok, "application/octet-stream", ref, b)
	case kind == "/manifests/" && q.Method == http.MethodPut:
		d := Digest(body)
		if strings.HasPrefix(ref, "sha256:") && ref != d {
			http.Error(w, `{"errors": [{"code": "DIGEST_INVALID"}]}`, http.StatusBadRequest)
			return
		}
		p.manifests[d] = stored{q.Header.Get("Content-Type"), body}
		if !strings.HasPrefix(ref, "sha256:") {
			p.tags[ref] = d
		}
		var v struct{ Subject *Descriptor }
		if json.Unmarshal(body, &v) == nil && v.Subject != nil && !r.opts.NoReferrers {
			w.Header().Set("OCI-Subject", v.Subject.Digest)
		}
		w.Header().Set("Docker-Content-Digest", d)
		w.WriteHeader(http.StatusCreated)
	case kind == "/manifests/" && q.Method == http.MethodDelete:
		_, ok := p.manifests[ref]
		delete(p.manifests, ref)
		w.WriteHeader(map[bool]int{true: http.StatusAccepted, false: http.StatusNotFound}[ok])
	case kind == "/manifests/" && (q.Method == http.MethodGet || q.Method == http.MethodHead):
		if d, ok := p.tags[ref]; ok {
			ref = d
		}
		m, ok := p.manifests[ref]
		r.content(w, q, ok, m.mediaType, ref, m.data)
	case kind == "/referrers/" && q.Method == http.MethodGet && !r.opts.NoReferrers:
		refs := r.referrers(p, ref)
		if t := q.URL.Query().Get("artifactType"); t != "" {
			refs = filter(refs, t)
			w.Header().Set("OCI-Filters-Applied", "artifactType")
		}
		b, _ := json.Marshal(map[string]any{"schemaVersion": 2, "mediaType": IndexType, "manifests": refs})
		r.content(w, q, true, IndexType, Digest(b), b)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func filter(ds []Descriptor, artifactType string) []Descriptor {
	out := []Descriptor{}
	for _, d := range ds {
		if d.ArtifactType == artifactType {
			out = append(out, d)
		}
	}
	return out
}

// content answers a GET or HEAD of stored content, or that there is none.
func (r *Registry) content(w http.ResponseWriter, q *http.Request, ok bool, mediaType, digest string, b []byte) {
	if !ok {
		http.Error(w, `{"errors": [{"code": "NOT_FOUND"}]}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	if q.Method == http.MethodGet {
		_, _ = io.Copy(w, bytes.NewReader(b))
	}
}
