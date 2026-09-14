package lb

import (
	"fmt"
	"testing"
)

func BenchmarkRoundRobin(b *testing.B) {
	backends := generateBackends(100)
	selector := NewRoundRobin()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkRoundRobin_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRoundRobin()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkRandom(b *testing.B) {
	backends := generateBackends(100)
	selector := NewRandom()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkRandom_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRandom()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkWeightedRR(b *testing.B) {
	backends := generateWeightedBackends(100)
	selector := NewWeightedRR()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkSmoothWeightedRR(b *testing.B) {
	backends := generateWeightedBackends(100)
	selector := NewSmoothWeightedRR()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkLeastConn(b *testing.B) {
	backends := generateBackends(100)
	selector := NewLeastConn()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkP2C(b *testing.B) {
	backends := generateBackends(100)
	selector := NewP2C()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkIPHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewIPHash()
	key := []byte("192.168.1.100")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkURIHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewURIHash(nil)
	key := []byte("/api/users/123")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

// BenchmarkRingHash_Select 测量 Select 包装路径（randomKey8 + SelectByHash，命中缓存快速路径）
func BenchmarkRingHash_Select(b *testing.B) {
	backends := generateBackends(50)
	selector := NewRingHash(nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkRingHash_Select_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRingHash(nil)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkMaglev(b *testing.B) {
	backends := generateBackends(50)
	selector := NewMaglev(&MaglevOptions{TableSize: 65537})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkRingHash_SelectByHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewRingHash(&RingHashOptions{RingSize: 65536})
	key := []byte("test-key")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkRingHash_SelectByHash_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRingHash(&RingHashOptions{RingSize: 65536})
			key := []byte("test-key")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.SelectByHash(backends, key)
			}
		})
	}
}

func BenchmarkMaglev_SelectByHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewMaglev(&MaglevOptions{TableSize: 65537})
	key := []byte("test-key")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkMaglev_SelectByHash_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewMaglev(&MaglevOptions{TableSize: 65537})
			key := []byte("test-key")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.SelectByHash(backends, key)
			}
		})
	}
}

// BenchmarkRendezvous_Select 补齐单协程 Select 包装路径（randomKey8 + SelectByHash，命中缓存快速路径）
func BenchmarkRendezvous_Select(b *testing.B) {
	backends := generateBackends(50)
	selector := NewRendezvous()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkLeastConn_Release(b *testing.B) {
	backends := generateBackends(100)
	selector := NewLeastConn()
	releaser := selector.(RequestReleaser)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		be := selector.Select(backends)
		releaser.Release(be)
	}
}

func BenchmarkP2C_Release(b *testing.B) {
	backends := generateBackends(100)
	selector := NewP2C()
	releaser := selector.(RequestReleaser)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		be := selector.Select(backends)
		releaser.Release(be)
	}
}

// BenchmarkARB_Release 测量 activeRequestBias 的 Release 成本
// （加锁 + addrIndex 查找 + connByIndex/connByAddr 双写 + 索引堆 siftUp 修复堆序，非 no-op）
func BenchmarkARB_Release(b *testing.B) {
	backends := generateWeightedBackends(100)
	selector := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})
	releaser := selector.(RequestReleaser)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		be := selector.Select(backends)
		releaser.Release(be)
	}
}

func BenchmarkWeightedRR_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateWeightedBackends(n)
			selector := NewWeightedRR()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkSmoothWeightedRR_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateWeightedBackends(n)
			selector := NewSmoothWeightedRR()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkLeastConn_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewLeastConn()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkP2C_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewP2C()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkIPHash_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewIPHash()
			key := []byte("192.168.1.100")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.SelectByHash(backends, key)
			}
		})
	}
}

func BenchmarkURIHash_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewURIHash(nil)
			key := []byte("/api/users/123")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.SelectByHash(backends, key)
			}
		})
	}
}

func BenchmarkLeastTime_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			configs := make([]ltConfig, n)
			for i := 0; i < n; i++ {
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
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkARB_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateWeightedBackends(n)
			selector := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

// BenchmarkLeastTime N=100 基线（LatencyBackend 热路径）
func BenchmarkLeastTime(b *testing.B) {
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
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

// BenchmarkARB N=100 基线（加权 ARB 热路径）
func BenchmarkARB(b *testing.B) {
	backends := generateWeightedBackends(100)
	selector := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

// BenchmarkRendezvous_Select_Ext 5 档扩展性曲线（Select 包装路径：randomKey8 + SelectByHash）
func BenchmarkRendezvous_Select_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRendezvous()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func generateBackends(n int) []Backend {
	backends := make([]Backend, n)
	for i := 0; i < n; i++ {
		backends[i] = NewBackend(fmt.Sprintf("backend-%d:8080", i))
	}
	return backends
}

func generateWeightedBackends(n int) []Backend {
	backends := make([]Backend, n)
	for i := 0; i < n; i++ {
		weight := (i % 10) + 1
		backends[i] = NewWeightedBackend(fmt.Sprintf("backend-%d:8080", i), weight)
	}
	return backends
}
