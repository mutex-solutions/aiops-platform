// SPDX-License-Identifier: Apache-2.0

package dispatch_test

import (
	"testing"

	"github.com/mutex-solutions/aiops-platform/examples/dispatch"
	echoconf "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0/conformance"
	stampconf "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	"github.com/mutex-solutions/aiops-platform/sdk/testkit/kerneltest"
)

// The dispatcher has no config: plugin.Define refuses any field.
func TestConfigIsChecked(t *testing.T) {
	if _, err := dispatch.Plugin.Decode(map[string]any{"color": "red"}); err == nil || err.Error() != "invalid config: config.color: unknown field" {
		t.Errorf("Decode = %v", err)
	}
}

// The dispatcher passes the guard half of the echoers suite.
func TestEchoersGuard(t *testing.T) {
	echoconf.Run(t, func(t *testing.T) (echoconf.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		return echoconf.Dispatcher(root.Plugin(dispatch.Plugin, nil)), root
	})
}

// The dispatcher passes the guard half of the stampers suite.
func TestStampersGuard(t *testing.T) {
	stampconf.Run(t, func(t *testing.T) (stampconf.Subject, *kernel.Root) {
		root := kerneltest.New(t)
		return stampconf.Dispatcher(root.Plugin(dispatch.Plugin, nil)), root
	})
}
