// Package promauto stands for the constructors that register with the default
// registry.
package promauto

import "github.com/prometheus/client_golang/prometheus"

type Counter struct{ name string }

func (c Counter) Name() string { return c.name }

func NewCounter(name string) Counter {
	c := Counter{name}
	prometheus.MustRegister(c)
	return c
}
