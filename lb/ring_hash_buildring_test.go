package lb

import (
	"fmt"
	"slices"
	"testing"
	"time"

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

	// RCU 快照不可变：读者取到的是原子发布的不可变快照，无需加锁、也无需复制即可安全断言
	snap := selector.(*ringHash).data.Load()
	gotRing := snap.ring
	gotMap := snap.hashToIdx

	wantRing, wantMap := referenceBuildRing(backends, vn)

	require.Len(t, gotRing, len(backends)*vn, "环大小应为 n×virtualNodes")
	assert.Equal(t, wantRing, gotRing, "哈希环内容/顺序与参考实现不一致")
	assert.Equal(t, wantMap, gotMap, "节点映射与参考实现不一致")
}

// TestRingHash_RebuildAllocBudget 固化慢路径重建的分配预算：
// RCU 化后 ring 与 hashToIdx 必须每次全新分配（跨重建复用会写穿在途读者仍持有的
// 旧快照），但分配次数仍是 O(1)、与 vnode 数无关；杜绝每 vnode 的字符串分配回归。
func TestRingHash_RebuildAllocBudget(t *testing.T) {
	backendsA, backendsB := rebuildBackendPairs(rebuildBenchBackends)
	selector := NewRingHash(nil)
	selector.Select(backendsA) // 预热：首次建环并完成容量准备

	allocs := testing.AllocsPerRun(50, func() {
		selector.Select(backendsB)
		selector.Select(backendsA)
	})
	// 预算说明：每次重建的 O(1) 常数 = 快照(1) + ring(1) + scratch(1) +
	// make(map[uint64]int, n×vnodes) 的运行时内部分配。后者占绝大部分且取决于
	// Go 运行时 map 实现：go1.25.13/darwin-arm64 实测 5000 条约 18 次分配，
	// 两次重建合计 42。预算 ≤64 留 ~50% 余量吸收跨版本/跨平台差异，
	// 仍比 vnode 级回归（fmt 时代 ~10049 allocs/闭包）低 2 个数量级以上：
	// 本断言只拦截 O(n×vnodes) 级分配回归，不锁定运行时相关的精确常数。
	//
	// 预算演进（RCU 前为 ≤32，实测 1 alloc/重建、81 B/op）：旧实现靠 clear(hashToIdx)
	// 保留桶存储把 map 分配压到近 0，但 RCU 快照不可变性禁止该复用——clear 会就地
	// 改写读者正在二分查找的映射（"新环 + 旧映射"撕裂状态），故每次重建的
	// B/op 升至 ~189KB、allocs 升至 21。
	assert.LessOrEqual(t, allocs, float64(64),
		"重建路径分配数回归：%.1f allocs/闭包（两次重建，预期 ≤64）", allocs)
}

// TestRingHash_ProbeFreeSlot_H2ZeroTerminates 钉住 double-hashing 探测的退化边界：
// h2 == 0 时探测序列 h1 + k*h2 恒等于 h1，若 h1 已被占用，未加固的实现会死循环；
// 且该循环位于 getDataSlow 持写锁路径上，一旦触发整个 selector 实例永久挂起。
// 由于 xxhash 种子公开、后端地址可来自外部输入（服务发现投毒场景），该退化可被构造。
// 修复后 h2 == 0 步长退化为 1（Maglev 原论文标准做法），必须在有限步内返回。
func TestRingHash_ProbeFreeSlot_H2ZeroTerminates(t *testing.T) {
	// h1 = 42 已被占用且 h2 = 0：未加固实现的探测序列恒为 42，永不终止
	nodeMap := map[uint64]int{42: 0}

	done := make(chan uint64, 1)
	go func() {
		done <- probeFreeSlot(nodeMap, 42, 0)
	}()

	select {
	case h := <-done:
		assert.Equal(t, uint64(43), h, "h2==0 退化后应以步长 1 探测到下一空闲槽位（42+1）")
		_, exists := nodeMap[h]
		assert.False(t, exists, "返回的槽位在 nodeMap 中必须空闲")
	case <-time.After(5 * time.Second):
		t.Fatal("probeFreeSlot 在 h2==0 且 h1 被占用时 5 秒内未返回：double-hashing 探测退化为死循环（持写锁将挂起整个 selector）")
	}
}

// TestRingHash_ProbeFreeSlot_H2NonZeroSequence 钉住 h2 != 0 的正常探测语义：
// 序列必须严格保持 h1 + k*h2（k = 0, 1, 2, ...），任何加固不得改变正常路径
// （环产出逐元素一致性由 TestRingHash_BuildRingMatchesReference 对照参考实现保证）。
func TestRingHash_ProbeFreeSlot_H2NonZeroSequence(t *testing.T) {
	t.Run("free_slot_returns_h1_directly", func(t *testing.T) {
		nodeMap := map[uint64]int{999: 0}
		assert.Equal(t, uint64(100), probeFreeSlot(nodeMap, 100, 7), "h1 空闲时应直接返回 h1")
	})

	t.Run("occupied_slots_follow_h1_plus_k_h2", func(t *testing.T) {
		// h1=100、h2=7：100 与 107 均被占用 → 应返回 100+2×7=114
		nodeMap := map[uint64]int{100: 0, 107: 1}
		assert.Equal(t, uint64(114), probeFreeSlot(nodeMap, 100, 7), "h2≠0 探测序列必须保持 h1 + k*h2")
	})

	t.Run("uint64_overflow_wraps_naturally", func(t *testing.T) {
		// h1 接近 2^64 上界：h1+h2 自然溢出回绕到小值，与 mod 2^64 语义一致
		const h1 = ^uint64(0) - 1 // 2^64-2
		const h2 = 3
		nodeMap := map[uint64]int{h1: 0}
		assert.Equal(t, uint64(1), probeFreeSlot(nodeMap, h1, h2), "溢出回绕后应返回 (h1+h2) mod 2^64")
	})
}
