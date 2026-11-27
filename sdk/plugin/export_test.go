// SPDX-License-Identifier: Apache-2.0

package plugin

// ConfigOf returns the *C of a value that the Decode of Define returned: the value
// itself, or the *C of the record that it returns for a config type with a Volatile
// field.
func ConfigOf(v any) any {
	if r, ok := v.(*decoded); ok {
		return r.cfg
	}
	return v
}
