// SPDX-License-Identifier: Apache-2.0

// Package echoer is example-echoer, the toy instantiable echoer of the S2a test
// release: it contributes to echoers and nothing else (ADR-060 decision 38). Its row
// config fixes its reply, so a pack with no code instantiates it by rows
// (08-profiles-roadmap-adrs.md:391). It is in no shipped bundle.
//
// Config: name, the name it registers under (default "example"), and prefix, what its
// reply puts before the message (default "<name>: ").
package echoer

import (
	"context"
	"errors"
	"fmt"

	echoers "github.com/mutex-solutions/aiops-platform/sdk/def/echoers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// ID is the plugin id.
const ID = "aiops/example-echoer"

// Plugin is the echoer.
var Plugin = kernel.Define(kernel.Plugin{
	ID:     ID,
	Inject: []kernel.Dep{kernel.Need(echoers.Key)},
	Decode: decode,
	Apply: func(c *kernel.Context, raw any) error {
		e := raw.(echo)
		e.c = c
		return echoers.Key.Of(c).Register(c, e.name, e)
	},
})

// echo answers prefix+msg while its fiber is live.
type echo struct {
	c            *kernel.Context
	name, prefix string
}

func (e echo) Echo(ctx context.Context, msg string) (string, error) {
	leave, ok := e.c.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return e.prefix + msg, nil
}

func decode(raw any) (any, error) {
	m, ok := raw.(map[string]any)
	if raw != nil && !ok {
		return nil, errors.New("invalid config: want a map")
	}
	e := echo{name: "example"}
	prefix, hasPrefix := "", false
	for k, v := range m {
		s, isString := v.(string)
		switch {
		case k != "name" && k != "prefix":
			return nil, fmt.Errorf("invalid config: unknown field %q", k)
		case !isString || k == "name" && s == "":
			return nil, fmt.Errorf("invalid config: %s must be a string, and a name not empty", k)
		case k == "name":
			e.name = s
		default:
			prefix, hasPrefix = s, true
		}
	}
	e.prefix = e.name + ": "
	if hasPrefix {
		e.prefix = prefix
	}
	return e, nil
}
