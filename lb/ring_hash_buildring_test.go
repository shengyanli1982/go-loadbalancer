package lb

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// referenceBuildRing 以原实现参考逻辑（fmt.Sprintf "%s#%d" + "#skip" 拼接）独立重建哈希环，
// 用于固化 buildRing 的零分配字节拼装与原格式逐字节一致、碰撞探测序列不变。
func referenceBuildRing(backends []Backend, virtualNodes int) ([]uint64, map[uint64]int) {
	ring := make([]uint64, 0, len(backends)*virtualNodes)
	nodeMap := make(map[uint64]int)
	for j, b := range backends {
		for i := 0; i < virtualNodes; i++ {
			nodeKey := fmt.Sprintf("%s#%d", b.Address(), i)
			h1 := hash64String(nodeKey)
			h2 := hash64String(nodeKey + "#skip")
			h := h1
			for {
				if _, exists := nodeMap[h]; !exists {
					break
				}
				h = h1 + h2
				h1 = h
			}
			ring = append(ring, h)
			nodeMap[h] = j
		}
	}
	slices.Sort(ring) // 与 buildRing 末尾的排序对齐
	return ring, nodeMap
}

// TestRingHash_BuildRingMatchesReference 验证优化后的 buildRing 与参考实现产出
// 完全一致的哈希环与节点映射（含重复地址触发 double-hashing 碰撞探测、
// 短首地址后出现长地址触发 scratch 扩容两种边界）。
func TestRingHash_BuildRingMatchesReference(t *testing.T) {
	backends := []Backend{
		NewBackend("a"), // 短地址置首，迫使后续长地址超出 capHint
		NewBackend("10.0.0.1:8080"),
		NewBackend("10.0.0.1:8080"), // 重复地址：key 空间重叠，必走碰撞探测
		NewBackend("very-long-backend-address-10.0.0.99:8080"),
		NewBackend("10.0.0.2:8080"),
	}
	const vn = 37

	selector := NewRingHash(&RingHashOptions{VirtualNodes: vn})
	require.NotNil(t, selector.SelectByHash(backends, []byte("warmup-key")))

	r := selector.(*ringHash)
	r.mu.RLock()
	gotRing := append([]uint64(nil), r.ring...)
	gotMap := make(map[uint64]int, len(r.nodeMap))
	for k, v := range r.nodeMap {
		gotMap[k] = v
	}
	r.mu.RUnlock()

	wantRing, wantMap := referenceBuildRing(backends, vn)

	require.Len(t, gotRing, len(backends)*vn, "环大小应为 n×virtualNodes")
	assert.Equal(t, wantRing, gotRing, "哈希环内容/顺序与参考实现不一致")
	assert.Equal(t, wantMap, gotMap, "节点映射与参考实现不一致")
}

// TestRingHash_RebuildAllocBudget 固化慢路径重建的分配预算：
// 交替后端触发整环重建时，ring/nodeMap 容量跨重建复用（clear 保留桶存储），
// 每次重建仅 O(1) 次分配，与 vnode 数无关；杜绝每 vnode 的字符串分配回归。
func TestRingHash_RebuildAllocBudget(t *testing.T) {
	backendsA, backendsB := rebuildBackendPairs(rebuildBenchBackends)
	selector := NewRingHash(nil)
	selector.Select(backendsA) // 预热：首次建环并完成容量准备

	allocs := testing.AllocsPerRun(50, func() {
		selector.Select(backendsB)
		selector.Select(backendsA)
	})
	// 预算说明：精确的 O(1) 常数取决于 Go 运行时 map 实现——
	// swiss map（Go 1.24+）clear 后重填保留桶存储，两次重建约 2 次分配；
	// 老式 hashmap（Go 1.23）clear 后重填 5000 条存在运行时内部桶分配，
	// 两次重建约 18 次分配。预算 ≤32 对 1.23-1.25 均稳健，
	// 仍比 vnode 级回归（fmt 时代 ~10049 allocs/闭包）低 2-3 个数量级：
	// 本断言只拦截 O(n×vnodes) 级分配回归，不锁定运行时相关的精确常数。
	assert.LessOrEqual(t, allocs, float64(32),
		"重建路径分配数回归：%.1f allocs/闭包（两次重建，预期 ≤32）", allocs)
}
