// SPDX-License-Identifier: Apache-2.0

// Command exitnet is the network side of the S2a exit's environments, which the test
// builds static into an image that holds it alone:
//
//	exitnet forward <listen> <to>   accepts on listen and joins each connection to to
//	exitnet serve <listen> <dir>    serves the files under dir over HTTP
//	exitnet dial <address>...       prints each address and "connected" or why not
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	a := os.Args[1:]
	switch {
	case len(a) == 3 && a[0] == "forward":
		l, err := net.Listen("tcp", a[1])
		for err == nil {
			var c net.Conn
			if c, err = l.Accept(); err == nil {
				go join(c, a[2])
			}
		}
		fail(err)
	case len(a) == 3 && a[0] == "serve":
		fail(http.ListenAndServe(a[1], http.FileServer(http.Dir(a[2]))))
	case len(a) > 1 && a[0] == "dial":
		for _, addr := range a[1:] {
			c, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err == nil {
				c.Close()
				fmt.Println(addr, "connected")
				continue
			}
			fmt.Println(addr, err)
		}
	default:
		fail(fmt.Errorf("usage: exitnet forward <listen> <to> | serve <listen> <dir> | dial <address>..."))
	}
}

// join copies c to a connection to addr and back until either side closes.
func join(c net.Conn, addr string) {
	defer c.Close()
	d, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer d.Close()
	go func() {
		io.Copy(d, c)
		d.Close()
	}()
	io.Copy(c, d)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "exitnet:", err)
	os.Exit(2)
}
