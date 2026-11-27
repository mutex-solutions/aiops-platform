package main

import (
	"database/sql"
	"database/sql/driver"
)

type drv struct{}

func (drv) Open(string) (driver.Conn, error) { return nil, nil }

func viaValue() { sql.Register("value", drv{}) }

// hook hides the registration behind a function value.
var hook func()

func init() {
	hook = viaValue
	hook()
}

func main() {}
