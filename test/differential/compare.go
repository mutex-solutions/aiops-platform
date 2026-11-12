// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Compare parses both traces and checks ordered semantic equality. The only
// automatic normalizations are bijective opaque UID renaming and the approved
// built-in service rename. Trace v1 has no timestamp fields; none are removed.
// User payloads (including properties named timestamp/events/provisionUID) remain
// untouched. Arrays are never sorted and no event or restart boundary is erased.
func Compare(expected, actual []byte) error {
	left, err := ParseTrace(expected)
	if err != nil {
		return fmt.Errorf("expected: %w", err)
	}
	right, err := ParseTrace(actual)
	if err != nil {
		return fmt.Errorf("actual: %w", err)
	}
	if left.Scenario != right.Scenario {
		return fmt.Errorf("scenario mismatch: %s != %s", left.Scenario, right.Scenario)
	}
	normalize(left)
	normalize(right)
	lbytes, err := json.Marshal(left)
	if err != nil {
		return err
	}
	rbytes, err := json.Marshal(right)
	if err != nil {
		return err
	}
	if !bytes.Equal(lbytes, rbytes) {
		// Include both complete normalized records: no filtered/summary trace is
		// substituted for the raw evidence retained by the caller.
		return fmt.Errorf("ordered semantic mismatch\nexpected: %s\nactual:   %s", lbytes, rbytes)
	}
	return nil
}

func normalize(trace *Trace) {
	fiberIDs, provisionIDs := make(map[string]string), make(map[string]string)
	rename := func(ids map[string]string, value any) any {
		if value == nil {
			return nil
		}
		bytes, _ := json.Marshal(value)
		key := string(bytes)
		if ids[key] == "" {
			ids[key] = fmt.Sprintf("uid:%d", len(ids)+1)
		}
		return ids[key]
	}
	builtin := func(key string) string {
		if key == "events" {
			return "hooks"
		}
		return key
	}
	for index := range trace.Events {
		event := &trace.Events[index]
		if event.Kind == "service" {
			if key, ok := event.Data["key"].(string); ok {
				event.Data["key"] = builtin(key)
				if key == "events" && event.Data["realm"] == "root:events" {
					event.Data["realm"] = "root:hooks"
				}
			}
		}
	}
	for ci := range trace.Checkpoints {
		checkpoint := &trace.Checkpoints[ci]
		for si := range checkpoint.States {
			state := &checkpoint.States[si]
			state.FiberUID = rename(fiberIDs, state.FiberUID)
			if state.Epoch != nil {
				for ni := range state.Epoch.Needs {
					binding := &state.Epoch.Needs[ni]
					binding.Key = builtin(binding.Key)
					binding.ProvisionUID = rename(provisionIDs, binding.ProvisionUID)
				}
			}
		}
		for pi := range checkpoint.Provisions {
			p := &checkpoint.Provisions[pi]
			if p.Key == "events" && p.Realm == "root:events" {
				p.Realm = "root:hooks"
			}
			p.Key = builtin(p.Key)
			p.ProvisionUID = rename(provisionIDs, p.ProvisionUID)
		}
	}
}
