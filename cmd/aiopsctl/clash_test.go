// SPDX-License-Identifier: Apache-2.0

package main

import (
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

// With the toolbox of the test distribution, an install of an artifact whose name the
// profile already holds is refused before the gate, by the refusal of ADR-047 decision
// 17: under another owner it names both artifacts and says to remove the first; of the
// same id at the same digest it says that the profile already installs it, and at
// another digest that it installs it at another digest. None writes anything.
func TestInstallOfANameTheProfileHolds(t *testing.T) {
	env, layout := installable(t)
	if r := doTest(t, env, standIns, "plugin", "install", layout, "--profile", "dev"); r.code != 0 {
		t.Fatalf("the first install: exit %d\n%s", r.code, r.errOut)
	}
	work := t.TempDir()
	other := map[string]string{}
	for k, v := range echoPack {
		other[k] = strings.NewReplacer("acme/echo-pack", "other/echo-pack", "{id: acme}", "{id: other}").Replace(v)
	}
	if err := writeFiles(work, other); err != nil {
		t.Fatal(err)
	}
	env2 := maps.Clone(env)
	env2["PWD"] = work
	for _, args := range [][]string{{"key", "generate", "other"}, {"trust", "add", "other.pub", "--ids", "other/*"},
		{"plugin", "pack", "--out", "out"}, {"plugin", "sign", "--key", "other.key", "--out", "out"}} {
		if r := doTest(t, env2, standIns, args...); r.code != 0 {
			t.Fatalf("%q: exit %d\n%s", args, r.code, r.errOut)
		}
	}
	// Another version of acme/echo-pack, signed by the author whom the home trusts.
	next := t.TempDir()
	newer := map[string]string{}
	for k, v := range echoPack {
		newer[k] = strings.Replace(v, "version: 0.1.0", "version: 0.2.0", 1)
	}
	if err := writeFiles(next, newer); err != nil {
		t.Fatal(err)
	}
	env3 := maps.Clone(env)
	env3["PWD"] = next
	for _, args := range [][]string{{"plugin", "pack", "--out", "out"}, {"plugin", "sign", "--key", filepath.Join(env["PWD"], "author.key"), "--out", "out"}} {
		if r := doTest(t, env3, standIns, args...); r.code != 0 {
			t.Fatalf("%q: exit %d\n%s", args, r.code, r.errOut)
		}
	}
	before := homeTree(t, env["AIOPS_HOME"])
	for _, c := range []struct {
		name, layout string
		want         []string
	}{
		{"another owner", filepath.Join(work, "out"), []string{"other/echo-pack", "acme/echo-pack", "plugin remove", "ADR-047 decision 17"}},
		{"the same digest", layout, []string{"already installs acme/echo-pack"}},
		{"another digest of the id", filepath.Join(next, "out"), []string{"installs acme/echo-pack at another digest", "plugin remove acme/echo-pack"}},
	} {
		r := doTest(t, env2, standIns, "plugin", "install", c.layout, "--profile", "dev")
		for _, w := range c.want {
			if r.code != 1 || !strings.Contains(r.errOut, w) {
				t.Errorf("%s: exit %d, stderr %q; want exit 1 and %q", c.name, r.code, r.errOut, w)
			}
		}
		if strings.Contains(r.errOut, "check 8") {
			t.Errorf("%s: the refusal came from the gate: %q", c.name, r.errOut)
		}
		if after := homeTree(t, env["AIOPS_HOME"]); !maps.Equal(after, before) {
			t.Errorf("%s: the refused install wrote the home", c.name)
		}
	}
}
