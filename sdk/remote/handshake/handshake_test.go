// Package handshake holds handshake.proto, the one message that opens every
// connection of the remote protocol, in the proto package aiops.remote.handshake
// (07-c4-plugin-sdk.md §5, rule 2). It is frozen-additive from its first release
// (00-decisions.md §4.3) and frozen as a proto file at S2a (ADR-060 decision 42).
// No Go is generated from it before S2b, so the package holds this test only and
// needs no protobuf module.
package handshake

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// frozen records handshake.proto with its comments left out. The file may differ
// from it only by fields added to Hello, each with a new name and a new number;
// comments and layout are free. A commit that adds a field records it here as
// well, which freezes that field too.
const frozen = `
syntax = "proto3";
package aiops.remote.handshake;
message Hello {
  repeated uint32 majors = 1;
  Kind kind = 2;
  map<string, string> diagnostics = 3;
}
enum Kind {
  KIND_UNSPECIFIED = 0;
  KERNEL = 1;
  PLUGIN = 2;
}
`

// token matches a comment, a string, a name, a number or any other single
// character, so nothing but white space and comments is dropped.
var token = regexp.MustCompile(`//[^\n]*|(?s:/\*.*?\*/)|"(?:[^"\\\n]|\\.)*"|[A-Za-z_][A-Za-z0-9_]*|[0-9]+|\S`)

// statements splits a proto source into its statements, each ended by ";", "{"
// or "}", with comments removed and the tokens joined by one space.
func statements(src string) []string {
	var out, cur []string
	for _, t := range token.FindAllString(src, -1) {
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") {
			continue
		}
		cur = append(cur, t)
		if t == ";" || t == "{" || t == "}" {
			out = append(out, strings.Join(cur, " "))
			cur = nil
		}
	}
	if len(cur) > 0 {
		out = append(out, strings.Join(cur, " "))
	}
	return out
}

const (
	ident = `[A-Za-z_][A-Za-z0-9_]*`
	typ   = `(?:\. )?` + ident + `(?: \. ` + ident + `)*`
)

// field matches a field statement with no options: a map, or a scalar, message
// or enum type with an optional label; then the name and the number.
var field = regexp.MustCompile(`^(?:map < ` + ident + ` , ` + typ + ` >|(?:(?:repeated|optional) )?` + typ + `) (` + ident + `) = ([0-9]+) ;$`)

// check returns an error for every way src differs from frozen other than a
// field added to Hello.
func check(src string) error {
	want := statements(frozen)
	i := slices.Index(want, "message Hello {")
	fields := want[i+1 : i+slices.Index(want[i:], "}")]
	names, numbers := map[string]bool{}, map[int]bool{}
	for _, s := range fields {
		m := field.FindStringSubmatch(s)
		if m == nil {
			return fmt.Errorf("frozen: %q is not a field declaration", s)
		}
		n, _ := strconv.Atoi(m[2])
		names[m[1]], numbers[n] = true, true
	}
	var errs []error
	var kept []string
	hello := false
	for _, s := range statements(src) {
		switch {
		case s == "message Hello {":
			hello = true
		case s == "}":
			hello = false
		case hello && !slices.Contains(fields, s):
			m := field.FindStringSubmatch(s)
			if m == nil {
				errs = append(errs, fmt.Errorf("Hello: %q is not a field declaration", s))
				continue
			}
			n, err := strconv.Atoi(m[2])
			switch {
			case err != nil || n < 1 || n > 536870911 || (n >= 19000 && n <= 19999):
				errs = append(errs, fmt.Errorf("Hello: %q has an invalid field number", s))
			case names[m[1]]:
				errs = append(errs, fmt.Errorf("Hello: %q reuses the name %s", s, m[1]))
			case numbers[n]:
				errs = append(errs, fmt.Errorf("Hello: %q reuses the number %d", s, n))
			}
			names[m[1]], numbers[n] = true, true
			continue
		}
		kept = append(kept, s)
	}
	if !slices.Equal(kept, want) {
		errs = append(errs, fmt.Errorf("without its added fields the file reads\n\t%s\nand the frozen message reads\n\t%s",
			strings.Join(kept, "\n\t"), strings.Join(want, "\n\t")))
	}
	return errors.Join(errs...)
}

func TestFrozen(t *testing.T) {
	src, err := os.ReadFile("handshake.proto")
	if err != nil {
		t.Fatal(err)
	}
	if err := check(string(src)); err != nil {
		t.Fatalf("handshake.proto changed other than by an added field:\n%v", err)
	}
}

func TestCheck(t *testing.T) {
	const last = "  map<string, string> diagnostics = 3;\n"
	for _, tc := range []struct {
		name, old, new string
		ok             bool
	}{
		{"unchanged", "", "", true},
		{"comments and layout", "message Hello {\n", "// The first message.\nmessage Hello\n{ /* opens */\n", true},
		{"added field", last, last + "  string note = 4;\n", true},
		{"added qualified field first", "message Hello {\n", "message Hello {\n  repeated .aiops.remote.handshake.Kind kinds = 7;\n", true},
		{"added optional and map fields", last, last + "  optional bool b = 5;\n  map<string, Kind> m = 6;\n", true},
		{"removed field", "  Kind kind = 2;\n", "", false},
		{"removed and reserved", "  Kind kind = 2;\n", "  reserved 2;\n", false},
		{"changed type", "repeated uint32 majors", "repeated uint64 majors", false},
		{"changed label", "repeated uint32 majors", "uint32 majors", false},
		{"renumbered", "kind = 2;", "kind = 4;", false},
		{"renamed", "kind = 2;", "sender = 2;", false},
		{"reordered", "  Kind kind = 2;\n" + last, last + "  Kind kind = 2;\n", false},
		{"added field reuses a number", last, last + "  string note = 3;\n", false},
		{"added field reuses a name", last, last + "  string kind = 4;\n", false},
		{"added fields share a number", last, last + "  string a = 4;\n  string b = 4;\n", false},
		{"added field in the reserved range", last, last + "  string note = 19000;\n", false},
		{"added field number zero", last, last + "  string note = 0;\n", false},
		{"added labelled map", last, last + "  repeated map<string, string> m = 4;\n", false},
		{"added field with an option", last, last + "  string note = 4 [deprecated = true];\n", false},
		{"added oneof", last, last + "  oneof o {\n    string note = 4;\n  }\n", false},
		{"added enum value", "  PLUGIN = 2;\n", "  PLUGIN = 2;\n  HOST = 3;\n", false},
		{"renamed enum value", "KERNEL = 1;", "CORE = 1;", false},
		{"added message", "enum Kind {", "message Other {\n}\nenum Kind {", false},
		{"added option", "package aiops.remote.handshake;\n", "package aiops.remote.handshake;\noption go_package = \"x\";\n", false},
		{"changed package", "aiops.remote.handshake", "aiops.remote.v0", false},
		{"changed syntax", `"proto3"`, `"proto2"`, false},
		{"renamed message", "message Hello {", "message Hi {", false},
		{"unterminated comment", "enum Kind {", "/* enum Kind {", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := frozen
			if tc.old != "" {
				if !strings.Contains(src, tc.old) {
					t.Fatalf("frozen holds no %q", tc.old)
				}
				src = strings.Replace(src, tc.old, tc.new, 1)
			}
			if err := check(src); (err == nil) != tc.ok {
				t.Fatalf("ok=%v, err=%v", tc.ok, err)
			}
		})
	}
}
