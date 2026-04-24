package benchmarks

import "cpu_bench_go/runner"

const (
	SizeSmall  = 1_000_000
	SizeMedium = 10_000_000
	SizeLarge  = 100_000_000
)

var expectedPrimes = map[int]int{
	SizeSmall:  78498,
	SizeMedium: 664579,
	SizeLarge:  5761455,
}

type Primes struct {
	n     int
	sieve []bool
}

func (p *Primes) Name() string { return "primes" }

func (p *Primes) Tags() []string { return []string{"integer", "memory", "cache"} }

func (p *Primes) Setup(n int) {
	if n <= 0 {
		n = SizeMedium
	}
	p.n = n
	p.sieve = make([]bool, n+1)
}

func (p *Primes) Run() any {
	n := p.n
	if n < 2 {
		return 0
	}
	sieve := p.sieve
	clear(sieve)
	for i := 2; i*i <= n; i++ {
		if !sieve[i] {
			for j := i * i; j <= n; j += i {
				sieve[j] = true
			}
		}
	}
	count := 0
	for i := 2; i <= n; i++ {
		if !sieve[i] {
			count++
		}
	}
	return count
}

func (p *Primes) Verify(result any) bool {
	got, ok := result.(int)
	if !ok {
		return false
	}
	if want, known := expectedPrimes[p.n]; known {
		return got == want
	}
	return got == piReference(p.n)
}

// piReference recomputes π(n) for custom sizes not in the lookup table.
func piReference(n int) int {
	if n < 2 {
		return 0
	}
	sieve := make([]bool, n+1)
	for i := 2; i*i <= n; i++ {
		if !sieve[i] {
			for j := i * i; j <= n; j += i {
				sieve[j] = true
			}
		}
	}
	count := 0
	for i := 2; i <= n; i++ {
		if !sieve[i] {
			count++
		}
	}
	return count
}

var _ runner.Benchmark = (*Primes)(nil)
