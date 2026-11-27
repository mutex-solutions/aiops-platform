// SPDX-License-Identifier: Apache-2.0

// Command exitnet is the network side of the S2a exit's environments, which the test
// builds static into an image that holds it alone:
//
//	exitnet forward <listen> <to>   accepts on listen and joins each connection to to
//	exitnet serve <listen> <dir>    serves the files under dir over HTTP
//	exitnet dial <address>...       prints each address and "connected" or why not
package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
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
	case len(a) > 1 && a[0] == "daemon":
		fail(daemon(a[1:]))
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

// daemon readies the container it runs in, the first process of a privileged container,
// for a container runtime, and replaces itself with the program of args: on cgroup v2 it
// moves itself out of the container's root cgroup into a child, since a cgroup that
// holds a process enables no controller for its children, and enables each controller
// for them; and it makes the directories that a runtime writes under, which an image of
// a few programs on no base layer lacks.
func daemon(args []string) error {
	if b, err := os.ReadFile("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		on := strings.Fields(string(b))
		for i := range on {
			on[i] = "+" + on[i]
		}
		if err := errors.Join(os.MkdirAll("/sys/fs/cgroup/init", 0o755),
			os.WriteFile("/sys/fs/cgroup/init/cgroup.procs", []byte(strconv.Itoa(os.Getpid())), 0),
			os.WriteFile("/sys/fs/cgroup/cgroup.subtree_control", []byte(strings.Join(on, " ")), 0)); err != nil {
			return err
		}
	}
	for _, d := range []string{"/etc", "/run", "/tmp", "/var/lib"} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	p, err := exec.LookPath(args[0])
	if err != nil {
		return err
	}
	return syscall.Exec(p, args, os.Environ())
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
