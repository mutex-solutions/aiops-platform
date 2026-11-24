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

// Factory is the constructors that register with r, nil for none.
type Factory struct{ r prometheus.Registerer }

func With(r prometheus.Registerer) Factory { return Factory{r} }

func (f Factory) NewCounter(name string) Counter {
	c := Counter{name}
	if f.r != nil {
		f.r.MustRegister(c)
	}
	return c
}
