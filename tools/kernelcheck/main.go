// Command kernelcheck enforces structural rules on the kernel module.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kernelcheck:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	usage := fmt.Errorf("usage: kernelcheck modfile <dir> [allowed-module]... | vocab <dir> <wordlist> | lines <dir> <cap> [provisional] [subpackage-dir]... | coverage <profile> <import-path> | testctor <repo-root> | imports <repo-root> | pluginvet <dir>...")
	if len(args) == 0 {
		return usage
	}
	if args[0] == "pluginvet" {
		if len(args) < 2 {
			return usage
		}
		return checkPluginVet(args[1:])
	}
	need := map[string]int{"modfile": 2, "vocab": 3, "lines": 3, "coverage": 3, "testctor": 2, "imports": 2}
	n, ok := need[args[0]]
	if (args[0] == "lines" || args[0] == "modfile") && len(args) > n {
		n = len(args)
	}
	if !ok || len(args) != n {
		return usage
	}
	switch args[0] {
	case "modfile":
		return checkModfile(args[1], args[2:]...)
	case "vocab":
		return checkVocab(args[1], args[2])
	case "lines":
		c, err := strconv.Atoi(args[2])
		if err != nil || c < 0 {
			return fmt.Errorf("invalid cap %q", args[2])
		}
		subs := args[3:]
		provisional := len(subs) > 0 && subs[0] == "provisional"
		if provisional {
			subs = subs[1:]
		}
		return checkLines(args[1], c, provisional, subs...)
	case "testctor":
		return checkTestCtor(args[1])
	case "imports":
		return checkImports(args[1], goListDeps)
	default:
		return checkCoverage(args[1], args[2])
	}
}

// testCtorHomes are the only directories, relative to the repository root, that
// may reference kernel.NewForTest (ADR-034 owner decision 1).
var testCtorHomes = []string{"sdk/kernel", "sdk/testkit/kerneltest"}

var skipDirs = map[string]bool{".git": true, ".claude": true, "node_modules": true, "testdata": true}

// checkTestCtor fails when a .go file outside testCtorHomes references NewForTest
// of a package imported from a path ending in sdk/kernel. The package name is
// taken from the file's own import, so an alias does not hide a use.
// ponytail: a local variable shadowing the import name is still flagged.
func checkTestCtor(root string) error {
	root = filepath.Clean(root)
	var bad []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, h := range testCtorHomes {
			if strings.HasPrefix(rel, h+"/") {
				return nil
			}
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		names := map[string]bool{}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			if path != "sdk/kernel" && !strings.HasSuffix(path, "/sdk/kernel") {
				continue
			}
			name := "kernel"
			if im.Name != nil {
				name = im.Name.Name
			}
			names[name] = true
		}
		if len(names) == 0 {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && x.Sel.Name == "NewForTest" && names[id.Name] {
					bad = append(bad, rel+": references NewForTest")
				}
			case *ast.Ident:
				if names["."] && x.Name == "NewForTest" {
					bad = append(bad, rel+": references NewForTest")
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("kernel.NewForTest outside %s:\n%s", strings.Join(testCtorHomes, " and "), strings.Join(bad, "\n"))
	}
	return nil
}

// checkModfile fails on any exclude directive of dir/go.mod, and on a require or
// replace directive for a module not in allowed. With no allowed module, as for
// sdk/kernel, every require and replace line fails. The go command parses the
// file, so the block and single-line forms read alike.
func checkModfile(dir string, allowed ...string) error {
	out, err := exec.Command("go", "mod", "edit", "-json", filepath.Join(dir, "go.mod")).Output()
	if err != nil {
		msg := err.Error()
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		return fmt.Errorf("%s/go.mod: %s", dir, msg)
	}
	var m struct {
		Require, Exclude []struct{ Path string }
		Replace          []struct{ Old struct{ Path string } }
	}
	if err := json.Unmarshal(out, &m); err != nil {
		return err
	}
	var bad []string
	for _, r := range m.Require {
		if !slices.Contains(allowed, r.Path) {
			bad = append(bad, "require "+r.Path)
		}
	}
	for _, r := range m.Replace {
		if !slices.Contains(allowed, r.Old.Path) {
			bad = append(bad, "replace "+r.Old.Path)
		}
	}
	for _, r := range m.Exclude {
		bad = append(bad, "exclude "+r.Path)
	}
	if len(bad) > 0 {
		return fmt.Errorf("%s/go.mod: forbidden directives (allowed modules: %q):\n%s", dir, allowed, strings.Join(bad, "\n"))
	}
	return nil
}

// sourceFiles lists non-test .go files directly in dir.
func sourceFiles(dir string) ([]string, error) {
	all, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range all {
		if !strings.HasSuffix(p, "_test.go") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

var wordToken = regexp.MustCompile(`[A-Za-z0-9]+`)

// splitCamel splits "tenantID" into "tenant", "ID" and "HTTPServer" into
// "HTTP", "Server".
func splitCamel(s string) []string {
	r := []rune(s)
	var parts []string
	start := 0
	for i := 1; i < len(r); i++ {
		prev, cur := r[i-1], r[i]
		boundary := (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(cur) ||
			unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(r) && unicode.IsLower(r[i+1])
		if boundary {
			parts = append(parts, string(r[start:i]))
			start = i
		}
	}
	return append(parts, string(r[start:]))
}

// readWords returns the words of a word list, one per line and lower-cased; "#"
// starts a comment.
func readWords(wordlist string) (map[string]bool, error) {
	data, err := os.ReadFile(wordlist)
	if err != nil {
		return nil, err
	}
	words := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		if i := strings.Index(l, "#"); i >= 0 {
			l = l[:i]
		}
		if l = strings.ToLower(strings.TrimSpace(l)); l != "" {
			words[l] = true
		}
	}
	return words, nil
}

func checkVocab(dir, wordlist string) error {
	banned, err := readWords(wordlist)
	if err != nil {
		return err
	}
	files, err := sourceFiles(dir)
	if err != nil {
		return err
	}
	var bad []string
	for _, p := range files {
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for n, line := range strings.Split(string(src), "\n") {
			// Underscores are not token characters, so they split too.
			for _, tok := range wordToken.FindAllString(line, -1) {
				for _, part := range splitCamel(tok) {
					if w := strings.ToLower(part); banned[w] {
						bad = append(bad, fmt.Sprintf("%s:%d: forbidden word %q", p, n+1, w))
					}
				}
			}
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("vocabulary violations:\n%s", strings.Join(bad, "\n"))
	}
	return nil
}

// checkLines holds dir to one package and a line cap. A provisional cap is
// printed as such: it stands until the budget is fixed by ADR-059 decision 11.
// Each of subs names a direct subdirectory that is a package of its own with a cap
// of its own, as the fake and the suite of a definition are (ADR-060 decision 35):
// it is neither counted nor refused.
func checkLines(dir string, limit int, provisional bool, subs ...string) error {
	clean := filepath.Clean(dir)
	err := filepath.WalkDir(clean, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || filepath.Dir(p) == clean && slices.Contains(subs, d.Name())) {
			return filepath.SkipDir
		}
		if !d.IsDir() && filepath.Dir(p) != clean && strings.HasSuffix(p, ".go") {
			return fmt.Errorf("%s: .go file in subdirectory; a second package needs an ADR", p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	files, err := sourceFiles(dir)
	if err != nil {
		return err
	}
	total := 0
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		n := strings.Count(string(b), "\n")
		if len(b) > 0 && b[len(b)-1] != '\n' {
			n++
		}
		fmt.Printf("%6d %s\n", n, p)
		total += n
	}
	kind := "cap"
	if provisional {
		kind = "provisional cap"
	}
	fmt.Printf("%6d total (%s %d)\n", total, kind, limit)
	if total > limit {
		return fmt.Errorf("total %d lines exceeds cap %d", total, limit)
	}
	return nil
}

func checkCoverage(profile, importPath string) error {
	f, err := os.Open(profile)
	if err != nil {
		return err
	}
	defer f.Close()
	type cnt struct{ stmts, covered int }
	files := map[string]*cnt{}
	prefix := strings.TrimSuffix(importPath, "/") + "/"
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if (n == 1 && strings.HasPrefix(line, "mode:")) || line == "" {
			continue
		}
		// name.go:l.c,l.c stmts count
		colon := strings.LastIndex(line, ":")
		fields := strings.Fields(line)
		if colon < 0 || len(fields) != 3 {
			return fmt.Errorf("%s:%d: malformed line %q", profile, n, line)
		}
		stmts, err1 := strconv.Atoi(fields[1])
		hits, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			return fmt.Errorf("%s:%d: malformed counts %q", profile, n, line)
		}
		name := line[:colon]
		if !strings.HasPrefix(name, prefix) || strings.HasSuffix(name, "_test.go") {
			continue
		}
		c := files[name]
		if c == nil {
			c = &cnt{}
			files[name] = c
		}
		c.stmts += stmts
		if hits > 0 {
			c.covered += stmts
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no files under %s in %s", importPath, profile)
	}
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	var bad []string
	for _, k := range names {
		c := files[k]
		pct := 100.0
		if c.stmts > 0 {
			pct = 100 * float64(c.covered) / float64(c.stmts)
		}
		fmt.Printf("%6.1f%% %s\n", pct, k)
		if c.covered < c.stmts {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("incomplete coverage: %s", strings.Join(bad, ", "))
	}
	return nil
}
