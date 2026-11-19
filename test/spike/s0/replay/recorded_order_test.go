// SPDX-License-Identifier: Apache-2.0

package replay

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mutex-solutions/aiops-platform/test/differential"
)

// serviceCut is the state a service event was recorded in: which fibers were
// mounted already and which statuses preceded it.
type serviceCut struct {
	mounted map[string]bool
	before  []string // "fiber:from>to" lines, in order
}

// observedOrder reduces a trace to what D09 and D14 pin: each fiber's status
// sequence, the service events in order, and the cut at each service event.
func observedOrder(events []differential.Event) (map[string][]string, []string, []serviceCut) {
	status := map[string][]string{}
	var services []string
	var cuts []serviceCut
	mounted := map[string]bool{}
	var seen []string
	for _, e := range events {
		fiber, _ := e.Data["fiber"].(string)
		switch e.Kind {
		case "plugin":
			mounted[fiber] = true
		case "status":
			line := fiber + ":" + e.Data["from"].(string) + ">" + e.Data["to"].(string)
			status[fiber] = append(status[fiber], line)
			seen = append(seen, line)
		case "service":
			value, _ := json.Marshal(e.Data["value"])
			services = append(services, e.Data["key"].(string)+"="+string(value))
			cut := serviceCut{mounted: map[string]bool{}, before: append([]string(nil), seen...)}
			for name := range mounted {
				cut.mounted[name] = true
			}
			cuts = append(cuts, cut)
		}
	}
	return status, services, cuts
}

// TestD09D14MatchRecordedOracleOrder replays the frozen scenarios on both
// candidates and compares them with the recorded oracle trace (no live node).
func TestD09D14MatchRecordedOracleOrder(t *testing.T) {
	for _, id := range []string{"D09", "D14"} {
		recorded := loadFixture(t, "../../../differential/results/corpus-final/"+id+".trace.json")
		oracle, err := differential.ParseTrace(recorded)
		if err != nil {
			t.Fatal(err)
		}
		wantStatus, wantServices, wantBefore := observedOrder(oracle.Events)
		script := loadFixture(t, corpusPath(id))
		for _, backend := range causalBackends {
			t.Run(id+"/"+backend.name, func(t *testing.T) {
				skipUnsupportedBackend(t, backend.make)
				result, runErr, closeErr := captureCausalActual(backend.make, script, true)
				if runErr != nil || closeErr != nil {
					t.Fatal(runErr, closeErr)
				}
				gotStatus, gotServices, gotBefore := observedOrder(result.Trace.Events)
				if !reflect.DeepEqual(gotStatus, wantStatus) {
					t.Fatalf("per-fiber status differs from the oracle:\n got  %v\n want %v", gotStatus, wantStatus)
				}
				if !reflect.DeepEqual(gotServices, wantServices) {
					t.Fatalf("service events differ from the oracle:\n got  %v\n want %v", gotServices, wantServices)
				}
				// Every status the oracle records before a service event, for a
				// fiber the candidate had already mounted, precedes it here too.
				for i := range wantBefore {
					have := map[string]bool{}
					for _, line := range gotBefore[i].before {
						have[line] = true
					}
					for _, line := range wantBefore[i].before {
						fiber := line[:strings.IndexByte(line, ':')]
						if gotBefore[i].mounted[fiber] && !have[line] {
							t.Fatalf("service event %d lacks %q that precedes it in the oracle (got %v)", i, line, gotBefore[i].before)
						}
					}
				}
			})
		}
	}
}
