package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cpu_bench_go/benchmarks"
	"cpu_bench_go/output"
	"cpu_bench_go/results"
	"cpu_bench_go/runner"
	"cpu_bench_go/scoring"
	"cpu_bench_go/stress"
)

const toolVersion = "0.1"

type benchEntry struct {
	name    string
	factory func() runner.Benchmark
	sizes   map[string]int
}

func registry() ([]benchEntry, map[string]benchEntry) {
	entries := []benchEntry{
		{
			name:    "primes",
			factory: func() runner.Benchmark { return &benchmarks.Primes{} },
			sizes: map[string]int{
				"small":  benchmarks.SizeSmall,
				"medium": benchmarks.SizeMedium,
				"large":  benchmarks.SizeLarge,
			},
		},
		{
			name:    "sorting",
			factory: func() runner.Benchmark { return &benchmarks.Sorting{} },
			sizes: map[string]int{
				"small":  benchmarks.SortSmall,
				"medium": benchmarks.SortMedium,
				"large":  benchmarks.SortLarge,
			},
		},
		{
			name:    "matrix_int",
			factory: func() runner.Benchmark { return &benchmarks.MatrixInt64{} },
			sizes: map[string]int{
				"small":  benchmarks.MatSmall,
				"medium": benchmarks.MatMedium,
				"large":  benchmarks.MatLarge,
			},
		},
		{
			name:    "matrix_float",
			factory: func() runner.Benchmark { return &benchmarks.MatrixFloat64{} },
			sizes: map[string]int{
				"small":  benchmarks.MatSmall,
				"medium": benchmarks.MatMedium,
				"large":  benchmarks.MatLarge,
			},
		},
		{
			name:    "hashing",
			factory: func() runner.Benchmark { return &benchmarks.Hashing{} },
			sizes: map[string]int{
				"small":  benchmarks.HashSmall,
				"medium": benchmarks.HashMedium,
				"large":  benchmarks.HashLarge,
			},
		},
		{
			name:    "json",
			factory: func() runner.Benchmark { return &benchmarks.JSON{} },
			sizes: map[string]int{
				"small":  benchmarks.JSONSmall,
				"medium": benchmarks.JSONMedium,
				"large":  benchmarks.JSONLarge,
			},
		},
	}
	byName := make(map[string]benchEntry, len(entries))
	for _, e := range entries {
		byName[e.name] = e
	}
	return entries, byName
}

func main() {
	duration := flag.Duration("duration", 10*time.Second, "measurement duration per benchmark")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration per benchmark")
	benchSel := flag.String("benchmarks", "all", "comma-separated benchmark names, or \"all\"")
	threads := flag.Int("threads", 0, "MT goroutines: 0=NumCPU (default), 1=skip MT (ST only), N>1=use N goroutines for MT")
	size := flag.String("size", "medium", "input size: small | medium | large")
	out := flag.String("output", "table", "output format: table | json | both")
	profile := flag.String("profile", "default", "scoring profile: default | database | batch")
	stressMode := flag.Bool("stress", false, "stress mode: max-heat CPU+memory load (ignores benchmark/scoring flags)")
	stressDuration := flag.Duration("stress-duration", 0, "stress mode duration (0 = run until Ctrl-C)")
	stressCPU := flag.Int("stress-cpu", 0, "stress mode CPU workers (0 = NumCPU)")
	stressMem := flag.Int("stress-mem", 0, "stress mode memory workers (0 = NumCPU/2, -1 = disable)")
	stressMemMiB := flag.Int("stress-mem-mib", 256, "stress mode bytes per memory worker, in MiB")
	flag.Parse()

	if *stressMode {
		runStress(*stressDuration, *stressCPU, *stressMem, *stressMemMiB)
		return
	}

	if *duration <= 0 {
		fail("--duration must be > 0")
	}
	if *warmup <= 0 {
		fail("--warmup must be > 0")
	}
	if *threads < 0 {
		fail("--threads must be >= 0")
	}
	switch *size {
	case "small", "medium", "large":
	default:
		fail("--size must be one of small|medium|large (got %q)", *size)
	}
	switch *out {
	case "table", "json", "both":
	default:
		fail("--output must be one of table|json|both (got %q)", *out)
	}
	if _, err := scoring.WeightsFor(*profile); err != nil {
		fail("invalid --profile: %v", err)
	}

	ordered, byName := registry()
	selected, err := resolveBenchmarks(*benchSel, ordered, byName)
	if err != nil {
		fail("%v", err)
	}

	stCfg := runner.Config{Duration: *duration, Warmup: *warmup, Threads: 1}
	mtCfg := runner.Config{Duration: *duration, Warmup: *warmup, Threads: *threads}
	runMT := *threads != 1

	var stResults, mtResults []*results.RawResult
	for _, entry := range selected {
		n := entry.sizes[*size]
		factory := func() runner.Benchmark {
			b := entry.factory()
			b.Setup(n)
			return b
		}
		st := runner.Run(factory, stCfg)
		stResults = append(stResults, st)
		if runMT {
			mt := runner.Run(factory, mtCfg)
			mtResults = append(mtResults, mt)
		}
	}

	score, err := scoring.Score(stResults, mtResults, *profile)
	if err != nil {
		fail("scoring failed: %v", err)
	}

	switch *out {
	case "table":
		output.RenderTable(os.Stdout, toolVersion, score, stResults, mtResults)
	case "json":
		if err := emitJSON(*size, mtCfg, score, stResults, mtResults); err != nil {
			fail("json output failed: %v", err)
		}
	case "both":
		output.RenderTable(os.Stdout, toolVersion, score, stResults, mtResults)
		fmt.Fprintln(os.Stdout)
		if err := emitJSON(*size, mtCfg, score, stResults, mtResults); err != nil {
			fail("json output failed: %v", err)
		}
	}
}

func resolveBenchmarks(sel string, ordered []benchEntry, byName map[string]benchEntry) ([]benchEntry, error) {
	sel = strings.ToLower(strings.TrimSpace(sel))
	if sel == "" || sel == "all" {
		return ordered, nil
	}
	tokens := strings.Split(sel, ",")
	want := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := byName[t]; !ok {
			return nil, fmt.Errorf("unknown benchmark %q (known: %s)", t, knownNames(ordered))
		}
		want[t] = true
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("--benchmarks resolved to empty selection")
	}
	out := make([]benchEntry, 0, len(want))
	for _, e := range ordered {
		if want[e.name] {
			out = append(out, e)
		}
	}
	return out, nil
}

func knownNames(ordered []benchEntry) string {
	names := make([]string, len(ordered))
	for i, e := range ordered {
		names[i] = e.name
	}
	return strings.Join(names, ", ")
}

func emitJSON(size string, cfg runner.Config, score *scoring.Result, st, mt []*results.RawResult) error {
	doc := output.BuildDocument(toolVersion, size, cfg, score, st, mt)
	b, err := output.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

func runStress(duration time.Duration, cpuWorkers, memWorkers, memMiB int) {
	if duration < 0 {
		fail("--stress-duration must be >= 0")
	}
	if memMiB <= 0 {
		fail("--stress-mem-mib must be > 0")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := stress.Config{
		Duration:     duration,
		CPUWorkers:   cpuWorkers,
		MemWorkers:   memWorkers,
		MemBytesPerW: memMiB << 20,
		DisableMem:   memWorkers < 0,
	}
	if cfg.DisableMem {
		cfg.MemWorkers = 0
	}

	if duration == 0 {
		fmt.Fprintln(os.Stderr, "stress mode running until Ctrl-C...")
	} else {
		fmt.Fprintf(os.Stderr, "stress mode running for %s (Ctrl-C to abort early)...\n", duration)
	}

	s := stress.Run(ctx, cfg, os.Stderr)

	fmt.Fprintf(os.Stderr, "\nstress complete: elapsed=%s cpu_ops=%.3e mem_streamed=%.2f GiB cpu_workers=%d mem_workers=%d\n",
		s.Elapsed.Round(time.Millisecond), float64(s.CPUOps), float64(s.MemBytes)/(1<<30),
		s.CPUWorkers, s.MemWorkers)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
