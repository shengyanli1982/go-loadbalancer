package lb

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEDF_NilBackends(t *testing.T) {
	s := NewEDF()
	result := s.Select(nil)
	assert.Nil(t, result)
}

func TestEDF_EmptyBackends(t *testing.T) {
	s := NewEDF()
	result := s.Select([]Backend{})
	assert.Nil(t, result)
}

func TestEDF_SingleBackend(t *testing.T) {
	s := NewEDF()
	backends := []Backend{NewWeightedBackend("a", 5)}

	for i := 0; i < 10; i++ {
		result := s.Select(backends)
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}

func TestEDF_EqualWeightRoundRobin(t *testing.T) {
	// 所有权重为 1 时，EDF 应等价于 Round Robin（轮询 1,2,3,1,2,3,...）
	s := NewEDF()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 1),
	}

	expected := []string{"a", "b", "c", "a", "b", "c", "a", "b", "c"}
	for i, want := range expected {
		result := s.Select(backends)
		require.NotNil(t, result, "iteration %d", i)
		assert.Equal(t, want, result.Address(), "iteration %d", i)
	}
}

func TestEDF_WeightedDistribution(t *testing.T) {
	s := NewEDF()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
	}

	counts := map[string]int{}
	picks := 10000
	for i := 0; i < picks; i++ {
		b := s.Select(backends)
		counts[b.Address()]++
	}

	// 权重 1:3 → a ~25%, b ~75%
	assert.InDelta(t, 2500, counts["a"], 100, "'a' (25%%) count")
	assert.InDelta(t, 7500, counts["b"], 100, "'b' (75%%) count")
}

func TestEDF_DistributionPrecise(t *testing.T) {
	// EDF 在一个完整周期内应该严格按权重分配
	// 权重 1:2 → 3 次选择 = a:1, b:2
	s := NewEDF()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
	}

	counts := map[string]int{}
	const cycles = 100
	picks := cycles * 3 // 每个周期 3 次选择
	for i := 0; i < picks; i++ {
		b := s.Select(backends)
		counts[b.Address()]++
	}

	assert.Equal(t, cycles, counts["a"], "'a' should be selected exactly %d times", cycles)
	assert.Equal(t, cycles*2, counts["b"], "'b' should be selected exactly %d times", cycles*2)
}

func TestEDF_FallbackToWeight1(t *testing.T) {
	s := NewEDF()
	backends := []Backend{
		NewBackend("a"),
		NewBackend("b"),
		NewBackend("c"),
	}

	// 非加权后端应轮询
	expected := []string{"a", "b", "c", "a", "b", "c"}
	for i, want := range expected {
		result := s.Select(backends)
		require.NotNil(t, result, "iteration %d", i)
		assert.Equal(t, want, result.Address(), "iteration %d", i)
	}
}

func TestEDF_Concurrent(t *testing.T) {
	s := NewEDF()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
		NewWeightedBackend("c", 2),
	}

	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := s.Select(backends)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

func TestEDF_BackendChangeTriggersRebuild(t *testing.T) {
	s := NewEDF()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 1),
	}

	// 先消耗几个
	for i := 0; i < 5; i++ {
		s.Select(backends)
	}

	// 更换后端列表
	newBackends := []Backend{
		NewWeightedBackend("x", 1),
		NewWeightedBackend("y", 1),
	}

	result := s.Select(newBackends)
	require.NotNil(t, result)
	addr := result.Address()
	assert.True(t, addr == "x" || addr == "y", "should select from new backends, got %s", addr)
}

func TestEDF_DynamicBackends_AddBackend(t *testing.T) {
	s := NewEDF()
	backends1 := []Backend{
		NewWeightedBackend("a", 2), NewWeightedBackend("b", 1),
	}
	for i := 0; i < 6; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 2), NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 5), NewWeightedBackend("d", 1),
	}
	counts := map[string]int{}
	for i := 0; i < 160; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d"})
		counts[r.Address()]++
	}
	assert.True(t, counts["c"] > 0, "new backend 'c' should be selected")
	assert.True(t, counts["d"] > 0, "new backend 'd' should be selected")
}

func TestEDF_DynamicBackends_RemoveBackend(t *testing.T) {
	s := NewEDF()
	backends1 := []Backend{
		NewWeightedBackend("a", 1), NewWeightedBackend("b", 2),
		NewWeightedBackend("c", 3), NewWeightedBackend("d", 4),
	}
	for i := 0; i < 20; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 1), NewWeightedBackend("b", 2),
	}
	counts := map[string]int{}
	for i := 0; i < 60; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
		counts[r.Address()]++
	}
	// 权重 1:2，b 应该是 a 的两倍
	assert.Equal(t, 20, counts["a"])
	assert.Equal(t, 40, counts["b"])
}

func TestEDF_DynamicBackends_ReplaceAll(t *testing.T) {
	s := NewEDF()
	backends1 := []Backend{
		NewWeightedBackend("old1", 1), NewWeightedBackend("old2", 1),
	}
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("new1", 1), NewWeightedBackend("new2", 1), NewWeightedBackend("new3", 1),
	}
	expected := []string{"new1", "new2", "new3"}
	for i, want := range expected {
		r := s.Select(backends2)
		require.NotNil(t, r, "iteration %d", i)
		assert.Equal(t, want, r.Address(), "iteration %d", i)
	}
}

func TestEDF_DynamicBackends_ChangeWeight(t *testing.T) {
	s := NewEDF()
	backends1 := []Backend{
		NewWeightedBackend("a", 1), NewWeightedBackend("b", 1),
	}
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 9), NewWeightedBackend("b", 1),
	}
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
		counts[r.Address()]++
	}
	ratio := float64(counts["a"]) / float64(counts["b"])
	assert.Greater(t, ratio, 5.0, "weight ratio 9:1 → a should get ~9x traffic, got ratio=%.2f", ratio)
}

func TestEDF_DynamicBackends_SizeFluctuation(t *testing.T) {
	s := NewEDF()
	for size := 2; size <= 20; size += 3 {
		backends := make([]Backend, size)
		addrs := make([]string, size)
		for i := 0; i < size; i++ {
			addr := fmt.Sprintf("b%d", i)
			addrs[i] = addr
			backends[i] = NewWeightedBackend(addr, (i%5)+1)
		}
		for i := 0; i < 30; i++ {
			r := s.Select(backends)
			assertOnlyFrom(t, r, addrs)
		}
	}
}

func BenchmarkEDF(b *testing.B) {
	backends := generateWeightedBackends(100)
	s := NewEDF()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.Select(backends)
	}
}

func BenchmarkEDF_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateWeightedBackends(n)
			s := NewEDF()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.Select(backends)
			}
		})
	}
}
