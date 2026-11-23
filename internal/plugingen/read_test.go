// SPDX-License-Identifier: Apache-2.0

package plugingen

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/mutex-solutions/aiops-platform/boot"
	"github.com/mutex-solutions/aiops-platform/sdk/plugin"
)

// boot may not import the plugin package, so it reads manifests into types of its
// own. These tests hold its reading of schema 1.0 equal to plugin.Manifest, the Go
// type plugingen writes from (ADR-060 decision 14; ADR-048 decision 15).

// shape lists the field paths of t, each with the type of its leaf; the fields of an
// embedded struct are its own.
func shape(t reflect.Type, path string) []string {
	switch t.Kind() {
	case reflect.Pointer:
		return shape(t.Elem(), path+"*")
	case reflect.Slice:
		return shape(t.Elem(), path+"[]")
	case reflect.Struct:
		var out []string
		for i := range t.NumField() {
			f := t.Field(i)
			p := path + "." + f.Name
			if f.Anonymous {
				p = path
			}
			out = append(out, shape(f.Type, p)...)
		}
		return out
	}
	return []string{path + " " + t.String()}
}

// filled adds to out the path of each leaf of v, as shape names it, that is not zero.
func filled(v reflect.Value, path string, out map[string]bool) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			filled(v.Elem(), path+"*", out)
		}
	case reflect.Slice:
		for i := range v.Len() {
			filled(v.Index(i), path+"[]", out)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			p := path + "." + f.Name
			if f.Anonymous {
				p = path
			}
			filled(v.Field(i), p, out)
		}
	default:
		if !v.IsZero() {
			out[path+" "+v.Type().String()] = true
		}
	}
}

// asJSON is v as JSON data, so that two types with one set of fields, and an int64
// and a float64 of one value, compare equal.
func asJSON(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

var readModes = []boot.ManifestMode{boot.ManifestLenient, boot.ManifestStrict}

// boot's types name every field and inner field of plugin.Manifest, in order, with
// the same Go types.
func TestBootReadsTheFieldsOfThePluginType(t *testing.T) {
	b, p := shape(reflect.TypeFor[boot.Manifest](), ""), shape(reflect.TypeFor[plugin.Manifest](), "")
	if !slices.Equal(b, p) {
		t.Fatalf("boot.Manifest\n%s\nplugin.Manifest\n%s", strings.Join(b, "\n"), strings.Join(p, "\n"))
	}
}

// Boot reads every field of a manifest, as the YAML library writes plugin.Manifest
// for plugingen, back to the value written, in both modes.
func TestBootReadsEveryFieldAsThePluginTypeWritesIt(t *testing.T) {
	schema := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object",
		"additionalProperties": false, "properties": map[string]any{
			"url":   map[string]any{"type": "string", "x-aiops-volatile": true, "x-aiops-tenant": true},
			"ratio": map[string]any{"type": "number", "minimum": 0.5, "default": 1.0}}}
	ms := []plugin.Manifest{{
		ManifestVersion: "1.0", ID: "acme/notifier-webhook", Version: "0.1.0", Title: "Webhook notifier",
		Description: "Sends a notification.", License: "Apache-2.0 OR LicenseRef-acme",
		Publisher: plugin.Publisher{ID: "acme", Name: "Acme", URL: "https://acme.example"},
		Source:    "https://acme.example/src", Support: &plugin.Support{URL: "https://acme.example/help", Email: "help@acme.example"},
		Stability: "preview", Scope: []string{"process", "tenant"}, Runtimes: []string{"inproc", "remote"},
		Capabilities: &plugin.Capabilities{Network: []string{"acme.example:443", "config:/url"}, Secrets: []string{"token"}},
		Integrates: []plugin.Integration{{System: "examplesys", Versions: "1.x", Tested: []string{"1.2", "1.3"},
			Credential: "broader", License: "MIT"}},
		Generated: plugin.Generated{
			Uses:         []plugin.Use{{Key: "dialer", Major: 0, As: "need"}, {Key: "notifiers", Major: 1, As: "want"}},
			Provides:     []plugin.KeyRef{{Key: "acme.ready", Major: 2}},
			Registers:    []plugin.KeyRef{{Key: "notifiers", Major: 1}},
			Hooks:        []plugin.HookRef{{Hook: "internal/status", Stage: "default"}},
			ConfigSchema: schema, Durable: []any{},
			Requires: plugin.Requires{PluginAPI: plugin.APIVersion, RemoteProtocol: []int{1},
				Definitions: map[string][]string{"notifiers": {"0.1", "1.0"}}},
		},
	}, {
		ManifestVersion: "1.0", ID: "aiops/echo-generic", Version: "1.0.0", Title: "Echo", License: "Apache-2.0",
		Publisher: plugin.Publisher{ID: "aiops"}, Stability: "ga", Scope: []string{"process"}, Runtimes: []string{"inproc"},
		Instantiable: true,
		Generated: plugin.Generated{Uses: []plugin.Use{}, Provides: []plugin.KeyRef{},
			Registers: []plugin.KeyRef{{Key: "echoers", Major: 0}}, Hooks: []plugin.HookRef{},
			ConfigSchema: map[string]any{}, Durable: []any{},
			Requires: plugin.Requires{PluginAPI: plugin.APIVersion, Definitions: map[string][]string{"echoers": {"0.1"}}}},
	}}
	seen := map[string]bool{}
	for _, m := range ms {
		filled(reflect.ValueOf(m), "", seen)
		src, err := yaml.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range readModes {
			got, err := boot.ReadManifest("plugin.yaml", src, mode)
			if err != nil {
				t.Fatalf("%s, mode %d: %v\n%s", m.ID, mode, err, src)
			}
			if a, b := asJSON(t, got), asJSON(t, m); !reflect.DeepEqual(a, b) {
				t.Errorf("%s, mode %d: boot reads\n%v\nplugingen wrote\n%v", m.ID, mode, a, b)
			}
		}
	}
	// durable holds no entry until S3 fixes its shape (ADR-048 decision 11).
	for _, p := range shape(reflect.TypeFor[plugin.Manifest](), "") {
		if !seen[p] && p != ".Durable[] interface {}" {
			t.Errorf("no manifest of this test sets %s", p)
		}
	}
}

// Every plugin.yaml of the repository reads, in both of boot's modes, as the YAML
// library reads it into plugin.Manifest with no unknown field.
func TestBootReadsEveryManifestOfTheRepositoryAsThePluginType(t *testing.T) {
	root := filepath.Join("..", "..")
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && p != root && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		case d.IsDir() || d.Name() != "plugin.yaml":
			return nil
		}
		n++
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dec := yaml.NewDecoder(bytes.NewReader(src))
		dec.KnownFields(true)
		var want plugin.Manifest
		if err := dec.Decode(&want); err != nil {
			t.Errorf("%s: %v", p, err)
			return nil
		}
		for _, mode := range readModes {
			got, err := boot.ReadManifest(p, src, mode)
			if err != nil {
				t.Errorf("mode %d: %v", mode, err)
			} else if a, b := asJSON(t, got), asJSON(t, want); !reflect.DeepEqual(a, b) {
				t.Errorf("%s, mode %d: boot reads\n%v\nthe plugin type reads\n%v", p, mode, a, b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 12 {
		t.Fatalf("found %d manifests; the module tables and this package's testdata hold at least 12", n)
	}
}

// boot reads serves as Serves writes it into the signed distribution manifest
// (ADR-060 decisions 9 and 20): the kernel part, and each served major with its
// compatibleFrom, deprecated, suite revision, classes and hooks.
func TestBootReadsServesAsServesWritesIt(t *testing.T) {
	ds := append(defs(t), Definition{Name: "notifiers", Major: 0, Version: "0.3", Suite: 2,
		Deprecated: map[string]string{"since": "0.4.0", "removeNotBefore": "2027-11-23", "successor": "v1"},
		Classes:    map[string]Class{"notifiers": {Provider: "bound", Consumer: "signed"}}})
	want := Serves(ds)
	out, err := yaml.Marshal(map[string]any{"distVersion": 1, "plugins": map[string]any{}, "modules": []any{}, "packages": []any{},
		"pins": []any{}, "bindings": map[string]any{}, "acceptLicenses": []any{}, "serves": want})
	if err != nil {
		t.Fatal(err)
	}
	d, err := boot.ReadDistribution("dist.yaml", out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	conv := func(sd boot.ServedDefinition) Served {
		s := Served{Version: sd.Version, CompatibleFrom: sd.CompatibleFrom, Deprecated: sd.Deprecated, Suite: sd.Suite, Classes: map[string]Class{}}
		for k, c := range sd.Classes {
			s.Classes[k] = Class(c)
		}
		for k, h := range sd.Hooks {
			if s.Hooks == nil {
				s.Hooks = map[string]Hook{}
			}
			s.Hooks[k] = Hook(h)
		}
		return s
	}
	kernel := conv(d.Serves.Kernel)
	got := ServesData{PluginAPI: d.Serves.PluginAPI, Kernel: KernelPart{Keys: kernel.Classes, Hooks: kernel.Hooks}, Definitions: map[string][]Served{}}
	for name, list := range d.Serves.Definitions {
		for _, sd := range list {
			got.Definitions[name] = append(got.Definitions[name], conv(sd))
		}
	}
	if !reflect.DeepEqual(got, want) || len(want.Definitions) != 4 {
		t.Errorf("boot reads\n%+v\nServes writes\n%+v", got, want)
	}
}
