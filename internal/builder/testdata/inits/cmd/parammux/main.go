package main

import "net/http"

// reg registers on whatever mux it is given.
func reg(m *http.ServeMux) { m.Handle("/d", http.NotFoundHandler()) }

func init() { reg(http.DefaultServeMux) }

func main() {}
