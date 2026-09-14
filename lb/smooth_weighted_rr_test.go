package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmoothWeightedRR_Select(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
		NewWeightedBackend("c", 3),
	}

	result := selector.Select(backends)
	require.NotNil(t, result)
}

func TestSmoothWeightedRR_NilBackends(t *testing.T) {
	selector := NewSmoothWeightedRR()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestSmoothWeightedRR_EmptyBackends(t *testing.T) {
	selector := NewSmoothWeightedRR()
	result := selector.Select([]Backend{})
	assert.Nil(t, result)
}

func TestSmoothWeightedRR_Distribution(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
	}
	picks := 10000

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	// SWRR 是确定性算法：权重 {1,3} 下选择序列以 4 为周期（b a b b，每周期结束
	// currentWeight 归零），10000 = 2500 个完整周期 ⇒ 计数精确为 a=2500、b=7500，
	// 无余数。原 InDelta(±1000) 允许 15%~35% 占比通过，突发式实现也能过；
	// 收紧为精确断言（序列级平滑性另由 TestSmoothWeightedRR_SmoothSequence 钉住）。
	require.Equal(t, 2500, counts["a"], "'a' (25%%) count")
	require.Equal(t, 7500, counts["b"], "'b' (75%%) count")
}

// TestSmoothWeightedRR_SmoothSequence 钉住 SWRR 的定义性属性——平滑性
// （README:「Even traffic spread across weighted backends (avoids burst)」）：
// 权重 {a:5, b:1, c:1} 下前 7 次选择精确等于 nginx 经典平滑序列 [a a b a c a a]，
// 即高权重后端被低权重后端均匀穿插，而非先连续吃满 5 次再轮到 b、c。
//
// 聚合计数断言（TestSmoothWeightedRR_Distribution）无法区分平滑与突发：
// 「a×5 b×1 c×1」的突发序列与平滑序列的总计数完全相同。本测试按选择次序
// 逐项断言，任何贪心/突发式实现（每轮恒选最大 effectiveWeight 等）都会在此失败。
//
// 期望值经运行实现实证确认（非仅手推）：totalWeight=7，每轮
// currentWeight += effectiveWeight 后取严格最大者（平局取低索引）再减 7，
// 第 7 轮结束 currentWeight 归零，序列以 7 为周期。
func TestSmoothWeightedRR_SmoothSequence(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 5),
		NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 1),
	}

	want := []string{"a", "a", "b", "a", "c", "a", "a"}
	got := make([]string, 0, len(want))
	for i := 0; i < len(want); i++ {
		b := selector.Select(backends)
		require.NotNil(t, b, "第 %d 次选择不应返回 nil", i+1)
		got = append(got, b.Address())
	}
	assert.Equal(t, want, got, "前 7 次选择应等于 nginx 经典平滑序列")
}
