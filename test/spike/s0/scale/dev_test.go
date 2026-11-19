// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestDevObserve is a development-host observation, NOT acceptance. It runs ONE
// root of one case in this process and prints one JSON line: stage latencies,
// retained heap, goroutines, stack use and the size of the raw trace. Run each
// case in its own process so a crash (for example a stack overflow at 10,000
// chained fibers) cannot take the others with it.
//
//	S0_SCALE_DEV=<shape>/<backend>/<fibers>        shape: independent chain fanout diamonds realms
//	S0_SCALE_DEV=trees/<backend>/<count>/<width>[/<cycles>]
func TestDevObserve(t *testing.T) {
	spec := os.Getenv("S0_SCALE_DEV")
	if spec == "" {
		t.Skip("development observation NOT RUN: set S0_SCALE_DEV")
	}
	parts := strings.Split(spec, "/")
	if len(parts) < 3 {
		t.Fatalf("bad S0_SCALE_DEV %q", spec)
	}
	shape, name := Topology(parts[0]), parts[1]
	var backend Backend
	for _, b := range Candidates() {
		if b.Name == name {
			backend = b
		}
	}
	if backend.New == nil {
		t.Fatalf("unknown backend %q", name)
	}
	num := func(i, def int) int {
		if i >= len(parts) {
			return def
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	var (
		f      Fixture
		err    error
		cycles = FrozenTreeCycles
	)
	if shape == Trees {
		f, err = TreeFixture(num(2, 0), num(3, 2))
		cycles = num(4, FrozenTreeCycles)
	} else {
		f, err = graphFixture(shape, num(2, 0))
	}
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result := RunCycles(t.Context(), backend, f, cycles)
	total := time.Since(started)
	metrics := retainedMetrics(result)
	raw := map[string][]int64{}
	for _, s := range result.Samples {
		raw[s.Operation] = append(raw[s.Operation], s.LatencyNS)
	}
	ops := map[string]map[string]float64{}
	for op, values := range raw {
		d := Summarize(values)
		ops[op] = map[string]float64{"n": float64(d.Count), "p50": float64(d.P50) / 1e6, "p99": float64(d.P99) / 1e6, "max": float64(d.Max) / 1e6}
	}
	counted := &countingWriter{w: io.Discard}
	zip := gzip.NewWriter(counted)
	_ = json.NewEncoder(zip).Encode(map[string]any{"events": result.Events, "fibers": result.FinalFibers, "provisions": result.FinalProvisions})
	_ = zip.Close()
	owners := map[string]int{}
	if n := len(result.Retained); n > 0 {
		for k, v := range result.Retained[n-1].Goroutines.Counts {
			owners[k] = v
		}
	}
	line, _ := json.Marshal(map[string]any{
		"case": spec, "NOT_ACCEPTANCE": true, "fibers": len(f.Nodes), "totalMs": total.Milliseconds(), "errors": result.Errors,
		"opsMs": ops, "emptyHeapMiB": float64(result.EmptyRoot.Memory.HeapAlloc) / (1 << 20), "retainedDeltaMiB": toMiB(metrics.DeltaBytes),
		"limitMiB": metrics.LimitBytes >> 20, "perFiberBytes": metrics.PerFiberBytes, "stackInuseDeltaKiB": metrics.StackInuseDelta >> 10,
		"goroutinesEmpty": len(result.EmptyRoot.Goroutines.Stacks), "goroutinesRetained": metrics.Goroutines, "ownersRetained": owners,
		"goroutinesPostClose": len(result.PostClose.Goroutines.Stacks), "events": len(result.Events), "archives": len(result.FinalFibers),
		"traceGzipMiB": float64(counted.n) / (1 << 20), "batches": []int{result.MountBatches, result.UpdateBatches, result.DisposeBatches},
	})
	fmt.Fprintf(os.Stdout, "DEV %s\n", line)
	if len(result.Errors) != 0 {
		t.Errorf("errors: %v", result.Errors)
	}
}

func toMiB(values []int64) []float64 {
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = float64(v) / (1 << 20)
	}
	return out
}
