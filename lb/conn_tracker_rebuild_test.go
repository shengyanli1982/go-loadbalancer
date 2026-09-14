package lb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// rebuildTrackerCounts 用 Select×4（全程不 Release）在 3 个等权后端上建立确定的连接计数 [2,1,1]。
//
// 计数推导（leastConn.selectLinear 等权分支 + rrIndex 平局轮转，算法完全确定）：
//
//	Select#1 rrIndex=0 → 三者平局 tied=[0,1,2] 取 tied[0%3]=0 → conn=[1,0,0]
//	Select#2 rrIndex=1 → 后两者平局 tied=[1,2]   取 tied[1%2]=2 → conn=[1,0,1]
//	Select#3 rrIndex=2 → 仅 index1 最优 tied=[1] 取 tied[2%1]=1 → conn=[1,1,1]
//	Select#4 rrIndex=3 → 三者平局 tied=[0,1,2] 取 tied[3%3]=0 → conn=[2,1,1]
//
// 前置断言失败即说明测试前提被破坏（选择语义变更），此时本测试的期望值需重新推导。
func rebuildTrackerCounts(t *testing.T) *leastConn {
	t.Helper()

	s, ok := NewLeastConn().(*leastConn)
	require.True(t, ok, "NewLeastConn 必须返回 *leastConn 以便检查内部计数状态")

	backends := newTestBackends("a", "b", "c")
	for i := 0; i < 4; i++ {
		require.NotNil(t, s.Select(backends))
	}
	require.Equal(t, []int{2, 1, 1}, s.connByIndex,
		"测试前提：Select×4 后位置计数必须为 [2,1,1]")

	return s
}

// TestConnectionTrackerRebuild_MigratesCountsByAddressNotPosition 钉住 rebuildIndex 的
// 连接计数迁移语义：计数必须按**后端地址**迁移，不得按**位置下标**继承。
//
// 缺陷形态（回归对象）：oldAddrCache := t.addrCache 只复制 slice header，与 t.addrCache
// 共享底层数组；随后 resizeSlice(t.addrCache, n) 在 cap 足够时返回同一底层数组，
// t.addrCache[i] = b.Address() 的写入会同步写穿 oldAddrCache 的前 min(n, oldLen) 个元素，
// 使"旧地址→旧计数"的落盘退化为"新地址[i]→旧计数[i]"的按位置继承。
//
// 触发条件为 cap(addrCache) >= 新 n，即所有"新集合长度 <= 历史最大长度"的成员变更。
// 每个场景同时覆盖 rebuildIndex 直调路径与 Select 触发路径（Select 不改写 connByAddr，
// 故两条路径的 connByAddr 期望值一致）。
func TestConnectionTrackerRebuild_MigratesCountsByAddressNotPosition(t *testing.T) {
	tests := []struct {
		name string
		// newAddrs 变更后的后端集合（初始集合固定为 a/b/c，计数 [2,1,1]）
		newAddrs []string
		// wantByAddr 期望的 connByAddr 精确内容（跨 rebuild 持久层）
		wantByAddr map[string]int
		// wantByIndex 期望的 connByIndex 精确内容（按 newAddrs 顺序）
		wantByIndex []int
	}{
		{
			// 缩容：a 被移除，b/c 必须保留各自的 1，b 不得继承 a 的 2
			name:        "shrink_remove_head",
			newAddrs:    []string{"b", "c"},
			wantByAddr:  map[string]int{"b": 1, "c": 1},
			wantByIndex: []int{1, 1},
		},
		{
			// 同长度整体替换：a/b/c 的计数不得泄漏给 x/y/z，新后端必须从 0 起算
			name:        "same_len_replace_all",
			newAddrs:    []string{"x", "y", "z"},
			wantByAddr:  map[string]int{},
			wantByIndex: []int{0, 0, 0},
		},
		{
			// 同集合换序：计数必须跟随地址而非下标（A=2 必须留在 A 上，不得漂到 C）
			name:        "same_set_reorder",
			newAddrs:    []string{"c", "b", "a"},
			wantByAddr:  map[string]int{"a": 2, "b": 1, "c": 1},
			wantByIndex: []int{1, 1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/via_rebuildIndex", func(t *testing.T) {
			s := rebuildTrackerCounts(t)

			s.rebuildIndex(newTestBackends(tt.newAddrs...))

			require.Equal(t, tt.wantByAddr, s.connByAddr,
				"connByAddr 必须按地址精确迁移旧计数")
			require.Equal(t, tt.wantByIndex, s.connByIndex,
				"connByIndex 必须与新地址顺序上的按地址计数一致")
		})

		t.Run(tt.name+"/via_Select", func(t *testing.T) {
			s := rebuildTrackerCounts(t)

			require.NotNil(t, s.Select(newTestBackends(tt.newAddrs...)))

			// Select 只递增 connByIndex，不写 connByAddr，故此处期望值与直调路径一致
			require.Equal(t, tt.wantByAddr, s.connByAddr,
				"经 Select 触发的 rebuild 同样必须按地址精确迁移旧计数")
		})
	}
}
