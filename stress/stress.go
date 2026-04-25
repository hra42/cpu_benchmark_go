// Package stress runs CPU + memory workers concurrently with the explicit goal
// of maximizing heat output (not measuring performance). CPU workers run a
// tight FMA loop on L1-resident buffers to keep ALUs saturated; memory workers
// stream large per-goroutine buffers to thrash the memory controller and DRAM.
// Running both together loads the ALU and memory subsystems simultaneously,
// which produces more heat than either alone.
package stress

import (
	"context"
	"fmt"
	"io"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Duration       time.Duration // 0 = run until ctx is cancelled
	CPUWorkers     int           // 0 = NumCPU
	MemWorkers     int           // 0 = NumCPU/2 (min 1)
	MemBytesPerW   int           // 0 = 256 MiB
	DisableMem     bool          // skip memory workers entirely (CPU-only stress)
	StatusInterval time.Duration // 0 = 1s; <0 = disabled
}

type Stats struct {
	Elapsed     time.Duration
	CPUOps      uint64 // total inner-loop iterations across CPU workers
	MemBytes    uint64 // total bytes touched across memory workers
	CPUWorkers  int
	MemWorkers  int
	MemBytesPerW int
}

// L1-resident scratch: 1 KiB of float64 (128 elements) easily fits in any L1d.
const cpuBufElems = 128

// Run launches CPU + memory workers and blocks until ctx is cancelled or
// cfg.Duration elapses. Status updates are written to status (may be nil).
func Run(ctx context.Context, cfg Config, status io.Writer) Stats {
	if cfg.CPUWorkers <= 0 {
		cfg.CPUWorkers = runtime.NumCPU()
	}
	if cfg.DisableMem {
		cfg.MemWorkers = 0
	} else {
		if cfg.MemWorkers < 0 {
			cfg.MemWorkers = 0
		}
		if cfg.MemWorkers == 0 {
			cfg.MemWorkers = runtime.NumCPU() / 2
			if cfg.MemWorkers < 1 {
				cfg.MemWorkers = 1
			}
		}
	}
	if cfg.MemBytesPerW <= 0 {
		cfg.MemBytesPerW = 256 << 20
	}
	if cfg.StatusInterval == 0 {
		cfg.StatusInterval = time.Second
	}

	prev := runtime.GOMAXPROCS(cfg.CPUWorkers + cfg.MemWorkers)
	defer runtime.GOMAXPROCS(prev)

	var stop atomic.Bool
	var cpuOps atomic.Uint64
	var memBytes atomic.Uint64

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if cfg.Duration > 0 {
		runCtx, cancel = context.WithTimeout(ctx, cfg.Duration)
		defer cancel()
	}

	// Stop watcher: flips the atomic when ctx is done. Workers poll the flag
	// instead of ctx so the hot path stays branch-light.
	go func() {
		<-runCtx.Done()
		stop.Store(true)
	}()

	var wg sync.WaitGroup

	// CPU workers. Pass the shared counter so workers publish progress
	// periodically (not just at the end) — needed for live status output.
	for i := 0; i < cfg.CPUWorkers; i++ {
		wg.Add(1)
		seed := float64(i + 1)
		go func() {
			defer wg.Done()
			cpuWorker(&stop, seed, &cpuOps)
		}()
	}

	// Memory workers — allocate up front so allocation cost isn't part of the hot loop.
	memBufs := make([][]uint64, cfg.MemWorkers)
	for i := range memBufs {
		memBufs[i] = make([]uint64, cfg.MemBytesPerW/8)
		// Touch every page once so the OS commits physical memory before the
		// hot loop starts; otherwise the first pass is dominated by page faults.
		for j := 0; j < len(memBufs[i]); j += 512 { // 4 KiB stride
			memBufs[i][j] = uint64(j)
		}
	}
	for i := 0; i < cfg.MemWorkers; i++ {
		wg.Add(1)
		buf := memBufs[i]
		go func() {
			defer wg.Done()
			memWorker(&stop, buf, &memBytes)
		}()
	}

	start := time.Now()

	// Status reporter.
	var statusWg sync.WaitGroup
	if status != nil && cfg.StatusInterval > 0 {
		statusWg.Add(1)
		go func() {
			defer statusWg.Done()
			tick := time.NewTicker(cfg.StatusInterval)
			defer tick.Stop()
			fmt.Fprintf(status, "stress: %d CPU workers, %d mem workers x %d MiB\n",
				cfg.CPUWorkers, cfg.MemWorkers, cfg.MemBytesPerW>>20)
			for {
				select {
				case <-runCtx.Done():
					return
				case <-tick.C:
					elapsed := time.Since(start)
					ops := cpuOps.Load()
					mb := memBytes.Load()
					fmt.Fprintf(status, "  t=%6.1fs  cpu=%.2e ops  mem=%6.1f GiB streamed\n",
						elapsed.Seconds(), float64(ops), float64(mb)/(1<<30))
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)
	statusWg.Wait()

	return Stats{
		Elapsed:      elapsed,
		CPUOps:       cpuOps.Load(),
		MemBytes:     memBytes.Load(),
		CPUWorkers:   cfg.CPUWorkers,
		MemWorkers:   cfg.MemWorkers,
		MemBytesPerW: cfg.MemBytesPerW,
	}
}

// cpuWorker runs a tight FMA-heavy loop on a stack-resident buffer. The inner
// 1 << 14 iterations keep the stop check off the hot path — at ~1 ns/op that's
// a check every ~16 us, fine for prompt shutdown but invisible to the ALUs.
//
//go:noinline
func cpuWorker(stop *atomic.Bool, seed float64, counter *atomic.Uint64) {
	var buf [cpuBufElems]float64
	for i := range buf {
		buf[i] = seed + float64(i)*0.5
	}
	const innerIters = 1 << 14
	a, b, c, d := seed, seed*1.0001, seed*0.9999, seed+1
	for !stop.Load() {
		for k := 0; k < innerIters; k++ {
			// 8 independent FMAs per iteration to keep multiple FP pipes busy.
			a = math.FMA(a, 1.0000001, 0.5)
			b = math.FMA(b, 0.9999999, 0.5)
			c = math.FMA(c, 1.0000003, -0.25)
			d = math.FMA(d, 0.9999997, -0.25)
			a = math.FMA(buf[k&(cpuBufElems-1)], a, b)
			b = math.FMA(buf[(k+1)&(cpuBufElems-1)], b, c)
			c = math.FMA(buf[(k+2)&(cpuBufElems-1)], c, d)
			d = math.FMA(buf[(k+3)&(cpuBufElems-1)], d, a)
			// Periodic sqrt to engage the FP divider/sqrt unit and prevent the
			// values from blowing up to inf.
			if k&63 == 0 {
				a = math.Sqrt(math.Abs(a) + 1)
				b = math.Sqrt(math.Abs(b) + 1)
				c = math.Sqrt(math.Abs(c) + 1)
				d = math.Sqrt(math.Abs(d) + 1)
			}
		}
		counter.Add(innerIters)
		// Sink: store back into buf so the optimizer cannot drop the loop.
		buf[0] = a
		buf[1] = b
		buf[2] = c
		buf[3] = d
	}
	cpuSink.Store(int64(math.Float64bits(buf[0] + buf[1] + buf[2] + buf[3])))
}

// cpuSink keeps the final accumulator alive past the function boundary.
var cpuSink atomic.Int64

// memWorker streams a large buffer doing read-modify-write. The 8-byte stride
// at uint64 granularity is sequential, which is what DRAM streamers prefer —
// we want maximum sustained bandwidth, not random-access latency. Each pass
// touches every cache line in the buffer.
//
//go:noinline
func memWorker(stop *atomic.Bool, buf []uint64, counter *atomic.Uint64) {
	bufBytes := uint64(len(buf)) * 8
	x := uint64(0x9E3779B97F4A7C15) // golden ratio constant; arbitrary nonzero seed
	for !stop.Load() {
		// Forward pass: RMW.
		for i := range buf {
			buf[i] = buf[i]*1664525 + x
			x += buf[i]
		}
		// Reverse pass: read+xor. Walking the buffer twice per stop-check
		// halves the per-byte cost of the atomic load.
		for i := len(buf) - 1; i >= 0; i-- {
			x ^= buf[i]
		}
		counter.Add(bufBytes * 2)
	}
	memSink.Store(x)
}

var memSink atomic.Uint64
