package lb

import (
	"fmt"
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

// TestARB_UniformWeightsEvenDistribution 验证等权重且全程不释放连接时分配仍均匀：
// 计数同步累积使所有后端持续平局，平局由 rrIndex 轮转打破，故每轮 n 次恰好各中一次。
// （bias 取值区间为 (0,1]，Bias: 0 回落默认值的等价性由 TestARB_ExplicitZeroBiasDefaultsToBias1 精确覆盖）
func TestARB_UniformWeightsEvenDistribution(t *testing.T) {
	s := NewActiveRequestBias()

	backends := []Backend{
		NewWeightedBackend("server1", 1),
		NewWeightedBackend("server2", 1),
		NewWeightedBackend("server3", 1),
	}

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

// arbSelectSequence 在相同输入序列下运行 steps 次 Select，返回被选后端的地址序列。
func arbSelectSequence(s Selector, backends []Backend, steps int) []string {
	seq := make([]string, 0, steps)
	for i := 0; i < steps; i++ {
		seq = append(seq, s.Select(backends).Address())
	}
	return seq
}

// arbHeapPathBackends 构造 n >= TreeThresholdARB 的加权后端集合，用于覆盖索引堆路径。
func arbHeapPathBackends() []Backend {
	backends := make([]Backend, 40)
	for i := range backends {
		backends[i] = NewWeightedBackend(fmt.Sprintf("backend-%d:8080", i), (i%10)+1)
	}
	return backends
}

// assertBiasDefaultsToBias1 断言每个候选构造器产出的选择器与 bias=1.0 参照实现在
// 相同输入序列下产出完全相同的选择序列。ARB 无随机源、完全确定性，故「序列等价」
// 即「配置等价」的精确判据（非统计区间）。参照实现取 NewActiveRequestBias()。
//
// 候选以构造器形式传入，使每个 subtest 都从 rrIndex=0、conn=0 的干净状态起算，
// 避免选择器内部状态跨 subtest 串扰。
//
// 两个 subtest 分别覆盖线性扫描路径（n < TreeThresholdARB）与索引堆路径（n >= 阈值）：
// 被删除的 bias==0 快速路径其 return 位于堆路径判断之前，曾同时短路这两条路径。
func assertBiasDefaultsToBias1(t *testing.T, newCandidates ...func() Selector) {
	t.Helper()

	t.Run("linear_path_n2_weighted", func(t *testing.T) {
		backends := []Backend{
			NewWeightedBackend("low", 1),
			NewWeightedBackend("high", 10),
		}
		want := arbSelectSequence(NewActiveRequestBias(), backends, 30)
		for _, newCandidate := range newCandidates {
			assert.Equal(t, want, arbSelectSequence(newCandidate(), backends, 30),
				"必须等价于 bias=1.0：相同输入下选择序列不一致")
		}
	})

	t.Run("heap_path_n40_weighted", func(t *testing.T) {
		backends := arbHeapPathBackends()
		want := arbSelectSequence(NewActiveRequestBias(), backends, 200)
		for _, newCandidate := range newCandidates {
			assert.Equal(t, want, arbSelectSequence(newCandidate(), backends, 200),
				"必须等价于 bias=1.0：相同输入下选择序列不一致")
		}
	})
}

// TestARB_OptionsZeroValueDefaultsToBias1 验证零值 == 默认：Go 零值 &ARBOptions{} 必须
// 得到 bias=1.0，与 ARBOptions.Bias 字段 godoc「默认 1.0」一致，并与同包 P2COptions
// 范式统一（lb/p2c.go 用 >0 && <1 守卫，零值即默认值）。
// 连同 &ARBOptions{Bias: 1.0} 一并断言，即「零值 / NewActiveRequestBias() / 显式 1.0」
// 三者在相同输入序列下产出完全相同的选择序列。
func TestARB_OptionsZeroValueDefaultsToBias1(t *testing.T) {
	assertBiasDefaultsToBias1(t,
		func() Selector { return NewActiveRequestBiasWithOptions(&ARBOptions{}) },
		func() Selector { return NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0}) },
	)
}

// TestARB_OptionsNegativeBiasDefaultsToBias1 验证负 bias 被守卫拒绝并回落到默认 1.0。
// 负 bias 无负载均衡意义（score 随连接数上升而升高，会持续加压到最忙的后端）。
func TestARB_OptionsNegativeBiasDefaultsToBias1(t *testing.T) {
	assertBiasDefaultsToBias1(t,
		func() Selector { return NewActiveRequestBiasWithOptions(&ARBOptions{Bias: -1}) },
	)
}

// TestARB_ExplicitZeroBiasDefaultsToBias1 验证显式传 Bias: 0 也回落到 1.0。
//
// 这是有意的 API 决策，不是疏漏：
//   - bias=0 时公式 score = weight/(conns+1)^0 = weight 恒选最大权重后端，
//     与库内散文注释「退化为纯 WRR（按比例分配）」自相矛盾，两者不可能同时成立；
//   - 「按权重比例分配」的需求已由 NewWeightedRR（前缀和 + 二分的正经 WRR）覆盖，
//     无需在 ARB 内重复实现第二套 WRR。
//
// 故 bias 取值区间收敛为 (0, 1]，0 与省略一律等价于默认 1.0。
func TestARB_ExplicitZeroBiasDefaultsToBias1(t *testing.T) {
	assertBiasDefaultsToBias1(t,
		func() Selector { return NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 0}) },
	)
}

// TestARB_WeightedDistributionRespectsWeight 验证 Bias: 0 这一旧实现中触发
// 「无权重纯位置轮询」快速路径的配置，修复后按 score = weight/(conns+1) 偏向高权重后端。
//
// 确定性推导（权重 low=1 / high=10，n=2 < TreeThresholdARB 走线性路径，全程不释放连接，
// 每次 Select 后 rrIndex++，故第 m 次 Select 前 rrIndex = m-1）：
//
//	m=1..10  high —— 10/(c_high+1) > 1/1；m=10 时 10/10 == 1/1 平局，rrIndex=9 → 9%2=1 仍取 high
//	m=11     low  —— 1/1 > 10/11
//	m=12..20 high —— 1/2 = 0.5 < 10/(c_high+1)
//	m=21     low  —— 10/20 == 1/2 平局，rrIndex=20 → 20%2=0 取 low
//	m=22..30 high —— 1/3 ≈ 0.333 < 10/(c_high+1)
//
// 合计 high=28、low=2。旧实现为 15/15（纯位置轮询），本断言必然失败。
func TestARB_WeightedDistributionRespectsWeight(t *testing.T) {
	s := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 0})
	backends := []Backend{
		NewWeightedBackend("low", 1),
		NewWeightedBackend("high", 10),
	}

	const steps = 30
	counts := make(map[string]int, len(backends))
	for i := 0; i < steps; i++ {
		counts[s.Select(backends).Address()]++
	}

	assert.Greater(t, counts["high"], counts["low"],
		"高权重后端被选次数必须严格多于低权重后端")
	assert.Equal(t, 28, counts["high"],
		"高权重后端被选次数应为确定性推导值 28")
	assert.Equal(t, 2, counts["low"],
		"低权重后端被选次数应为确定性推导值 2")
}

func TestARB_BiasOneIsLeastConn(t *testing.T) {
	s := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})

	backend1 := NewBackend("server1")
	backend2 := NewBackend("server2")
	backends := []Backend{backend1, backend2}

	// 模拟 backend1 有 5 个连接
	releaser := s.(RequestReleaser)
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

	releaser := s.(RequestReleaser)

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
	releaser := s.(RequestReleaser)
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

	releaser, ok := s.(RequestReleaser)
	require.True(t, ok, "should implement RequestReleaser")

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
	releaser, ok := s.(RequestReleaser)
	require.True(t, ok)

	// Release(nil) 不应该 panic
	assert.NotPanics(t, func() {
		releaser.Release(nil)
	})
}

func TestARB_Concurrent(t *testing.T) {
	s := NewActiveRequestBias()
	releaser := s.(RequestReleaser)

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
