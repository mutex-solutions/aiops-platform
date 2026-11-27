// SPDX-License-Identifier: Apache-2.0

// Package commands is the definition of the commands registry: the commands of
// aiopsctl, which every command row contributes and the cli row dispatches (ADR-061
// decisions 1 to 11; 00-decisions.md:222). The key is plural, a registry of
// contributions of class `-` on both sides, with no remote contribution; it holds no
// tenant data and owns no hook.
//
// The rules of decisions 8 to 10, how argv is parsed, how help is printed and which
// exit code an outcome gets, are one text in this package, Table, which the cli row
// provides and the recording fake runs, so the two dispatch by the same rules, as
// launch.RequiredSet.Unmet is one reading that readiness and reload share. The fake is
// in the fake package and the suite, with its obligations half and its guard half, in
// the conformance package.
//
// The package imports the standard library and sdk/kernel only.
package commands
