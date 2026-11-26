// SPDX-License-Identifier: Apache-2.0

package operations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content/oci"

	def "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// rawTar returns a tar entry written by hand: a ustar header of name, type and size,
// and data padded to its block, so that a test can write what archive/tar's writer
// does not, such as GNU.sparse records.
func rawTar(name string, typ byte, data []byte) []byte {
	h := make([]byte, 512)
	copy(h, name)
	copy(h[100:], "0000644\x00")
	copy(h[108:], "0000000\x00")
	copy(h[116:], "0000000\x00")
	copy(h[124:], fmt.Sprintf("%011o\x00", len(data)))
	copy(h[136:], "00000000000\x00")
	h[156] = typ
	copy(h[257:], "ustar\x0000")
	copy(h[148:], "        ")
	sum := 0
	for _, c := range h {
		sum += int(c)
	}
	copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
	return append(append(h, data...), make([]byte, (512-len(data)%512)%512)...)
}

// pax returns one record of a PAX extended header.
func pax(k, v string) string {
	r := " " + k + "=" + v + "\n"
	n := len(r) + 1
	for len(fmt.Sprint(n))+len(r) != n {
		n++
	}
	return fmt.Sprint(n) + r
}

// untar keeps no more than the layer holds: an entry that PAX records make sparse, which
// archive/tar expands to its real size as zeros, is refused, and so is a tar whose
// entries claim more bytes than the layer, so that a content or fixtures layer of a few
// KiB never fills memory or disk at install or verify.
func TestUntarKeepsNoMoreThanTheLayer(t *testing.T) {
	records := pax("GNU.sparse.numblocks", "1") + pax("GNU.sparse.map", "0,1") + pax("GNU.sparse.realsize", fmt.Sprint(1<<20))
	sparse := append(rawTar("PaxHeaders/big", 'x', []byte(records)), rawTar("big", '0', []byte("x"))...)
	sparse = append(sparse, make([]byte, 1024)...)
	held := 0
	err := untar(sparse, func(_ string, data []byte) error { held += len(data); return nil })
	if !errors.Is(err, def.ErrRefused) || held > len(sparse) {
		t.Errorf("a sparse entry of %d bytes in a tar of %d: %v, kept %d bytes; want ErrRefused", 1<<20, len(sparse), err, held)
	}
}

// fetches is a store that records the digest of each blob it serves.
type fetches struct {
	*oci.Store
	mu  sync.Mutex
	got []string
}

func (f *fetches) Fetch(ctx context.Context, d ocispec.Descriptor) (io.ReadCloser, error) {
	f.mu.Lock()
	f.got = append(f.got, string(d.Digest))
	f.mu.Unlock()
	return f.Store.Fetch(ctx, d)
}

// verified reads no sigstore bundle larger than the 1 MiB of a file, and no more than
// sixteen bundle referrers of an artifact, before it decides on trust: whoever can
// attach referrers to an artifact's repository cannot make install or verify of an
// artifact that no key signs read many times 64 MiB first.
func TestReferrersAreBoundedBeforeTrust(t *testing.T) {
	ctx := t.Context()
	for _, c := range []struct {
		name  string
		n     int
		bytes int
	}{{"three bundles of 2 MiB", 3, 2 << 20}, {"seventeen bundles", 17, 64}} {
		s, err := oci.NewWithContext(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		d, err := pushArtifact(ctx, s, artifactType, nil, nil, blob{layerManifest, []byte("id: acme/x\nversion: 0.1.0\n"), nil}, blob{layerBundle, []byte("[]\n"), nil})
		if err != nil {
			t.Fatal(err)
		}
		var junk []string
		for i := range c.n {
			b := append(bytes.Repeat([]byte("x"), c.bytes), byte(i))
			r, err := pushArtifact(ctx, s, bundleType, &d, map[string]string{predicateKey: signURI}, blob{bundleType, b, nil})
			if err != nil {
				t.Fatal(err)
			}
			junk = append(junk, string(r.Digest))
		}
		f := &fetches{Store: s}
		_, err = verified(ctx, Home{Dir: t.TempDir()}, f, d)
		read := 0
		for _, g := range f.got {
			if g != string(d.Digest) && !strings.Contains(strings.Join(junk, " "), g) {
				read++ // a bundle layer
			}
		}
		if !errors.Is(err, def.ErrRefused) || read > 0 && c.bytes > maxFile || read > 16 {
			t.Errorf("%s: %v, %d bundle layers read; want ErrRefused, none above 1 MiB and at most 16", c.name, err, read)
		}
	}
}

// A registry that takes the connection and never answers, as one behind a filter that
// drops what it is sent, fails a request of the registry client within its wait, which
// no retry repeats, not minutes later.
func TestAnUnansweringRegistryFailsSoon(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0") // never accepted: the kernel completes the handshake
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+ln.Addr().String()+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := registryClient(200 * time.Millisecond).Do(req)
	if err == nil {
		resp.Body.Close()
	}
	if took := time.Since(start); err == nil || ctx.Err() != nil || took > 2*time.Second {
		t.Errorf("a request of a registry that never answers: %v after %v; want an error of its own within 2s", err, took)
	}
}
