package lb

import (
	"fmt"
	"testing"
)

func BenchmarkRoundRobin(b *testing.B) {
	backends := generateBackends(100)
	selector := NewRoundRobin()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkRoundRobin_Ext(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRoundRobin()
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkRandom_Ext(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRandom()
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkSmoothWeightedRR(b *testing.B) {
	backends := generateWeightedBackends(100)
	selector := NewSmoothWeightedRR()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkLeastConn(b *testing.B) {
	backends := generateBackends(100)
	selector := NewLeastConn()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkP2C(b *testing.B) {
	backends := generateBackends(100)
	selector := NewP2C()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkIPHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewIPHash()
	key := []byte("192.168.1.100")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkURIHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewURIHash(nil)
	key := []byte("/api/users/123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkRingHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewRingHash(&RingHashOptions{RingSize: 65536})
	key := []byte("test-key")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkMaglev(b *testing.B) {
	backends := generateBackends(50)
	selector := NewMaglev(&MaglevOptions{TableSize: 65537})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.Select(backends)
	}
}

func BenchmarkRingHash_SelectByHash(b *testing.B) {
	backends := generateBackends(50)
	selector := NewRingHash(&RingHashOptions{RingSize: 65536})
	key := []byte("test-key")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkRingHash_SelectByHash_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewRingHash(&RingHashOptions{RingSize: 65536})
			key := []byte("test-key")
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
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		selector.SelectByHash(backends, key)
	}
}

func BenchmarkMaglev_SelectByHash_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewMaglev(&MaglevOptions{TableSize: 65537})
			key := []byte("test-key")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.SelectByHash(backends, key)
			}
		})
	}
}

func BenchmarkLeastConn_Release(b *testing.B) {
	backends := generateBackends(100)
	selector := NewLeastConn()
	releaser := selector.(LeastConnReleaser)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		be := selector.Select(backends)
		releaser.Release(be)
	}
}

func BenchmarkP2C_Release(b *testing.B) {
	backends := generateBackends(100)
	selector := NewP2C()
	releaser := selector.(P2CReleaser)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		be := selector.Select(backends)
		releaser.Release(be)
	}
}

func BenchmarkWeightedRR_Ext(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateWeightedBackends(n)
			selector := NewWeightedRR()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkSmoothWeightedRR_Ext(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateWeightedBackends(n)
			selector := NewSmoothWeightedRR()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkLeastConn_Ext(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewLeastConn()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				selector.Select(backends)
			}
		})
	}
}

func BenchmarkP2C_Ext(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			selector := NewP2C()
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
		backends[i] = &testBackend{addr: fmt.Sprintf("backend-%d:8080", i)}
	}
	return backends
}

func generateWeightedBackends(n int) []Backend {
	backends := make([]Backend, n)
	for i := 0; i < n; i++ {
		weight := (i % 10) + 1
		backends[i] = &simpleWeightedBackend{
			addr:   fmt.Sprintf("backend-%d:8080", i),
			weight: weight,
		}
	}
	return backends
}
