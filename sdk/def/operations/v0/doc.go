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
// UseDist (dist sign and dist use, on an OCI image reference), and Pack, Sign, Push,
// Inspect and Verify (the OCI artifact of ADR-047 decision 1, its signature and its
// attestation as referrers, ADR-020 decisions 2, 9 and 24). Verify reports its suite
// step as not built until the suites of package check land (ADR-060 decision 46). The
// other methods of ADR-061 decision 16 join with the parts they need: the builder and
// the gate of the toolbox for Check, Gate, Build, Smoke, New, Install, Enable, Disable
// and Remove.
package operations
