// SPDX-License-Identifier: Apache-2.0

// Package templates holds the templates of aiopsctl plugin new, embedded in aiopsctl
// (ADR-060 decision 24; 07-c4-plugin-sdk.md:1549-1559): a Go template for each registry
// that exists, echoers, stampers and commands (ADR-061 decision 6), and the template of
// a connector pack with no code (ADR-047 decision 12). Every template file carries the
// suffix .tmpl, so the Go tool builds none of them; each is a text/template rendered
// with Data, and its path, without the suffix, is the path it is written to.
//
// A Go template writes the files of 07-c4-plugin-sdk.md:1551-1559 but the two generated
// tests and the generated half of plugin.yaml, which plugin check --write writes from
// the plugin's value. The connector template writes the files of a pack but its bundle,
// whose rows plugin new takes from the release it runs against.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"text/template"
)

// FS holds the template files: shared/ for every template, go/ for every Go template,
// one directory per registry, and connector/.
//
//go:embed all:shared all:go all:echoers all:stampers all:commands all:connector
var FS embed.FS

// Registries are the registries that have a Go template, sorted.
var Registries = []string{"commands", "echoers", "stampers"}

// Connector names the template of a connector pack.
const Connector = "connector"

// Data is what a template is rendered with.
type Data struct {
	ID, Owner, Name string // the plugin id and its two segments
	Package         string // the Go package name
	Module          string // the module path
	SDK             string // the version of the SDK modules that go.mod requires
	Registry        string // the registry, or Connector
	Contrib         bool   // whether the plugin lives under contrib/ of this repository
}

const repo = "github.com/mutex-solutions/aiops-platform"

var idForm = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*/[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// NewData returns the data of the plugin id at the SDK version sdk. With contrib the
// owner is contrib and the module is contrib/<name> of this repository (08-profiles-
// roadmap-adrs.md:407; ADR-048 decision 23); otherwise the module is
// example.com/<owner>/<name>, which the author replaces. The owners aiops and contrib
// are reserved to the release pipeline, so an id names neither (ADR-048 decision 14).
func NewData(id, sdk string, contrib bool) (Data, error) {
	if !idForm.MatchString(id) {
		return Data{}, fmt.Errorf("%q is not a plugin id <owner>/<name>, each segment kebab-case", id)
	}
	owner, name, _ := strings.Cut(id, "/")
	if owner == "aiops" || owner == "contrib" {
		return Data{}, fmt.Errorf("the owner %s is reserved to the release pipeline; --contrib writes a plugin under contrib/", owner)
	}
	d := Data{ID: id, Owner: owner, Name: name, Package: strings.ReplaceAll(name, "-", ""), SDK: sdk,
		Module: "example.com/" + id, Contrib: contrib}
	if contrib {
		d.ID, d.Owner, d.Module = "contrib/"+name, "contrib", repo+"/contrib/"+name
	}
	return d, nil
}

// Render returns the files of the template of registry, a registry of Registries or
// Connector, rendered with d, by slash path.
func Render(registry string, d Data) (map[string][]byte, error) {
	dirs := []string{"shared", Connector}
	if registry != Connector {
		if !slices.Contains(Registries, registry) {
			return nil, fmt.Errorf("no template for the registry %q: the templates are %s and %s", registry, strings.Join(Registries, ", "), Connector)
		}
		dirs = []string{"shared", "go", registry}
	}
	d.Registry = registry
	out := map[string][]byte{}
	for _, dir := range dirs {
		err := fs.WalkDir(FS, dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			src, err := fs.ReadFile(FS, p)
			if err != nil {
				return err
			}
			t, err := template.New(p).Option("missingkey=error").Parse(string(src))
			if err != nil {
				return err
			}
			var b bytes.Buffer
			if err := t.Execute(&b, d); err != nil {
				return err
			}
			out[strings.TrimSuffix(strings.TrimPrefix(p, dir+"/"), ".tmpl")] = b.Bytes()
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Title returns the title of a plugin of the template: its name with the words
// capitalised.
func (d Data) Title() string {
	words := strings.Split(d.Name, "-")
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
