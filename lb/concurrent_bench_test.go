package lb

import (
	"sync"
	"testing"
)

func benchmarkConcurrentSelect(b *testing.B, selector Selector, backends []Backend) {
	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selector.Select(backends)
		}()
		wg.Wait()
	}
}

func benchmarkConcurrentSelectByHash(b *testing.B, selector HashSelector, backends []Backend, key []byte) {
	var wg sync.WaitGroup
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selector.SelectByHash(backends, key)
		}()
		wg.Wait()
	}
}

func BenchmarkConcurrent_RoundRobin(b *testing.B) {
	benchmarkConcurrentSelect(b, NewRoundRobin(), generateBackends(100))
}

func BenchmarkConcurrent_Random(b *testing.B) {
	benchmarkConcurrentSelect(b, NewRandom(), generateBackends(100))
}

func BenchmarkConcurrent_WeightedRR(b *testing.B) {
	benchmarkConcurrentSelect(b, NewWeightedRR(), generateWeightedBackends(100))
}

func BenchmarkConcurrent_SmoothWeightedRR(b *testing.B) {
	benchmarkConcurrentSelect(b, NewSmoothWeightedRR(), generateWeightedBackends(100))
}

func BenchmarkConcurrent_LeastConn(b *testing.B) {
	benchmarkConcurrentSelect(b, NewLeastConn(), generateBackends(100))
}

func BenchmarkConcurrent_P2C(b *testing.B) {
	benchmarkConcurrentSelect(b, NewP2C(), generateBackends(100))
}

func BenchmarkConcurrent_RingHash(b *testing.B) {
	benchmarkConcurrentSelectByHash(b, NewRingHash(&RingHashOptions{RingSize: 65536}), generateBackends(50), []byte("test-key-concurrent"))
}

func BenchmarkConcurrent_Maglev(b *testing.B) {
	benchmarkConcurrentSelectByHash(b, NewMaglev(&MaglevOptions{TableSize: 65537}), generateBackends(50), []byte("test-key-concurrent"))
}
