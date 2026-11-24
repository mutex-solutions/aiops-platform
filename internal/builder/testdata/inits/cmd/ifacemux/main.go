package main

import "net/http"

// The default mux reached through an interface value.
func init() {
	var m interface{ Handle(string, http.Handler) } = http.DefaultServeMux
	m.Handle("/a", http.NotFoundHandler())
}

func main() {}
