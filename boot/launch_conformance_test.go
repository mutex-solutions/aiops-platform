// SPDX-License-Identifier: Apache-2.0

package boot

import (
	"sync"
	"syscall"
	"testing"
	"testing/fstest"

	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0/conformance"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// The launcher, the provider of the six launch keys, passes the launch definition's
// suite, as its fake does (T2, 07-c4-plugin-sdk.md §8.2). Each case boots the toy
// distribution with the suite's consumer in the module table and among the consumers
// of profileContext and appExit; the release is the stop of the process.
func TestLaunchConformance(t *testing.T) {
	envOfSwitch := map[string]string{launch.AgentsPaused: EnvAgentsPaused, launch.TelemetryDisabled: EnvTelemetryDisabled}
	conformance.Run(t, func(t *testing.T, s conformance.Setup) (*kernel.Root, func()) {
		f := newFixture(t, happyBundle, ModuleEntry{ID: s.Consumer.ID, Module: "example.test/conformance", Version: "v0.1.0", Plugin: s.Consumer})
		f.dist.Consumers = []Binding{
			{Key: launch.ProfileContextKey.Name(), Plugins: []string{LoaderID, s.Consumer.ID}},
			{Key: launch.AppExitKey.Name(), Plugins: []string{s.Consumer.ID}},
		}
		f.files["profiles/"+s.Profile+"/profile.yaml"] = &fstest.MapFile{Data: []byte("bundles: [b]")}
		f.opt.Profile, f.opt.Role, f.opt.Args, f.opt.Flags = s.Profile, s.Role, s.Args, s.Flags
		env := map[string]string{}
		f.opt.Environ = nil
		for n, v := range s.Env {
			env[n] = v
			f.opt.Environ = append(f.opt.Environ, n+"="+v)
		}
		f.opt.Stdio, f.opt.Dir = Stdio{In: s.Stdin, Out: s.Stdout, Err: s.Stderr}, s.Dir
		for _, n := range s.Switches {
			env[envOfSwitch[n]] = "1"
		}
		f.opt.Env = func(n string) string { return env[n] }
		var root *kernel.Root
		f.opt.Kernel = func(r *kernel.Root) { root = r }
		done := f.start(t.Context())
		f.waitReady(t, done)
		var once sync.Once
		release := func() {
			once.Do(func() {
				f.signal <- syscall.SIGTERM
				<-done
			})
		}
		t.Cleanup(release)
		return root, release
	})
}
