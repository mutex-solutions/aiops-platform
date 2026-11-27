package main

import (
	"net/http"
	"time"
)

// init hands the registration to a timer, which the runtime calls right after init.
func init() {
	time.AfterFunc(0, func() { http.HandleFunc("/after", func(http.ResponseWriter, *http.Request) {}) })
}

func main() {}
