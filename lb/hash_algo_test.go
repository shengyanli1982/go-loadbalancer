package lb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPHash_SelectByHash(t *testing.T) {
	selector := NewIPHash()
	backends := newTestBackends("a", "b", "c")

	result := selector.SelectByHash(backends, []byte("192.168.1.1"))
	require.NotNil(t, result)
}

func TestIPHash_NilBackends(t *testing.T) {
	selector := NewIPHash()
	result := selector.SelectByHash(nil, []byte("192.168.1.1"))
	assert.Nil(t, result)
}

func TestIPHash_SameIPSameBackend(t *testing.T) {
	selector := NewIPHash()
	backends := newTestBackends("a", "b", "c")

	result1 := selector.SelectByHash(backends, []byte("192.168.1.1"))
	result2 := selector.SelectByHash(backends, []byte("192.168.1.1"))

	assert.Equal(t, result1.Address(), result2.Address())
}

func TestURIHash_SelectByHash(t *testing.T) {
	selector := NewURIHash(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.SelectByHash(backends, []byte("/api/users"))
	require.NotNil(t, result)
}

func TestURIHash_SameURISameBackend(t *testing.T) {
	selector := NewURIHash(nil)
	backends := newTestBackends("a", "b", "c")

	result1 := selector.SelectByHash(backends, []byte("/api/users"))
	result2 := selector.SelectByHash(backends, []byte("/api/users"))

	assert.Equal(t, result1.Address(), result2.Address())
}

func TestRingHash_Select(t *testing.T) {
	selector := NewRingHash(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	require.NotNil(t, result)
}

func TestMaglev_Select(t *testing.T) {
	selector := NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	require.NotNil(t, result)
}

func TestRingHash_SameKeySameBackend(t *testing.T) {
	selector := NewRingHash(nil)
	backends := newTestBackends("a", "b", "c", "d", "e")
	key := []byte("test-key-12345")

	result1 := selector.SelectByHash(backends, key)
	result2 := selector.SelectByHash(backends, key)
	result3 := selector.SelectByHash(backends, key)

	require.NotNil(t, result1)
	require.NotNil(t, result2)
	require.NotNil(t, result3)
	assert.Equal(t, result1.Address(), result2.Address())
	assert.Equal(t, result2.Address(), result3.Address())
}

func TestRingHash_DifferentKeysDistribution(t *testing.T) {
	selector := NewRingHash(nil)
	backends := newTestBackends("a", "b", "c")

	picks := 10000
	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		result := selector.SelectByHash(backends, key)
		if result != nil {
			counts[result.Address()]++
		}
	}

	for _, addr := range []string{"a", "b", "c"} {
		assert.True(t, counts[addr] > 0, "backend %s should be selected at least once", addr)
		assert.InDelta(t, picks/3, counts[addr], 2000,
			"%s distribution unusual: %d", addr, counts[addr])
	}
}

func TestRingHash_MinimalRemapping(t *testing.T) {
	selector := NewRingHash(nil)

	backends3 := newTestBackends("a", "b", "c")
	backends4 := newTestBackends("a", "b", "c", "d")

	keys := make([][]byte, 1000)
	for i := 0; i < 1000; i++ {
		keys[i] = []byte(fmt.Sprintf("key-%d", i))
	}

	mappingBefore := make(map[string]string)
	for _, key := range keys {
		result := selector.SelectByHash(backends3, key)
		if result != nil {
			mappingBefore[string(key)] = result.Address()
		}
	}

	unchanged := 0
	for _, key := range keys {
		result := selector.SelectByHash(backends4, key)
		if result != nil && mappingBefore[string(key)] == result.Address() {
			unchanged++
		}
	}

	remapRatio := float64(unchanged) / float64(1000)
	assert.Greater(t, remapRatio, 0.5,
		"expected >50%% keys unchanged when adding backend, got %.2f%%", remapRatio*100)
}

func TestRingHash_SelectByHashConsistency(t *testing.T) {
	selector := NewRingHash(&RingHashOptions{RingSize: 1024})
	backends := newTestBackends("server1", "server2", "server3")

	for trial := 0; trial < 100; trial++ {
		key := []byte(fmt.Sprintf("consistent-key-%d", trial))
		result1 := selector.SelectByHash(backends, key)
		result2 := selector.SelectByHash(backends, key)

		require.NotNil(t, result1)
		require.NotNil(t, result2)
		assert.Equal(t, result1.Address(), result2.Address(),
			"inconsistent result for same key")
	}
}

func TestMaglev_SelectByHash(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.SelectByHash(backends, []byte("test-key"))
	require.NotNil(t, result)
	assert.Contains(t, []string{"a", "b", "c"}, result.Address())
}

func TestMaglev_SameKeySameBackend(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")
	key := []byte("maglev-key-12345")

	result1 := selector.SelectByHash(backends, key)
	result2 := selector.SelectByHash(backends, key)
	result3 := selector.SelectByHash(backends, key)

	require.NotNil(t, result1)
	require.NotNil(t, result2)
	require.NotNil(t, result3)
	assert.Equal(t, result1.Address(), result2.Address())
	assert.Equal(t, result2.Address(), result3.Address())
}

func TestMaglev_UniformDistribution(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")

	picks := 10000
	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		key := []byte(fmt.Sprintf("maglev-key-%d", i))
		result := selector.SelectByHash(backends, key)
		if result != nil {
			counts[result.Address()]++
		}
	}

	for _, addr := range []string{"a", "b", "c"} {
		assert.True(t, counts[addr] > 0, "backend %s should be selected", addr)
		assert.InDelta(t, picks/3, counts[addr], 1800,
			"%s distribution unusual: %d", addr, counts[addr])
	}
}

func TestWeightedFingerprint_LargeWeights(t *testing.T) {
	// 两个后端，权重顺序不同但值相同 → 指纹应不同（顺序不同）
	be1 := []Backend{
		NewWeightedBackend("a", 100),
		NewWeightedBackend("b", 100),
	}
	be2 := []Backend{
		NewWeightedBackend("b", 100),
		NewWeightedBackend("a", 100),
	}
	fp1 := computeWeightedFingerprint(be1)
	fp2 := computeWeightedFingerprint(be2)
	assert.NotEqual(t, fp1, fp2, "fingerprint should differ when backend order differs")

	// 测试 >65535 权重的截断 bug：交换权重后指纹应不同
	be3 := []Backend{
		NewWeightedBackend("a", 10),
		NewWeightedBackend("b", 65537),
	}
	be4 := []Backend{
		NewWeightedBackend("a", 65537),
		NewWeightedBackend("b", 10),
	}
	fp3 := computeWeightedFingerprint(be3)
	fp4 := computeWeightedFingerprint(be4)
	assert.NotEqual(t, fp3, fp4, "BUG: fingerprint should differ for swapped large weights (65537 vs 10)")
}

func TestMaglev_DifferentKeysSelectDifferentBackends(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c", "d", "e")

	selected := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("maglev-key-%d", i))
		result := selector.SelectByHash(backends, key)
		if result != nil {
			selected[result.Address()] = true
		}
	}

	assert.GreaterOrEqual(t, len(selected), 3,
		"expected at least 3 different backends selected, got %d: %v", len(selected), selected)
}

// TestFingerprint_Injectivity 指纹单射性回归测试（固化审计 #P1-4 的 length-prefix 修复）
//
// 背景：基于 "|" 分隔符的编码会让不同的后端列表产生相同指纹，
// 例如 fp(["a|b","c"]) == fp(["a","b|c"])，导致所有指纹系选择器把
// 含 "|" 的地址列表迁移误判为"未变化"而跳过内部数据结构重建。
//
// 历史教训（禁止再次为性能移除 length-prefix）：
//   - 012e12a 用 length-prefix 修复该碰撞
//   - 3b3c726 回归到 "|" 分隔符，碰撞重现
//   - e83fe0c 仅修复加权路径，本测试固化双路径的完整修复
func TestFingerprint_Injectivity(t *testing.T) {
	t.Run("BackendsFingerprint_NoCollision", func(t *testing.T) {
		cases := []struct {
			name string
			a    []Backend
			b    []Backend
		}{
			{"separator pipe collision", newTestBackends("a|b", "c"), newTestBackends("a", "b|c")},
			{"adjacent boundary shift", newTestBackends("ab", "c"), newTestBackends("a", "bc")},
			{"concat to single element", newTestBackends("a", "b"), newTestBackends("ab")},
			{"empty list vs single empty address", newTestBackends(), newTestBackends("")},
			{"order sensitive", newTestBackends("a", "b"), newTestBackends("b", "a")},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				assert.NotEqual(t, computeBackendsFingerprint(tc.a), computeBackendsFingerprint(tc.b),
					"fingerprint collision between different backend lists")
			})
		}
	})

	t.Run("WeightedFingerprint_NoCollision", func(t *testing.T) {
		// 地址边界歧义：["a|b","c"] 与 ["a","b|c"]（权重均为 1）
		sepA := []Backend{NewWeightedBackend("a|b", 1), NewWeightedBackend("c", 1)}
		sepB := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b|c", 1)}
		assert.NotEqual(t, computeWeightedFingerprint(sepA), computeWeightedFingerprint(sepB),
			"fingerprint collision between different weighted backend lists")

		// 地址相同、权重分布不同 → 指纹必须不同
		wA := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 2)}
		wB := []Backend{NewWeightedBackend("a", 2), NewWeightedBackend("b", 1)}
		assert.NotEqual(t, computeWeightedFingerprint(wA), computeWeightedFingerprint(wB))

		// 单一地址、不同权重 → 指纹必须不同
		sA := []Backend{NewWeightedBackend("a", 1)}
		sB := []Backend{NewWeightedBackend("a", 2)}
		assert.NotEqual(t, computeWeightedFingerprint(sA), computeWeightedFingerprint(sB))
	})

	t.Run("WeightedFingerprint_WeightSemanticsPreserved", func(t *testing.T) {
		// 保留现有语义：非 WeightedBackend 或权重 <= 0 一律记为 1
		fp := computeWeightedFingerprint([]Backend{NewWeightedBackend("a", 1)})
		assert.Equal(t, fp, computeWeightedFingerprint([]Backend{NewBackend("a")}),
			"non-weighted backend should be treated as weight 1")
		assert.Equal(t, fp, computeWeightedFingerprint([]Backend{NewWeightedBackend("a", 0)}),
			"zero weight should be treated as weight 1")
		assert.Equal(t, fp, computeWeightedFingerprint([]Backend{NewWeightedBackend("a", -5)}),
			"negative weight should be treated as weight 1")
	})

	t.Run("Stability", func(t *testing.T) {
		// 相同输入多次计算结果稳定
		bs := newTestBackends("a", "b|c", "d")
		assert.Equal(t, computeBackendsFingerprint(bs), computeBackendsFingerprint(bs))

		ws := []Backend{NewWeightedBackend("a|b", 3), NewWeightedBackend("c", 1)}
		assert.Equal(t, computeWeightedFingerprint(ws), computeWeightedFingerprint(ws))
	})
}
