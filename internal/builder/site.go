// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
	"golang.org/x/mod/module"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// Site is the site's dist.yaml, its four fields (08:401; ADR-044 decision 22; ADR-060
// decision 20): the release by digest, the plugin modules, what the site adds to the
// bindings and the required set, and the licences it accepts.
type Site struct {
	Release        string
	Plugins        []module.Version
	Sums           map[string]string // the sum of each module of Plugins, by path
	Bindings       boot.DistManifest
	AcceptLicenses []string
}

var byDigest = regexp.MustCompile(`^(oci://)?[^@\s]+@sha256:[0-9a-f]{64}$`)

// ReadSite reads the site's dist.yaml:
//
//	release: <OCI reference by digest to the release's dist.yaml>
//	plugins: [{module, version, sum}]
//	bindings: {providers, consumers, sealedHooks, required: {keys, rows}}   optional
//	acceptLicenses: [<SPDX id>]                                               optional
//
// It refuses an unknown field, a module, version or sum that is not well formed, and
// a module listed twice; bindings are read by boot's reader of dist.yaml.
func ReadSite(data []byte) (Site, error) {
	var raw struct {
		Release string `yaml:"release"`
		Plugins []struct {
			Module, Version, Sum string
		} `yaml:"plugins"`
		Bindings struct {
			Providers   map[string][]string `yaml:"providers"`
			Consumers   map[string][]string `yaml:"consumers"`
			SealedHooks map[string][]string `yaml:"sealedHooks"`
			Required    struct {
				Keys []string `yaml:"keys"`
				Rows []string `yaml:"rows"`
			} `yaml:"required"`
		} `yaml:"bindings"`
		AcceptLicenses []string `yaml:"acceptLicenses"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return Site{}, fmt.Errorf("dist.yaml: %w", err)
	}
	if !byDigest.MatchString(raw.Release) {
		return Site{}, fmt.Errorf("dist.yaml: release %q is not an OCI reference by digest (08:401)", raw.Release)
	}
	s := Site{Release: raw.Release, Sums: map[string]string{}, AcceptLicenses: raw.AcceptLicenses}
	for _, p := range raw.Plugins {
		v := module.Version{Path: p.Module, Version: p.Version}
		if err := module.Check(v.Path, v.Version); err != nil || !strings.HasPrefix(p.Sum, "h1:") {
			return Site{}, fmt.Errorf("dist.yaml: plugin %s %s %s: a module path, a version and an h1 sum (%v)", p.Module, p.Version, p.Sum, err)
		}
		if _, ok := s.Sums[v.Path]; ok {
			return Site{}, fmt.Errorf("dist.yaml: plugin module %s is listed twice", v.Path)
		}
		s.Plugins, s.Sums[v.Path] = append(s.Plugins, v), p.Sum
	}
	b := raw.Bindings
	flat, err := yaml.Marshal(map[string]any{"providers": cmpMap(b.Providers), "consumers": cmpMap(b.Consumers),
		"sealedHooks": cmpMap(b.SealedHooks), "requiredKeys": cmpList(b.Required.Keys), "requiredRows": cmpList(b.Required.Rows)})
	if err == nil {
		s.Bindings, err = boot.ParseDist("dist.yaml bindings", flat)
	}
	if err != nil {
		return Site{}, err
	}
	for _, id := range s.AcceptLicenses {
		if !boot.Licensed(id, []string{id}) || strings.ContainsAny(id, " ()") {
			return Site{}, fmt.Errorf("dist.yaml: acceptLicenses names %q, which is not one SPDX id", id)
		}
	}
	return s, nil
}

func cmpMap(m map[string][]string) map[string][]string {
	if m == nil {
		return map[string][]string{}
	}
	return m
}

func cmpList(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}
