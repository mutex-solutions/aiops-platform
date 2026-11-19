// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	cc "github.com/mutex-solutions/aiops-platform/test/spike/s0/concurrency"
)

type Classification string

const (
	Pass         Classification = "pass"
	Fail         Classification = "fail"
	Invalid      Classification = "invalid"
	Inconclusive Classification = "inconclusive"
)

type Memory struct {
	HeapAlloc  uint64 `json:"heapAlloc"`
	HeapInuse  uint64 `json:"heapInuse"`
	StackInuse uint64 `json:"stackInuse"`
	Sys        uint64 `json:"sys"`
	TotalAlloc uint64 `json:"totalAlloc"`
	Mallocs    uint64 `json:"mallocs"`
	NumGC      uint32 `json:"numGC"`
}

// Goroutines records stack ownership rather than subtracting global counts.
// Stacks are raw observed identities; Counts groups by owning package frame.
// The harness capture stack is retained too, never treated as a candidate leak.
type Goroutines struct {
	Counts map[string]int `json:"counts"`
	Stacks []string       `json:"stacks"`
}

func captureGoroutines() Goroutines {
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return parseGoroutines(string(buf[:n]))
		}
		buf = make([]byte, len(buf)*2)
	}
}

func parseGoroutines(raw string) Goroutines {
	out := Goroutines{Counts: make(map[string]int)}
	for _, stack := range strings.Split(strings.TrimSpace(raw), "\n\n") {
		if stack == "" {
			continue
		}
		out.Stacks = append(out.Stacks, stack)
		owner := "external"
		for _, line := range strings.Split(stack, "\n") {
			if strings.Contains(line, "/test/spike/s0/concurrency.") || strings.Contains(line, "/test/spike/s0/internal/eventqueue.") {
				owner = strings.TrimPrefix(line, "created by ")
				if index := strings.LastIndex(owner, "("); index >= 0 {
					owner = owner[:index]
				}
				break
			}
		}
		out.Counts[owner]++
	}
	return out
}

type Checkpoint struct {
	Memory          Memory             `json:"memory"`
	Goroutines      Goroutines         `json:"goroutines"`
	ResourcesActive int64              `json:"resourcesActive"`
	Leases          int64              `json:"leases"`
	Observers       *cc.ObserverStatus `json:"observers,omitempty"`
}

func checkpoint(resources *Resources, gc bool) Checkpoint {
	if gc {
		runtime.GC()
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	out := Checkpoint{Memory: Memory{HeapAlloc: mem.HeapAlloc, HeapInuse: mem.HeapInuse, StackInuse: mem.StackInuse, Sys: mem.Sys,
		TotalAlloc: mem.TotalAlloc, Mallocs: mem.Mallocs, NumGC: mem.NumGC}, Goroutines: captureGoroutines()}
	if resources != nil {
		out.ResourcesActive, out.Leases = resources.Active.Load(), resources.Leases.Load()
	}
	return out
}

type Sample struct {
	Operation    string     `json:"operation"`
	Cycle        int        `json:"cycle"`
	Tree         int        `json:"tree"` // -1 for whole-graph operations.
	ScheduledUTC time.Time  `json:"scheduledUTC"`
	StartedUTC   time.Time  `json:"startedUTC"`
	EndedUTC     time.Time  `json:"endedUTC"`
	LatencyNS    int64      `json:"latencyNS"`
	Before       Checkpoint `json:"before"`
	After        Checkpoint `json:"after"`
	Error        string     `json:"error,omitempty"`
	EventsBefore int        `json:"eventHistoryBefore"`
	EventsAfter  int        `json:"eventHistoryAfter"`
	// AfterSettle is the actual control-path cut BEFORE the explicit observer
	// barrier. EventBacklog is Accepted-Delivered, including an in-flight event;
	// After.Observers is captured only after AwaitObservers completes.
	AfterSettle              *cc.ObserverStatus `json:"afterSettleObservers"`
	EventBacklog             *int               `json:"eventBacklog"`
	BacklogNote              string             `json:"backlogNote"`
	ObserversEndedUTC        time.Time          `json:"observersEndedUTC"`
	ObserverWaitNS           int64              `json:"observerWaitNS"`
	SettleObserversLatencyNS int64              `json:"settleObserversLatencyNS"`
}

type Workload struct {
	Name      string   `json:"name"`
	Topology  Topology `json:"topology"`
	Fibers    int      `json:"fibers"`
	Trees     int      `json:"trees"`
	TreeWidth int      `json:"treeWidth"`
	Seed      uint64   `json:"seed"`
	PRNG      string   `json:"prng"`
}

// RunResult is one closed real root, not a benchmark ns/op average.
// EmptyRoot/SharedOnly/DisposedWithArchives are distinct heap baselines. Closing
// a root cannot erase evidence already held by a Go pointer; PostClose is labeled
// accordingly and Clear happens only after all archive inspection is finished.
type RunResult struct {
	Backend              string                 `json:"backend"`
	Cycles               int                    `json:"treeCycles"`
	Workload             Workload               `json:"workload"`
	Repetition           int                    `json:"repetition"`
	Trial                int                    `json:"trial"`
	StartedUTC           time.Time              `json:"startedUTC"`
	EndedUTC             time.Time              `json:"endedUTC"`
	GOMAXPROCS           int                    `json:"gomaxprocs"`
	HarnessBaseline      Checkpoint             `json:"harnessBaseline"`
	EmptyRoot            Checkpoint             `json:"emptyRoot"`
	SharedOnly           *Checkpoint            `json:"sharedOnly,omitempty"`
	Retained             []Checkpoint           `json:"retained"`
	DisposedWithArchives Checkpoint             `json:"disposedWithArchives"`
	PostClose            Checkpoint             `json:"postCloseRootStillReferenced"`
	Samples              []Sample               `json:"samples"`
	ActiveChecks         int                    `json:"activeChecks"`
	FinalFibers          []cc.Snapshot          `json:"finalFibers"`
	FinalProvisions      []cc.ProvisionSnapshot `json:"finalProvisions"`
	Events               []cc.Event             `json:"events"`
	Retention            *cc.RetentionStatus    `json:"retention,omitempty"` // Candidate history bounds and their use at the end of the run.
	DisposedArchives     int                    `json:"disposedArchives"`
	ActiveProvisionsEnd  int                    `json:"activeProvisionsEnd"`
	ResourcesEnd         int64                  `json:"resourcesEnd"`
	Errors               []string               `json:"errors,omitempty"`
	Gaps                 []string               `json:"gaps,omitempty"`
	Classification       Classification         `json:"classification"`
	BatchCalls           int                    `json:"nativeBatchCalls"`
	MountBatches         int                    `json:"nativeMountBatches"`
	UpdateBatches        int                    `json:"nativeUpdateBatches"`
	DisposeBatches       int                    `json:"nativeDisposeBatches"`
}

type Distribution struct {
	Count int   `json:"count"`
	P50   int64 `json:"p50NS"`
	P95   int64 `json:"p95NS"`
	P99   int64 `json:"p99NS"`
	Max   int64 `json:"maxNS"`
}

// NearestRank is ceil(p*n), one-based. Only the private numeric copy is sorted;
// caller samples and lifecycle/event traces keep their original causal order.
func NearestRank(values []int64, percentile float64) (int64, error) {
	if len(values) == 0 || math.IsNaN(percentile) || percentile <= 0 || percentile > 1 {
		return 0, fmt.Errorf("nonempty samples and percentile in (0,1] required")
	}
	ordered := slices.Clone(values)
	slices.Sort(ordered)
	return ordered[int(math.Ceil(percentile*float64(len(ordered))))-1], nil
}

func Summarize(values []int64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	p50, _ := NearestRank(values, .50)
	p95, _ := NearestRank(values, .95)
	p99, _ := NearestRank(values, .99)
	return Distribution{Count: len(values), P50: p50, P95: p95, P99: p99, Max: slices.Max(values)}
}

// CV is population standard deviation / mean across independent run throughput,
// not the CV of individual latencies. The frozen >10% noise rule remains fixed.
func CV(values []float64) (float64, error) {
	if len(values) < 2 {
		return 0, fmt.Errorf("at least two independent throughputs required")
	}
	var mean float64
	for _, value := range values {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, fmt.Errorf("finite positive throughput required")
		}
		mean += value / float64(len(values))
	}
	var variance float64
	for _, value := range values {
		variance += (value - mean) * (value - mean) / float64(len(values))
	}
	return math.Sqrt(variance) / mean, nil
}

func validateObserverStatus(status cc.ObserverStatus) error {
	if status.Delivered > status.Accepted || status.Remaining != status.Accepted-status.Delivered || status.Queued < 0 {
		return fmt.Errorf("inconsistent native observer counters: %+v", status)
	}
	work := uint64(status.Queued)
	if status.InFlight {
		work++
	}
	if status.Remaining != work {
		return fmt.Errorf("native observer pending count excludes queued/in-flight work: %+v", status)
	}
	return nil
}

func WriteJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func WriteCSV(w io.Writer, results []RunResult) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"backend", "workload", "repetition", "trial", "operation", "cycle", "tree", "scheduled_utc", "started_utc", "ended_utc", "latency_ns", "heap_before", "heap_after", "mallocs_before", "mallocs_after", "resources_after", "leases_after", "events_before", "events_after", "event_backlog", "observers_accepted_after_settle", "observers_delivered_after_settle", "observers_queued_after_settle", "observers_in_flight_after_settle", "observer_wait_ns", "settle_observers_latency_ns", "observers_ended_utc", "error"}); err != nil {
		return err
	}
	for _, result := range results {
		for _, sample := range result.Samples {
			fields := []string{result.Backend, result.Workload.Name, strconv.Itoa(result.Repetition), strconv.Itoa(result.Trial), sample.Operation, strconv.Itoa(sample.Cycle), strconv.Itoa(sample.Tree), sample.ScheduledUTC.Format(time.RFC3339Nano), sample.StartedUTC.Format(time.RFC3339Nano), sample.EndedUTC.Format(time.RFC3339Nano)}
			for _, value := range []int64{sample.LatencyNS, int64(sample.Before.Memory.HeapAlloc), int64(sample.After.Memory.HeapAlloc), int64(sample.Before.Memory.Mallocs), int64(sample.After.Memory.Mallocs), sample.After.ResourcesActive, sample.After.Leases, int64(sample.EventsBefore), int64(sample.EventsAfter)} {
				fields = append(fields, strconv.FormatInt(value, 10))
			}
			backlog := "unavailable"
			if sample.EventBacklog != nil {
				backlog = strconv.Itoa(*sample.EventBacklog)
			}
			accepted, delivered, queued, inFlight := "unavailable", "unavailable", "unavailable", "unavailable"
			if status := sample.AfterSettle; status != nil {
				accepted, delivered = strconv.FormatUint(status.Accepted, 10), strconv.FormatUint(status.Delivered, 10)
				queued, inFlight = strconv.Itoa(status.Queued), strconv.FormatBool(status.InFlight)
			}
			fields = append(fields, backlog, accepted, delivered, queued, inFlight, strconv.FormatInt(sample.ObserverWaitNS, 10), strconv.FormatInt(sample.SettleObserversLatencyNS, 10), sample.ObserversEndedUTC.Format(time.RFC3339Nano), sample.Error)
			if err := out.Write(fields); err != nil {
				return err
			}
		}
	}
	out.Flush()
	return out.Error()
}
