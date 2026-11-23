package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

const decisionsFile = "docs/architecture/00-decisions.md"

// place is a text that the name lint U1 reads (ADR-054 decisions 14 and 22). key is
// what the places of an item of exceptions and the directories of an entry are matched
// against: the repository path of a file, "ids" for an id, or a documentation place
// such as "00 §2.1 row 9".
type place struct{ where, key, text string }

// lint returns one line per name of an entry that a place holds where neither the
// register nor its exceptions give it the place: U1, and U3 in a fake or a suite
// (decision 15).
func lint(r register, places []place) []string {
	var bad []string
	for _, p := range places {
		w := words(p.text)
		for _, e := range r.Entries {
			for _, n := range e.Names {
				if !holds(w, n) || permitted(r, e, n, p.key) {
					continue
				}
				check := "U1"
				if neutral(p.key) {
					check = "U3"
				}
				bad = append(bad, fmt.Sprintf("%s: %s: %q is a name of the register entry %s", p.where, check, n, e.ID))
			}
		}
	}
	return bad
}

// permitted reports whether the name n of the entry e may stand at the place key: where
// an item of exceptions that lists n gives it the place (decision 9), or, outside an
// id, in the directories of e's plugins and importers (decision 17). An id carries a
// name only where an item gives it: the clause of decision 14 on a suffix of an entry
// marked suffix-ok has no case before the first row of a listed bundle that wraps an
// upstream (S3), and lands with it.
func permitted(r register, e entry, n, key string) bool {
	for _, x := range r.Exceptions {
		if slices.Contains(x.Tokens, n) && slices.ContainsFunc(x.Places, func(s string) bool { return within(key, s) }) {
			return true
		}
	}
	if key == "ids" {
		return false
	}
	for _, p := range e.Plugins {
		if strings.HasPrefix(key, "plugins/"+path.Base(p)+"/") {
			return true
		}
	}
	return slices.ContainsFunc(e.Importers, func(i importer) bool { return strings.HasPrefix(key, i.Dir+"/") })
}

// within reports whether the place key sits in the place s of an item of exceptions:
// "every place", a directory written with its trailing slash, or a place named as a
// key is, "ids" or a documentation place. Any other place of an item is prose that no
// check reads before its first case lands.
func within(key, s string) bool {
	return s == "every place" || key == s || strings.HasSuffix(s, "/") && strings.HasPrefix(key, s)
}

// neutral reports whether the repository path is in a fake or a suite of a definition
// or in sdk/testkit/sqlfake, the places of suite neutrality U3 (decision 15;
// 07-c4-plugin-sdk.md:28). Its other clause, no import of an entry's module there, is
// the import analyzer's: the rule on the definitions refuses any module outside the
// repository, and U2 any module whose entry does not list sdk/testkit. Its clause on
// cassettes has no case before the first cassette (S8a).
func neutral(key string) bool {
	seg := strings.Split(key, "/")
	return strings.HasPrefix(key, "sdk/def/") && (slices.Contains(seg, "fake") || slices.Contains(seg, "conformance")) ||
		strings.HasPrefix(key, "sdk/testkit/sqlfake/")
}

var wordRun = regexp.MustCompile(`[A-Za-z0-9]+`)

// words returns the words of text, lower-cased, twice: each run of letters and digits
// whole, and each split at its case changes and digits.
func words(text string) [2][]string {
	var w [2][]string
	for _, run := range wordRun.FindAllString(text, -1) {
		w[0] = append(w[0], strings.ToLower(run))
		for _, part := range splitCamel(run) {
			w[1] = append(w[1], strings.ToLower(part))
		}
	}
	return w
}

// holds reports whether w holds the name as a whole word, a kebab-case or snake-case
// segment or a camel-case part: its parts, split at "-", "." and "_", as consecutive
// words of one list.
func holds(w [2][]string, name string) bool {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '.' || r == '_' })
	for _, seq := range w {
		for i := 0; i+len(parts) <= len(seq); i++ {
			if slices.Equal(seq[i:i+len(parts)], parts) {
				return true
			}
		}
	}
	return false
}

// splitCamel splits "riverQueue" into "river", "Queue", "HTTPServer" into "HTTP",
// "Server" and "Kafka3" into "Kafka", "3", as the vocabulary check of
// tools/kernelcheck does.
func splitCamel(s string) []string {
	r := []rune(s)
	var parts []string
	start := 0
	for i := 1; i < len(r); i++ {
		prev, cur := r[i-1], r[i]
		boundary := unicode.IsLower(prev) && unicode.IsUpper(cur) || unicode.IsDigit(prev) != unicode.IsDigit(cur) ||
			unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(r) && unicode.IsLower(r[i+1])
		if boundary {
			parts = append(parts, string(r[start:i]))
			start = i
		}
	}
	return append(parts, string(r[start:]))
}

// places returns what U1 reads at S2a (decisions 14 and 22): every file and its path
// under sdk/, boot/ and loader/, the SDK tree holding every definition, hook, fake and
// suite; the id of every plugin under plugins/, which at S2a are the rows of aiops-core
// and aiops-cli; and the documentation places of 00-decisions.md. The dumps of the
// shipped profiles enter with their first file.
func places(root string) ([]place, error) {
	var out []place
	for _, d := range []string{"sdk", "boot", "loader"} {
		err := filepath.WalkDir(filepath.Join(root, d), func(p string, de fs.DirEntry, err error) error {
			if err != nil || de.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel := repoPath(root, p)
			out = append(out, place{rel, rel, rel})
			if bytes.IndexByte(b, 0) >= 0 {
				return nil // a binary file is read by its path only
			}
			for i, l := range strings.Split(string(b), "\n") {
				out = append(out, place{fmt.Sprintf("%s:%d", rel, i+1), rel, l})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	manifests, _ := filepath.Glob(filepath.Join(root, "plugins", "*", "plugin.yaml"))
	for _, m := range manifests {
		b, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		var id struct {
			ID string `yaml:"id"`
		}
		if err := yaml.Unmarshal(b, &id); err != nil {
			return nil, fmt.Errorf("%s: %w", repoPath(root, m), err)
		}
		out = append(out, place{repoPath(root, m) + ": id", "ids", id.ID})
	}
	doc, err := os.ReadFile(filepath.Join(root, decisionsFile))
	if err != nil {
		return nil, err
	}
	return append(out, docPlaces(string(doc))...), nil
}

func repoPath(root, p string) string {
	rel, _ := filepath.Rel(root, p)
	return filepath.ToSlash(rel)
}

var (
	afterDefault = regexp.MustCompile(`(?i)default:[^)]*`)
	stage        = regexp.MustCompile(`^\s+[0-9]+\. `)
)

// docPlaces returns the documentation places of decision 22 in 00-decisions.md: the
// Decision column of §2.1, without the text after "default:" up to the parenthesis
// that closes it; the classes of the credential table of §5.4; and the stages of §5.6.
// The Rationale column is not read.
func docPlaces(doc string) []place {
	var out []place
	sec := ""
	lines := strings.Split(doc, "\n")
	for i, l := range lines {
		if s, ok := strings.CutPrefix(l, "### "); ok {
			sec, _, _ = strings.Cut(s, " ")
		}
		cells := strings.Split(l, "|")
		row := strings.HasPrefix(l, "|") && !separator(l) && (i+1 == len(lines) || !separator(lines[i+1]))
		at := fmt.Sprintf("%s:%d", decisionsFile, i+1)
		switch {
		case sec == "2.1" && row && len(cells) > 3:
			key := "00 §2.1 row " + strings.TrimSpace(cells[1])
			out = append(out, place{at + " (" + key + ")", key, afterDefault.ReplaceAllString(cells[3], "")})
		case sec == "5.4" && row:
			out = append(out, place{at + " (00 §5.4)", "00 §5.4", cells[1]})
		case sec == "5.6" && stage.MatchString(l):
			out = append(out, place{at + " (00 §5.6)", "00 §5.6", l})
		}
	}
	return out
}

// separator reports whether l is the line under the header of a Markdown table.
func separator(l string) bool { return strings.HasPrefix(l, "|") && strings.Trim(l, "|-: ") == "" }
