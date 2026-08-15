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

// TestCacheRebuild_FreshSliceSameContent 验证 maglev/ringHash/rendezvous 的缓存模式：
// 重建与否只由 fingerprint（内容）决定；"同内容新 slice"不触发全量重建，
// 且 slicePtr/sliceLen 在慢路径无条件更新，使后续调用恢复快速路径、结果保持一致。
// （weighted_rr 等无 rebuilds 计数，重建次数不可观测，故不在本测试覆盖范围）
func TestCacheRebuild_FreshSliceSameContent(t *testing.T) {
	key := []byte("cache-rebuild-key")

	t.Run("maglev", func(t *testing.T) {
		m := NewMaglev(&MaglevOptions{TableSize: 257}).(*maglev)
		a, b := sameContentSlices()
		require.NotEqual(t, backendsSlicePtr(a), backendsSlicePtr(b), "测试前提：B 必须是全新底层数组")

		expected := m.SelectByHash(a, key)
		require.NotNil(t, expected)
		require.Equal(t, 1, m.rebuilds, "首次调用应恰好触发一次构建")

		for i := 0; i < 5; i++ {
			got := m.SelectByHash(b, key)
			require.NotNil(t, got)
			require.Equal(t, expected.Address(), got.Address(), "同内容新 slice 应返回与 A 一致的结果")
		}
		require.Equal(t, 1, m.rebuilds, "同内容新 slice 不应触发重建")
		require.Equal(t, backendsSlicePtr(b), m.slicePtr, "慢路径必须无条件更新 slicePtr 以恢复快速路径")
	})

	t.Run("ringHash", func(t *testing.T) {
		r := NewRingHash(nil).(*ringHash)
		a, b := sameContentSlices()
		require.NotEqual(t, backendsSlicePtr(a), backendsSlicePtr(b), "测试前提：B 必须是全新底层数组")

		expected := r.SelectByHash(a, key)
		require.NotNil(t, expected)
		require.Equal(t, 1, r.rebuilds, "首次调用应恰好触发一次构建")

		for i := 0; i < 5; i++ {
			got := r.SelectByHash(b, key)
			require.NotNil(t, got)
			require.Equal(t, expected.Address(), got.Address(), "同内容新 slice 应返回与 A 一致的结果")
		}
		require.Equal(t, 1, r.rebuilds, "同内容新 slice 不应触发重建")
		require.Equal(t, backendsSlicePtr(b), r.slicePtr, "慢路径必须无条件更新 slicePtr 以恢复快速路径")
	})

	t.Run("rendezvous", func(t *testing.T) {
		r := NewRendezvous().(*rendezvous)
		a, b := sameContentSlices()
		require.NotEqual(t, backendsSlicePtr(a), backendsSlicePtr(b), "测试前提：B 必须是全新底层数组")

		expected := r.SelectByHash(a, key)
		require.NotNil(t, expected)
		require.Equal(t, 1, r.rebuilds, "首次调用应恰好触发一次构建")

		for i := 0; i < 5; i++ {
			got := r.SelectByHash(b, key)
			require.NotNil(t, got)
			require.Equal(t, expected.Address(), got.Address(), "同内容新 slice 应返回与 A 一致的结果")
		}
		require.Equal(t, 1, r.rebuilds, "同内容新 slice 不应触发重建")
		require.Equal(t, backendsSlicePtr(b), r.slicePtr, "慢路径必须无条件更新 slicePtr 以恢复快速路径")
	})
}
