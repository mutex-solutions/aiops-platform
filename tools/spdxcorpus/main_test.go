// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"
)

// The text a template requires leaves out its optional parts, nested or not, and each
// variable that may be empty, and keeps every other variable at its original text.
func TestRequired(t *testing.T) {
	tmpl := `<<beginOptional>>MIT License<<endOptional>> <<var;name="copyright";original="Copyright (c) <year> <holders>";match=".{0,5000}">>
Permission is granted<<beginOptional>> to any <<beginOptional>>nested<<endOptional>> person<<endOptional>> to use the <<var;name="s";original="Software";match="Software|Materials">>.`
	want := " \nPermission is granted to use the Software."
	if got := required(tmpl); got != want {
		t.Errorf("required = %q, want %q", got, want)
	}
}

// The committed corpus is what this program writes from the SPDX License List data at
// the pin, read from the module cache, or the case skips: go mod download the module to
// run it, as the CI job builder does.
func TestCorpusParity(t *testing.T) {
	dir, err := moduleDir()
	if err != nil {
		t.Skip(err)
	}
	if err := run(filepath.Join("..", ".."), false); err != nil {
		t.Errorf("%v (the data at %s)", err, dir)
	}
}
