package lb

import (
	"fmt"
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

func BenchmarkConcurrent_EDF(b *testing.B) {
	selector := NewEDF()
	backends := generateWeightedBackends(100)
	b.ReportAllocs()
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_Rendezvous(b *testing.B) {
	selector := NewRendezvous()
	backends := generateBackends(100)
	b.ReportAllocs()
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_LeastTime(b *testing.B) {
	configs := make([]ltConfig, 100)
	for i := 0; i < 100; i++ {
		configs[i] = ltConfig{
			addr:    fmt.Sprintf("svc-%d:80", i),
			weight:  (i % 3) + 1,
			latency: float64(i*5 + 1),
			conns:   i % 10,
		}
	}
	backends := newLatencyBackends(configs)
	selector := NewLeastTime()
	b.ReportAllocs()
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_ARB(b *testing.B) {
	selector := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})
	backends := generateWeightedBackends(100)
	b.ReportAllocs()
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_IPHash(b *testing.B) {
	selector := NewIPHash()
	backends := generateBackends(50)
	key := []byte("192.168.1.100")
	b.ReportAllocs()
	b.ResetTimer()
	benchmarkConcurrentSelectByHash(b, selector, backends, key)
}

func BenchmarkConcurrent_URIHash(b *testing.B) {
	selector := NewURIHash(nil)
	backends := generateBackends(50)
	key := []byte("/api/users/123")
	b.ReportAllocs()
	b.ResetTimer()
	benchmarkConcurrentSelectByHash(b, selector, backends, key)
}
