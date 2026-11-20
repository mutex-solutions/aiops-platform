// Command aiopsctl is the command line of the platform. At S1 it has one
// subcommand, `profile render`; see run.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.DirFS("/"), os.Getenv))
}
