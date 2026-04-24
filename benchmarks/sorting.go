package benchmarks

import (
	"math/rand"

	"cpu_bench_go/runner"
)

const (
	SortSmall  = 10_000
	SortMedium = 100_000
	SortLarge  = 1_000_000
)

type Sorting struct {
	n    int
	seed int64
	arr  []int64
	rng  *rand.Rand
}

func (s *Sorting) Name() string { return "sorting" }

func (s *Sorting) Tags() []string { return []string{"integer", "memory", "branch"} }

func (s *Sorting) Setup(n int) {
	if n <= 0 {
		n = SortMedium
	}
	s.n = n
	s.seed = 0x5eed
	s.arr = make([]int64, n)
	s.rng = rand.New(rand.NewSource(s.seed))
}

func (s *Sorting) Run() any {
	arr := s.arr
	r := s.rng
	for i := range arr {
		arr[i] = r.Int63()
	}
	quicksort(arr, 0, len(arr)-1)
	return arr
}

func (s *Sorting) Verify(result any) bool {
	arr, ok := result.([]int64)
	if !ok || len(arr) != s.n {
		return false
	}
	for i := 1; i < len(arr); i++ {
		if arr[i-1] > arr[i] {
			return false
		}
	}
	return true
}

const insertionCutoff = 16

func quicksort(a []int64, lo, hi int) {
	for hi-lo > insertionCutoff {
		p := medianOfThree(a, lo, hi)
		pivot := a[p]
		a[p], a[hi] = a[hi], a[p]
		i := lo - 1
		for j := lo; j < hi; j++ {
			if a[j] <= pivot {
				i++
				a[i], a[j] = a[j], a[i]
			}
		}
		i++
		a[i], a[hi] = a[hi], a[i]
		if i-lo < hi-i {
			quicksort(a, lo, i-1)
			lo = i + 1
		} else {
			quicksort(a, i+1, hi)
			hi = i - 1
		}
	}
	insertionSort(a, lo, hi)
}

func medianOfThree(a []int64, lo, hi int) int {
	mid := lo + (hi-lo)/2
	if a[lo] > a[mid] {
		a[lo], a[mid] = a[mid], a[lo]
	}
	if a[lo] > a[hi] {
		a[lo], a[hi] = a[hi], a[lo]
	}
	if a[mid] > a[hi] {
		a[mid], a[hi] = a[hi], a[mid]
	}
	return mid
}

func insertionSort(a []int64, lo, hi int) {
	for i := lo + 1; i <= hi; i++ {
		v := a[i]
		j := i - 1
		for j >= lo && a[j] > v {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = v
	}
}

var _ runner.Benchmark = (*Sorting)(nil)
