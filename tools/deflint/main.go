// Command deflint is the definition lint of the standing rule 1 of S2a (ADR-060
// decision 32; 08-profiles-roadmap-adrs.md:676). This is its outline: no check is
// written yet.
package main

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
)

const (
	modPath    = "github.com/mutex-solutions/aiops-platform"
	kernelPath = modPath + "/sdk/kernel"
)

var pendingSuites = []string{"clock"}

func main() {}

type definition struct {
	name  string
	major int
	dir   string
	rel   string
	keys  []string
}

type plugin struct {
	id                 string
	provides, consumes []string
}

type listed struct{}

type checker struct{ fset *token.FileSet }

var errTODO = errors.New("deflint: not written")

func lint(string, []plugin) ([]string, int, error)                        { return nil, 0, errTODO }
func distributions() []plugin                                             { return nil }
func files(*definition) []string                                          { return nil }
func layout(*definition) []string                                         { return nil }
func imports(*definition) []string                                        { return nil }
func roles([]*definition, []plugin) []string                              { return nil }
func list(string, ...string) (map[string]*listed, error)                  { return nil, nil }
func newChecker(map[string]*listed) *checker                              { return &checker{fset: token.NewFileSet()} }
func shapes(*types.Package) []string                                      { return nil }
func sideDoors(*token.FileSet, string, *types.Info, []*ast.File) []string { return nil }

func (c *checker) check(path string, files []*ast.File) (*types.Package, *types.Info, error) {
	return types.NewPackage(path, "x"), &types.Info{}, nil
}
