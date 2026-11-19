// SPDX-License-Identifier: Apache-2.0

// Package kerneltest builds a kernel for a test: New constructs it with both
// binding maps off, so a plugin mounts alone with a fake of any key, and Mount
// mounts plugins on it and fails the test when one does not become ACTIVE
// (07-c4-plugin-sdk.md:564, :1407). Only _test.go files may import it.
package kerneltest
