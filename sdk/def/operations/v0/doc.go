// SPDX-License-Identifier: Apache-2.0

// Package operations is the operations definition (ADR-061 decisions 14 to 19): the
// one library that writes a home, $AIOPS_HOME, under one write lock, and that the
// command rows of aiopsctl consume, and from S6a the plugin manager. The row that
// serves it is plugins/operations; the in-memory fake is in the fake package and the
// suite in the conformance package.
//
// At this minor the key serves the methods whose every step is built: Home, Render,
// VerifyProfile, InitProfile and SanitizeProfile (the profile commands, through the
// launcher's toolbox), AddTrust (trust add), GenerateKey (key generate), SignDist and
// UseDist (dist sign and dist use, on an OCI image reference), Pack, Sign, Push,
// Inspect and Verify (the OCI artifact of ADR-047 decision 1, its signature and its
// attestation as referrers, ADR-020 decisions 2, 9 and 24), New and Check (plugin new
// and plugin check, through the toolbox), and Gate, Install, Enable, Disable and
// Remove (plugin gate --against and the plugins of a profile, which submit over the
// socket of ADR-062). Pack and Verify run the suites of plugin check as Check runs
// them, and a binary that does not carry them reports the suite step as not built
// (ADR-060 decision 46). Build, added at 0.3, is dist build through the builder of
// the toolbox (ADR-044); Smoke, added at 0.4, is plugin smoke through the same builder,
// with which Verify builds in-process code since 0.4.
package operations
