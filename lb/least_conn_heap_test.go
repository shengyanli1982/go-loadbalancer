package lb

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// expectedLeastConnIndex 暴力求解最小 (conn/weight, index) 后端索引，
// 作为堆路径选择语义的参照模型（最小 conn，平局取最小 index，加权按 conn/weight）。
func expectedLeastConnIndex(conn, weights []int) int {
	best := 0
	for i := 1; i < len(conn); i++ {
		lhs := int64(conn[i]) * int64(weights[best])
		rhs := int64(conn[best]) * int64(weights[i])
		if lhs < rhs || (lhs == rhs && i < best) {
			best = i
		}
	}
	return best
}

// TestLeastConn_HeapPath_Equivalence 验证 n >= TreeThresholdLeastConn 时
// 每次 Select 的后端与暴力参照模型逐次一致（等权与加权两种场景）。
func TestLeastConn_HeapPath_Equivalence(t *testing.T) {
	for _, n := range []int{32, 33, 64, 100, 257} {
		t.Run("uniform_weights", func(t *testing.T) {
			backends := generateBackends(n)
			weights := make([]int, n)
			for i := range weights {
				weights[i] = 1
			}
			runLeastConnEquivalence(t, backends, weights)
		})
		t.Run("mixed_weights", func(t *testing.T) {
			backends := generateWeightedBackends(n)
			weights := make([]int, n)
			for i := range weights {
				weights[i] = (i % 10) + 1 // 与 generateWeightedBackends 一致
			}
			runLeastConnEquivalence(t, backends, weights)
		})
	}
}

// runLeastConnEquivalence 用固定种子随机交织 Select/Release，逐步对照参照模型。
func runLeastConnEquivalence(t *testing.T, backends []Backend, weights []int) {
	n := len(backends)
	selector := NewLeastConn()
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

		want := expectedLeastConnIndex(conn, weights)
		got := selector.Select(backends)
		require.NotNil(t, got)
		require.Equal(t, backends[want].Address(), got.Address(),
			"step %d: 堆路径选择应与参照模型一致", step)
		conn[want]++
		outstanding = append(outstanding, want)
	}
}
