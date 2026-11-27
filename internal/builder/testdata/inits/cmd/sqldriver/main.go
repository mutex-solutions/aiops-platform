package main

import (
	"database/sql"
	"database/sql/driver"
)

type drv struct{}

func (drv) Open(string) (driver.Conn, error) { return nil, nil }

func init() { sql.Register("fixture", drv{}) }

func main() {}
