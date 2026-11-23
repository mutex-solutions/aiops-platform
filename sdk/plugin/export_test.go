// SPDX-License-Identifier: Apache-2.0

package plugin

import "reflect"

// DerivedVolatile returns the volatile paths derived from C, which Describe does not
// report yet (see Volatile).
func DerivedVolatile[C any]() ([][]string, error) {
	d, err := describe(reflect.TypeFor[C]())
	if err != nil {
		return nil, err
	}
	return d.volatile, nil
}
