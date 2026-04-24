package runner

import (
	"runtime"
	"sync"
	"time"

	"cpu_bench_go/results"
)

// Run() returns any so the caller can feed the result into Verify() —
// this prevents the compiler from eliminating the benchmark body as dead code.
type Benchmark interface {
	Name() string
	Tags() []string
	Setup(n int)
	Run() any
	Verify(result any) bool
}

// Factory returns a fresh Benchmark that has already had Setup() called on it.
// Each goroutine in the MT path gets its own instance, so benchmarks may
// safely cache scratch buffers as private fields.
type Factory func() Benchmark

type Config struct {
	Duration time.Duration
	Warmup   time.Duration
	Threads  int
}

const (
	defaultDuration = 10 * time.Second
	defaultWarmup   = 2 * time.Second
)

// Keeps warmup Run() return values alive so the compiler cannot elide the call.
var sink any

func Run(factory Factory, cfg Config) *results.RawResult {
	if cfg.Duration <= 0 {
		cfg.Duration = defaultDuration
	}
	if cfg.Warmup <= 0 {
		cfg.Warmup = defaultWarmup
	}
	threads := cfg.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
	}

	prev := runtime.GOMAXPROCS(threads)
	defer runtime.GOMAXPROCS(prev)

	capHint := 1024
	var samples []results.Sample
	var lastResult any
	var name string
	var tags []string

	if threads == 1 {
		b := factory()
		name, tags = b.Name(), b.Tags()

		warmupDeadline := time.Now().Add(cfg.Warmup)
		for time.Now().Before(warmupDeadline) {
			sink = b.Run()
		}
		runtime.GC()

		samples = make([]results.Sample, 0, capHint)
		measureStart := time.Now()
		deadline := measureStart.Add(cfg.Duration)
		lastResult = measureLoop(b, deadline, &samples)
		totalDuration := time.Since(measureStart)

		return finalize(name, tags, factory, samples, lastResult, totalDuration)
	}

	// MT: one benchmark instance per goroutine — private scratch, no sharing.
	instances := make([]Benchmark, threads)
	for i := range instances {
		instances[i] = factory()
	}
	name, tags = instances[0].Name(), instances[0].Tags()

	var warmWg sync.WaitGroup
	warmWg.Add(threads)
	warmupDeadline := time.Now().Add(cfg.Warmup)
	for i := 0; i < threads; i++ {
		b := instances[i]
		go func() {
			defer warmWg.Done()
			for time.Now().Before(warmupDeadline) {
				sink = b.Run()
			}
		}()
	}
	warmWg.Wait()
	runtime.GC()

	perG := make([][]results.Sample, threads)
	firsts := make([]any, threads)
	var wg sync.WaitGroup
	wg.Add(threads)
	measureStart := time.Now()
	deadline := measureStart.Add(cfg.Duration)
	for i := 0; i < threads; i++ {
		i := i
		b := instances[i]
		go func() {
			defer wg.Done()
			local := make([]results.Sample, 0, capHint)
			firsts[i] = measureLoop(b, deadline, &local)
			perG[i] = local
		}()
	}
	wg.Wait()
	totalDuration := time.Since(measureStart)

	total := 0
	for _, s := range perG {
		total += len(s)
	}
	samples = make([]results.Sample, 0, total)
	for _, s := range perG {
		samples = append(samples, s...)
	}
	lastResult = firsts[0]

	return finalize(name, tags, factory, samples, lastResult, totalDuration)
}

func finalize(name string, tags []string, factory Factory, samples []results.Sample, lastResult any, totalDuration time.Duration) *results.RawResult {
	median, p95, p99 := results.Compute(samples)
	opsPerSec := 0.0
	if totalDuration > 0 {
		opsPerSec = float64(len(samples)) / totalDuration.Seconds()
	}
	// Verify on a fresh instance to avoid any dependence on goroutine-0's state
	// after the measurement loop mutated its scratch buffers.
	verified := factory().Verify(lastResult)
	return &results.RawResult{
		Name:          name,
		Tags:          tags,
		Iterations:    len(samples),
		TotalDuration: totalDuration,
		OpsPerSec:     opsPerSec,
		MedianNs:      median,
		P95Ns:         p95,
		P99Ns:         p99,
		Verified:      verified,
	}
}

func measureLoop(b Benchmark, deadline time.Time, out *[]results.Sample) (last any) {
	for time.Now().Before(deadline) {
		t0 := time.Now()
		last = b.Run()
		*out = append(*out, results.Sample{Duration: time.Since(t0)})
	}
	return last
}
