// SPDX-License-Identifier: Apache-2.0

package reloadsocket

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

// TestMain creates testdata/run, the dir of testdata/cycle.yaml, with mode 0700
// before the tests and removes it after them, since Git records no directory mode
// (ADR-062 decision 18). The row serves on Linux only, so elsewhere no test runs.
func TestMain(m *testing.M) {
	if runtime.GOOS != "linux" {
		fmt.Println("reload-socket serves on Linux only; no test runs on", runtime.GOOS)
		os.Exit(0)
	}
	if os.Getenv("AIOPS_PEER_SOCKET") != "" { // the client of the peer test, as another user
		os.Exit(m.Run())
	}
	if err := os.RemoveAll("testdata/run"); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if err := os.Mkdir("testdata/run", 0o700); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll("testdata/run"); err != nil && code == 0 {
		fmt.Println(err)
		code = 1
	}
	os.Exit(code)
}
