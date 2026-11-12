// SPDX-License-Identifier: Apache-2.0

package differential

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFrozenDisposalScriptsAndRecordedTraces(t *testing.T) {
	for _, id := range []string{"D25", "D26"} {
		t.Run(id, func(t *testing.T) {
			script, err := ParseScript(load(t, filepath.Join("corpus", id+".json")))
			if err != nil {
				t.Fatal(err)
			}
			traceBytes := load(t, filepath.Join("results", id+".trace.json"))
			trace, err := ParseTrace(traceBytes)
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckExpectations(*script, *trace); err != nil {
				t.Fatal(err)
			}
			if err := Compare(traceBytes, traceBytes); err != nil {
				t.Fatal(err)
			}
			if id == "D26" && (trace.Diagnostic == nil || trace.Diagnostic.Code != "SELF_DISPOSE_AWAIT_BLOCKED") {
				t.Fatal("awaited self-disposal must remain a diagnostic")
			}
		})
	}
}

func TestParseScriptRejectsInvalidInputs(t *testing.T) {
	valid := string(load(t, "corpus/D25.json"))
	for name, input := range map[string]string{
		"unknown-field":       strings.Replace(valid, `"title":`, `"extra":true,"title":`, 1),
		"duplicate-key":       strings.Replace(valid, `"schema":`, `"id":"D00","schema":`, 1),
		"bad-schema":          strings.Replace(valid, ScriptSchema, "bad", 1),
		"bad-id":              strings.Replace(valid, "D25", "DXX", 1),
		"bad-op":              strings.Replace(valid, `"selfDispose"`, `"sleep"`, 1),
		"trailing":            valid + `{}`,
		"bad-pointer":         strings.Replace(valid, "/states/0/state", "/states/~9/state", 1),
		"missing-mount-fiber": strings.Replace(valid, `"fiber": "self",`, "", 1),
		"null":                `null`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseScript([]byte(input)); err == nil {
				t.Fatal("accepted invalid script")
			}
		})
	}
}

func TestParseTraceRejectsInvalidInputs(t *testing.T) {
	valid := string(load(t, "results/D25.trace.json"))
	for name, input := range map[string]string{
		"unknown-field":        strings.Replace(valid, `"scenario":`, `"extra":true,"scenario":`, 1),
		"duplicate-key":        strings.Replace(valid, `"scenario":`, `"schema":"duplicate","scenario":`, 1),
		"bad-event-sequence":   strings.Replace(valid, `"seq": 2`, `"seq": 7`, 1),
		"bad-state":            strings.ReplaceAll(valid, "DISPOSED", "DONE"),
		"unknown-event":        strings.Replace(valid, `"kind": "apply"`, `"kind": "omitted"`, 1),
		"bad-fiber-uid":        strings.Replace(valid, `"fiberUID": null`, `"fiberUID": -3`, 1),
		"null-events":          `{"schema":"s0-trace/v1","scenario":"D25","events":null,"checkpoints":[]}`,
		"duplicate-checkpoint": strings.Replace(valid, `"label": "requested"`, `"label": ""`, 1),
		"trailing":             valid + `{}`,
		"empty":                ``,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTrace([]byte(input)); err == nil {
				t.Fatal("accepted invalid trace")
			}
		})
	}
}

const identityTrace = `{"schema":"s0-trace/v1","scenario":"D14","events":[{"seq":1,"kind":"service","data":{"key":"events","realm":"root:events","value":"value"}}],"checkpoints":[{"label":"before","states":[{"fiber":"f","state":"ACTIVE","fiberUID":11,"revision":0,"epoch":{"revision":0,"needs":[{"key":"events","provisionUID":21}]},"rawConfig":{},"effectiveConfig":{}}],"provisions":[{"key":"events","realm":"root:events","provider":"p","provisionUID":21,"value":"value","active":true}],"outputs":{"payload":{"events":"unchanged","provisionUID":999,"timestamp":10}}},{"label":"after","states":[{"fiber":"f","state":"ACTIVE","fiberUID":11,"revision":1,"epoch":{"revision":1,"needs":[{"key":"events","provisionUID":22}]},"rawConfig":{},"effectiveConfig":{}}],"provisions":[{"key":"events","realm":"root:events","provider":"p","provisionUID":22,"value":"value","active":true}],"outputs":{}}]}`

func TestCompareOnlyDeclaredIdentityNormalization(t *testing.T) {
	actual := strings.ReplaceAll(identityTrace, `"fiberUID":11`, `"fiberUID":"opaque-fiber"`)
	actual = strings.ReplaceAll(actual, `"provisionUID":21`, `"provisionUID":"old"`)
	actual = strings.ReplaceAll(actual, `"provisionUID":22`, `"provisionUID":"new"`)
	actual = strings.ReplaceAll(actual, `"key":"events"`, `"key":"hooks"`)
	actual = strings.ReplaceAll(actual, `"realm":"root:events"`, `"realm":"root:hooks"`)
	if err := Compare([]byte(identityTrace), []byte(actual)); err != nil {
		t.Fatal(err)
	}
	for name, mutated := range map[string]string{
		"epoch-reused":      strings.ReplaceAll(actual, `"provisionUID":"new"`, `"provisionUID":"old"`),
		"binding-mismatch":  strings.Replace(actual, `"provisionUID":"old"`, `"provisionUID":"third"`, 1),
		"restart-erased":    strings.Replace(actual, `"revision":1`, `"revision":0`, 1),
		"payload-renamed":   strings.Replace(actual, `"events":"unchanged"`, `"hooks":"unchanged"`, 1),
		"payload-uid":       strings.Replace(actual, `"provisionUID":999`, `"provisionUID":998`, 1),
		"payload-timestamp": strings.Replace(actual, `"timestamp":10`, `"timestamp":20`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Compare([]byte(identityTrace), []byte(mutated)); err == nil {
				t.Fatal("erased semantic mismatch")
			}
		})
	}
}

func TestCompareDoesNotSortDeleteOrCoalesceEvents(t *testing.T) {
	raw := load(t, "results/D25.trace.json")
	trace, err := ParseTrace(raw)
	if err != nil {
		t.Fatal(err)
	}
	trace.Events[2], trace.Events[3] = trace.Events[3], trace.Events[2]
	for i := range trace.Events {
		trace.Events[i].Seq = i + 1
	}
	reordered, _ := json.Marshal(trace)
	if err := Compare(raw, reordered); err == nil {
		t.Fatal("event reordering accepted")
	}
	trace.Events = trace.Events[:len(trace.Events)-1]
	deleted, _ := json.Marshal(trace)
	if err := Compare(raw, deleted); err == nil {
		t.Fatal("event deletion accepted")
	}
}

func TestExpectationErrors(t *testing.T) {
	script, _ := ParseScript(load(t, "corpus/D25.json"))
	trace, _ := ParseTrace(load(t, "results/D25.trace.json"))
	script.Expect[0].Equals = "ACTIVE"
	if err := CheckExpectations(*script, *trace); err == nil {
		t.Fatal("wrong expectation accepted")
	}
	script.Expect[0].At = "absent"
	if err := CheckExpectations(*script, *trace); err == nil {
		t.Fatal("absent barrier accepted")
	}
}

func TestRunOracleRequiresExplicitRuntime(t *testing.T) {
	if _, err := RunOracle(context.Background(), "", "oracle", "corpus/D25.json"); err == nil {
		t.Fatal("implicit host node accepted")
	}
}

func TestOracleIntegrationExplicit(t *testing.T) {
	if os.Getenv("S0_ORACLE_RUN") != "1" {
		t.Skip("not an oracle pass: set S0_ORACLE_RUN=1 and explicit S0_NODE for isolated live replay")
	}
	node := os.Getenv("S0_NODE")
	if node == "" {
		t.Fatal("S0_NODE required when integration explicitly requested")
	}
	for _, id := range corpusIDs() {
		t.Run(id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			trace, err := RunOracle(ctx, node, "oracle", filepath.Join("corpus", id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			observed, _ := json.Marshal(trace)
			recorded := load(t, filepath.Join("results", "corpus-final", id+".trace.json"))
			if err := Compare(recorded, observed); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func FuzzParseTrace(f *testing.F) {
	data, err := os.ReadFile("results/D25.trace.json")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		trace, err := ParseTrace(data)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(trace)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseTrace(encoded); err != nil {
			t.Fatal(err)
		}
		if err := Compare(encoded, encoded); err != nil {
			t.Fatal(err)
		}
	})
}

func TestOversizeRejected(t *testing.T) {
	if _, err := ParseTrace(bytes.Repeat([]byte(" "), maxDocumentBytes+1)); err == nil {
		t.Fatal("oversize accepted")
	}
}
