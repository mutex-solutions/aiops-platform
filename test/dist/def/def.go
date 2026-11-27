// SPDX-License-Identifier: Apache-2.0

// Package def holds what the toy and fault plugins of the test distribution share: the
// strict reader of their configs, bounded waits and the trigger file of a fault. Their
// keys are the test definitions spine/v0 and toy/v0 below it, each with its
// definition.yaml (ADR-060 decision 28). No shipped distribution links any of them
// (ADR-059 decision 9; 07-c4-plugin-sdk.md:47).
package def

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mutex-solutions/aiops-platform/internal/runfile"
	launch "github.com/mutex-solutions/aiops-platform/sdk/def/launch/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
	spine "github.com/mutex-solutions/aiops-platform/test/dist/def/spine/v0"
	toy "github.com/mutex-solutions/aiops-platform/test/dist/def/toy/v0"
)

// Keys lists the keys of the test definitions for the key table.
func Keys() []kernel.AnyKey {
	return []kernel.AnyKey{toy.StoreKey, spine.Key, toy.PolicyKey, toy.FaultKey}
}

// TriggerPath is the file whose appearance fires a fault that has a trigger: name in
// dir or, when dir is empty, in the runtime directory of the home. A test creates it
// once the process has booted, so that the fault happens to a running process at a
// moment the test chose, not after a guessed delay.
func TriggerPath(c *kernel.Context, dir, name string) (string, error) {
	if dir != "" {
		return filepath.Join(dir, name), nil
	}
	home, ok := launch.CmdlineKey.Of(c).Flag("home")
	if !ok {
		return "", errors.New("a trigger needs --home")
	}
	return filepath.Join(runfile.Dir(home), name), nil
}

// AwaitFile polls for the file until it exists or stop is closed, and reports whether
// it exists. The poll is a timer, not a sleep of a fixed length that a test relies on.
func AwaitFile(path string, stop <-chan struct{}) bool {
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		if !Wait(10*time.Millisecond, stop) {
			return false
		}
	}
}

// Config reads the config of one plugin: a plain map with a closed set of fields,
// the form that boot hands the kernel (launch.Row). A field that is not read, or a
// value of the wrong type, is an error of Err, so that a row with a wrong config is
// FAILED with "invalid config" (08-profiles-roadmap-adrs.md:467).
type Config struct {
	m    map[string]any
	used map[string]bool
	errs []error
}

// Parse starts reading raw, which is nil or a map.
func Parse(raw any) *Config {
	c := &Config{used: map[string]bool{}}
	switch m := raw.(type) {
	case nil:
	case map[string]any:
		c.m = m
	default:
		c.errs = append(c.errs, fmt.Errorf("config is %T, want a map", raw))
	}
	return c
}

func (c *Config) get(name string) (any, bool) {
	c.used[name] = true
	v, ok := c.m[name]
	return v, ok
}

// Duration reads a duration given as a string such as "50ms"; def when absent.
func (c *Config) Duration(name string, def time.Duration) time.Duration {
	v, ok := c.get(name)
	if !ok {
		return def
	}
	s, _ := v.(string)
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		c.errs = append(c.errs, fmt.Errorf("%s must be a duration such as 50ms, got %v", name, v))
		return def
	}
	return d
}

// Int reads a whole number; def when absent.
func (c *Config) Int(name string, def int) int {
	v, ok := c.get(name)
	if !ok {
		return def
	}
	n, isInt := v.(int64)
	if !isInt || n < 0 || n > 1<<20 {
		c.errs = append(c.errs, fmt.Errorf("%s must be a whole number, got %v", name, v))
		return def
	}
	return int(n)
}

// String reads a string; def when absent.
func (c *Config) String(name, def string) string {
	v, ok := c.get(name)
	if !ok {
		return def
	}
	s, isStr := v.(string)
	if !isStr {
		c.errs = append(c.errs, fmt.Errorf("%s must be a string, got %v", name, v))
		return def
	}
	return s
}

// Bool reads a boolean; false when absent.
func (c *Config) Bool(name string) bool {
	v, ok := c.get(name)
	if !ok {
		return false
	}
	b, isBool := v.(bool)
	if !isBool {
		c.errs = append(c.errs, fmt.Errorf("%s must be true or false, got %v", name, v))
	}
	return b
}

// Err returns the errors found, with the fields that no read asked for, or nil.
func (c *Config) Err() error {
	var unknown []string
	for k := range c.m {
		if !c.used[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	errs := c.errs
	if len(unknown) > 0 {
		errs = append(errs[:len(errs):len(errs)], fmt.Errorf("unknown field %s", strings.Join(unknown, ", ")))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}

// Wait waits d, or until done is closed when d is zero or the wait is allowed to end
// with it. It returns false when done ended the wait. A delay in a plugin is data,
// not synchronisation: the plugin is a fault or a slow provider on purpose.
func Wait(d time.Duration, done <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-done:
		return false
	}
}
