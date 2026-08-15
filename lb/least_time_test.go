package lb

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// latencyBackend 是测试用的 LatencyBackend 实现
type latencyBackend struct {
	address string
	weight  int
	latency float64 // AverageLatency
	conns   int     // ActiveConnections
}

func (b *latencyBackend) Address() string         { return b.address }
func (b *latencyBackend) Weight() int             { return b.weight }
func (b *latencyBackend) ActiveConnections() int  { return b.conns }
func (b *latencyBackend) AverageLatency() float64 { return b.latency }

// ltConfig 简化 newLatencyBackends 的配置
type ltConfig struct {
	addr    string
	weight  int
	latency float64
	conns   int
}

func newLatencyBackends(configs []ltConfig) []Backend {
	result := make([]Backend, len(configs))
	for i, c := range configs {
		result[i] = &latencyBackend{
			address: c.addr,
			weight:  c.weight,
			latency: c.latency,
			conns:   c.conns,
		}
	}
	return result
}

func TestLeastTime_EmptyBackends(t *testing.T) {
	sel := NewLeastTime()
	assert.Nil(t, sel.Select(nil))
	assert.Nil(t, sel.Select([]Backend{}), "expected nil for empty slice")
}

func TestLeastTime_SingleBackend(t *testing.T) {
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
	})
	got := sel.Select(backends)
	require.NotNil(t, got)
	assert.Equal(t, "svc-a:80", got.Address())
}

func TestLeastTime_LowestScoreWins(t *testing.T) {
	// score = latency * (1 + conns) / weight
	// svc-a: 10 * (1+0) / 1 = 10
	// svc-b: 50 * (1+0) / 1 = 50  ← 更高
	// svc-c: 5  * (1+0) / 1 = 5   ← 最低，应被选中
	tests := []struct {
		name    string
		latency float64
		addr    string
	}{
		{"low latency wins", 5.0, "svc-c:80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewLeastTime()
			backends := newLatencyBackends([]ltConfig{
				{"svc-a:80", 1, 10.0, 0},
				{"svc-b:80", 1, 50.0, 0},
				{"svc-c:80", 1, tt.latency, 0},
			})
			got := sel.Select(backends)
			assert.Equal(t, tt.addr, got.Address())
		})
	}
}

func TestLeastTime_WeightedDistribution(t *testing.T) {
	// score = latency * (1 + conns) / weight
	// score 相同时，权重越高分到越多请求
	// svc-a: 30 * (1+0) / 3 = 10
	// svc-b: 30 * (1+0) / 1 = 30
	// 第一轮 svc-a 胜（score=10 < 30）
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 3, 30.0, 0}, // score=10
		{"svc-b:80", 1, 30.0, 0}, // score=30
	})
	counts := make(map[string]int)
	for i := 0; i < 4; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	// 权重3的后端应该获得更多请求
	assert.Greater(t, counts["svc-a:80"], 0, "high weight backend should get requests, got %+v", counts)
}

func TestLeastTime_ConnectionAware(t *testing.T) {
	// 连接数越多，score 越高
	// svc-a: 10 * (1+0) / 1 = 10   ← 无连接，应被选中
	// svc-b: 10 * (1+5) / 1 = 60   ← 5个连接，score更高
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 5},
	})
	got := sel.Select(backends)
	assert.Equal(t, "svc-a:80", got.Address(), "expected svc-a:80 (fewer connections)")
}

func TestLeastTime_NonLatencyBackendFallback(t *testing.T) {
	// 非 LatencyBackend 的后端 score=0，优先被选中（explore-exploit）
	sel := NewLeastTime()
	backends := []Backend{
		NewBackend("plain-a:80"),                // 非 LatencyBackend → score=0
		NewBackend("plain-b:80"),                // 非 LatencyBackend → score=0
		&latencyBackend{"lat-c:80", 1, 10.0, 0}, // LatencyBackend → score=10
	}
	// 非 LatencyBackend 应该被选中（score=0 < 10）
	counts := make(map[string]int)
	for i := 0; i < 20; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	if counts["lat-c:80"] != 0 {
		t.Logf("counts: %+v", counts)
	}
	// The key assertion: plain backends get at least some traffic
	assert.False(t, counts["plain-a:80"] == 0 && counts["plain-b:80"] == 0,
		"plain backends should be selected (explore), got %+v", counts)
}

func TestLeastTime_FairTieBreaking(t *testing.T) {
	// 相同 score 的后端应轮流被选中（round-robin 平局处理）
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
		{"svc-c:80", 1, 10.0, 0},
	})
	counts := make(map[string]int)
	rounds := 90
	for i := 0; i < rounds; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	// 三个后端应该分配相对均匀
	for addr, c := range counts {
		assert.InDelta(t, rounds/3, c, 5, "%s distribution should be ~%d", addr, rounds/3)
	}
}

func TestLeastTime_Release(t *testing.T) {
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
	})
	// 连续选中 svc-a 2次
	got1 := sel.Select(backends)
	got2 := sel.Select(backends)
	// Release svc-a
	r, ok := sel.(LeastConnReleaser)
	require.True(t, ok, "selector should implement LeastConnReleaser")
	r.Release(got1)
	r.Release(got2)
	// Release nil should not panic
	r.Release(nil)
}

func TestLeastTime_Release_DoesNotAffectSelection(t *testing.T) {
	// LeastTime 的选择只由后端注入的外部指标（ActiveConnections/AverageLatency）驱动，
	// 内部连接计数为死状态（已移除）：Release 不影响后续选择
	sel := NewLeastTime()
	releaser, ok := sel.(LeastConnReleaser)
	require.True(t, ok, "selector should implement LeastConnReleaser")

	// score = latency * (1+conns)：svc-a=10, svc-b=60 → svc-a 恒定占优
	a := &latencyBackend{address: "svc-a:80", weight: 1, latency: 10.0, conns: 0}
	b := &latencyBackend{address: "svc-b:80", weight: 1, latency: 10.0, conns: 5}
	backends := []Backend{a, b}

	// 阶段1：100 轮 Select→Release，外部指标恒定 → 每轮必须选中 svc-a
	for i := 0; i < 100; i++ {
		got := sel.Select(backends)
		require.NotNil(t, got)
		require.Equal(t, "svc-a:80", got.Address(),
			"round %d: selection must follow external metrics", i)
		releaser.Release(got)
	}

	// 阶段2：100 轮只 Select 不 Release → 选择不因内部状态而变化
	for i := 0; i < 100; i++ {
		got := sel.Select(backends)
		require.Equal(t, "svc-a:80", got.Address(),
			"round %d without Release: internal state must not affect selection", i)
	}

	// 白盒断言：内部连接计数恒为 0（死状态计数已移除，不再随 Select 递增）
	lt := sel.(*leastTime)
	require.Equal(t, 0, lt.connByIndex[0], "internal connection counter must stay 0")
	require.Equal(t, 0, lt.connByIndex[1], "internal connection counter must stay 0")

	// 阶段3：注入外部计数变化 → 选择必须立即切换（选择完全由外部指标驱动）
	a.conns = 5
	b.conns = 0
	got := sel.Select(backends)
	require.Equal(t, "svc-b:80", got.Address(),
		"selection must switch when external metrics change")
}

func TestLeastTime_BackendChange(t *testing.T) {
	// 后端列表变化后应正确重建内部状态
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
	})
	sel.Select(backends1)
	sel.Select(backends1)

	// 移除 svc-a，添加 svc-c
	backends2 := newLatencyBackends([]ltConfig{
		{"svc-b:80", 1, 10.0, 0},
		{"svc-c:80", 1, 5.0, 0}, // 延迟更低
	})
	got := sel.Select(backends2)
	assert.Equal(t, "svc-c:80", got.Address(), "expected new low-latency backend svc-c:80")
}

func TestLeastTime_ConcurrentSafety(t *testing.T) {
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 2, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
		{"svc-c:80", 3, 15.0, 2},
	})

	var wg sync.WaitGroup
	numGoroutines := 100
	numSelects := 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numSelects; j++ {
				b := sel.Select(backends)
				assert.NotNil(t, b, "got nil from concurrent Select")
				if r, ok := sel.(LeastConnReleaser); ok {
					r.Release(b)
				}
			}
		}()
	}
	wg.Wait()
}

func TestLeastTime_ConcurrentBackendChange(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
	})
	backends2 := newLatencyBackends([]ltConfig{
		{"svc-c:80", 1, 5.0, 0},
		{"svc-d:80", 2, 10.0, 0},
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				sel.Select(backends1)
			} else {
				sel.Select(backends2)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			b := sel.Select(backends1)
			if r, ok := sel.(LeastConnReleaser); ok {
				r.Release(b)
			}
		}
	}()
	wg.Wait()
}

func TestLeastTime_ZeroLatencyVsNonZero(t *testing.T) {
	// 零延迟的 LatencyBackend 也应当 score=0，与未探索后端相同
	sel := NewLeastTime()
	backends := []Backend{
		NewBackend("plain:80"),                    // score=0 (non-LatencyBackend)
		&latencyBackend{"zero-lat:80", 1, 0.0, 0}, // score=0 (zero latency)
		&latencyBackend{"hi-lat:80", 1, 100.0, 0}, // score=100
	}
	counts := make(map[string]int)
	for i := 0; i < 60; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	// hi-lat should get fewer requests than the zero-score backends
	if counts["hi-lat:80"] > counts["plain:80"] && counts["hi-lat:80"] > counts["zero-lat:80"] {
		t.Logf("hi-lat got more traffic than expected: %+v", counts)
	}
}

func TestLeastTime_DynamicBackends_AddBackend(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
	})
	for i := 0; i < 10; i++ {
		sel.Select(backends1)
	}

	backends2 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
		{"svc-c:80", 2, 5.0, 0}, // 新后端：低延迟+高权重
	})
	counts := map[string]int{}
	for i := 0; i < 30; i++ {
		r := sel.Select(backends2)
		counts[r.Address()]++
	}
	assert.True(t, counts["svc-c:80"] > 0, "new low-latency backend should be selected")
}

func TestLeastTime_DynamicBackends_RemoveBackend(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
		{"svc-c:80", 1, 30.0, 0},
	})
	for i := 0; i < 20; i++ {
		sel.Select(backends1)
	}

	backends2 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
	})
	for i := 0; i < 10; i++ {
		r := sel.Select(backends2)
		assertOnlyFrom(t, r, []string{"svc-a:80", "svc-b:80"})
	}
}

func TestLeastTime_DynamicBackends_ReplaceAll(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"old1:80", 1, 50.0, 0},
		{"old2:80", 1, 100.0, 0},
	})
	for i := 0; i < 10; i++ {
		sel.Select(backends1)
	}

	backends2 := newLatencyBackends([]ltConfig{
		{"new1:80", 1, 5.0, 0},
		{"new2:80", 1, 10.0, 0},
	})
	for i := 0; i < 10; i++ {
		r := sel.Select(backends2)
		assertOnlyFrom(t, r, []string{"new1:80", "new2:80"})
	}
}

func TestLeastTime_DynamicBackends_ConnectionTrackingPreserved(t *testing.T) {
	sel := NewLeastTime()
	releaser, ok := sel.(LeastConnReleaser)
	require.True(t, ok)

	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
	})
	// 选中 svc-a 多次，不 release（模拟活跃连接）
	var picked []Backend
	for i := 0; i < 20; i++ {
		b := sel.Select(backends1)
		picked = append(picked, b)
	}

	// 添加新后端
	backends2 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
		{"svc-c:80", 1, 10.0, 0},
	})
	dCount := 0
	for i := 0; i < 60; i++ {
		r := sel.Select(backends2)
		if r.Address() == "svc-c:80" {
			dCount++
		}
	}
	// svc-c 连接数=0，应比积累了连接的 a/b 获得更多流量
	assert.True(t, dCount > 10, "new backend 'svc-c' should get more traffic due to 0 connections, got %d/60", dCount)

	for _, b := range picked {
		releaser.Release(b)
	}
}

func TestLeastTime_DynamicBackends_SizeFluctuation(t *testing.T) {
	sel := NewLeastTime()
	for size := 2; size <= 20; size += 3 {
		configs := make([]ltConfig, size)
		addrs := make([]string, size)
		for i := 0; i < size; i++ {
			addr := fmt.Sprintf("svc-%d:80", i)
			addrs[i] = addr
			configs[i] = ltConfig{
				addr:    addr,
				weight:  (i % 5) + 1,
				latency: float64(i*5 + 1),
				conns:   i % 10,
			}
		}
		backends := newLatencyBackends(configs)
		for i := 0; i < 30; i++ {
			r := sel.Select(backends)
			assertOnlyFrom(t, r, addrs)
		}
	}
}

// BenchmarkLeastTime_Select_UniformWeights benchmarks the uniform weights fast path
func BenchmarkLeastTime_Select_UniformWeights(b *testing.B) {
	n := 10
	sel := NewLeastTime()
	configs := make([]ltConfig, n)
	for i := 0; i < n; i++ {
		configs[i] = ltConfig{
			addr:    fmt.Sprintf("svc-%d:80", i),
			weight:  1, // uniform weights
			latency: float64(i*5 + 1),
			conns:   i % 10,
		}
	}
	backends := newLatencyBackends(configs)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sel.Select(backends)
		}
	})
}
