# cpu_bench_go

A reproducible, portable CPU benchmark for Linux and macOS (Go). Measures single-thread and multi-thread performance across six workloads, normalizes scores against a reference CPU, and exports results as JSON so machines can be compared directly.

## What it measures

| Benchmark      | Workload                              | Primary signal                          |
|----------------|---------------------------------------|-----------------------------------------|
| `primes`       | Sieve of Eratosthenes (1M / 10M / 100M)| Integer throughput, memory bandwidth    |
| `sorting`      | Quicksort on random `[]int64`         | Branch prediction, cache behavior       |
| `matrix_int`   | Integer matrix multiply (64² / 256² / 512²) | Integer ALU, cache hierarchy       |
| `matrix_float` | Float64 matrix multiply               | FP ALU, cache hierarchy                 |
| `hashing`      | SHA-256 over a fixed buffer (1 MiB / 64 MiB) | Crypto/ALU throughput, bandwidth |
| `json`         | JSON decode + encode of nested structs | Allocator, GC, reflection              |

Each benchmark runs for a fixed duration (default 10 s) after a warmup (default 2 s), collects per-iteration latencies, computes median / P95 / P99, and verifies its own output so the compiler cannot elide the work.

## Scoring

Each benchmark is normalized against a reference measurement:

- **Throughput:** `score = measured / reference × 1000`
- **Latency (P99, lower is better):** `score = reference / measured × 1000`

Benchmarks are grouped into four categories, then combined into a weighted total:

| Category   | Inputs                                  | Default weight |
|------------|-----------------------------------------|----------------|
| Integer    | `primes`, `sorting`                     | 30%            |
| Memory     | `matrix_int`, `matrix_float`, `hashing` | 30%            |
| Throughput | `hashing`, `json`                       | 25%            |
| Latency    | P99 of all six benchmarks               | 15%            |

Alternative profiles (`database`, `batch`) rebalance the weights. Single-thread and multi-thread totals are always reported separately.

### Reference CPU

**1000 points = Apple M3 Pro** (6P + 6E, 12 logical cores), macOS 26.3.1, Go 1.26.1, `darwin/arm64`, `--size medium`, `--duration 10s`, `--warmup 2s`, default profile.

Chosen for availability, not as a canonical baseline — see [Recalibration](#recalibration) below.

### Labels

| Total score     | Label                  |
|-----------------|------------------------|
| < 500           | Embedded / Low-Power   |
| 500 – 900       | Entry Level            |
| 900 – 1100      | Referenz               |
| 1100 – 2000     | Performance            |
| 2000 – 4000     | High Performance       |
| > 4000          | Workstation / Server   |

## Install & run

```sh
git clone ssh://git@git.hra42.com:2222/hra42/cpu_bench_go.git
cd cpu_bench_go
go build -o cpu_bench .
./cpu_bench
```

Requires Go 1.26+.

### CLI flags

```
--duration     Messdauer pro Benchmark        (default: 10s)
--warmup       Warmup-Dauer                   (default: 2s)
--benchmarks   Kommaseparierte Auswahl        (default: all)
--threads      0=NumCPU, 1=Single-Thread only, N=N-Threads (default: 0)
--size         small | medium | large         (default: medium)
--output       table | json | both            (default: table)
--profile      default | database | batch     (default: default)
```

### Examples

```sh
# Quick smoke test (1s duration, small inputs)
./cpu_bench --size small --duration 1s --warmup 500ms

# Full run with both table and JSON output
./cpu_bench --output both > run.log

# Just the integer-heavy benchmarks
./cpu_bench --benchmarks primes,sorting,matrix_int

# Only single-thread measurement
./cpu_bench --threads 1
```

## Output

### Terminal

```
cpu-benchmark v0.1 — darwin/arm64 — go1.26.1
CPU: Apple M3 Pro — 12 Cores / 12 Threads
────────────────────────────────────────────────────────────────────
Benchmark     ST Score    MT Score    Median        P99
Integer       1000        1059        …             …
Memory        998         1016        …             …
Throughput    1078        1295        …             …
Latency       999         1130        —             —
────────────────────────────────────────────────────────────────────
Gesamt (ST)        1017        [Referenz]
Gesamt (MT)        1116        [Performance]
────────────────────────────────────────────────────────────────────
Referenz: Apple M3 Pro — Score 1000
```

### JSON

`--output json` emits a structured document with `meta` (CPU, OS, Go version), `config` (duration, weights, profile), `scores` (ST + MT), and `raw` (per-benchmark `ops_sec`, `median_ns`, `p95_ns`, `p99_ns`, `iterations` for both modes). See `output/export.go` for the exact schema.

## Project layout

```
cpu_bench_go/
├── main.go                  # CLI, flag parsing, orchestration
├── runner/runner.go         # Benchmark harness (warmup, measurement, MT fan-out)
├── benchmarks/              # primes, sorting, matrix, hashing, json
├── scoring/                 # Normalization, weights, labels, reference values
├── results/                 # RawResult, percentile computation
├── output/                  # Terminal table + JSON export
└── dev-plan/plan.md         # Original design document (German)
```

### Runner model

Each benchmark satisfies the `runner.Benchmark` interface (`Name`, `Tags`, `Setup`, `Run`, `Verify`). The runner takes a **factory** (`func() Benchmark` that returns a setup-complete instance); in multi-thread mode every goroutine gets its own instance, so benchmarks cache scratch buffers as private state without contention.

Per-iteration allocation is avoided where possible: primes reuses a sieve and clears it, sorting refills a cached array, matrix reuses its output buffer, and json reuses its decode slice and encoder buffer.

### MT scaling on the reference machine

Measured speedups on the M3 Pro (12 logical cores), single-thread → multi-thread throughput:

| Benchmark      | ST ops/s | MT ops/s | Speedup |
|----------------|---------:|---------:|--------:|
| `primes`       |       82 |       86 |   1.05× |
| `sorting`      |      238 |     2100 |   8.8×  |
| `matrix_int`   |      104 |      932 |   9.0×  |
| `matrix_float` |      140 |     1130 |   8.1×  |
| `hashing`      |     2713 |    25295 |   9.3×  |
| `json`         |      279 |     2185 |   7.8×  |

**`primes` at `--size medium` is bandwidth-bound on Apple Silicon:** a 10 MB sieve exceeds per-core cache, and 12 cores sharing the unified memory fabric saturate before they can parallelize. This is a genuine platform property, not a benchmark bug — x86 desktops with dedicated DRAM channels per socket will scale this workload substantially better, and the score will reflect that.

## Recalibration

The reference map lives in `scoring/scoring.go` and is anchored to one specific machine. To re-anchor on a different CPU:

1. Run the benchmark on the target machine with `--size medium --output json --duration 10s --warmup 2s --profile default`.
2. Copy each `raw.{name}_st.ops_sec`, `raw.{name}_mt.ops_sec`, `raw.{name}_st.p99_ns`, and `raw.{name}_mt.p99_ns` into the corresponding entries in the `references` map.
3. Update the capture-conditions comment above the map (CPU model, OS, Go version, date).
4. Run `go test ./scoring/...` — the `TestReferenceMachineScoresExactly1000` test will fail if the map is inconsistent with itself.

## Tests

```sh
go test ./...
```

- `scoring/scoring_test.go` — feeds reference values back through `Score()` and asserts every category and total = 1000 for both ST and MT under the default profile. Locks the invariant that the reference machine scores 1000.
- `output/export_test.go` — validates the JSON export schema.

## License

No license specified yet.
