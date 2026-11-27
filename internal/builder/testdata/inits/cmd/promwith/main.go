package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// The constructors of a factory on the default registry.
var requests = promauto.With(prometheus.DefaultRegisterer).NewCounter("requests")

func main() { _ = requests }
