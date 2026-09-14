package lb

import (
	"fmt"
	"testing"
)

// benchmarkConcurrentSelect 使用 b.RunParallel 实现真正的并发 Benchmark
// 修正说明：原实现对每次迭代只启动一个 goroutine 然后 wg.Wait()，实际是串行执行。
// 改用 b.RunParallel 让多个 P 并行执行 Select，模拟真实并发负载。
// ReportAllocs 在 helper 内统一开启：所有调用方（含 _Ext 孪生之外的基础并发
// benchmark）报告口径一致，B/op 与 allocs/op 是零分配承诺的回归信号。
// 注意：构造 selector/backends 的开销由调用方在调用本 helper 前 b.ResetTimer() 排除。
func benchmarkConcurrentSelect(b *testing.B, selector Selector, backends []Backend) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			selector.Select(backends)
		}
	})
}

// benchmarkConcurrentSelectByHash 使用 b.RunParallel 实现真正的并发 Hash Benchmark
// ReportAllocs 统一开启的理由同 benchmarkConcurrentSelect
func benchmarkConcurrentSelectByHash(b *testing.B, selector HashSelector, backends []Backend, key []byte) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			selector.SelectByHash(backends, key)
		}
	})
}

// 以下 8 个基础并发 benchmark 统一为「先构造、后 ResetTimer、再进 helper」的形态
// （与 lb_bench_test.go 及本文件 EDF/Rendezvous 等既有条目一致）：
// 原写法把 generateBackends(100) 作为实参在计时器启动后求值，构造开销混入 ns/op；
// ResetTimer 置于调用方而非 helper 内——helper 收到的已是构造完成的 backends。

func BenchmarkConcurrent_RoundRobin(b *testing.B) {
	selector := NewRoundRobin()
	backends := generateBackends(100)
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_Random(b *testing.B) {
	selector := NewRandom()
	backends := generateBackends(100)
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_WeightedRR(b *testing.B) {
	selector := NewWeightedRR()
	backends := generateWeightedBackends(100)
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_SmoothWeightedRR(b *testing.B) {
	selector := NewSmoothWeightedRR()
	backends := generateWeightedBackends(100)
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_LeastConn(b *testing.B) {
	selector := NewLeastConn()
	backends := generateBackends(100)
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

func BenchmarkConcurrent_P2C(b *testing.B) {
	selector := NewP2C()
	backends := generateBackends(100)
	b.ResetTimer()
	benchmarkConcurrentSelect(b, selector, backends)
}

// BenchmarkConcurrent_LeastConn_SelectRelease 测量并发 Select/Release 成对交织的真实竞争（连接计数稳态，不单调增长）
func BenchmarkConcurrent_LeastConn_SelectRelease(b *testing.B) {
	selector := NewLeastConn()
	releaser := selector.(RequestReleaser)
	backends := generateBackends(100)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			be := selector.Select(backends)
			releaser.Release(be)
		}
	})
}

// BenchmarkConcurrent_P2C_SelectRelease 测量并发 Select/Release 成对交织的真实竞争（负载计数稳态，不单调增长）
func BenchmarkConcurrent_P2C_SelectRelease(b *testing.B) {
	selector := NewP2C()
	releaser := selector.(RequestReleaser)
	backends := generateBackends(100)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			be := selector.Select(backends)
			releaser.Release(be)
		}
	})
}

func BenchmarkConcurrent_RingHash(b *testing.B) {
	selector := NewRingHash(&RingHashOptions{RingSize: 65536})
	backends := generateBackends(50)
	key := []byte("test-key-concurrent")
	b.ResetTimer()
	benchmarkConcurrentSelectByHash(b, selector, backends, key)
}

func BenchmarkConcurrent_Maglev(b *testing.B) {
	selector := NewMaglev(&MaglevOptions{TableSize: 65537})
	backends := generateBackends(50)
	key := []byte("test-key-concurrent")
	b.ResetTimer()
	benchmarkConcurrentSelectByHash(b, selector, backends, key)
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
