// SPDX-License-Identifier: Apache-2.0

// Package def holds the keys of the toy and fault plugins of the test distribution
// and the strict reader of their configs. It is a test-only definition: no shipped
// distribution links it (ADR-059 decision 9; 07-c4-plugin-sdk.md:47).
package def

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Store is what toy.store serves: the id of the plugin that provides it.
type Store struct{ Owner string }

// Spine is what toy.spine serves: the number of workers its provider holds, and Call,
// a call into the provider under a lease of its fiber, as a dispatcher makes one.
type Spine struct {
	Workers int
	call    func(hold time.Duration) error
}

// NewSpine returns the value of a spine whose Call is call.
func NewSpine(workers int, call func(hold time.Duration) error) Spine {
	return Spine{Workers: workers, call: call}
}

// Call calls the provider and holds its lease for hold. It returns the error of the
// provider: kernel.ErrInactive once the provider has begun to unload.
func (s Spine) Call(hold time.Duration) error {
	if s.call == nil {
		return nil
	}
	return s.call(hold)
}

// Policy is what toy.policy serves.
type Policy struct{ Owner string }

// Fault is what toy.fault serves, whichever fault plugin provides it.
type Fault struct{ Owner string }

// The keys of the test distribution. All are class "-" (open) but toy.policy, which
// is provider-bound, so the rogue-provider fault has a bound key to provide.
var (
	StoreKey  = kernel.NewKey[Store]("toy.store", 0)
	SpineKey  = kernel.NewKey[Spine]("toy.spine", 0)
	PolicyKey = kernel.NewKey[Policy]("toy.policy", 0, kernel.ProviderBound())
	FaultKey  = kernel.NewKey[Fault]("toy.fault", 0)
)

// Keys lists the keys above for the key table.
func Keys() []kernel.AnyKey { return []kernel.AnyKey{StoreKey, SpineKey, PolicyKey, FaultKey} }

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
