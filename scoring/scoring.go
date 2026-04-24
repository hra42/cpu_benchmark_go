package scoring

import (
	"fmt"

	"cpu_bench_go/results"
)

type Category string

const (
	CategoryInteger    Category = "integer"
	CategoryMemory     Category = "memory"
	CategoryThroughput Category = "throughput"
	CategoryLatency    Category = "latency"
)

type Weights struct {
	Integer    float64
	Memory     float64
	Throughput float64
	Latency    float64
}

type ModeScore struct {
	Integer    float64
	Memory     float64
	Throughput float64
	Latency    float64
	Total      float64
	Label      string
}

type Result struct {
	SingleThread ModeScore
	MultiThread  ModeScore
	Weights      Weights
	Profile      string
}

type referenceValues struct {
	OpsPerSecST float64
	OpsPerSecMT float64
	P99NsST     int64
	P99NsMT     int64
}

// Reference = 1000 points. Captured on Apple M3 Pro (6P + 6E, 12 logical),
// macOS 26.3.1, Go 1.26.1, darwin/arm64, --size medium, --duration 10s,
// --warmup 2s, default profile. Recalibrate by running:
//
//	go run . --size medium --output json
//
// and copying ops_sec / p99_ns from the raw block into this map.
//
// Note: primes-MT scales only ~1.05× vs ST on this machine because the
// medium sieve (10M bytes) is memory-bandwidth bound and 12 cores saturate
// the SoC memory fabric. This is a real property of the platform, not a
// bug — benchmarks are instantiated per-goroutine and use cached scratch.
var references = map[string]referenceValues{
	"primes":       {OpsPerSecST: 82.09, OpsPerSecMT: 85.97, P99NsST: 13_400_000, P99NsMT: 177_496_500},
	"sorting":      {OpsPerSecST: 237.85, OpsPerSecMT: 2099.50, P99NsST: 4_565_875, P99NsMT: 9_854_042},
	"matrix_int":   {OpsPerSecST: 103.84, OpsPerSecMT: 931.61, P99NsST: 10_132_708, P99NsMT: 20_951_625},
	"matrix_float": {OpsPerSecST: 139.67, OpsPerSecMT: 1129.75, P99NsST: 7_535_500, P99NsMT: 18_790_000},
	"hashing":      {OpsPerSecST: 2712.85, OpsPerSecMT: 25294.70, P99NsST: 413_584, P99NsMT: 1_253_125},
	"json":         {OpsPerSecST: 279.48, OpsPerSecMT: 2184.63, P99NsST: 5_312_125, P99NsMT: 11_374_208},
}

// BenchmarksFor returns the benchmark names that feed into the given category.
// Latency is derived from all benchmarks' P99 latencies.
func BenchmarksFor(c Category) []string {
	switch c {
	case CategoryInteger:
		return []string{"primes", "sorting"}
	case CategoryMemory:
		return []string{"matrix_int", "matrix_float", "hashing"}
	case CategoryThroughput:
		return []string{"hashing", "json"}
	case CategoryLatency:
		return []string{"primes", "sorting", "matrix_int", "matrix_float", "hashing", "json"}
	default:
		return nil
	}
}

func WeightsFor(profile string) (Weights, error) {
	switch profile {
	case "default", "":
		return Weights{Integer: 0.30, Memory: 0.30, Throughput: 0.25, Latency: 0.15}, nil
	case "database":
		return Weights{Integer: 0.20, Memory: 0.20, Throughput: 0.35, Latency: 0.25}, nil
	case "batch":
		return Weights{Integer: 0.40, Memory: 0.20, Throughput: 0.30, Latency: 0.10}, nil
	default:
		return Weights{}, fmt.Errorf("unknown profile %q", profile)
	}
}

func normalize(measured, reference float64) float64 {
	if reference <= 0 {
		return 0
	}
	return measured / reference * 1000
}

func normalizeLatency(measuredNs, referenceNs int64) float64 {
	if measuredNs <= 0 || referenceNs <= 0 {
		return 0
	}
	return float64(referenceNs) / float64(measuredNs) * 1000
}

func Label(total float64) string {
	switch {
	case total < 500:
		return "Embedded / Low-Power"
	case total < 900:
		return "Entry Level"
	case total < 1100:
		return "Referenz"
	case total < 2000:
		return "Performance"
	case total < 4000:
		return "High Performance"
	default:
		return "Workstation / Server"
	}
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func computeMode(rs []*results.RawResult, mt bool, w Weights) ModeScore {
	byName := make(map[string]*results.RawResult, len(rs))
	for _, r := range rs {
		if r != nil {
			byName[r.Name] = r
		}
	}

	refOps := func(name string) float64 {
		ref := references[name]
		if mt {
			return ref.OpsPerSecMT
		}
		return ref.OpsPerSecST
	}
	refLat := func(name string) int64 {
		ref := references[name]
		if mt {
			return ref.P99NsMT
		}
		return ref.P99NsST
	}

	collectOps := func(names ...string) []float64 {
		out := make([]float64, 0, len(names))
		for _, n := range names {
			if r, ok := byName[n]; ok {
				out = append(out, normalize(r.OpsPerSec, refOps(n)))
			}
		}
		return out
	}

	integer := mean(collectOps("primes", "sorting"))
	memory := mean(collectOps("matrix_int", "matrix_float", "hashing"))
	throughput := mean(collectOps("hashing", "json"))

	allNames := []string{"primes", "sorting", "matrix_int", "matrix_float", "hashing", "json"}
	latencyScores := make([]float64, 0, len(allNames))
	for _, n := range allNames {
		if r, ok := byName[n]; ok {
			latencyScores = append(latencyScores, normalizeLatency(r.P99Ns, refLat(n)))
		}
	}
	latency := mean(latencyScores)

	total := integer*w.Integer + memory*w.Memory + throughput*w.Throughput + latency*w.Latency

	return ModeScore{
		Integer:    integer,
		Memory:     memory,
		Throughput: throughput,
		Latency:    latency,
		Total:      total,
		Label:      Label(total),
	}
}

func Score(stResults, mtResults []*results.RawResult, profile string) (*Result, error) {
	w, err := WeightsFor(profile)
	if err != nil {
		return nil, err
	}
	if profile == "" {
		profile = "default"
	}
	return &Result{
		SingleThread: computeMode(stResults, false, w),
		MultiThread:  computeMode(mtResults, true, w),
		Weights:      w,
		Profile:      profile,
	}, nil
}
