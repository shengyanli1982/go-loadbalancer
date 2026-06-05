package lb

import (
	"testing"
)

// benchmarkConcurrentSelect 使用 b.RunParallel 实现真正的并发 Benchmark
// 修正说明：原实现对每次迭代只启动一个 goroutine 然后 wg.Wait()，实际是串行执行。
// 改用 b.RunParallel 让多个 P 并行执行 Select，模拟真实并发负载。
func benchmarkConcurrentSelect(b *testing.B, selector Selector, backends []Backend) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			selector.Select(backends)
		}
	})
}

// benchmarkConcurrentSelectByHash 使用 b.RunParallel 实现真正的并发 Hash Benchmark
func benchmarkConcurrentSelectByHash(b *testing.B, selector HashSelector, backends []Backend, key []byte) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			selector.SelectByHash(backends, key)
		}
	})
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
