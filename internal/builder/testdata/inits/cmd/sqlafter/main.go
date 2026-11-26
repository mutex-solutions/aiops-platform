package main

import (
	"database/sql"
	"database/sql/driver"
	"time"
)

type drv struct{}

func (drv) Open(string) (driver.Conn, error) { return nil, nil }

func register() { sql.Register("after", drv{}) }

// init hands a function that registers to a timer, as a value.
func init() { time.AfterFunc(0, register) }

func main() {}
