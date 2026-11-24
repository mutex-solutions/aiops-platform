// SPDX-License-Identifier: Apache-2.0

// Package operations is the operations definition (ADR-061 decisions 14 to 19): the
// one library that writes a home, $AIOPS_HOME, under one write lock, and that the
// command rows of aiopsctl consume, and from S6a the plugin manager. The row that
// serves it is plugins/operations; the in-memory fake is in the fake package and the
// suite in the conformance package.
//
// At this minor the key serves the methods whose every step is built: Home,
// AddTrust (trust add), GenerateKey (key generate), SignDist and UseDist (dist sign
// and dist use, on the local form of an image). The other methods of ADR-061
// decision 16 join with the parts they need: the launcher's toolbox for Render,
// VerifyProfile, Check, Pack, Gate, Build, Smoke, Verify, New, Install, Enable,
// Disable and Remove; the OCI client of ADR-047 decision 4 for Sign, Push, Inspect
// and the registry forms of an image.
package operations
