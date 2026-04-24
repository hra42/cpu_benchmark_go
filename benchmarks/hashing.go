package benchmarks

import (
	"crypto/sha256"

	"cpu_bench_go/runner"
)

const (
	HashSmall  = 1 << 20  // 1 MiB
	HashMedium = 1 << 20  // 1 MiB (default)
	HashLarge  = 64 << 20 // 64 MiB
)

type Hashing struct {
	n        int
	buf      []byte
	expected [32]byte
}

func (h *Hashing) Name() string { return "hashing" }

func (h *Hashing) Tags() []string { return []string{"hashing", "integer", "bandwidth"} }

func (h *Hashing) Setup(n int) {
	if n <= 0 {
		n = HashMedium
	}
	h.n = n
	h.buf = make([]byte, n)
	for i := range h.buf {
		h.buf[i] = byte(i*31 + 7)
	}
	h.expected = sha256.Sum256(h.buf)
}

func (h *Hashing) Run() any {
	return sha256.Sum256(h.buf)
}

func (h *Hashing) Verify(result any) bool {
	got, ok := result.([32]byte)
	if !ok {
		return false
	}
	return got == h.expected
}

var _ runner.Benchmark = (*Hashing)(nil)
