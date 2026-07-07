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
