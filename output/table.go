package output

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"cpu_bench_go/results"
	"cpu_bench_go/scoring"
)

const tableDivider = "────────────────────────────────────────────────────────────────────"

func RenderTable(
	w io.Writer,
	toolVersion string,
	score *scoring.Result,
	stResults, mtResults []*results.RawResult,
) {
	logicalCPUs := runtime.NumCPU()
	fmt.Fprintf(w, "cpu-benchmark v%s — %s/%s — %s\n",
		toolVersion, runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Fprintf(w, "CPU: %s — %d Cores / %d Threads\n",
		detectCPUModel(), logicalCPUs, logicalCPUs)
	fmt.Fprintln(w, tableDivider)

	tw := tabwriter.NewWriter(w, 0, 0, 4, ' ', 0)
	fmt.Fprintln(tw, "Benchmark\tST Score\tMT Score\tMedian\tP99")

	stByName := indexByName(stResults)
	_ = mtResults // MT percentiles are reflected in MT score; table shows ST timing only.

	categories := []struct {
		label string
		cat   scoring.Category
		st    float64
		mt    float64
	}{
		{"Integer", scoring.CategoryInteger, score.SingleThread.Integer, score.MultiThread.Integer},
		{"Memory", scoring.CategoryMemory, score.SingleThread.Memory, score.MultiThread.Memory},
		{"Throughput", scoring.CategoryThroughput, score.SingleThread.Throughput, score.MultiThread.Throughput},
		{"Latency", scoring.CategoryLatency, score.SingleThread.Latency, score.MultiThread.Latency},
	}

	for _, c := range categories {
		var medianStr, p99Str string
		if c.cat == scoring.CategoryLatency {
			medianStr, p99Str = "—", "—"
		} else {
			names := scoring.BenchmarksFor(c.cat)
			// Use ST percentiles for the table's Median/P99 columns (ST is the
			// dominant single-core signal; MT values are reflected in the MT score).
			medianNs := medianOfMedians(names, stByName)
			p99Ns := maxP99(names, stByName)
			medianStr = formatDuration(medianNs)
			p99Str = formatDuration(p99Ns)
		}
		fmt.Fprintf(tw, "%s\t%.0f\t%.0f\t%s\t%s\n",
			c.label, c.st, c.mt, medianStr, p99Str)
	}
	tw.Flush()

	fmt.Fprintln(w, tableDivider)
	fmt.Fprintf(w, "Gesamt (ST)        %.0f        [%s]\n",
		score.SingleThread.Total, score.SingleThread.Label)
	fmt.Fprintf(w, "Gesamt (MT)        %.0f        [%s]\n",
		score.MultiThread.Total, score.MultiThread.Label)
	fmt.Fprintln(w, tableDivider)
	fmt.Fprintln(w, "Referenz: Apple M3 Pro — Score 1000")
}

func indexByName(rs []*results.RawResult) map[string]*results.RawResult {
	m := make(map[string]*results.RawResult, len(rs))
	for _, r := range rs {
		if r != nil {
			m[r.Name] = r
		}
	}
	return m
}

func medianOfMedians(names []string, byName map[string]*results.RawResult) int64 {
	vals := make([]int64, 0, len(names))
	for _, n := range names {
		if r, ok := byName[n]; ok {
			vals = append(vals, r.MedianNs)
		}
	}
	if len(vals) == 0 {
		return 0
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	return vals[len(vals)/2]
}

func maxP99(names []string, byName map[string]*results.RawResult) int64 {
	var max int64
	for _, n := range names {
		if r, ok := byName[n]; ok && r.P99Ns > max {
			max = r.P99Ns
		}
	}
	return max
}

func formatDuration(ns int64) string {
	if ns <= 0 {
		return "—"
	}
	d := time.Duration(ns)
	s := d.String()
	// Trim trailing zero fractions like "68.000ms" — Go's Duration.String
	// already does this, but strip noise suffixes if any slip through.
	return strings.TrimSuffix(s, "0s")
}
