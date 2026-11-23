// SPDX-License-Identifier: Apache-2.0

package deftest

import (
	"reflect"
	"strings"
	"testing"
)

const head = "key: k\nversion: \"0.2\"\nstability: preview\n"

func TestParseReadsTheFixedLayout(t *testing.T) {
	src := `# a comment
key: launch   # trailing
version: "0.1"
stability: ga
class:
  a: {provider: bound, consumer: signed}
  b: {provider: open, consumer: open}
remote: "no"
compatibleFrom: "0.1"
deprecated: {since: "0.3.0", removeNotBefore: 2027-11-01, successor: v1}
suite: 3
hooks:
  x/y: {stages: [first, second], access: sealed, remote: "no"}
  x/z: {stages: [default], access: open, remote: yes}
`
	f, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want := File{Key: "launch", Version: "0.1", Stability: "ga", Remote: "no", PerKey: true,
		Class:          map[string]Class{"a": {"bound", "signed"}, "b": {"open", "open"}},
		CompatibleFrom: "0.1", Suite: 3,
		Deprecated: map[string]string{"since": "0.3.0", "removeNotBefore": "2027-11-01", "successor": "v1"},
		Hooks: map[string]HookDecl{"x/y": {[]string{"first", "second"}, "sealed", "no"},
			"x/z": {[]string{"default"}, "open", "yes"}}}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("Parse =\n%+v, want\n%+v", f, want)
	}
	f, err = Parse(head + "class: {provider: open, consumer: bound}\nremote: contrib\nsuite: 0\n")
	if err != nil || f.PerKey || !reflect.DeepEqual(f.Class, map[string]Class{"k": {"open", "bound"}}) || f.Hooks != nil {
		t.Fatalf("one class: %+v, %v", f, err)
	}
	if m, ok := Major("12.3"); !ok || m != 12 {
		t.Fatalf("Major(12.3) = %d, %v", m, ok)
	}
}

func TestParseRefusesAnyOtherForm(t *testing.T) {
	const class = "class: {provider: open, consumer: open}\n"
	const tail = class + "remote: \"no\"\nsuite: 1\n"
	for name, src := range map[string]string{
		"missing field":      head + class + "suite: 1\n",
		"missing suite":      head + class + "remote: \"no\"\n",
		"out of order":       "version: \"0.1\"\nkey: k\nstability: preview\n" + tail,
		"repeated":           head + "key: k\n" + tail,
		"unknown field":      head + tail + "extra: 1\n",
		"bad version":        "key: k\nversion: \"0.1.2\"\nstability: preview\n" + tail,
		"leading zero":       "key: k\nversion: \"01.1\"\nstability: preview\n" + tail,
		"bad stability":      "key: k\nversion: \"0.1\"\nstability: beta\n" + tail,
		"bad class":          head + "class: {provider: signed, consumer: open}\nremote: \"no\"\nsuite: 1\n",
		"class without side": head + "class: {provider: open}\nremote: \"no\"\nsuite: 1\n",
		"class extra side":   head + "class: {provider: open, consumer: open, x: open}\nremote: \"no\"\nsuite: 1\n",
		"repeated key class": head + "class:\n  a: {provider: open, consumer: open}\n  a: {provider: open, consumer: open}\nremote: \"no\"\nsuite: 1\n",
		"bad remote":         head + class + "remote: maybe\nsuite: 1\n",
		"negative suite":     head + class + "remote: \"no\"\nsuite: -1\n",
		"suite not a number": head + class + "remote: \"no\"\nsuite: one\n",
		"empty value":        head + class + "remote:\nsuite: 1\n",
		"block on scalar":    head + class + "remote:\n  x: y\nsuite: 1\n",
		"stray indent":       "  key: k\n" + head[7:] + tail,
		"bad quote":          head + class + "remote: \"no\nsuite: 1\n",
		"escape":             head + class + "remote: \"n\\o\"\nsuite: 1\n",
		"flow in scalar":     "key: {a: b}\nversion: \"0.1\"\nstability: preview\n" + tail,
		"bad flow":           head + "class: {provider: open, consumer: open\nremote: \"no\"\nsuite: 1\n",
		"trailing comma":     head + "class: {provider: open, consumer: open,}\nremote: \"no\"\nsuite: 1\n",
		"deprecated short":   head + tail[:len(tail)-9] + "deprecated: {since: x}\nsuite: 1\n",
		"hook no stages":     head + tail + "hooks:\n  h: {stages: [], access: open, remote: \"no\"}\n",
		"hook repeat stage":  head + tail + "hooks:\n  h: {stages: [a, a], access: open, remote: \"no\"}\n",
		"hook bad access":    head + tail + "hooks:\n  h: {stages: [a], access: closed, remote: \"no\"}\n",
		"hook bad remote":    head + tail + "hooks:\n  h: {stages: [a], access: open, remote: contrib}\n",
		"hook extra field":   head + tail + "hooks:\n  h: {stages: [a], access: open, remote: \"no\", x: y}\n",
		"hook list twice":    head + tail + "hooks:\n  h: {stages: [a], access: [b], remote: \"no\"}\n",
		"hook repeated":      head + tail + "hooks:\n  h: {stages: [a], access: open, remote: \"no\"}\n  h: {stages: [a], access: open, remote: \"no\"}\n",
		"hook nested list":   head + tail + "hooks:\n  h: {stages: [[a]], access: open, remote: \"no\"}\n",
		"hook line":          head + tail + "hooks:\n  just text\n",
	} {
		if f, err := Parse(src); err == nil {
			t.Errorf("%s: Parse accepted %q as %+v", name, strings.ReplaceAll(src, "\n", `\n`), f)
		}
	}
}
