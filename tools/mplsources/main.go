// Command mplsources is the MPL-2.0 release check of ADR-044 decision 6
// (08-profiles-roadmap-adrs.md §5.1 L8): it fails when a module that the release image
// links under MPL-2.0 has no zip in the archive the release keeps whose h1 hash equals
// its pin. It reads a release's output directory as the second phase of the release
// build writes it: distribution.yaml, the signed manifest, whose modules are those the
// server links, each with its version, sum and detected licence; and mpl-sources/, the
// archive of the zips the build checked.
//
//	go run ./tools/mplsources <release output directory>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/sumdb/dirhash"

	"github.com/mutex-solutions/aiops-platform/boot"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mplsources <release output directory>")
		os.Exit(2)
	}
	bad, err := check(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "mplsources:", err)
		os.Exit(1)
	}
	if len(bad) > 0 {
		fmt.Fprintln(os.Stderr, "mplsources: the MPL-2.0 release check fails (ADR-044 decision 6):\n"+strings.Join(bad, "\n"))
		os.Exit(1)
	}
	fmt.Println("mplsources: every MPL-2.0 module of the release image has a zip of its pin in mpl-sources/")
}

// check returns one line per MPL-2.0 module of the signed manifest in dir whose pin is
// the hash of no zip under dir/mpl-sources.
func check(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "distribution.yaml"))
	if err != nil {
		return nil, err
	}
	d, err := boot.ReadDistribution("distribution.yaml", data)
	if err != nil {
		return nil, err
	}
	zips, _ := filepath.Glob(filepath.Join(dir, "mpl-sources", "*.zip"))
	var hashes []string
	for _, z := range zips {
		h, err := dirhash.HashZip(z, dirhash.Hash1)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", z, err)
		}
		hashes = append(hashes, h)
	}
	var bad []string
	for _, m := range d.Modules {
		if slices.Contains(strings.Fields(m.Licence), "MPL-2.0") && !slices.Contains(hashes, m.Sum) {
			bad = append(bad, fmt.Sprintf("%s %s (%s) is linked under %s, and mpl-sources/ holds no zip of that hash", m.Path, m.Version, m.Sum, m.Licence))
		}
	}
	return bad, nil
}
