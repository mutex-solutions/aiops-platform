// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package proc

import "os/exec"

// setAttr is a no-op: Pdeathsig and the process group are Linux mechanisms. Elsewhere
// only t.Cleanup and the context reap the child, so a test process killed from
// outside can leave it running (the suites run in CI on Linux).
func setAttr(*exec.Cmd) {}

// killGroup kills the child only.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
