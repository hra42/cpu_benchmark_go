package results

import (
	"sort"
	"time"
)

type Sample struct {
	Duration time.Duration
}

type RawResult struct {
	Name          string
	Tags          []string
	Iterations    int
	TotalDuration time.Duration
	OpsPerSec     float64
	MedianNs      int64
	P95Ns         int64
	P99Ns         int64
	Verified      bool
}

func Compute(samples []Sample) (median, p95, p99 int64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	ns := make([]int64, len(samples))
	for i, s := range samples {
		ns[i] = s.Duration.Nanoseconds()
	}
	sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })
	return percentile(ns, 0.50), percentile(ns, 0.95), percentile(ns, 0.99)
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}
