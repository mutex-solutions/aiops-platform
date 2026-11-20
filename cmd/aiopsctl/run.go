package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/mutex-solutions/aiops-platform/boot"
)

// Exit codes. The documents give none for aiopsctl, so these are the
// conventional ones (an open point of S1).
const (
	exitOK      = 0
	exitRefused = 1 // the composition is refused: a read, compose, gate, graph or required-set error
	exitUsage   = 2
)

const usage = `usage: aiopsctl profile render --profile <name> --dist <dir> [flags]

  --profile <name>        profile to compose (required)
  --home <dir>            home with profiles/<name>/ (default $AIOPS_HOME)
  --bundles <dir>         directory with bundles/<name>/bundle.yaml (default: the home)
  --dist <dir>            directory with dist.yaml and modules.yaml (required)
  --patch <file>          layer 4 overlay; repeat for several
  --role <role>           edge, api or worker; repeat or comma-separate (default: all)
  --server                every inserted row must state roles
  --dump                  print the composed tree
  --dump-hooks            print every hook chain
  --dump-config-schema    print the merged config schema

A relative path is relative to $PWD. Dumps go to stdout, diagnostics to stderr.
Exit codes: 0 ok, 1 refused, 2 usage.
`

// list is a repeatable flag.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

// run is the whole program, with its inputs injected so that a test can drive
// it. fsys is rooted at "/" in main, and env supplies AIOPS_HOME and PWD.
func run(args []string, _ io.Reader, stdout, stderr io.Writer, fsys fs.FS, env func(string) string) int {
	if len(args) < 2 || args[0] != "profile" || args[1] != "render" {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	fl := flag.NewFlagSet("profile render", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	var patches, roles list
	profile := fl.String("profile", "", "")
	home := fl.String("home", env("AIOPS_HOME"), "")
	bundles := fl.String("bundles", "", "")
	dist := fl.String("dist", "", "")
	server := fl.Bool("server", false, "")
	dump := fl.Bool("dump", false, "")
	hooks := fl.Bool("dump-hooks", false, "")
	schema := fl.Bool("dump-config-schema", false, "")
	fl.Var(&patches, "patch", "")
	fl.Var(&roles, "role", "")
	if err := fl.Parse(args[2:]); err != nil {
		fmt.Fprintf(stderr, "aiopsctl: %v\n%s", err, usage)
		return exitUsage
	}
	if *profile == "" || *dist == "" || *home == "" || fl.NArg() > 0 {
		fmt.Fprintf(stderr, "aiopsctl: --profile, --dist and a home (--home or $AIOPS_HOME) are required, and there are no arguments\n%s", usage)
		return exitUsage
	}
	rs, err := parseRoles(roles)
	if err != nil {
		fmt.Fprintf(stderr, "aiopsctl: %v\n%s", err, usage)
		return exitUsage
	}
	if *bundles == "" {
		*bundles = *home
	}
	c := &cli{fsys: fsys, pwd: strings.TrimPrefix(path.Clean("/"+env("PWD")), "/")}
	mods, d, err := c.readDist(*dist)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRefused
	}
	src := boot.Sources{Profile: *profile, Home: c.sub(*home), Bundles: c.sub(*bundles)}
	for _, p := range patches {
		src.Patches = append(src.Patches, c.file(p))
	}
	comp, warns, err := boot.Compose(src, boot.Options{Server: *server, Modules: mods})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRefused
	}
	for _, w := range warns {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	// boot mounts the loader itself, so a row may need what it provides; a profile that
	// holds a row "loader" of its own (the fixtures of the boot package do) is admitted as it is.
	if _, ok := mods[boot.LoaderID]; ok && !slices.ContainsFunc(comp.Rows, func(r boot.ComposedRow) bool { return r.ID == boot.LoaderRowID }) {
		comp = boot.WithLoader(comp)
	}
	a := boot.Admit(comp, mods, d, rs)
	for _, line := range a.Diagnostics() {
		fmt.Fprintln(stderr, line)
	}
	var out []section
	if *dump {
		out = append(out, section{"--dump", boot.RenderDump(a)})
	}
	if *hooks {
		out = append(out, section{"--dump-hooks", boot.RenderHooks(a)})
	}
	if *schema {
		out = append(out, section{"--dump-config-schema", boot.RenderConfigSchema(a)})
	}
	for _, s := range out {
		if len(out) > 1 {
			fmt.Fprintf(stdout, "=== %s ===\n", s.name)
		}
		fmt.Fprint(stdout, s.text)
	}
	if a.Refused() {
		return exitRefused
	}
	return exitOK
}

type section struct{ name, text string }

func parseRoles(in list) ([]boot.Role, error) {
	var out []boot.Role
	for _, item := range in {
		for _, s := range strings.Split(item, ",") {
			switch r := boot.Role(strings.TrimSpace(s)); r {
			case boot.RoleEdge, boot.RoleAPI, boot.RoleWorker:
				out = append(out, r)
			default:
				return nil, fmt.Errorf("role %q is not one of edge, api, worker", s)
			}
		}
	}
	return out, nil
}

// cli resolves paths. fsys is rooted at "/" and pwd is the working directory
// without its leading slash ("" for the root).
type cli struct {
	fsys fs.FS
	pwd  string
}

// rooted returns p as a path in fsys: an absolute path loses its slash and a
// relative one is joined to the working directory.
func (c *cli) rooted(p string) string {
	if !path.IsAbs(p) {
		p = path.Join("/", c.pwd, p)
	}
	return strings.TrimPrefix(path.Clean(p), "/")
}

// sub returns the directory p of fsys as a file system.
func (c *cli) sub(p string) fs.FS {
	r := c.rooted(p)
	if r == "" {
		r = "."
	}
	s, _ := fs.Sub(c.fsys, r) // r is a clean slash path with no "..": Sub cannot fail
	return s
}

// file returns a --patch file. A relative path stays relative to the working
// directory in provenance, so a dump never holds an absolute path unless the
// operator wrote one.
func (c *cli) file(p string) boot.File {
	if clean := path.Clean(p); !path.IsAbs(p) && fs.ValidPath(clean) {
		return boot.File{FS: c.sub("."), Path: clean}
	}
	return boot.File{FS: c.fsys, Path: c.rooted(p)}
}

// readDist reads dist.yaml and modules.yaml from a directory.
func (c *cli) readDist(dir string) (boot.ModuleTable, boot.DistManifest, error) {
	d := c.sub(dir)
	md, err := fs.ReadFile(d, "modules.yaml")
	if err != nil {
		return nil, boot.DistManifest{}, err
	}
	mods, err := boot.ParseModules("modules.yaml", md)
	if err != nil {
		return nil, boot.DistManifest{}, err
	}
	dd, err := fs.ReadFile(d, "dist.yaml")
	if err != nil {
		return nil, boot.DistManifest{}, err
	}
	dist, err := boot.ParseDist("dist.yaml", dd)
	return mods, dist, err
}
