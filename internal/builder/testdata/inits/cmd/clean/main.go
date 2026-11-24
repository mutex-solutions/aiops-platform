package main

import "net/http"

// A mux of its own is no default mux.
var mux = http.NewServeMux()

func init() { mux.HandleFunc("/z", func(http.ResponseWriter, *http.Request) {}) }

func main() { _ = mux }
