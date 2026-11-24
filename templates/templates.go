// SPDX-License-Identifier: Apache-2.0

// Package templates holds the templates of aiopsctl plugin new (ADR-060 decision 24).
package templates

import (
	"embed"
	"errors"
)

// FS holds the template files.
var FS embed.FS

// Registries are the registries that have a Go template.
var Registries []string

// Data is what a template is rendered with.
type Data struct {
	ID, Owner, Name string
	Package, Module string
	SDK             string
}

// NewData returns the data of the plugin id.
func NewData(id, sdk string, contrib bool) (Data, error) { return Data{}, errors.New("not built yet") }

// Render returns the files of the template of registry, by path.
func Render(registry string, d Data) (map[string][]byte, error) {
	return nil, errors.New("not built yet")
}
