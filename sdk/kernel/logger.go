// SPDX-License-Identifier: Apache-2.0
// Adapted from the Cordis lifecycle model; see NOTICE.

package kernel

import "log/slog"

// Logger is the built-in logger: "a log/slog front with fiber attributes"
// (plugin record :55, :179; 03-c3-kernel.md:62; 07-c4-plugin-sdk.md:27). The
// kernel writes to one slog.Handler that the launcher passes in Config.LogHandler;
// an exporter is a plugin that supplies that handler (plugin record :540).
type Logger interface {
	// For returns a logger that carries the attributes of c's fiber (the plugin
	// id under "plugin") and writes to the kernel's handler. A nil c gives the
	// logger without fiber attributes. Plugin record :179 gives the per-fiber form
	// "For(c)"; its exact shape is an open point.
	For(c *Context) *slog.Logger
}

// logger is the kernel's Logger.
type logger struct{ base *slog.Logger }

func newLogger(h slog.Handler) *logger { return &logger{base: slog.New(h)} }

func (l *logger) For(c *Context) *slog.Logger {
	if c == nil {
		return l.base
	}
	return l.base.With("plugin", c.frame.id)
}
