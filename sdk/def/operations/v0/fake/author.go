// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"path/filepath"
	"slices"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	operations "github.com/mutex-solutions/aiops-platform/sdk/def/operations/v0"
)

// New is plugin new on the template of the config, the Go template of echoers: it
// answers the paths it would write, and refuses a directory it wrote before. It writes
// no file.
func (s served) New(_ context.Context, r operations.NewRequest) (operations.Written, error) {
	return do(s, "New", func() (operations.Written, error) {
		switch {
		case !filepath.IsAbs(r.Dir):
			return operations.Written{}, usage("the plugin directory %q is not an absolute path", r.Dir)
		case r.Lang != "go":
			return operations.Written{}, refused("no template in %q: the Python template arrives at S2b", r.Lang)
		case r.Registry != "echoers":
			return operations.Written{}, refused("no template for the registry %q", r.Registry)
		}
		s.f.mu.Lock()
		defer s.f.mu.Unlock()
		if s.f.news[r.Dir] {
			return operations.Written{}, refused("%s holds files: plugin new writes into an empty or new directory", r.Dir)
		}
		s.f.news[r.Dir] = true
		var out operations.Written
		for _, p := range slices.Sorted(maps.Keys(s.f.cfg.Template)) {
			out.Paths = append(out.Paths, filepath.Join(r.Dir, filepath.FromSlash(p)))
		}
		slices.Sort(out.Paths)
		return out, nil
	})
}

// Check is plugin check on the plugin of the config: steps 1 to 4 and 7 pass, 6 and 8
// do not apply to code, and step 5 fails each obligation of Failed; the attestation of
// a pass names each registry the plugin registers with at suite revision 1.
func (s served) Check(_ context.Context, r operations.CheckRequest) (operations.CheckReport, error) {
	return do(s, "Check", func() (operations.CheckReport, error) {
		if !filepath.IsAbs(r.Dir) {
			return operations.CheckReport{}, usage("the plugin directory %q is not an absolute path", r.Dir)
		}
		c := s.f.cfg
		outcome := []string{operations.StepPass, operations.StepPass, operations.StepPass, operations.StepPass,
			operations.StepPass, operations.StepNotApplicable, operations.StepPass, operations.StepNotApplicable}
		var rep operations.CheckReport
		for i, o := range outcome {
			st := operations.Step{N: i + 1, Name: operations.Steps[i], Outcome: o}
			switch {
			case i == 4 && len(c.Failed) > 0:
				st.Outcome = operations.StepFail
				for _, id := range c.Failed {
					st.Diagnostics = append(st.Diagnostics, launch.Diagnostic{Check: id, Text: id + ": the obligation failed"})
				}
			case i > 4 && len(c.Failed) > 0:
				st.Outcome = operations.StepSkipped
			}
			rep.Steps = append(rep.Steps, st)
		}
		if !rep.Passed() {
			return rep, nil
		}
		defs := map[string]operations.Attested{}
		for name, vs := range c.Summary.Definitions {
			defs[name] = operations.Attested{Version: vs[len(vs)-1]}
		}
		for _, k := range c.Summary.Registers {
			if d, ok := defs[k]; ok {
				one := 1
				d.Suite, d.Halves = &one, []string{"obligations"}
				defs[k] = d
			}
		}
		sum := sha256.Sum256([]byte(c.Summary.ID + "@" + c.Summary.Version))
		rep.Attestation = operations.Statement(c.Summary.ID, c.Summary.Version, hex.EncodeToString(sum[:]), defs, nil, c.Skipped)
		return rep, nil
	})
}

// Build answers what the config says the builder built, and writes no home.
func (s served) Build(_ context.Context, r operations.BuildRequest) (launch.Built, error) {
	return do(s, "Build", func() (launch.Built, error) {
		if !filepath.IsAbs(r.DistYAML) || r.ModCache != "" && !filepath.IsAbs(r.ModCache) {
			return launch.Built{}, usage("dist.yaml %q and the module cache %q are absolute paths", r.DistYAML, r.ModCache)
		}
		return s.f.cfg.Built, nil
	})
}

// Smoke answers the rows of what the config says the builder built, and fails as
// plugin smoke fails on them. It writes no home.
func (s served) Smoke(_ context.Context, r operations.BuildRequest) (launch.Built, error) {
	return do(s, "Smoke", func() (launch.Built, error) {
		if err := operations.SmokeForm(r); err != nil {
			return launch.Built{}, err
		}
		return s.f.cfg.Built, operations.Smoked(s.f.cfg.Built.Rows)
	})
}
