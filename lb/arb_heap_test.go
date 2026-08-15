package lb

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// arbScore 参照模型评分：score = weight / (conn+1)^bias
func arbScore(weight, conn int, bias float64) float64 {
	return float64(weight) / math.Pow(float64(conn+1), bias)
}

// expectedARBIndex 暴力求解 max score 后端索引（平局取最小 index）。
// 评分公式与 betterScore 一致（同序浮点运算），bias=1 时测试规模下
// 浮点比较与整数交叉乘法序关系等价（分数差下界 ~1e-8 ≫ float64 eps）
func expectedARBIndex(conn, weights []int, bias float64) int {
	best := 0
	bestScore := arbScore(weights[0], conn[0], bias)
	for i := 1; i < len(conn); i++ {
		s := arbScore(weights[i], conn[i], bias)
		if s > bestScore {
			bestScore = s
			best = i
		}
	}
	return best
}

// TestARB_HeapPath_Equivalence 验证 n >= TreeThresholdARB 时
// 每次 Select 的后端与暴力参照模型逐次一致（bias 1.0/0.5 × 等权/加权）
func TestARB_HeapPath_Equivalence(t *testing.T) {
	for _, n := range []int{32, 33, 64, 100, 257} {
		for _, bias := range []float64{1.0, 0.5} {
			t.Run(fmt.Sprintf("uniform_weights_bias_%v_n_%d", bias, n), func(t *testing.T) {
				backends := generateBackends(n)
				weights := make([]int, n)
				for i := range weights {
					weights[i] = 1
				}
				runARBEquivalence(t, backends, weights, bias)
			})
			t.Run(fmt.Sprintf("mixed_weights_bias_%v_n_%d", bias, n), func(t *testing.T) {
				backends := generateWeightedBackends(n)
				weights := make([]int, n)
				for i := range weights {
					weights[i] = (i % 10) + 1 // 与 generateWeightedBackends 一致
				}
				runARBEquivalence(t, backends, weights, bias)
			})
		}
	}
}

// runARBEquivalence 用固定种子随机交织 Select/Release，逐步对照参照模型
func runARBEquivalence(t *testing.T, backends []Backend, weights []int, bias float64) {
	n := len(backends)
	selector := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: bias})
	releaser, ok := selector.(LeastConnReleaser)
	require.True(t, ok)

	conn := make([]int, n)
	outstanding := make([]int, 0, 64) // 已 Select 未 Release 的后端索引
	rng := rand.New(rand.NewPCG(1, uint64(n)))

	for step := 0; step < 5000; step++ {
		// 约 1/3 概率 Release（有未释放项时），其余 Select
		if len(outstanding) > 0 && rng.IntN(3) == 0 {
			k := rng.IntN(len(outstanding))
			idx := outstanding[k]
			outstanding = append(outstanding[:k], outstanding[k+1:]...)
			releaser.Release(backends[idx])
			conn[idx]--
			continue
		}

		want := expectedARBIndex(conn, weights, bias)
		got := selector.Select(backends)
		require.NotNil(t, got)
		require.Equal(t, backends[want].Address(), got.Address(),
			"step %d: 堆路径选择应与参照模型一致", step)
		conn[want]++
		outstanding = append(outstanding, want)
	}
}
