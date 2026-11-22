// SPDX-License-Identifier: Apache-2.0

//go:build linux

package proc

import (
	"errors"
	"os/exec"
	"syscall"
)

// setAttr puts the child in its own process group and asks the kernel to SIGKILL it
// when the thread that started it dies.
func setAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// killGroup kills the child and every process of its group.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return cmd.Process.Kill()
	}
	return nil
}
