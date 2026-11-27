package main

import (
	"database/sql"
	"database/sql/driver"
	"iter"
)

type drv struct{}

func (drv) Open(string) (driver.Conn, error) { return nil, nil }

// init runs a sequence that registers through iter.Pull, whose coroutine the runtime
// runs through no edge of the call graph.
func init() {
	next, stop := iter.Pull(func(yield func(int) bool) {
		sql.Register("pulled", drv{})
		yield(1)
	})
	defer stop()
	next()
}

func main() {}
