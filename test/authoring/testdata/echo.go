// Package echo is the echoer of acme/echo, as its author fills in the stub of the
// template: the fixture of step A2 of the S2a exit (ADR-060 decision 30).
package echo

import (
	"context"

	"github.com/mutex-solutions/aiops-platform/sdk/kernel"
)

// Echoer answers messages for the activation of the plugin that registered it.
type Echoer struct{ c *kernel.Context }

// New returns the echoer of the activation c.
func New(c *kernel.Context) Echoer { return Echoer{c: c} }

// Echo returns "echo: " and msg while the activation is live.
func (e Echoer) Echo(ctx context.Context, msg string) (string, error) {
	leave, ok := e.c.Enter()
	if !ok {
		return "", kernel.ErrInactive
	}
	defer leave()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "echo: " + msg, nil
}
