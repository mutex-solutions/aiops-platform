package main

import "net/http"

// The default mux reached through a method value.
var f = http.DefaultServeMux.HandleFunc

func init() { f("/b", func(http.ResponseWriter, *http.Request) {}) }

func main() {}
