package lb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// sameContentSlices 返回两个内容完全相同（地址与权重一致）但底层数组不同的 Backend slice
func sameContentSlices() (a, b []Backend) {
	a = []Backend{
		NewWeightedBackend("a:8080", 1),
		NewWeightedBackend("b:8080", 2),
		NewWeightedBackend("c:8080", 3),
	}
	b = make([]Backend, len(a))
	copy(b, a)
	return a, b
}

// cacheRebuildProbe 描述一个带 rebuilds 计数的 RCU 型 selector 的构造与观测方式
// （表驱动范式仿照同包 pin_test.go 的 pinProbes；observe 读取实现侧标注
// 「仅测试观测用」的 rebuilds 计数与已发布快照的 slicePtr）
type cacheRebuildProbe struct {
	name    string
	newSel  func() HashSelector
	observe func(sel HashSelector) (rebuilds int, slicePtr uintptr)
}

// cacheRebuildProbes 覆盖全部三个带 rebuilds 观测字段的 RCU 型 selector；
// maglev 用 257 槽小表控制建表成本（与 rcu_snapshot_test.go 的 rcuStressCases 先例一致）
func cacheRebuildProbes() []cacheRebuildProbe {
	return []cacheRebuildProbe{
		{"maglev", func() HashSelector { return NewMaglev(&MaglevOptions{TableSize: 257}) },
			func(s HashSelector) (int, uintptr) { m := s.(*maglev); return m.rebuilds, m.data.Load().slicePtr }},
		{"ring_hash", func() HashSelector { return NewRingHash(nil) },
			func(s HashSelector) (int, uintptr) { r := s.(*ringHash); return r.rebuilds, r.data.Load().slicePtr }},
		{"rendezvous", func() HashSelector { return NewRendezvous() },
			func(s HashSelector) (int, uintptr) { r := s.(*rendezvous); return r.rebuilds, r.data.Load().slicePtr }},
	}
}

// TestCacheRebuild_FreshSliceSameContent 验证 maglev/ringHash/rendezvous 的缓存模式：
// 重建与否只由 fingerprint（内容）决定；"同内容新 slice"不触发全量重建，
// 且 slicePtr/sliceLen 在慢路径无条件更新，使后续调用恢复快速路径、结果保持一致。
//
// 范围边界（有意为之，非遗漏）：weighted_rr 等其余 7 个缓存型 selector 无
// rebuilds 计数，重建次数不可观测，故不在本测试覆盖范围——为测试可观测性给
// 生产结构体添加观测字段已被裁决不做，本测试只覆盖具备观测能力的三个 RCU 型。
func TestCacheRebuild_FreshSliceSameContent(t *testing.T) {
	key := []byte("cache-rebuild-key")

	for _, p := range cacheRebuildProbes() {
		t.Run(p.name, func(t *testing.T) {
			sel := p.newSel()
			a, b := sameContentSlices()
			require.NotEqual(t, backendsSlicePtr(a), backendsSlicePtr(b), "测试前提：B 必须是全新底层数组")

			expected := sel.SelectByHash(a, key)
			require.NotNil(t, expected)
			rebuilds, _ := p.observe(sel)
			require.Equal(t, 1, rebuilds, "首次调用应恰好触发一次构建")

			for i := 0; i < 5; i++ {
				got := sel.SelectByHash(b, key)
				require.NotNil(t, got)
				require.Equal(t, expected.Address(), got.Address(), "同内容新 slice 应返回与 A 一致的结果")
			}
			rebuilds, slicePtr := p.observe(sel)
			require.Equal(t, 1, rebuilds, "同内容新 slice 不应触发重建")
			require.Equal(t, backendsSlicePtr(b), slicePtr, "慢路径必须无条件更新 slicePtr 以恢复快速路径")
		})
	}
}
