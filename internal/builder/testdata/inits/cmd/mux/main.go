package main

import "net/http"

func init() { http.HandleFunc("/x", func(http.ResponseWriter, *http.Request) {}) }

func main() {}
