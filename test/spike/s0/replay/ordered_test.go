// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/differential"
	"github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

const orderedScript = `{
  "schema":"s0-script/v1", "id":"D16", "title":"actual ordered needs", "covers":["ordered-needs"],
  "plugins":[
    {"id":"provider","apply":[{"op":"provide","key":"alpha","value":"a"},{"op":"provide","key":"zeta","value":"z"}]},
    {"id":"consumer","needs":{"zeta":null,"alpha":null},"apply":[{"op":"read","key":"zeta","to":"z"},{"op":"read","key":"alpha","to":"a"}]}
  ],
  "steps":[{"op":"mount","fiber":"p","plugin":"provider","config":{}},{"op":"mount","fiber":"c","plugin":"consumer","config":{}},{"op":"settle","label":"active"},{"op":"dispose","fiber":"c"},{"op":"dispose","fiber":"p"},{"op":"settle","label":"disposed"}]
}`

func TestRunJSONPreservesNeedsOrderFromRawObject(t *testing.T) {
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := RunJSON(ctx, runtime, []byte(orderedScript))
		if err != nil {
			t.Fatal(err)
		}
		bindings := result.Trace.Checkpoints[0].States[1].Epoch.Needs
		if len(bindings) != 2 || bindings[0].Key != "zeta" || bindings[1].Key != "alpha" {
			t.Fatalf("lost raw needs declaration order: %+v", bindings)
		}
		provisions := result.RuntimeInspections["active"].Provisions
		if len(provisions) != 2 || provisions[0].Key != "alpha" || provisions[1].Key != "zeta" ||
			bindings[0].ProvisionUID != provisions[1].UID || bindings[1].ProvisionUID != provisions[0].UID {
			t.Fatalf("target UIDs rebound by sorted map order: bindings=%+v provisions=%+v", bindings, provisions)
		}
		if result.ComparisonError != nil && strings.Contains(result.ComparisonError.Error(), "needs order") {
			t.Fatalf("raw ordered needs unexpectedly unavailable: %v", result.ComparisonError)
		}
	})
}

func TestTypedMultiKeyNeedsIsExplicitlyLexicalOnly(t *testing.T) {
	script, err := differential.ParseScript([]byte(orderedScript))
	if err != nil {
		t.Fatal(err)
	}
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		result, err := Run(ctx, runtime, *script)
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(result.ComparisonError, ErrUnsupported) || !strings.Contains(result.ComparisonError.Error(), "needs order") {
			t.Fatalf("typed multi-key map claimed recoverable order: %v", result.ComparisonError)
		}
		bindings := result.Trace.Checkpoints[0].States[1].Epoch.Needs
		if bindings[0].Key != "alpha" || bindings[1].Key != "zeta" {
			t.Fatalf("typed fallback is not the documented lexical execution: %+v", bindings)
		}
	})
}

func TestDecodeNeedsOrderDoesNotReadNestedOrExpectedNeeds(t *testing.T) {
	data := []byte(`{"plugins":[{"id":"p","apply":[{"value":{"needs":{"ignored":null}}}],"needs":{"z":null,"a":null,"middle":null}},{"id":"empty"}],"expect":[{"equals":{"needs":{"injected":null}}}]}`)
	actual, err := decodeNeedsOrder(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, map[string][]string{"p": {"z", "a", "middle"}, "empty": {}}) {
		t.Fatalf("wrong object order/path: %v", actual)
	}
}

func TestRunJSONKeepsStrictProtocolValidation(t *testing.T) {
	data := strings.Replace(orderedScript, `"zeta":null,"alpha":null`, `"zeta":null,"zeta":null`, 1)
	eachRuntime(t, func(t *testing.T, ctx context.Context, runtime concurrency.Runtime) {
		_, err := RunJSON(ctx, runtime, []byte(data))
		if err == nil || !strings.Contains(err.Error(), "duplicate JSON key") {
			t.Fatalf("ordered decoder bypassed frozen protocol validation: %v", err)
		}
		snapshots, err := runtime.Snapshots(ctx)
		if err != nil || len(snapshots) != 0 {
			t.Fatalf("invalid script executed before validation: snapshots=%v error=%v", snapshots, err)
		}
	})
}
