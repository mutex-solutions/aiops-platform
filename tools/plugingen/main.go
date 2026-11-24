// Command plugingen runs internal/plugingen in this repository: the manifest parity
// check of CI (ADR-060 decisions 11 and 36), and with -write the generator. Each
// argument is a directory searched for plugin.yaml files, outside testdata and
// hidden directories; each directory that holds one is a Go plugin. An argument that
// holds dist/dist.yaml is the repository root: the signed distribution manifest of
// each binary built in it (distributions), which the builder writes from its build
// (builder.Distribution), is checked, or written, too, and the release's serves names
// no definition outside sdk/def (servesOnlySDK).
//
//	go run ./tools/plugingen .                        # parity of every plugin and manifest
//	go run ./tools/plugingen --write plugins/clock     # write one plugin's files
//
// aiopsctl plugin check --write runs the same package for an author.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/internal/builder"
	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
)

const modPath = "github.com/mutex-solutions/aiops-platform"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "plugingen:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("plugingen", flag.ContinueOnError)
	write := flags.Bool("write", false, "write each plugin's generated half and generated tests")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return errors.New("usage: plugingen [--write] <dir>...")
	}
	dirs, err := pluginDirs(flags.Args())
	errs := []error{err}
	for _, d := range dirs {
		if *write {
			errs = append(errs, plugingen.Write(d))
		} else {
			errs = append(errs, plugingen.Check(d))
		}
	}
	n := 0
	for _, root := range flags.Args() {
		if _, err := os.Stat(filepath.Join(root, "dist", "dist.yaml")); err == nil {
			for _, d := range distributions() {
				n++
				errs = append(errs, distribution(root, d[0], d[1], *write))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "plugingen: %d plugins and %d distribution manifests, each as plugingen writes it\n", len(dirs), n)
	return nil
}

// distributions are the binaries built inside the repository, each main package with
// the directory of the builder's input dist.yaml and of the signed distribution
// manifest it embeds (ADR-060 decisions 20 and 28).
func distributions() [][2]string {
	return [][2]string{{"./cmd/aiops", "dist"}, {"./test/dist/cmd/aiops-test", "test/dist"}}
}

// distribution checks, or writes, dir/distribution.yaml, the manifest of main.
func distribution(root, main, dir string, write bool) error {
	input, err := os.ReadFile(filepath.Join(root, dir, "dist.yaml"))
	if err != nil {
		return err
	}
	want, err := builder.Distribution(context.Background(), builder.Go{Dir: root}, main, input)
	if err != nil {
		return fmt.Errorf("%s: %w", main, err)
	}
	if dir == "dist" { // the release
		if err := servesOnlySDK(root, want); err != nil {
			return err
		}
	}
	file := filepath.Join(root, dir, "distribution.yaml")
	if write {
		return os.WriteFile(file, want, 0o644)
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(got, want) {
		return fmt.Errorf("%s differs from what plugingen writes for %s; run go run ./tools/plugingen --write . (%v)", file, main, err)
	}
	return nil
}

// servesOnlySDK fails when the signed manifest of a release links a package of the
// repository at root that holds a definition.yaml outside sdk/def, which its serves
// names: the test definitions are fixtures of the launcher's tests, at suite revision
// 0 (ADR-060 decisions 28 and 55).
func servesOnlySDK(root string, manifest []byte) error {
	d, err := boot.ReadDistribution("distribution.yaml", manifest)
	if err != nil {
		return err
	}
	var out []string
	for _, p := range d.Packages {
		rel, ok := strings.CutPrefix(p, modPath+"/")
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel), "definition.yaml")); ok && !strings.HasPrefix(rel, "sdk/def/") && err == nil {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return fmt.Errorf("the release serves definitions outside sdk/def: %s (ADR-060 decision 28)", strings.Join(out, ", "))
	}
	return nil
}

// pluginDirs returns each directory under roots that holds a plugin.yaml.
func pluginDirs(roots []string) ([]string, error) {
	var dirs []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case d.IsDir() && p != root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")):
				return filepath.SkipDir
			case !d.IsDir() && d.Name() == "plugin.yaml":
				dirs = append(dirs, filepath.Dir(p))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return dirs, nil
}
