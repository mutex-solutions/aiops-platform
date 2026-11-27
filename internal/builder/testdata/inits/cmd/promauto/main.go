package main

import "github.com/prometheus/client_golang/prometheus/promauto"

var requests = promauto.NewCounter("requests")

func main() { _ = requests }
