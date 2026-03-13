package registry_test

import (
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/shengyanli1982/go-loadbalancer/registry"
)

func BenchmarkManagerGetAlgorithm(b *testing.B) {
	m := benchmarkManagerWithAlgorithms(b, 4096)
	existing := "algo_2048"
	missing := "algo_missing"

	b.Run("hit_serial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plugin, ok := m.GetAlgorithm(existing)
			if !ok || plugin == nil {
				b.Fatal("expected algorithm hit")
			}
		}
	})

	b.Run("miss_serial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, ok := m.GetAlgorithm(missing); ok {
				b.Fatal("expected algorithm miss")
			}
		}
	})

	b.Run("hit_parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				plugin, ok := m.GetAlgorithm(existing)
				if !ok || plugin == nil {
					b.Fatal("expected algorithm hit")
				}
			}
		})
	})
}

func BenchmarkManagerHasAlgorithm(b *testing.B) {
	m := benchmarkManagerWithAlgorithms(b, 4096)
	existing := "algo_1024"
	missing := "algo_missing"

	b.Run("hit_serial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !m.HasAlgorithm(existing) {
				b.Fatal("expected algorithm exists")
			}
		}
	})

	b.Run("miss_serial", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if m.HasAlgorithm(missing) {
				b.Fatal("expected algorithm miss")
			}
		}
	})

	b.Run("hit_parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if !m.HasAlgorithm(existing) {
					b.Fatal("expected algorithm exists")
				}
			}
		})
	})
}

func BenchmarkManagerRegisterAlgorithmParallel(b *testing.B) {
	m := registry.NewManager()
	names := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		names[i] = "algo_bench_" + strconv.Itoa(i)
	}
	var index uint64

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := int(atomic.AddUint64(&index, 1) - 1)
			if err := m.RegisterAlgorithm(algorithmStub{name: names[i]}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkManagerWithAlgorithms(b *testing.B, count int) *registry.Manager {
	b.Helper()
	m := registry.NewManager()
	for i := 0; i < count; i++ {
		if err := m.RegisterAlgorithm(algorithmStub{name: "algo_" + strconv.Itoa(i)}); err != nil {
			b.Fatalf("register algorithm: %v", err)
		}
	}
	return m
}
