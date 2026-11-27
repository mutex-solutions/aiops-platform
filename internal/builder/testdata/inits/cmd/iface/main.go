package main

import (
	"database/sql"
	"database/sql/driver"
)

type drv struct{}

func (drv) Open(string) (driver.Conn, error) { return nil, nil }

// registrar hides the registration behind an interface method.
type registrar interface{ do() }

type viaMethod struct{}

func (viaMethod) do() { sql.Register("method", drv{}) }

var r registrar = viaMethod{}

func init() { r.do() }

func main() {}
