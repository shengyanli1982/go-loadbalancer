package lb

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARB_NilBackends(t *testing.T) {
	s := NewActiveRequestBias()
	result := s.Select(nil)
	assert.Nil(t, result)
}

func TestARB_EmptyBackends(t *testing.T) {
	s := NewActiveRequestBias()
	result := s.Select([]Backend{})
	assert.Nil(t, result)
}

func TestARB_SingleBackend(t *testing.T) {
	s := NewActiveRequestBias()

	backend := NewBackend("server1")
	backends := []Backend{backend}

	for i := 0; i < 10; i++ {
		result := s.Select(backends)
		assert.Equal(t, backend, result)
	}
}

func TestARB_BiasZeroDegeneratesToWRR(t *testing.T) {
	s := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 0})

	backends := []Backend{
		NewWeightedBackend("server1", 1),
		NewWeightedBackend("server2", 1),
		NewWeightedBackend("server3", 1),
	}

	// bias=0 退化为 RR，分配应均匀
	counts := make(map[string]int)
	n := 300
	for i := 0; i < n; i++ {
		result := s.Select(backends)
		counts[result.Address()]++
	}

	// 均匀分配，每个后端应获得约 1/3 的流量
	for _, b := range backends {
		addr := b.Address()
		assert.InDelta(t, n/3, counts[addr], 10, "backend %s should get ~1/3 traffic", addr)
	}
}

func TestARB_BiasOneIsLeastConn(t *testing.T) {
	s := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})

	backend1 := NewBackend("server1")
	backend2 := NewBackend("server2")
	backends := []Backend{backend1, backend2}

	// 模拟 backend1 有 5 个连接
	releaser := s.(LeastConnReleaser)
	for i := 0; i < 5; i++ {
		selected := s.Select(backends)
		if selected == backend1 {
			// 不释放，保持连接数
		} else {
			releaser.Release(selected)
		}
	}

	// 现在 backend1 连接数多，应该选 backend2
	for i := 0; i < 10; i++ {
		result := s.Select(backends)
		assert.Equal(t, backend2, result, "should prefer backend with fewer connections")
		releaser.Release(result)
	}
}

func TestARB_HigherWeightGetsMoreTraffic(t *testing.T) {
	s := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})

	backends := []Backend{
		NewWeightedBackend("high", 10),
		NewWeightedBackend("low", 1),
	}

	// 高权重后端应获得更多流量
	counts := make(map[string]int)
	n := 1000
	for i := 0; i < n; i++ {
		result := s.Select(backends)
		counts[result.Address()]++
	}

	// 高权重应有更多连接
	assert.Greater(t, counts["high"], counts["low"], "high weight backend should get more traffic")
}

func TestARB_LessConnectionsGetsMoreTraffic(t *testing.T) {
	s := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})

	backend1 := NewWeightedBackend("server1", 1)
	backend2 := NewWeightedBackend("server2", 1)
	backends := []Backend{backend1, backend2}

	releaser := s.(LeastConnReleaser)

	// 给 backend1 增加 20 个未释放的连接（确保差距足够大）
	for i := 0; i < 20; i++ {
		selected := s.Select(backends)
		if selected == backend2 {
			releaser.Release(selected) // 只释放 backend2 的选择
		}
		// backend1 选中时不释放，累积连接数
	}

	// 现在 backend1 连接数远多于 backend2
	// 后续选择中 backend2 应该获得更多流量
	counts := make(map[string]int)
	for i := 0; i < 100; i++ {
		result := s.Select(backends)
		counts[result.Address()]++
		releaser.Release(result)
	}

	// backend2 应该获得更多流量（因为 backend1 连接数初始更多）
	assert.Greater(t, counts["server2"], counts["server1"],
		"backend with fewer connections should get more traffic")
}

func TestARB_FairTieBreaking(t *testing.T) {
	s := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})

	backends := []Backend{
		NewBackend("server1"),
		NewBackend("server2"),
		NewBackend("server3"),
	}

	// 等权重 + 无连接，应该轮转公平
	counts := make(map[string]int)
	n := 300
	releaser := s.(LeastConnReleaser)
	for i := 0; i < n; i++ {
		result := s.Select(backends)
		counts[result.Address()]++
		releaser.Release(result)
	}

	// 均匀分配
	for _, b := range backends {
		addr := b.Address()
		assert.InDelta(t, n/3, counts[addr], 10, "backend %s should get ~1/3 traffic", addr)
	}
}

func TestARB_Release(t *testing.T) {
	s := NewActiveRequestBias()

	backend1 := NewBackend("server1")
	backend2 := NewBackend("server2")
	backends := []Backend{backend1, backend2}

	releaser, ok := s.(LeastConnReleaser)
	require.True(t, ok, "should implement LeastConnReleaser")

	// 选择 backend1 三次，不释放
	for i := 0; i < 3; i++ {
		result := s.Select(backends)
		if result == backend1 {
			continue // 不释放
		}
		releaser.Release(result)
	}

	// 现在 backend1 有连接，应该选 backend2
	for i := 0; i < 5; i++ {
		result := s.Select(backends)
		assert.Equal(t, backend2, result)
		releaser.Release(result)
	}

	// 释放 backend1 的所有连接
	for i := 0; i < 3; i++ {
		releaser.Release(backend1)
	}

	// 现在两个后端连接数相同，应该公平轮转
	counts := make(map[string]int)
	for i := 0; i < 100; i++ {
		result := s.Select(backends)
		counts[result.Address()]++
		releaser.Release(result)
	}

	assert.InDelta(t, 50, counts["server1"], 10)
	assert.InDelta(t, 50, counts["server2"], 10)
}

func TestARB_ReleaseNil(t *testing.T) {
	s := NewActiveRequestBias()
	releaser, ok := s.(LeastConnReleaser)
	require.True(t, ok)

	// Release(nil) 不应该 panic
	assert.NotPanics(t, func() {
		releaser.Release(nil)
	})
}

func TestARB_Concurrent(t *testing.T) {
	s := NewActiveRequestBias()
	releaser := s.(LeastConnReleaser)

	backends := []Backend{
		NewBackend("server1"),
		NewBackend("server2"),
		NewBackend("server3"),
	}

	const workers = 10
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				result := s.Select(backends)
				releaser.Release(result)
			}
		}()
	}

	wg.Wait()
}
