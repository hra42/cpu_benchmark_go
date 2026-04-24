package scoring

import (
	"math"
	"testing"

	"cpu_bench_go/results"
)

// Feeding the reference values back through Score must yield 1000 in every
// category and total, for both ST and MT, under the default profile.
func TestReferenceMachineScoresExactly1000(t *testing.T) {
	st := make([]*results.RawResult, 0, len(references))
	mt := make([]*results.RawResult, 0, len(references))
	for name, ref := range references {
		st = append(st, &results.RawResult{
			Name:      name,
			OpsPerSec: ref.OpsPerSecST,
			P99Ns:     ref.P99NsST,
		})
		mt = append(mt, &results.RawResult{
			Name:      name,
			OpsPerSec: ref.OpsPerSecMT,
			P99Ns:     ref.P99NsMT,
		})
	}

	res, err := Score(st, mt, "default")
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}

	checkMode(t, "single-thread", res.SingleThread)
	checkMode(t, "multi-thread", res.MultiThread)

	if got := Label(1000); got != "Referenz" {
		t.Errorf("Label(1000) = %q, want %q", got, "Referenz")
	}
}

func checkMode(t *testing.T, label string, m ModeScore) {
	t.Helper()
	const eps = 1e-6
	checks := []struct {
		field string
		got   float64
	}{
		{"Integer", m.Integer},
		{"Memory", m.Memory},
		{"Throughput", m.Throughput},
		{"Latency", m.Latency},
		{"Total", m.Total},
	}
	for _, c := range checks {
		if math.Abs(c.got-1000) > eps {
			t.Errorf("%s %s = %v, want 1000", label, c.field, c.got)
		}
	}
	if m.Label != "Referenz" {
		t.Errorf("%s Label = %q, want %q", label, m.Label, "Referenz")
	}
}
