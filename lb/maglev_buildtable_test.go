package lb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTableReference 保留 2026-09 OPT-2 优化前的未优化 buildTable 逻辑
// （table 全新 make + 双遍填 -1 + offsets/skips/next 三个独立数组 + 硬件取模），
// 作为优化后实现的硬不变式对照：相同输入（backends 序列 + tableSize）下
// 产出的查找表必须逐元素一致——任何偏差都是静默的线上误路由。
func buildTableReference(backends []Backend, tableSize int) []int {
	n := len(backends)
	if n == 0 {
		return nil
	}

	table := make([]int, tableSize)
	for i := range table {
		table[i] = -1
	}

	offsets := make([]int, n)
	skips := make([]int, n)
	for i, b := range backends {
		offsets[i] = int(hash64([]byte("offset:"+b.Address())) % uint64(tableSize))
		skips[i] = int(hash64([]byte("skip:"+b.Address()))%uint64(tableSize-1)) + 1
	}

	next := make([]int, n)
	for filled := 0; filled < tableSize; {
		for i := 0; i < n; i++ {
			c := (offsets[i] + next[i]*skips[i]) % tableSize
			next[i]++
			if table[c] < 0 {
				table[c] = i
				filled++
				if filled >= tableSize {
					break
				}
			}
		}
	}
	return table
}

// maglevTestBackends 构造 n 个地址互异的后端
func maglevTestBackends(prefix string, n int) []Backend {
	backends := make([]Backend, n)
	for i := range backends {
		backends[i] = NewBackend(fmt.Sprintf("%s-%d:8080", prefix, i))
	}
	return backends
}

// TestMaglev_BuildTableMatchesReference 验证优化后的 buildTable 与参考实现在
// 多组输入下产出逐元素一致的查找表。
// 覆盖：n=1/2/50(标准规模)/64 × tableSize=257 与默认 65537（DefaultMaglevTableSize）、
// 重复地址（offset/skip 相同，探测序列交叠）、空串地址。
func TestMaglev_BuildTableMatchesReference(t *testing.T) {
	dupBackends := []Backend{
		NewBackend("dup:8080"),
		NewBackend("dup:8080"), // 重复地址：与上一后端 offset/skip 完全相同
		NewBackend("other:8080"),
	}
	emptyBackends := []Backend{
		NewBackend(""), // 空串地址：hash 输入退化为 "offset:" / "skip:"
		NewBackend("x:8080"),
		NewBackend(""),
	}

	cases := []struct {
		name      string
		backends  []Backend
		tableSize int
	}{
		{"n=1/ts=257", maglevTestBackends("solo", 1), 257},
		{"n=1/ts=default65537", maglevTestBackends("solo", 1), DefaultMaglevTableSize},
		{"n=2/ts=257", maglevTestBackends("pair", 2), 257},
		{"n=2/ts=default65537", maglevTestBackends("pair", 2), DefaultMaglevTableSize},
		{"n=50/ts=257", maglevTestBackends("std", 50), 257},
		{"n=50/ts=default65537", maglevTestBackends("std", 50), DefaultMaglevTableSize},
		{"n=64/ts=257", maglevTestBackends("wide", 64), 257},
		{"n=64/ts=default65537", maglevTestBackends("wide", 64), DefaultMaglevTableSize},
		{"duplicate_addrs/ts=257", dupBackends, 257},
		{"duplicate_addrs/ts=default65537", dupBackends, DefaultMaglevTableSize},
		{"empty_addrs/ts=257", emptyBackends, 257},
		{"empty_addrs/ts=default65537", emptyBackends, DefaultMaglevTableSize},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &maglev{tableSize: tc.tableSize}
			got := m.buildTable(tc.backends)
			require.Equal(t, buildTableReference(tc.backends, tc.tableSize), got.table,
				"优化后 buildTable 输出必须与参考实现逐元素一致（偏差=静默误路由）")
		})
	}
}

// TestMaglev_BuildTableReuseMatchesReference 在同一实例上连续重建
// （n 大→小→大→空→恢复），验证每次全新分配的查找表都不残留上一轮数据、
// 空后端返回 table 为 nil 的快照后仍能正确重建。每一步均与参考实现逐元素比对。
func TestMaglev_BuildTableReuseMatchesReference(t *testing.T) {
	const ts = 257
	a := maglevTestBackends("reuse-a", 50)
	b := maglevTestBackends("reuse-b", 64)
	c := maglevTestBackends("reuse-c", 1)
	m := &maglev{tableSize: ts}

	steps := [][]Backend{a, c, b, a, nil, b}
	for i, backends := range steps {
		got := m.buildTable(backends)
		if len(backends) == 0 {
			require.Nil(t, got.table, "步骤 %d：空后端应置 table 为 nil", i)
			continue
		}
		require.Equal(t, buildTableReference(backends, ts), got.table,
			"步骤 %d：连续重建后的输出与参考实现不一致", i)
	}
}

// TestMaglev_RebuildAllocBudget 固化慢路径重建的分配预算。
// RCU 化后每次重建都必须发布全新的不可变快照，稳态每次重建 3 alloc：
// maglevData 快照(1) + table 全新分配(1) + 探测游标与 skips 合并的单次 2n 分配(1)。
// 闭包内两次重建，预算 ≤6；maglev 重建不涉及 map 等运行时内部分配，可精确断言。
//
// 预算演进（OPT-2 时代为 ≤2，即每次重建 1 alloc）：RCU 快照不可变 ⟹ table 不得
// 跨重建复用容量（resizeSlice 会写穿在途读者仍持有的旧快照，读者将读到新旧拓扑
// 混合的槽位 = 静默误路由），B/op 因此由 1,174 回升至 ~533KB；
// 「快照对象 + table」2 alloc/重建是 RCU 下的理论下限，故旧预算 ≤2 已不可达。
// 本断言仍拦截每后端级分配回归（如恢复 offsets/skips/next 三个独立数组 → 8 alloc/重建）。
func TestMaglev_RebuildAllocBudget(t *testing.T) {
	backendsA, backendsB := rebuildBackendPairs(rebuildBenchBackends)
	selector := NewMaglev(&MaglevOptions{TableSize: 4099})
	selector.Select(backendsA) // 预热：首次建表并完成容量准备

	allocs := testing.AllocsPerRun(50, func() {
		selector.Select(backendsB)
		selector.Select(backendsA)
	})
	assert.LessOrEqual(t, allocs, float64(6),
		"重建路径分配数回归：%.1f allocs/闭包（两次重建，预期 ≤6，即每次重建 ≤3）", allocs)
}
