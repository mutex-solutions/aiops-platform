package main

import "net/http"

// The default mux reached through a variable that holds it.
var m = http.DefaultServeMux

func init() { m.Handle("/c", http.NotFoundHandler()) }

func main() {}
