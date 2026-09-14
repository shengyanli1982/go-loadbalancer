package lb

import (
	"fmt"
	"math"
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

	// 权重 3 的后端应该获得约 75% 的流量。
	// 二项模型下 counts["a"] ~ Binomial(n=10000, p=0.25)，σ = sqrt(n·p·(1-p)) ≈ 43.3。
	// 原 delta=1500 合 34.6σ，10%~40% 占比都能通过，属近乎空断言（项目先例：
	// random_test.go 对同模式 delta=1500 的收紧）；实测偏差仅约 25（≈0.6σ）。
	// 收紧到 150 ≈ 3.5σ，且为实测偏差的约 6 倍：key 序列固定（"key-0".."key-9999"）
	// 且 xxhash 确定性 ⇒ 计数完全可复现，不存在 flaky 风险，
	// 但足以抓到分布退化（权重评分错误、哈希偏斜、指纹缓存陈旧）。
	assert.InDelta(t, 2500, counts["a"], 150, "'a' (25%%) count")
	assert.InDelta(t, 7500, counts["b"], 150, "'b' (75%%) count")
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

	// 空 key 不应 panic；契约为确定性返回 backends[0]（与其余四个哈希算法一致，
	// 见 hash_empty_key_test.go 的横向钉死）。原 NotNil 断言过弱：
	// 返回任意后端都能通过，无法区分「契约行为」与「恰好走了哈希路径」。
	result := s.SelectByHash(backends, nil)
	require.NotNil(t, result)
	assert.Equal(t, "a", result.Address(), "nil key 应返回 backends[0]")

	result2 := s.SelectByHash(backends, []byte{})
	require.NotNil(t, result2)
	assert.Equal(t, "a", result2.Address(), "[]byte{} key 应返回 backends[0]")
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

// TestRendezvous_NegLog2TableMatchesMathLog2 钉住预计算表的建表逻辑：
// negLog2Table[i] 必须逐条目位级等于 -math.Log2(1 + i/256)。
// 该表由 init() 迁移为包级 var 初始化表达式后行为必须完全等价，
// 任何建表公式漂移（区间端点、条目数、符号）都会被本测试拦截。
func TestRendezvous_NegLog2TableMatchesMathLog2(t *testing.T) {
	want := [rendezvousLogTableSize + 1]float64{}
	for i := 0; i <= rendezvousLogTableSize; i++ {
		want[i] = -math.Log2(1.0 + float64(i)/rendezvousLogTableSize)
	}
	assert.Equal(t, want, negLog2Table,
		"negLog2Table 应逐条目位级等于 -log2(1+i/256)")
	assert.Equal(t, want, buildNegLog2Table(),
		"buildNegLog2Table() 重算结果应与已发布的表位级一致")
}

// TestRendezvous_FastNegLog2Precision 对照 math.Log2 验证 fastNegLog2 的近似精度上界。
//
// 界的推导（非拍脑袋数字）：误差仅来自 [1,2) 上 -log2(m) 的 256 段线性插值，
// 理论上界 = (1/8)·h²·max|f″(m)| = (1/8)·(1/256)²·(1/ln2) ≈ 2.752e-6；
// 结构化扫描实测最大绝对误差 2.741e-6（落于首个插值区间中点附近，与理论一致），
// -log2(x) >= 1 域内实测最大相对误差 1.372e-6（最劣点 x≈0.2505，误差/want≈2.74e-6/2）。
// 断言界取绝对 5e-6（≈1.8×实测）、相对 3e-6（≈2.2×实测）：
// 跨平台浮点余量充足（正常波动在 ulp 量级 ~1e-16），同时能拦截表条目减半
// （误差×4 ≈ 1.1e-5 越界）与插值退化为最近邻（误差 ~1e-3 越界）等量级回归。
//
// 相对误差仅在 -log2(x) >= 1（x <= 0.5）域内断言：x→1 时真值→0，
// 相对误差指标天然发散（绝对误差仍 ≤ 2.75e-6），不属于近似质量问题。
func TestRendezvous_FastNegLog2Precision(t *testing.T) {
	const (
		maxAbsErr = 5e-6 // 绝对误差上界，推导见函数注释
		maxRelErr = 3e-6 // 相对误差上界（-log2(x) >= 1 域），推导见函数注释
	)

	// errorsOf 返回单个采样点的绝对/相对误差；相对误差仅在 want >= 1 时有意义
	errorsOf := func(x float64) (absErr, relErr float64, relValid bool) {
		got := fastNegLog2(x)
		want := -math.Log2(x)
		absErr = math.Abs(got - want)
		if want >= 1 {
			return absErr, absErr / want, true
		}
		return absErr, 0, false
	}

	t.Run("absolute_error_structured_sweep", func(t *testing.T) {
		// 结构化扫描：指数 e ∈ [0,64] × 插值段内 5 个代表位置（含段中点=线性插值最劣点）
		var worstAbs float64
		var worstX float64
		for e := 0; e <= 64; e++ {
			for i := 0; i < rendezvousLogTableSize; i++ {
				for _, off := range []float64{0, 0.25, 0.5, 0.75, 0.999999} {
					x := math.Ldexp(1.0+(float64(i)+off)/rendezvousLogTableSize, -e)
					if x <= 0 || x > 1 {
						continue // 定义域 (0,1]
					}
					if absErr, _, _ := errorsOf(x); absErr > worstAbs {
						worstAbs, worstX = absErr, x
					}
				}
			}
		}
		assert.LessOrEqual(t, worstAbs, maxAbsErr,
			"结构化扫描最大绝对误差 %.3e（x=%.17g）超出上界 %.1e", worstAbs, worstX, maxAbsErr)
	})

	t.Run("absolute_error_production_shaped_sampling", func(t *testing.T) {
		// 生产形态采样：x = (v+1)/2^64 与 lookup 的调用形态一致；
		// splitmix64 确定性序列，不依赖 math/rand 全局状态，跨运行可复现
		var worstAbs float64
		s := uint64(0x9E3779B97F4A7C15)
		for k := 0; k < 1<<20; k++ {
			s += 0x9E3779B97F4A7C15
			z := s
			z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
			z = (z ^ (z >> 27)) * 0x94D049BB133111EB
			z ^= z >> 31
			x := (float64(z) + 1.0) / maxUint64Plus1
			if absErr, _, _ := errorsOf(x); absErr > worstAbs {
				worstAbs = absErr
			}
		}
		assert.LessOrEqual(t, worstAbs, maxAbsErr,
			"生产形态采样最大绝对误差 %.3e 超出上界 %.1e", worstAbs, maxAbsErr)
	})

	t.Run("relative_error_where_log_magnitude_at_least_one", func(t *testing.T) {
		// 相对误差域：x <= 0.5（即 -log2(x) >= 1），结构化扫描 + 生产形态采样合并断言
		var worstRel float64
		var worstX float64
		for e := 1; e <= 64; e++ {
			for i := 0; i < rendezvousLogTableSize; i++ {
				for _, off := range []float64{0, 0.25, 0.5, 0.75, 0.999999} {
					x := math.Ldexp(1.0+(float64(i)+off)/rendezvousLogTableSize, -e)
					if x <= 0 || x > 1 {
						continue
					}
					if _, relErr, ok := errorsOf(x); ok && relErr > worstRel {
						worstRel, worstX = relErr, x
					}
				}
			}
		}
		s := uint64(0x9E3779B97F4A7C15)
		for k := 0; k < 1<<20; k++ {
			s += 0x9E3779B97F4A7C15
			z := s
			z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
			z = (z ^ (z >> 27)) * 0x94D049BB133111EB
			z ^= z >> 31
			x := (float64(z) + 1.0) / maxUint64Plus1
			if _, relErr, ok := errorsOf(x); ok && relErr > worstRel {
				worstRel, worstX = relErr, x
			}
		}
		assert.LessOrEqual(t, worstRel, maxRelErr,
			"-log2(x)>=1 域最大相对误差 %.3e（x=%.17g）超出上界 %.1e", worstRel, worstX, maxRelErr)
	})

	t.Run("exact_at_powers_of_two", func(t *testing.T) {
		// x = 2^-k 时尾数恰为 1.0，查表零插值，结果应精确等于 k（生产域 k ∈ [1,64]）
		// k=0（x=1.0）由 x>=1.0 特殊分支处理，不再走查表路径，
		// 由 TestFastNegLog2_ReturnsPositiveForOneDotZero 独立验证
		for k := 1; k <= 64; k++ {
			x := math.Ldexp(1, -k)
			assert.Equal(t, float64(k), fastNegLog2(x),
				"fastNegLog2(2^-%d) 应精确等于 %d", k, k)
		}
	})

	t.Run("non_positive_x_returns_documented_max", func(t *testing.T) {
		// 文档化边界：x <= 0 返回最大有效值 64（-log2(2^-64) = 64）
		assert.Equal(t, float64(64), fastNegLog2(0), "x=0 应返回文档化的最大有效值 64")
		assert.Equal(t, float64(64), fastNegLog2(-1), "x<0 应返回文档化的最大有效值 64")
	})
}

// TestFastNegLog2_ReturnsPositiveForOneDotZero 验证 x=1.0 时返回正值而非 0。
//
// 根因：当 hashCombine 返回值接近 uint64 最大值时，float64(combined) 因 IEEE 754
// 精度向上舍入到 2^64，导致 x = (float64(combined)+1.0)/maxUint64Plus1 = 1.0。
// fastNegLog2(1.0) 数学上 = 0（正确），但 score = weight / (0 * Ln2) = +Inf，
// 在「取最大 score」域中 +Inf 是全域最优 → 确定性流量垄断。
func TestFastNegLog2_ReturnsPositiveForOneDotZero(t *testing.T) {
	result := fastNegLog2(1.0)

	assert.Greater(t, result, 0.0, "fastNegLog2(1.0) 应返回正值，而非 0")
	assert.False(t, math.IsInf(result, 1), "fastNegLog2(1.0) 不应返回 +Inf")

	// 验证 score 计算不会产生 +Inf 流量垄断
	// score = weight / (fastNegLog2(x) * math.Ln2)
	negLog := result * math.Ln2
	score := 10.0 / negLog // weight=10
	assert.False(t, math.IsInf(score, 1), "score 不应为 +Inf（流量垄断根因）")
	assert.Greater(t, score, 0.0, "score 应为有限正值")
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
