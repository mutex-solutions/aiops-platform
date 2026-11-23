// SPDX-License-Identifier: Apache-2.0

// Package stamper is example-stamper, the toy stamper of the S2a test release: it
// contributes to stampers and nothing else, so the toy caller has a stamper to call
// (ADR-060 decision 37). Its stamp is its name and the CRC-32 of the body. It is in
// no shipped bundle.
//
// Config: name, the name it registers under (default "example").
package stamper

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"

	stampers "github.com/mutex-solutions/aiops-platform/sdk/def/stampers/v0"
	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// ID is the plugin id.
const ID = "aiops/example-stamper"

// Plugin is the stamper.
var Plugin = kernel.Define(kernel.Plugin{
	ID:     ID,
	Inject: []kernel.Dep{kernel.Need(stampers.Key)},
	Decode: decode,
	Apply: func(c *kernel.Context, raw any) error {
		s := raw.(stamp)
		s.c = c
		return stampers.Key.Of(c).Register(c, s.name, s)
	},
})

// stamp stamps a body while its fiber is live.
type stamp struct {
	c    *kernel.Context
	name string
}

func (s stamp) Stamp(ctx context.Context, body string) (string, error) {
	leave, ok := s.c.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%08x", s.name, crc32.ChecksumIEEE([]byte(body))), nil
}

func decode(raw any) (any, error) {
	m, ok := raw.(map[string]any)
	if raw != nil && !ok {
		return nil, errors.New("invalid config: want a map")
	}
	s := stamp{name: "example"}
	for k, v := range m {
		name, isString := v.(string)
		if k != "name" || !isString || name == "" {
			return nil, fmt.Errorf("invalid config: %q: the one field is name, a string that is not empty", k)
		}
		s.name = name
	}
	return s, nil
}
