package lb

import (
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件钉住 Maglev / RingHash / Rendezvous 三个选择器 RCU 化后的快照语义
// （范式来源：同包 p2c.getData/getDataSlow/rebuildData）。
//
// 三条不可退让的性质：
//  1. 快照不可变：已发布的快照（含其持有的切片底层数组与 map）在后续 rebuild 后
//     必须逐元素不变。cap 复用（resizeSlice / ring[:0]）与 map clear 复用都会写穿
//     仍被在途读者持有的旧快照，导致读者看到半更新状态。
//  2. 陈旧读者一致：rebuild 之前取到快照的读者，rebuild 之后用同一快照 + 同一
//     后端列表 + 同一 key 计算，必须得到与 rebuild 之前完全相同的结果。
//  3. 高并发 + 快速拓扑变更下不 panic、不返回 nil、不返回入参集合之外的后端。

// rcuKeys 陈旧读者一致性断言用的固定 key 集合
var rcuKeys = [][]byte{
	[]byte("rcu-key-0"), []byte("rcu-key-1"), []byte("rcu-key-2"),
	[]byte("rcu-key-3"), []byte("session/42"), []byte("user:1001"),
}

// TestRCU_Rendezvous_SnapshotImmutableAcrossRebuild 验证 rendezvous 已发布快照的
// cachedAddrHashes / cachedWeights 底层数组在 rebuild 后不被写穿。
// 覆盖等权（hasUniformWeights=true）与加权（false）两条 lookup 分支。
func TestRCU_Rendezvous_SnapshotImmutableAcrossRebuild(t *testing.T) {
	cases := []struct {
		name string
		pair func(n int) ([]Backend, []Backend)
	}{
		{"uniform_weights", rebuildBackendPairs},
		{"weighted", rebuildWeightedBackendPairs},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backendsA, backendsB := tc.pair(8)
			r := NewRendezvous().(*rendezvous)
			require.NotNil(t, r.SelectByHash(backendsA, rcuKeys[0]))

			before := r.data.Load()
			require.NotNil(t, before.cachedAddrHashes, "首次 Select 后快照应已填充")
			wantHashes := slices.Clone(before.cachedAddrHashes)
			wantWeights := slices.Clone(before.cachedWeights)

			require.NotNil(t, r.SelectByHash(backendsB, rcuKeys[0]), "换拓扑应触发重建")
			require.NotSame(t, before, r.data.Load(), "重建必须发布全新的快照对象")

			assert.Equal(t, wantHashes, before.cachedAddrHashes,
				"旧快照的 cachedAddrHashes 被 rebuild 写穿（RCU 快照必须不可变）")
			assert.Equal(t, wantWeights, before.cachedWeights,
				"旧快照的 cachedWeights 被 rebuild 写穿（RCU 快照必须不可变）")
		})
	}
}

// TestRCU_Rendezvous_StaleReaderConsistentResult 验证 rebuild 之前取得快照的读者，
// 在 rebuild 之后用该旧快照计算仍得到与 rebuild 之前逐次一致的结果。
func TestRCU_Rendezvous_StaleReaderConsistentResult(t *testing.T) {
	backendsA, backendsB := rebuildWeightedBackendPairs(8)
	r := NewRendezvous().(*rendezvous)
	require.NotNil(t, r.SelectByHash(backendsA, rcuKeys[0]))

	// 模拟一个在 rebuild 前完成 Load 的在途读者
	stale := r.data.Load()
	want := make([]string, len(rcuKeys))
	for i, key := range rcuKeys {
		got := stale.lookup(backendsA, key)
		require.NotNil(t, got, "旧快照读者不得返回 nil")
		want[i] = got.Address()
	}

	require.NotNil(t, r.SelectByHash(backendsB, rcuKeys[0]), "触发一次完整重建")
	require.NotSame(t, stale, r.data.Load())

	for i, key := range rcuKeys {
		got := stale.lookup(backendsA, key)
		require.NotNil(t, got, "重建后旧快照读者不得返回 nil")
		assert.Equal(t, want[i], got.Address(),
			"重建后旧快照读者的选择结果发生变化（快照被就地修改）")
	}
}

// TestRCU_RingHash_SnapshotImmutableAcrossRebuild 验证 ringHash 已发布快照的
// ring 底层数组与 hashToIdx 在 rebuild 后不被写穿。
// ring[:0] 的容量复用与 clear(hashToIdx) 的桶复用都会就地改写旧快照，
// 使仍在环上二分查找的读者读到"新环 + 旧映射"的混合状态。
func TestRCU_RingHash_SnapshotImmutableAcrossRebuild(t *testing.T) {
	backendsA, backendsB := rebuildBackendPairs(8)
	r := NewRingHash(&RingHashOptions{VirtualNodes: 16}).(*ringHash)
	require.NotNil(t, r.SelectByHash(backendsA, rcuKeys[0]))

	before := r.data.Load()
	require.NotEmpty(t, before.ring, "首次 Select 后哈希环应已构建")
	wantRing := slices.Clone(before.ring)
	wantMap := maps.Clone(before.hashToIdx)

	require.NotNil(t, r.SelectByHash(backendsB, rcuKeys[0]), "换拓扑应触发重建")
	require.NotSame(t, before, r.data.Load(), "重建必须发布全新的快照对象")

	assert.Equal(t, wantRing, before.ring,
		"旧快照的 ring 被 rebuild 写穿（RCU 快照必须不可变）")
	assert.Equal(t, wantMap, before.hashToIdx,
		"旧快照的 hashToIdx 被 rebuild 写穿（clear 复用会就地改写读者正在查找的映射）")
}

// TestRCU_RingHash_StaleReaderConsistentResult 验证 rebuild 之前取得快照的读者，
// 在 rebuild 之后用该旧快照在环上查找仍得到与 rebuild 之前逐次一致的结果。
func TestRCU_RingHash_StaleReaderConsistentResult(t *testing.T) {
	backendsA, backendsB := rebuildBackendPairs(8)
	r := NewRingHash(&RingHashOptions{VirtualNodes: 16}).(*ringHash)
	require.NotNil(t, r.SelectByHash(backendsA, rcuKeys[0]))

	// 模拟一个在 rebuild 前完成 Load 的在途读者
	stale := r.data.Load()
	want := make([]string, len(rcuKeys))
	for i, key := range rcuKeys {
		got := stale.lookup(backendsA, key)
		require.NotNil(t, got, "旧快照读者不得返回 nil")
		want[i] = got.Address()
	}

	require.NotNil(t, r.SelectByHash(backendsB, rcuKeys[0]), "触发一次完整重建")
	require.NotSame(t, stale, r.data.Load())

	for i, key := range rcuKeys {
		got := stale.lookup(backendsA, key)
		require.NotNil(t, got, "重建后旧快照读者不得返回 nil")
		assert.Equal(t, want[i], got.Address(),
			"重建后旧快照读者的选择结果发生变化（快照被就地修改）")
	}
}

// TestRCU_Maglev_SnapshotImmutableAcrossRebuild 验证 maglev 已发布快照的查找表
// 底层数组在 rebuild 后不被写穿。
//
// 这是 OPT-2 的 table cap 跨重建复用（resizeSlice）与 RCU 快照不可变性的直接冲突点：
// 等长拓扑交替时 cap 恒足够，resizeSlice 会返回同一底层数组的别名，
// 新一轮填表就地改写仍被在途读者持有的旧快照（读者会读到"新旧拓扑混合"的槽位）。
func TestRCU_Maglev_SnapshotImmutableAcrossRebuild(t *testing.T) {
	backendsA, backendsB := rebuildBackendPairs(8)
	m := NewMaglev(&MaglevOptions{TableSize: 4099}).(*maglev)
	require.NotNil(t, m.SelectByHash(backendsA, rcuKeys[0]))

	before := m.data.Load()
	require.Len(t, before.table, 4099, "首次 Select 后查找表应已构建")
	wantTable := slices.Clone(before.table)

	require.NotNil(t, m.SelectByHash(backendsB, rcuKeys[0]), "换拓扑应触发重建")
	require.NotSame(t, before, m.data.Load(), "重建必须发布全新的快照对象")

	assert.Equal(t, wantTable, before.table,
		"旧快照的查找表被 rebuild 写穿（table cap 复用与 RCU 不可变性冲突）")
}

// TestRCU_Maglev_StaleReaderConsistentResult 验证 rebuild 之前取得快照的读者，
// 在 rebuild 之后用该旧快照查表仍得到与 rebuild 之前逐次一致的结果。
func TestRCU_Maglev_StaleReaderConsistentResult(t *testing.T) {
	const tableSize = 4099
	backendsA, backendsB := rebuildBackendPairs(8)
	m := NewMaglev(&MaglevOptions{TableSize: tableSize}).(*maglev)
	require.NotNil(t, m.SelectByHash(backendsA, rcuKeys[0]))

	// 模拟一个在 rebuild 前完成 Load 的在途读者
	stale := m.data.Load()
	want := make([]string, len(rcuKeys))
	for i, key := range rcuKeys {
		got := stale.lookup(backendsA, key, tableSize)
		require.NotNil(t, got, "旧快照读者不得返回 nil")
		want[i] = got.Address()
	}

	require.NotNil(t, m.SelectByHash(backendsB, rcuKeys[0]), "触发一次完整重建")
	require.NotSame(t, stale, m.data.Load())

	for i, key := range rcuKeys {
		got := stale.lookup(backendsA, key, tableSize)
		require.NotNil(t, got, "重建后旧快照读者不得返回 nil")
		assert.Equal(t, want[i], got.Address(),
			"重建后旧快照读者的选择结果发生变化（查找表被就地修改）")
	}
}

// rcuSelector 压力测试所需的最小接口（Select + SelectByHash）。
// 三个算法的构造函数统一返回 ConsistentHashSelector（Selector + HashSelector 组合），
// 此处保留本地最小接口以维持表驱动用例。
type rcuSelector interface {
	Selector
	HashSelector
}

// rcuStressCases 三个 RCU 化选择器的压力测试用例。
// maglev 用 257 槽小表、ringHash 用 8 个虚拟节点，使"每轮都触发重建"的
// 压力测试保持在毫秒级（默认 65537 槽 / 100 vnode 下单次重建即达百微秒~毫秒级）。
func rcuStressCases() []struct {
	name   string
	newSel func() rcuSelector
} {
	return []struct {
		name   string
		newSel func() rcuSelector
	}{
		{"maglev", func() rcuSelector { return NewMaglev(&MaglevOptions{TableSize: 257}) }},
		{"ring_hash", func() rcuSelector { return NewRingHash(&RingHashOptions{VirtualNodes: 8}) }},
		{"rendezvous", func() rcuSelector { return NewRendezvous() }},
	}
}

// rcuStressTopologies 构造压力测试用的拓扑集合：长度递增的 4 组 + 两组与
// generateBackends(8) 等长但内容互异的后端列表。等长异内容用于覆盖
// "fingerprint 失配 → 完整重建" 与 "fingerprint 匹配但 slicePtr 失配 → 浅拷贝"
// 两条慢路径分支（复用既有 helper，不新造生成函数）。
func rcuStressTopologies() [][]Backend {
	topologies := [][]Backend{
		generateBackends(2),
		generateBackends(3),
		generateBackends(5),
		generateBackends(8),
	}
	pairA, pairB := rebuildBackendPairs(8)
	return append(topologies, pairA, pairB)
}

// TestRCU_ConcurrentReadDuringRapidTopologyChange 验证后端集合快速变化 + 高并发读时，
// 三个 RCU 选择器均：不 panic、不返回 nil、不返回入参集合之外的后端，
// 且同一 (拓扑, key) 在所有读者眼中结果唯一确定。
//
// 最后一条是"不会读到半更新状态"的语义级断言：三个算法都是
// (后端内容, key) 的纯函数，若某个读者读到"新表 + 旧后端"或
// "旧环 + 新映射"之类的撕裂快照，其结果必然偏离单线程 oracle 的期望值。
func TestRCU_ConcurrentReadDuringRapidTopologyChange(t *testing.T) {
	const (
		readers = 16
		rounds  = 60
	)
	topologies := rcuStressTopologies()

	// 每个拓扑的成员集合：断言"不返回集合外后端"
	members := make([]map[string]struct{}, len(topologies))
	for ti, topo := range topologies {
		members[ti] = make(map[string]struct{}, len(topo))
		for _, b := range topo {
			members[ti][b.Address()] = struct{}{}
		}
	}

	for _, tc := range rcuStressCases() {
		t.Run(tc.name, func(t *testing.T) {
			// 单线程 oracle：独立实例预先算出每个 (拓扑, key) 的确定性期望结果
			oracle := tc.newSel()
			want := make([][]string, len(topologies))
			for ti, topo := range topologies {
				want[ti] = make([]string, len(rcuKeys))
				for ki, key := range rcuKeys {
					got := oracle.SelectByHash(topo, key)
					require.NotNil(t, got, "oracle 不得返回 nil")
					want[ti][ki] = got.Address()
				}
			}

			sel := tc.newSel()
			var wg sync.WaitGroup
			wg.Add(readers)
			for g := 0; g < readers; g++ {
				go func(g int) {
					defer wg.Done()
					for round := 0; round < rounds; round++ {
						ti := (round + g) % len(topologies)
						topo := topologies[ti]

						if round&1 == 0 {
							// 定值 key：结果必须与 oracle 逐次一致
							ki := (round/2 + g) % len(rcuKeys)
							got := sel.SelectByHash(topo, rcuKeys[ki])
							if !assert.NotNil(t, got, "并发读不得返回 nil") {
								continue
							}
							assert.Equal(t, want[ti][ki], got.Address(),
								"同一 (拓扑, key) 在并发重建期间出现撕裂结果")
						} else {
							// 随机 key：只断言非 nil 且属于入参集合
							got := sel.Select(topo)
							if !assert.NotNil(t, got, "并发读不得返回 nil") {
								continue
							}
							_, ok := members[ti][got.Address()]
							assert.True(t, ok,
								"返回了入参后端集合之外的后端 %s", got.Address())
						}
					}
				}(g)
			}
			wg.Wait()
		})
	}
}

// TestRCU_SteadyStateFastPathZeroAlloc 钉住稳态热路径零分配：
// 后端列表不变时反复 SelectByHash 只走 atomic.Pointer.Load 快路径，
// 不得触发任何分配（RCU 改造不得把重建成本泄漏到稳态读路径）。
func TestRCU_SteadyStateFastPathZeroAlloc(t *testing.T) {
	backends := generateBackends(50)
	key := rcuKeys[0]

	for _, tc := range rcuStressCases() {
		t.Run(tc.name, func(t *testing.T) {
			sel := tc.newSel()
			require.NotNil(t, sel.SelectByHash(backends, key), "预热：首次建表/建环/建缓存")

			allocs := testing.AllocsPerRun(1000, func() {
				sel.SelectByHash(backends, key)
			})
			assert.Equal(t, float64(0), allocs,
				"稳态快路径出现分配：%.1f allocs/op", allocs)
		})
	}
}
