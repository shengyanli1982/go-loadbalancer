package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandom_Select(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Address())
}

func TestRandom_NilBackends(t *testing.T) {
	selector := NewRandom()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestRandom_EmptyBackends(t *testing.T) {
	selector := NewRandom()
	result := selector.Select([]Backend{})
	assert.Nil(t, result)
}

func TestRandom_SingleBackend(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a")

	for i := 0; i < 10; i++ {
		result := selector.Select(backends)
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}

func TestRandom_Distribution(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c")
	picks := 10000

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	for _, addr := range []string{"a", "b", "c"} {
		// Random 为真随机均匀抽样 ⇒ counts[addr] ~ Binomial(n=10000, p=1/3)。
		// σ = sqrt(n·p·(1-p)) = sqrt(10000 · 1/3 · 2/3) = 47.14。
		// 原 delta=1500 合 31.8σ，属近乎空断言（300 次独立运行实测 max|count-mean| 仅 162.3=3.44σ）。
		// 收紧到 300 = 6.36σ：单次误报概率 ~2e-10，×3 后端 ×50 轮 ≈ 3e-8，不构成 flaky，
		// 但足以抓到分布退化（如随机源失效、取模偏斜、后端集合被错误缓存）。
		// picks/3 的整数除法得 3333，与真实均值 3333.33 仅差 0.33（0.007σ），可忽略。
		assert.InDelta(t, picks/3, counts[addr], 300,
			"%s count should be ~3333", addr)
	}
}
