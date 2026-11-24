// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// The artifact of the fake: it packs every plugin directory to the Artifact of its
// config, keeps it in memory by layout and by spec, and records the key ids that
// signed it. It writes no layout.

// artifact is an artifact the fake packed.
type artifact struct {
	in      operations.Inspected
	signers []string
}

// artifactAt returns the artifact at a layout or a spec, or a refusal.
func (f *Fake) artifactAt(where string) (*artifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.artifacts[where]
	if !ok {
		return nil, refused("%s holds no artifact this fake packed", where)
	}
	return a, nil
}

func (s served) Pack(_ context.Context, r operations.PackRequest) (operations.Written, error) {
	leave, err := s.enter("Pack")
	if err != nil {
		return operations.Written{}, err
	}
	defer leave()
	if !filepath.IsAbs(r.Dir) || !filepath.IsAbs(r.Out) {
		return operations.Written{}, usage("the plugin directory and the layout are absolute paths")
	}
	entries, _ := os.ReadDir(r.Out)
	if _, err := s.f.artifactAt(r.Out); err == nil || len(entries) > 0 {
		return operations.Written{}, refused("%s is not empty: a layout holds one artifact", r.Out)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "bundle.yaml")); err != nil {
		return operations.Written{}, refused("every artifact carries a bundle: %v", err)
	}
	in := s.f.cfg.Artifact
	in.Digest = digestOf(in.ID + "@" + in.Version)
	s.f.mu.Lock()
	s.f.artifacts[r.Out] = &artifact{in: in}
	s.f.mu.Unlock()
	return operations.Written{Paths: []string{r.Out}}, nil
}

func (s served) Sign(_ context.Context, r operations.SignRequest) (operations.Written, error) {
	leave, err := s.enter("Sign")
	if err != nil {
		return operations.Written{}, err
	}
	defer leave()
	pw, err := password(r.Caller)
	if err != nil {
		return operations.Written{}, err
	}
	a, err := s.f.artifactAt(r.Out)
	if err != nil {
		return operations.Written{}, err
	}
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	k, ok := s.f.keys[r.Key]
	if !ok || !k.private || k.password != pw {
		return operations.Written{}, refused("%s is no private key this fake made, or the password is not its own", r.Key)
	}
	a.signers = append(a.signers, k.id)
	return operations.Written{Paths: []string{r.Out}}, nil
}

func (s served) Push(_ context.Context, r operations.PushRequest) (operations.Pushed, error) {
	leave, err := s.enter("Push")
	if err != nil {
		return operations.Pushed{}, err
	}
	defer leave()
	a, err := s.f.artifactAt(r.Out)
	if err != nil {
		return operations.Pushed{}, err
	}
	repo := strings.TrimPrefix(r.Registry, "oci://")
	spec := "oci://" + repo + "@" + a.in.Digest
	s.f.mu.Lock()
	s.f.artifacts[spec], s.f.artifacts["oci://"+repo+":"+a.in.Version] = a, a
	s.f.mu.Unlock()
	return operations.Pushed{Spec: spec}, nil
}

func (s served) Inspect(_ context.Context, r operations.SpecRequest) (operations.Inspected, error) {
	leave, err := s.enter("Inspect")
	if err != nil {
		return operations.Inspected{}, err
	}
	defer leave()
	a, err := s.f.artifactAt(r.Spec)
	if err != nil {
		return operations.Inspected{}, err
	}
	return a.in, nil
}

// Verify verifies the signature by the key ids that signed the artifact, against the
// signers of the home that cover its id and runtimes; the fake runs no suite.
func (s served) Verify(_ context.Context, r operations.SpecRequest) (operations.Verified, error) {
	leave, err := s.enter("Verify")
	if err != nil {
		return operations.Verified{}, err
	}
	defer leave()
	if err := homeOf(r.Home); err != nil {
		return operations.Verified{}, err
	}
	a, err := s.f.artifactAt(r.Spec)
	if err != nil {
		return operations.Verified{}, err
	}
	s.f.mu.Lock()
	signers := s.f.home(r.Home).state.signers
	s.f.mu.Unlock()
	owner, _, _ := strings.Cut(a.in.ID, "/")
	i := slices.IndexFunc(signers, func(sg operations.Signer) bool {
		return slices.Contains(a.signers, sg.KeyID) && (slices.Contains(sg.IDs, a.in.ID) || slices.Contains(sg.IDs, owner+"/*")) &&
			!slices.ContainsFunc(a.in.Runtimes, func(rt string) bool { return !slices.Contains(sg.Runtimes, rt) })
	})
	if i < 0 {
		return operations.Verified{}, refused("check 5: no signer of the home that covers %s signed it", a.in.ID)
	}
	return operations.Verified{Digest: a.in.Digest, Signer: signers[i].KeyID, Steps: []operations.VerifyStep{{Name: "signature", Outcome: "pass"},
		{Name: "attestation", Outcome: "pass"}, {Name: "suites", Outcome: "not-built", Reason: "the fake runs no suite"}}}, nil
}
