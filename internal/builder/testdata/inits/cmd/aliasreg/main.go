package main

import "github.com/prometheus/client_golang/prometheus"

type c struct{}

func (c) Name() string { return "c" }

// The default registry reached through a variable that holds it.
var r = prometheus.DefaultRegisterer

func init() { r.MustRegister(c{}) }

func main() {}
