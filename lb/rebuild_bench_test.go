package lb

import (
	"fmt"
	"testing"
)

// 慢路径重建基准族：setup 预构造两份内容不同的等长后端 slice，
// 循环内交替使用使指纹每次都失配，强制每次迭代触发完整重建，测量纯重建成本
// （含 RingHash 建环排序、Maglev 建表 65537 槽等大表结构）。
// 重建族为冷路径，不受热路径零分配约束，allocs 由 -benchmem 展示真实值。

// rebuildBenchBackends 重建基准统一后端规模（重建是 O(n·vnodes) 冷路径，无需 Ext 系列）
const rebuildBenchBackends = 50

// rebuildBackendPairs 构造两份内容不同（地址不同）的等长后端 slice，交替使用强制指纹失配
func rebuildBackendPairs(n int) (backendsA, backendsB []Backend) {
	backendsA = make([]Backend, n)
	backendsB = make([]Backend, n)
	for i := 0; i < n; i++ {
		backendsA[i] = NewBackend(fmt.Sprintf("backend-a-%d:8080", i))
		backendsB[i] = NewBackend(fmt.Sprintf("backend-b-%d:8080", i))
	}
	return backendsA, backendsB
}

// rebuildWeightedBackendPairs 构造两份内容不同的等长加权后端 slice，交替使用强制指纹失配
func rebuildWeightedBackendPairs(n int) (backendsA, backendsB []Backend) {
	backendsA = make([]Backend, n)
	backendsB = make([]Backend, n)
	for i := 0; i < n; i++ {
		weight := (i % 10) + 1
		backendsA[i] = NewWeightedBackend(fmt.Sprintf("backend-a-%d:8080", i), weight)
		backendsB[i] = NewWeightedBackend(fmt.Sprintf("backend-b-%d:8080", i), weight)
	}
	return backendsA, backendsB
}

// BenchmarkRebuild_RingHash 测量 ringHash 慢路径重建成本（每次迭代重建完整哈希环并排序）
func BenchmarkRebuild_RingHash(b *testing.B) {
	backendsA, backendsB := rebuildBackendPairs(rebuildBenchBackends)
	selector := NewRingHash(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			selector.Select(backendsA)
		} else {
			selector.Select(backendsB)
		}
	}
}

// BenchmarkRebuild_Maglev 测量 maglev 慢路径重建成本（每次迭代重建 65537 槽查找表）
func BenchmarkRebuild_Maglev(b *testing.B) {
	backendsA, backendsB := rebuildBackendPairs(rebuildBenchBackends)
	selector := NewMaglev(&MaglevOptions{TableSize: 65537})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			selector.Select(backendsA)
		} else {
			selector.Select(backendsB)
		}
	}
}

// BenchmarkRebuild_Rendezvous 测量 rendezvous 慢路径重建成本（每次迭代重算地址哈希与权重缓存）
func BenchmarkRebuild_Rendezvous(b *testing.B) {
	backendsA, backendsB := rebuildBackendPairs(rebuildBenchBackends)
	selector := NewRendezvous()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			selector.Select(backendsA)
		} else {
			selector.Select(backendsB)
		}
	}
}

// BenchmarkRebuild_WeightedRR 测量 weightedRR 慢路径重建成本（每次迭代重建累积权重数组）
func BenchmarkRebuild_WeightedRR(b *testing.B) {
	backendsA, backendsB := rebuildWeightedBackendPairs(rebuildBenchBackends)
	selector := NewWeightedRR()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			selector.Select(backendsA)
		} else {
			selector.Select(backendsB)
		}
	}
}

// BenchmarkRebuild_SmoothWeightedRR 测量 smoothWeightedRR 慢路径重建成本（每次迭代重建权重状态）
func BenchmarkRebuild_SmoothWeightedRR(b *testing.B) {
	backendsA, backendsB := rebuildWeightedBackendPairs(rebuildBenchBackends)
	selector := NewSmoothWeightedRR()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			selector.Select(backendsA)
		} else {
			selector.Select(backendsB)
		}
	}
}

// BenchmarkRebuild_LeastConn 测量 leastConn 慢路径重建成本（每次迭代重建连接索引与索引堆）
func BenchmarkRebuild_LeastConn(b *testing.B) {
	backendsA, backendsB := rebuildBackendPairs(rebuildBenchBackends)
	selector := NewLeastConn()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			selector.Select(backendsA)
		} else {
			selector.Select(backendsB)
		}
	}
}
