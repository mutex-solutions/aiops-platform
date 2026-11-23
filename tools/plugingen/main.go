// Command plugingen runs internal/plugingen in this repository: the manifest parity
// check of CI (ADR-060 decisions 11 and 36), and with -write the generator. Each
// argument is a directory searched for plugin.yaml files, outside testdata and
// hidden directories; each directory that holds one is a Go plugin.
//
//	go run ./tools/plugingen .                       # parity of every plugin
//	go run ./tools/plugingen -write plugins/clock     # write one plugin's files
//
// aiopsctl plugin check --write runs the same package for an author.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mutex-solutions/aiops-platform/internal/plugingen"
)

func main() {
	args := os.Args[1:]
	write := len(args) > 0 && args[0] == "-write"
	if write {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: plugingen [-write] <dir>...")
		os.Exit(2)
	}
	dirs, err := pluginDirs(args)
	var errs []error
	for _, d := range dirs {
		if write {
			errs = append(errs, plugingen.Write(d))
		} else {
			errs = append(errs, plugingen.Check(d))
		}
	}
	if err := errors.Join(append(errs, err)...); err != nil {
		fmt.Fprintln(os.Stderr, "plugingen:", err)
		os.Exit(1)
	}
	fmt.Printf("plugingen: %d plugins, every generated half and generated test as plugingen writes it\n", len(dirs))
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
