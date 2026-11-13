// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/mutex-solutions/aiops-platform/test/differential"
)

// decodeNeedsOrder is deliberately private: differential.ParseScript remains the
// protocol validator. RawMessage keeps object member order before PluginSpec's
// map loses it, without changing frozen protocol types or reserializing scripts.
func decodeNeedsOrder(data []byte) (map[string][]string, error) {
	var document struct {
		Plugins []struct {
			ID    string          `json:"id"`
			Needs json.RawMessage `json:"needs"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	ordered := make(map[string][]string, len(document.Plugins))
	for _, plugin := range document.Plugins {
		keys := []string{}
		if len(plugin.Needs) != 0 && !bytes.Equal(bytes.TrimSpace(plugin.Needs), []byte("null")) {
			decoder := json.NewDecoder(bytes.NewReader(plugin.Needs))
			start, err := decoder.Token()
			if err != nil || start != json.Delim('{') {
				return nil, fmt.Errorf("plugin %q needs must be a JSON object", plugin.ID)
			}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := token.(string)
				if !ok {
					return nil, fmt.Errorf("plugin %q needs has a non-string key", plugin.ID)
				}
				keys = append(keys, key)
				var ignored json.RawMessage
				if err := decoder.Decode(&ignored); err != nil {
					return nil, err
				}
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
		}
		ordered[plugin.ID] = keys
	}
	return ordered, nil
}

// applyMissingValues preserves the source-language distinction between a missing
// action.value and an explicit JSON null. The typed protocol uses omitempty and
// cannot recover that distinction after decoding, so RunJSON restores it from the
// actual input tokens, never from an expectation or saved trace.
func applyMissingValues(data []byte, script *differential.Script) error {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	var fixActions func(json.RawMessage, []differential.Action) error
	fixActions = func(raw json.RawMessage, actions []differential.Action) error {
		var encoded []map[string]json.RawMessage
		if len(raw) == 0 {
			if len(actions) != 0 {
				return fmt.Errorf("action structure changed during decode")
			}
			return nil
		}
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return err
		}
		if len(encoded) != len(actions) {
			return fmt.Errorf("action structure changed during decode")
		}
		for i, fields := range encoded {
			if _, present := fields["value"]; !present {
				actions[i].Value = undefined()
			}
			if err := fixActions(fields["program"], actions[i].Program); err != nil {
				return err
			}
			if err := fixActions(fields["cleanup"], actions[i].Cleanup); err != nil {
				return err
			}
		}
		return nil
	}
	if err := fixActions(document["steps"], script.Steps); err != nil {
		return err
	}
	var plugins []map[string]json.RawMessage
	if err := json.Unmarshal(document["plugins"], &plugins); err != nil {
		return err
	}
	if len(plugins) != len(script.Plugins) {
		return fmt.Errorf("plugin structure changed during decode")
	}
	for i, plugin := range plugins {
		if err := fixActions(plugin["apply"], script.Plugins[i].Apply); err != nil {
			return err
		}
		if err := fixActions(plugin["cleanup"], script.Plugins[i].Cleanup); err != nil {
			return err
		}
	}
	return nil
}
