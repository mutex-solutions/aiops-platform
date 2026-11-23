// Command plugingen runs internal/plugingen in this repository: the manifest parity
// check of CI (ADR-060 decisions 11 and 36), and with -write the generator. Each
// argument is a directory searched for plugin.yaml files, outside testdata and
// hidden directories; each directory that holds one is a Go plugin.
//
//	go run ./tools/plugingen .                        # parity of every plugin
//	go run ./tools/plugingen --write plugins/clock     # write one plugin's files
//
// aiopsctl plugin check --write runs the same package for an author.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
)

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
	if err := errors.Join(errs...); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "plugingen: %d plugins, every generated half and generated test as plugingen writes it\n", len(dirs))
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
