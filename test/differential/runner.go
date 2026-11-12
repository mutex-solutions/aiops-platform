// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunOracle replays one neutral script under the explicit isolated Node binary.
// No host node/npm fallback is allowed. Context cancellation is the outer process
// guard in addition to the oracle's internal diagnostic/normal timeout guards.
func RunOracle(ctx context.Context, node, oracleDir, scriptPath string) (*Trace, error) {
	if node == "" {
		return nil, fmt.Errorf("S0_NODE must name the isolated Node 24.22.1 executable")
	}
	node, err := filepath.Abs(node)
	if err != nil {
		return nil, err
	}
	oracleDir, err = filepath.Abs(oracleDir)
	if err != nil {
		return nil, err
	}
	scriptPath, err = filepath.Abs(scriptPath)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, node, "--import", filepath.Join(oracleDir, "register.mjs"), filepath.Join(oracleDir, "replay.mjs"), scriptPath)
	// Prevent NODE_OPTIONS/NODE_PATH and shared harness environment injection.
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "NODE_OPTIONS=") || strings.HasPrefix(entry, "NODE_PATH=") || strings.HasPrefix(entry, "CORDIS_SHARED=") {
			continue
		}
		command.Env = append(command.Env, entry)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("oracle execution: %w; stderr: %s", err, stderr.String())
	}
	trace, err := ParseTrace(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("oracle output: %w", err)
	}
	return trace, nil
}
