// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Key is the commands registry, class `-` (open) on both sides, at major 0 (ADR-061
// decision 1).
var Key = kernel.NewKey[Registry]("commands", 0)

// The errors of the definition, matched with errors.Is. A call on a released registry,
// or into a command whose fiber is quiescing, returns an error that matches
// kernel.ErrInactive (07-c4-plugin-sdk.md:518).
var (
	// ErrUsage is a usage error: Run returns an error that matches it for a command line
	// that the command cannot run as written, and the dispatcher answers exit 2 with the
	// error and the synopsis (decision 10).
	ErrUsage = errors.New("usage")
	// ErrPath refuses a registration whose path breaks the word rule, is taken, or is a
	// prefix of a registered path or has one as its prefix, so that every path names one
	// command (decision 3).
	ErrPath = errors.New("commands: the path breaks the word rule, is taken, or overlaps a registered path")
)

// Command is the contribution: one command of aiopsctl (decision 2).
type Command interface {
	// Run runs the command. It returns nil on success, an error that matches ErrUsage
	// for a usage error, and any other error for a denial, a failed step or
	// rebuild-required. It writes only to the streams of inv, and it returns when ctx
	// ends, with an error that matches ctx's.
	Run(ctx context.Context, inv Invocation) error
}

// Kind is the kind of a flag.
type Kind int

// The three kinds of a flag (decision 2).
const (
	Bool   Kind = iota // takes no value unless written --name=false or --name=true
	String             // takes one value; the last one written wins
	List               // takes one value and repeats
)

// Flag is one flag of a command. Its name keeps the word rule. Value is how the
// synopsis names its value, "<p>" for example; Default is the value of a bool or a
// string flag that the command line does not give; Required makes a command line
// without the flag a usage error.
type Flag struct {
	Name     string
	Kind     Kind
	Value    string
	Default  string
	Usage    string
	Required bool
}

// Spec is what a command is registered with (decision 2). Path is one or more words,
// each [a-z][a-z0-9-]*, and its first word is not help, which the dispatcher owns.
// Args names the positional arguments in the synopsis, "<p>" or "[<p>]" for example,
// and Min and Max are the least and greatest number of them; a Max below 0 is no limit.
type Spec struct {
	Path     []string
	Summary  string
	Args     []string
	Min, Max int
	Flags    []Flag
}

// Name is the path as one string, "profile render".
func (s Spec) Name() string { return strings.Join(s.Path, " ") }

// Streams are the three streams of an invocation.
type Streams struct {
	In       io.Reader
	Out, Err io.Writer
}

// Invocation is one call of a command (decision 2): its positional arguments, the value
// of each of its flags by name, the streams, the working directory Dir and the lookup
// Env of the environment, both from cmdline, and the home that the launcher resolved,
// "" when there is none. A command resolves a relative path against Dir and reads the
// environment only through Env.
type Invocation struct {
	Path  []string
	Args  []string
	Flags map[string][]string
	Streams
	Dir  string
	Env  func(name string) (string, bool)
	Home string
}

// Bool is the value of a bool flag.
func (i Invocation) Bool(name string) bool { return i.Value(name) == "true" }

// Value is the value of a string flag, "" for a flag that has none.
func (i Invocation) Value(name string) string {
	if v := i.Flags[name]; len(v) > 0 {
		return v[len(v)-1]
	}
	return ""
}

// List is the values of a list flag, in the order written.
func (i Invocation) List(name string) []string { return i.Flags[name] }

// Registry is what Key serves (decision 3).
type Registry interface {
	// Register adds cmd under s as an effect of c's fiber, labelled Label(s.Path), so
	// the registration leaves when that fiber unloads (07-c4-plugin-sdk.md:519). It
	// returns an error that matches ErrPath for a path that breaks the word rule, is
	// taken or overlaps a registered path, and one that matches
	// kernel.ErrInactiveEffect when c's fiber is unloading.
	Register(c *kernel.Context, s Spec, cmd Command) error
	// Dispatch parses argv by the rules of decision 8, prints help by decision 9, calls
	// the command inside a lease on the fiber that registered it, recovers a panic of
	// its Run, and returns the exit code of decision 10 with the error that decided it,
	// nil for code 0.
	Dispatch(ctx context.Context, argv []string, s Streams) (code int, err error)
	// Inventory lists the registered paths, sorted. The fiber that owns each is the one
	// whose ledger holds its Label (07-c4-plugin-sdk.md:521; ADR-060 decision 5). It is
	// empty once the registry is released.
	Inventory() []string
}

// Label is the ledger label of the registration of path.
func Label(path []string) string { return "commands:" + strings.Join(path, " ") }

// Pending is a row of the composition that needs commands and is not ACTIVE: its id,
// its state and the keys it waits for (decision 9).
type Pending struct {
	Row      string
	State    string
	WaitsFor []string
}

// Environment is what a Table gives every invocation, and the rows it names in help
// and in the answer to a path that no row registered.
type Environment struct {
	Dir     string
	Env     func(name string) (string, bool)
	Home    string
	Pending func() []Pending // nil for none
}
