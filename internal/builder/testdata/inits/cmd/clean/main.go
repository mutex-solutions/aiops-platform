package main

import "net/http"

// A mux of its own is no default mux.
var mux = http.NewServeMux()

func init() { mux.HandleFunc("/z", func(http.ResponseWriter, *http.Request) {}) }

// The default mux used after init is not the deny-list's.
func main() { _, _ = mux, http.DefaultServeMux }
