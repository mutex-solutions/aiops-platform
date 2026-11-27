package main

import "net/http"

func init() { http.DefaultServeMux.Handle("/y", http.NotFoundHandler()) }

func main() {}
