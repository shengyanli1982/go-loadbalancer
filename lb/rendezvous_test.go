package lb

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRendezvous_NilBackends(t *testing.T) {
	s := NewRendezvous()
	result := s.SelectByHash(nil, []byte("key"))
	assert.Nil(t, result)
}

func TestRendezvous_EmptyBackends(t *testing.T) {
	s := NewRendezvous()
	result := s.SelectByHash([]Backend{}, []byte("key"))
	assert.Nil(t, result)
}

func TestRendezvous_SingleBackend(t *testing.T) {
	s := NewRendezvous()
	backends := []Backend{NewWeightedBackend("a", 5)}

	for i := 0; i < 10; i++ {
		result := s.SelectByHash(backends, []byte(fmt.Sprintf("key-%d", i)))
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}

func TestRendezvous_Deterministic(t *testing.T) {
	s := NewRendezvous()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
		NewWeightedBackend("c", 3),
	}

	// 相同 key 必须始终映射到同一后端
	for i := 0; i < 100; i++ {
		key := []byte("same-key")
		first := s.SelectByHash(backends, key)
		second := s.SelectByHash(backends, key)
		assert.Equal(t, first.Address(), second.Address())
	}
}

func TestRendezvous_WeightedDistribution(t *testing.T) {
	s := NewRendezvous()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
	}

	counts := map[string]int{}
	picks := 10000
	for i := 0; i < picks; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		b := s.SelectByHash(backends, key)
		counts[b.Address()]++
	}

	// 权重 3 的后端应该获得约 75% 的流量
	assert.InDelta(t, 2500, counts["a"], 1500, "'a' (25%%) count")
	assert.InDelta(t, 7500, counts["b"], 1500, "'b' (75%%) count")
}

func TestRendezvous_AllBackendsSelected(t *testing.T) {
	s := NewRendezvous()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 1),
	}

	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		b := s.SelectByHash(backends, key)
		seen[b.Address()] = true
	}

	assert.True(t, seen["a"], "'a' should be selected")
	assert.True(t, seen["b"], "'b' should be selected")
	assert.True(t, seen["c"], "'c' should be selected")
}

func TestRendezvous_FallbackToWeight1(t *testing.T) {
	s := NewRendezvous()
	// 非加权后端，权重应默认为 1
	backends := []Backend{
		NewBackend("a"),
		NewBackend("b"),
		NewBackend("c"),
	}

	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		b := s.SelectByHash(backends, key)
		seen[b.Address()] = true
	}

	assert.True(t, seen["a"], "'a' should be selected")
	assert.True(t, seen["b"], "'b' should be selected")
	assert.True(t, seen["c"], "'c' should be selected")
}

func TestRendezvous_EmptyKey(t *testing.T) {
	s := NewRendezvous()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
	}

	// 空 key 不应 panic，应返回第一个后端
	result := s.SelectByHash(backends, nil)
	require.NotNil(t, result)

	result2 := s.SelectByHash(backends, []byte{})
	require.NotNil(t, result2)
}

func TestRendezvous_Concurrent(t *testing.T) {
	s := NewRendezvous()
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
		go func(id int) {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j))
				b := s.SelectByHash(backends, key)
				assert.NotNil(t, b)
			}
		}(i)
	}
	wg.Wait()
}

func TestRendezvous_DynamicBackends_AddBackend(t *testing.T) {
	s := NewRendezvous()
	backends1 := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 1)}
	backends2 := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 1), NewWeightedBackend("c", 1)}

	for i := 0; i < 20; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		r1 := s.SelectByHash(backends1, key)
		assertOnlyFrom(t, r1, []string{"a", "b"})
		r2 := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r2, []string{"a", "b", "c"})
	}
}

func TestRendezvous_DynamicBackends_RemoveBackend(t *testing.T) {
	s := NewRendezvous()
	backends1 := []Backend{
		NewWeightedBackend("a", 1), NewWeightedBackend("b", 1), NewWeightedBackend("c", 1),
	}
	backends2 := []Backend{NewWeightedBackend("a", 1)}

	for i := 0; i < 20; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		r := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r, []string{"a"})
	}
	// 确保移除后不影响现有
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		r := s.SelectByHash(backends1, key)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	}
}

func TestRendezvous_DynamicBackends_MappingStability(t *testing.T) {
	s := NewRendezvous()
	backends1 := []Backend{
		NewWeightedBackend("a", 1), NewWeightedBackend("b", 1), NewWeightedBackend("c", 1),
	}
	backends2 := []Backend{
		NewWeightedBackend("a", 1), NewWeightedBackend("b", 1), NewWeightedBackend("c", 1), NewWeightedBackend("d", 1),
	}

	beforeMapping := map[string]string{}
	keys := make([][]byte, 200)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%d", i))
		r := s.SelectByHash(backends1, keys[i])
		require.NotNil(t, r)
		beforeMapping[string(keys[i])] = r.Address()
	}

	unchanged := 0
	for _, key := range keys {
		r := s.SelectByHash(backends2, key)
		if beforeMapping[string(key)] == r.Address() {
			unchanged++
		}
	}
	// 增删单个节点，大约 1/n 的 key 需要重映射
	// 3→4 节点，大约 25% 重映射，75% 不变
	assert.Greater(t, unchanged, len(keys)/2, "at least 50%% of keys should stay on same backend, got %d/%d", unchanged, len(keys))
}

func TestRendezvous_DynamicBackends_SizeFluctuation(t *testing.T) {
	s := NewRendezvous()
	for size := 2; size <= 20; size += 3 {
		backends := make([]Backend, size)
		addrs := make([]string, size)
		for i := 0; i < size; i++ {
			addr := fmt.Sprintf("b%d", i)
			addrs[i] = addr
			backends[i] = NewWeightedBackend(addr, (i%5)+1)
		}
		for i := 0; i < 30; i++ {
			key := []byte(fmt.Sprintf("key-%d-%d", size, i))
			r := s.SelectByHash(backends, key)
			assertOnlyFrom(t, r, addrs)
		}
	}
}

func TestRendezvous_DynamicBackends_ChangeWeight(t *testing.T) {
	s := NewRendezvous()
	backendsLow := []Backend{
		NewWeightedBackend("a", 1), NewWeightedBackend("b", 1),
	}
	backendsHigh := []Backend{
		NewWeightedBackend("a", 10), NewWeightedBackend("b", 1),
	}

	countsHigh := map[string]int{}
	for i := 0; i < 3000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		b := s.SelectByHash(backendsHigh, key)
		countsHigh[b.Address()]++
	}
	// 权重 10:1，a 应获得远多于 b 的流量
	assert.Greater(t, countsHigh["a"], countsHigh["b"], "higher weight 'a' should get more traffic")

	countsLow := map[string]int{}
	for i := 0; i < 3000; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		b := s.SelectByHash(backendsLow, key)
		countsLow[b.Address()]++
	}
	// 等权时应大致均分
	assert.InDelta(t, 1500, countsLow["a"], 500, "equal weight should yield ~50%%")
}

func BenchmarkRendezvous(b *testing.B) {
	backends := generateWeightedBackends(50)
	s := NewRendezvous()
	key := []byte("test-key")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.SelectByHash(backends, key)
	}
}

func BenchmarkRendezvous_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateBackends(n)
			s := NewRendezvous()
			key := []byte("test-key")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.SelectByHash(backends, key)
			}
		})
	}
}

func BenchmarkRendezvous_Weighted_Ext(b *testing.B) {
	for _, n := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("%d_backends", n), func(b *testing.B) {
			backends := generateWeightedBackends(n)
			s := NewRendezvous()
			key := []byte("test-key")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.SelectByHash(backends, key)
			}
		})
	}
}
