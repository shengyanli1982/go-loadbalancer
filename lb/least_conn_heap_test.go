package lb

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// assertIdxHeapInvariants 断言索引堆在每步随机操作后的三条不变量（P2-4b）：
//  1. 堆序：子不优于父——对所有位置 p>0，!less(heap[p], heap[(p-1)/2])。
//     less 传入 selector 的 heapLess（比较语义的唯一权威来源）：该断言成立
//     即证明 least_conn.go / arb.go 各自内联的 siftDown/siftUp（为避免闭包
//     间接调用开销而内联，属合理性能取舍、不合并回 idxHeap）没有偏离
//     heapLess 定义的序——「最小 conn / 最大 score + 平局取小 index」这一
//     分散在多处的比较语义在此被钉住为同一语义；
//  2. pos 在界：0 <= pos[i] < len(heap)；
//  3. pos/heap 互逆：heap[pos[i]] == i。这是索引堆最易出错的双结构同步点：
//     swap 时漏更新 pos 会让 Release 的 siftUp 从错误位置起修，产生静默错位。
//
// 刻意用原生循环 + t.Fatalf 而非逐元素 testify 断言：本函数在 5000 步随机
// 对拍的每一步之后调用，原生循环开销比 testify 小约两个数量级。
// 调用时机：Select/Release 返回后、测试 goroutine 内单线程调用（白盒读取无并发）。
func assertIdxHeapInvariants(t *testing.T, heap idxHeap, less func(i, j int) bool, ctx string) {
	t.Helper()
	for p := 1; p < len(heap.heap); p++ {
		parent := (p - 1) / 2
		if less(heap.heap[p], heap.heap[parent]) {
			t.Fatalf("%s: 堆序被破坏：位置 %d 的索引 %d 优于父位置 %d 的索引 %d（内联 sift 偏离 heapLess 定义的序）",
				ctx, p, heap.heap[p], parent, heap.heap[parent])
		}
	}
	for i, p := range heap.pos {
		if p < 0 || p >= len(heap.heap) {
			t.Fatalf("%s: pos[%d]=%d 越界（len(heap)=%d）", ctx, i, p, len(heap.heap))
		}
		if heap.heap[p] != i {
			t.Fatalf("%s: pos/heap 不互逆：pos[%d]=%d 但 heap[%d]=%d", ctx, i, p, p, heap.heap[p])
		}
	}
}

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
// 每步操作后追加堆不变量校验（assertIdxHeapInvariants）：Select 递增堆顶后
// 内联 siftDown、Release 递减后内联 siftUp，二者的产物必须始终满足
// heapLess 堆序与 pos/heap 互逆。
func runLeastConnEquivalence(t *testing.T, backends []Backend, weights []int) {
	n := len(backends)
	selector := NewLeastConn()
	lc := selector.(*leastConn) // 白盒读取 heap/heapLess（单线程对拍，无并发）
	releaser, ok := selector.(RequestReleaser)
	require.True(t, ok)

	conn := make([]int, n)
	outstanding := make([]int, 0, 64) // 已 Select 未 Release 的后端索引
	rng := rand.New(rand.NewPCG(1, uint64(n)))
	less := lc.heapLess

	for step := 0; step < 5000; step++ {
		// 约 1/3 概率 Release（有未释放项时），其余 Select
		if len(outstanding) > 0 && rng.IntN(3) == 0 {
			k := rng.IntN(len(outstanding))
			idx := outstanding[k]
			outstanding = append(outstanding[:k], outstanding[k+1:]...)
			releaser.Release(backends[idx])
			conn[idx]--
			assertIdxHeapInvariants(t, lc.heap, less,
				fmt.Sprintf("n=%d step %d Release(%d) 后", n, step, idx))
			continue
		}

		want := expectedLeastConnIndex(conn, weights)
		got := selector.Select(backends)
		require.NotNil(t, got)
		require.Equal(t, backends[want].Address(), got.Address(),
			"step %d: 堆路径选择应与参照模型一致", step)
		conn[want]++
		outstanding = append(outstanding, want)
		assertIdxHeapInvariants(t, lc.heap, less,
			fmt.Sprintf("n=%d step %d Select(%d) 后", n, step, want))
	}
}
