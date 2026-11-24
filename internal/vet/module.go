// SPDX-License-Identifier: Apache-2.0

// Package vet is the import analyzer and plugin vet: step 3 of plugin check, T0
// static (ADR-060 decision 23; 07-c4-plugin-sdk.md:1569).
package vet

import (
	"errors"
	"fmt"
)

// Finding is one finding of step 3: its file, relative to the plugin directory, its
// line and its text.
type Finding struct {
	File string
	Line int
	Text string
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.Text) }

// Module runs the import analyzer and plugin vet on the plugin in dir, a directory of
// any Go module.
func Module(dir string) ([]Finding, error) { return nil, errors.New("vet: not built yet") }
