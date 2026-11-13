// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

// This is deliberately ONE tiny real lifecycle, not a 10k budget run. The same
// 16-fiber realm fixture is exercised once per real candidate, serially.
func TestSmallRealLifecycleSmoke(t *testing.T) {
	f, err := RealmFixture(4, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range Candidates() {
		t.Run(backend.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result := RunT(ctx, backend, f)
			if len(result.Errors) != 0 || result.Classification != Inconclusive {
				t.Fatalf("small lifecycle (NOT scale acceptance): %+v", result)
			}
			if result.ActiveChecks != 4 || result.DisposedArchives != 16 || result.ResourcesEnd != 0 || result.ActiveProvisionsEnd != 0 {
				t.Fatalf("wrong actual lifecycle ledger: activeChecks=%d disposed=%d resources=%d provisions=%d", result.ActiveChecks, result.DisposedArchives, result.ResourcesEnd, result.ActiveProvisionsEnd)
			}
			if len(result.Samples) != 5 || len(result.FinalFibers) != 16 || len(result.FinalProvisions) != 0 || len(result.Events) == 0 {
				t.Fatalf("raw ledger missing: samples=%d archives=%d provisions=%d events=%d", len(result.Samples), len(result.FinalFibers), len(result.FinalProvisions), len(result.Events))
			}
			for i, event := range result.Events {
				if event.Sequence != uint64(i+1) {
					t.Fatalf("raw final event order at %d: %d", i, event.Sequence)
				}
			}
			for _, sample := range result.Samples {
				if sample.Error != "" || sample.EventBacklog != nil || sample.LatencyNS <= 0 {
					t.Fatalf("bad raw observation: %+v", sample)
				}
			}
		})
	}
}

func TestNearestRankAndCV(t *testing.T) {
	values := []int64{10, 1, 9, 2, 8, 3, 7, 4, 6, 5}
	for _, check := range []struct {
		p    float64
		want int64
	}{{.50, 5}, {.95, 10}, {.99, 10}, {1, 10}, {.01, 1}} {
		got, err := NearestRank(values, check.p)
		if err != nil || got != check.want {
			t.Fatalf("rank(%v) = %d, %v", check.p, got, err)
		}
	}
	if values[0] != 10 {
		t.Fatal("numeric summary reordered original causal sample stream")
	}
	if _, err := NearestRank(nil, .99); err == nil {
		t.Fatal("empty quantile accepted")
	}
	if _, err := NearestRank(values, 0); err == nil {
		t.Fatal("zero percentile accepted")
	}
	cv, err := CV([]float64{90, 110})
	if err != nil || math.Abs(cv-.1) > 1e-12 {
		t.Fatalf("population CV = %v, %v", cv, err)
	}
	if _, err := CV([]float64{100}); err == nil {
		t.Fatal("single repetition accepted")
	}
	if _, err := CV([]float64{100, 0}); err == nil {
		t.Fatal("invalid throughput accepted")
	}
	d := Summarize(values)
	if d.Count != 10 || d.P50 != 5 || d.P99 != 10 || d.Max != 10 {
		t.Fatalf("distribution = %+v", d)
	}
}

func TestStackOwnershipAndRawWriters(t *testing.T) {
	observed := parseGoroutines("goroutine 7 [chan receive]:\nexample/test/spike/s0/concurrency.(*rcOwner).run(0x01)\n /owner.go:1\n\ngoroutine 8 [running]:\nexternal.worker(0x02)\n /worker.go:2\n")
	if observed.Counts["example/test/spike/s0/concurrency.(*rcOwner).run"] != 1 || observed.Counts["external"] != 1 {
		t.Fatalf("stack owners = %+v", observed.Counts)
	}
	result := RunResult{Backend: "fixture", Workload: Workload{Name: "tiny"}, Classification: Inconclusive, Samples: []Sample{{Operation: "mount-settle", Tree: -1, LatencyNS: 42}}}
	var json, csv bytes.Buffer
	if err := WriteJSON(&json, result); err != nil || !strings.Contains(json.String(), "\"latencyNS\": 42") {
		t.Fatalf("JSON: %q %v", json.String(), err)
	}
	if err := WriteCSV(&csv, []RunResult{result}); err != nil || !strings.Contains(csv.String(), "unavailable") {
		t.Fatalf("CSV: %q %v", csv.String(), err)
	}
}
