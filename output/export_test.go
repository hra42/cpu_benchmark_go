package output

import (
	"encoding/json"
	"testing"
	"time"

	"cpu_bench_go/results"
	"cpu_bench_go/runner"
	"cpu_bench_go/scoring"
)

func TestBuildDocumentAndMarshal(t *testing.T) {
	st := []*results.RawResult{
		{Name: "primes", Iterations: 10, OpsPerSec: 100, MedianNs: 1_000_000, P95Ns: 1_200_000, P99Ns: 1_300_000, Verified: true},
		{Name: "sorting", Iterations: 20, OpsPerSec: 200, MedianNs: 500_000, P95Ns: 600_000, P99Ns: 650_000, Verified: true},
	}
	mt := []*results.RawResult{
		{Name: "primes", Iterations: 80, OpsPerSec: 800, MedianNs: 200_000, P95Ns: 240_000, P99Ns: 260_000, Verified: true},
	}
	score := &scoring.Result{
		SingleThread: scoring.ModeScore{Integer: 1000, Memory: 1100, Throughput: 900, Latency: 950, Total: 990, Label: "Referenz"},
		MultiThread:  scoring.ModeScore{Integer: 3000, Memory: 3200, Throughput: 2800, Latency: 2900, Total: 3000, Label: "High Performance"},
		Weights:      scoring.Weights{Integer: 0.3, Memory: 0.3, Throughput: 0.25, Latency: 0.15},
		Profile:      "default",
	}
	cfg := runner.Config{Duration: 3 * time.Second, Warmup: 500 * time.Millisecond}

	doc := BuildDocument("0.1", "medium", cfg, score, st, mt)
	b, err := Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"meta", "config", "scores", "raw"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("missing top-level key %q", k)
		}
	}

	scores, ok := parsed["scores"].(map[string]any)
	if !ok {
		t.Fatal("scores not an object")
	}
	stObj, ok := scores["single_thread"].(map[string]any)
	if !ok {
		t.Fatal("scores.single_thread not an object")
	}
	if label, _ := stObj["label"].(string); label == "" {
		t.Error("scores.single_thread.label is empty")
	}

	raw, _ := parsed["raw"].(map[string]any)
	if _, ok := raw["primes_st"]; !ok {
		t.Error("raw.primes_st missing")
	}
	if _, ok := raw["primes_mt"]; !ok {
		t.Error("raw.primes_mt missing")
	}
}
