// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const maxDocumentBytes = 8 << 20

var scenarioID = regexp.MustCompile(`^D[0-9]{2}$`)
var lifecycleStates = map[string]bool{"PENDING": true, "LOADING": true, "ACTIVE": true, "FAILED": true, "UNLOADING": true, "DISPOSED": true}
var operations = map[string]bool{
	"mount": true, "child": true, "settle": true, "snapshot": true, "waitMark": true,
	"mark": true, "gate": true, "release": true, "provide": true, "notify": true,
	"set": true, "output": true, "read": true, "update": true, "restart": true,
	"rawConfig": true, "resolveConfig": true, "dispose": true, "selfDispose": true,
	"effect": true, "lateEffect": true, "fail": true, "listen": true, "dispatch": true,
	"return": true, "next": true, "awaitTask": true, "awaitFiber": true,
	"context": true, "when": true, "withdraw": true, "inspect": true,
}
var eventKinds = map[string]bool{
	"plugin": true, "status": true, "service": true, "apply": true, "cleanup": true,
	"mark": true, "gate": true, "output": true, "config": true, "update": true,
	"commit": true, "listener": true, "inner": true, "error": true, "selfDispose": true,
}

// decodeStrict rejects duplicate keys (encoding/json otherwise silently accepts
// them), unknown typed fields, trailing documents and oversized input.
func decodeStrict(data []byte, target any) error {
	if len(data) > maxDocumentBytes {
		return fmt.Errorf("document exceeds %d bytes", maxDocumentBytes)
	}
	probe := json.NewDecoder(bytes.NewReader(data))
	probe.UseNumber()
	if err := walkJSON(probe); err != nil {
		return err
	}
	if _, err := probe.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func walkJSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if keys[name] {
				return fmt.Errorf("duplicate JSON key %q", name)
			}
			keys[name] = true
			if err := walkJSON(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkJSON(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	_, err = decoder.Token()
	return err
}

func ParseScript(data []byte) (*Script, error) {
	var script Script
	if err := decodeStrict(data, &script); err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	if script.Schema != ScriptSchema || !scenarioID.MatchString(script.ID) || script.Title == "" {
		return nil, fmt.Errorf("invalid script identity")
	}
	if len(script.Covers) == 0 || len(script.Steps) == 0 {
		return nil, fmt.Errorf("script requires coverage and steps")
	}
	if script.Diagnostic != "" && script.Diagnostic != "self-dispose-await" {
		return nil, fmt.Errorf("unknown diagnostic %q", script.Diagnostic)
	}
	contexts := map[string]bool{"root": true}
	for _, ctx := range script.Contexts {
		if ctx.ID == "" || contexts[ctx.ID] || !contexts[ctx.Parent] {
			return nil, fmt.Errorf("invalid context %q", ctx.ID)
		}
		if ctx.Isolate != nil && ctx.Isolate.Key == "" {
			return nil, fmt.Errorf("empty isolation key")
		}
		if ctx.Intercept != nil && ctx.Intercept.Key == "" {
			return nil, fmt.Errorf("empty intercept key")
		}
		contexts[ctx.ID] = true
	}
	plugins := make(map[string]bool)
	for _, plugin := range script.Plugins {
		if plugin.ID == "" || plugins[plugin.ID] {
			return nil, fmt.Errorf("invalid plugin %q", plugin.ID)
		}
		if plugin.Update != "" && plugin.Update != "retain" && plugin.Update != "next" && plugin.Update != "reject" && plugin.Update != "unchanged" {
			return nil, fmt.Errorf("unknown update mode")
		}
		plugins[plugin.ID] = true
		if err := validateActions(plugin.Apply, 0); err != nil {
			return nil, err
		}
		if err := validateActions(plugin.Cleanup, 0); err != nil {
			return nil, err
		}
	}
	if err := validateActions(script.Steps, 0); err != nil {
		return nil, err
	}
	for _, assertion := range script.Expect {
		if assertion.At == "" || (assertion.Path != "" && !strings.HasPrefix(assertion.Path, "/")) {
			return nil, fmt.Errorf("invalid assertion pointer")
		}
		for _, part := range strings.Split(assertion.Path, "/") {
			for i := 0; i < len(part); i++ {
				if part[i] == '~' {
					if i+1 >= len(part) || (part[i+1] != '0' && part[i+1] != '1') {
						return nil, fmt.Errorf("invalid pointer escape")
					}
					i++
				}
			}
		}
	}
	return &script, nil
}

func validateActions(actions []Action, depth int) error {
	if depth > 32 {
		return fmt.Errorf("action nesting exceeds 32")
	}
	for _, a := range actions {
		if !operations[a.Op] {
			return fmt.Errorf("unknown action %q", a.Op)
		}
		require := func(value, name string) error {
			if value == "" {
				return fmt.Errorf("%s requires %s", a.Op, name)
			}
			return nil
		}
		var err error
		switch a.Op {
		case "mount", "child":
			if err = require(a.Fiber, "fiber"); err == nil {
				err = require(a.Plugin, "plugin")
			}
		case "settle", "snapshot", "waitMark", "mark", "gate", "release", "effect":
			err = require(a.Label, "label")
		case "provide", "set", "read", "listen", "dispatch":
			err = require(a.Key, "key")
		case "update", "restart", "dispose", "rawConfig", "resolveConfig", "lateEffect", "awaitFiber", "inspect":
			err = require(a.Fiber, "fiber")
		case "output", "context":
			err = require(a.To, "to")
		case "awaitTask", "withdraw":
			err = require(a.Task, "task")
		}
		if err != nil {
			return err
		}
		if a.Count < 0 {
			return fmt.Errorf("negative count")
		}
		if a.Op == "dispatch" && a.Mode != "emit" && a.Mode != "parallel" && a.Mode != "serial" && a.Mode != "bail" && a.Mode != "waterfall" {
			return fmt.Errorf("unknown dispatch mode")
		}
		if err := validateActions(a.Program, depth+1); err != nil {
			return err
		}
		if err := validateActions(a.Cleanup, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func ParseTrace(data []byte) (*Trace, error) {
	var trace Trace
	if err := decodeStrict(data, &trace); err != nil {
		return nil, fmt.Errorf("trace: %w", err)
	}
	if trace.Schema != TraceSchema || !scenarioID.MatchString(trace.Scenario) {
		return nil, fmt.Errorf("invalid trace identity")
	}
	if trace.Events == nil || trace.Checkpoints == nil {
		return nil, fmt.Errorf("events and checkpoints must be arrays")
	}
	for index, event := range trace.Events {
		if event.Seq != index+1 || !eventKinds[event.Kind] || event.Data == nil {
			return nil, fmt.Errorf("invalid event at index %d", index)
		}
		if event.Kind == "status" {
			from, okFrom := event.Data["from"].(string)
			to, okTo := event.Data["to"].(string)
			fiber, okFiber := event.Data["fiber"].(string)
			if !okFrom || !okTo || !okFiber || fiber == "" || !lifecycleStates[from] || !lifecycleStates[to] || from == to {
				return nil, fmt.Errorf("invalid status event")
			}
		}
	}
	labels := make(map[string]bool)
	for _, checkpoint := range trace.Checkpoints {
		if checkpoint.Label == "" || labels[checkpoint.Label] || checkpoint.States == nil || checkpoint.Provisions == nil || checkpoint.Outputs == nil {
			return nil, fmt.Errorf("invalid checkpoint %q", checkpoint.Label)
		}
		labels[checkpoint.Label] = true
		fibers := make(map[string]bool)
		for _, state := range checkpoint.States {
			if state.Fiber == "" || fibers[state.Fiber] || !lifecycleStates[state.State] || state.Revision < 0 {
				return nil, fmt.Errorf("invalid fiber state")
			}
			fibers[state.Fiber] = true
			if state.FiberUID != nil && !validUID(state.FiberUID) {
				return nil, fmt.Errorf("invalid fiber UID")
			}
			if state.Epoch != nil {
				if state.Epoch.Revision < 0 || state.Epoch.Needs == nil {
					return nil, fmt.Errorf("invalid epoch")
				}
				keys := make(map[string]bool)
				for _, binding := range state.Epoch.Needs {
					if binding.Key == "" || keys[binding.Key] || !validUID(binding.ProvisionUID) {
						return nil, fmt.Errorf("invalid epoch binding")
					}
					keys[binding.Key] = true
				}
			}
		}
		for _, provision := range checkpoint.Provisions {
			if provision.Key == "" || provision.Realm == "" || provision.Provider == "" || !validUID(provision.ProvisionUID) {
				return nil, fmt.Errorf("invalid provision")
			}
		}
	}
	if d := trace.Diagnostic; d != nil {
		if d.Code != "SELF_DISPOSE_AWAIT_BLOCKED" || d.BoundMS <= 0 || d.Message == "" {
			return nil, fmt.Errorf("invalid diagnostic")
		}
	}
	return &trace, nil
}

func validUID(value any) bool {
	switch id := value.(type) {
	case string:
		return id != ""
	case json.Number:
		n, err := strconv.ParseUint(string(id), 10, 64)
		return err == nil && n > 0
	default:
		return false
	}
}

// CheckExpectations checks declarative assertions against observed checkpoints.
// Assertions are never an input to the runtime being evaluated.
func CheckExpectations(script Script, trace Trace) error {
	if script.ID != trace.Scenario {
		return fmt.Errorf("script/trace scenario mismatch")
	}
	for _, assertion := range script.Expect {
		var checkpoint *Checkpoint
		for index := range trace.Checkpoints {
			if trace.Checkpoints[index].Label == assertion.At {
				checkpoint = &trace.Checkpoints[index]
				break
			}
		}
		if checkpoint == nil {
			return fmt.Errorf("missing checkpoint %q", assertion.At)
		}
		data, _ := json.Marshal(checkpoint)
		var current any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&current); err != nil {
			return err
		}
		if assertion.Path != "" {
			for _, key := range strings.Split(assertion.Path[1:], "/") {
				key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
				switch object := current.(type) {
				case map[string]any:
					value, exists := object[key]
					if !exists {
						return fmt.Errorf("missing assertion path %s", assertion.Path)
					}
					current = value
				case []any:
					index, err := strconv.Atoi(key)
					if err != nil || index < 0 || index >= len(object) {
						return fmt.Errorf("invalid assertion index")
					}
					current = object[index]
				default:
					return fmt.Errorf("assertion traverses scalar")
				}
			}
		}
		actual, _ := json.Marshal(current)
		expected, _ := json.Marshal(assertion.Equals)
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("%s %s%s: actual %s != expected %s", script.ID, assertion.At, assertion.Path, actual, expected)
		}
	}
	return nil
}
