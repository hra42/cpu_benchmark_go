package benchmarks

import (
	"math"

	"cpu_bench_go/runner"
)

const (
	MatSmall  = 64
	MatMedium = 256
	MatLarge  = 512
)

// ---- Int64 ----

type MatrixInt64 struct {
	n       int
	a, b, c []int64
}

func (m *MatrixInt64) Name() string { return "matrix_int" }

func (m *MatrixInt64) Tags() []string { return []string{"integer", "memory"} }

func (m *MatrixInt64) Setup(n int) {
	if n <= 0 {
		n = MatMedium
	}
	m.n = n
	m.a = make([]int64, n*n)
	m.b = make([]int64, n*n)
	m.c = make([]int64, n*n)
	fillIntMat(m.a, 1)
	fillIntMat(m.b, 2)
}

func (m *MatrixInt64) Run() any {
	c := m.c
	clear(c)
	mulIntMat(c, m.a, m.b, m.n)
	return c
}

func (m *MatrixInt64) Verify(result any) bool {
	c, ok := result.([]int64)
	if !ok || len(c) != m.n*m.n {
		return false
	}
	n := m.n
	// Spot-check first and last element against reference recomputation.
	c0 := dotIntRow(m.a, m.b, 0, 0, n)
	if c[0] != c0 {
		return false
	}
	cl := dotIntRow(m.a, m.b, n-1, n-1, n)
	if c[n*n-1] != cl {
		return false
	}
	if n == MatSmall {
		// Full check for small matrix.
		ref := make([]int64, n*n)
		mulIntMat(ref, m.a, m.b, n)
		for i := range ref {
			if ref[i] != c[i] {
				return false
			}
		}
	}
	return true
}

func fillIntMat(a []int64, seed int64) {
	for i := range a {
		a[i] = int64(i%7) + seed
	}
}

func mulIntMat(c, a, b []int64, n int) {
	for i := 0; i < n; i++ {
		for k := 0; k < n; k++ {
			aik := a[i*n+k]
			for j := 0; j < n; j++ {
				c[i*n+j] += aik * b[k*n+j]
			}
		}
	}
}

func dotIntRow(a, b []int64, row, col, n int) int64 {
	var s int64
	for k := 0; k < n; k++ {
		s += a[row*n+k] * b[k*n+col]
	}
	return s
}

// ---- Float64 ----

type MatrixFloat64 struct {
	n       int
	a, b, c []float64
}

func (m *MatrixFloat64) Name() string { return "matrix_float" }

func (m *MatrixFloat64) Tags() []string { return []string{"float", "memory"} }

func (m *MatrixFloat64) Setup(n int) {
	if n <= 0 {
		n = MatMedium
	}
	m.n = n
	m.a = make([]float64, n*n)
	m.b = make([]float64, n*n)
	m.c = make([]float64, n*n)
	fillFloatMat(m.a, 1)
	fillFloatMat(m.b, 2)
}

func (m *MatrixFloat64) Run() any {
	c := m.c
	clear(c)
	mulFloatMat(c, m.a, m.b, m.n)
	return c
}

func (m *MatrixFloat64) Verify(result any) bool {
	c, ok := result.([]float64)
	if !ok || len(c) != m.n*m.n {
		return false
	}
	n := m.n
	tol := 1e-6 * float64(n)
	c0 := dotFloatRow(m.a, m.b, 0, 0, n)
	if math.Abs(c[0]-c0) > tol*math.Max(1, math.Abs(c0)) {
		return false
	}
	cl := dotFloatRow(m.a, m.b, n-1, n-1, n)
	if math.Abs(c[n*n-1]-cl) > tol*math.Max(1, math.Abs(cl)) {
		return false
	}
	if n == MatSmall {
		ref := make([]float64, n*n)
		mulFloatMat(ref, m.a, m.b, n)
		for i := range ref {
			if math.Abs(ref[i]-c[i]) > tol*math.Max(1, math.Abs(ref[i])) {
				return false
			}
		}
	}
	return true
}

func fillFloatMat(a []float64, seed float64) {
	for i := range a {
		a[i] = float64(i%13)*0.5 + seed
	}
}

func mulFloatMat(c, a, b []float64, n int) {
	for i := 0; i < n; i++ {
		for k := 0; k < n; k++ {
			aik := a[i*n+k]
			for j := 0; j < n; j++ {
				c[i*n+j] += aik * b[k*n+j]
			}
		}
	}
}

func dotFloatRow(a, b []float64, row, col, n int) float64 {
	var s float64
	for k := 0; k < n; k++ {
		s += a[row*n+k] * b[k*n+col]
	}
	return s
}

var _ runner.Benchmark = (*MatrixInt64)(nil)
var _ runner.Benchmark = (*MatrixFloat64)(nil)
