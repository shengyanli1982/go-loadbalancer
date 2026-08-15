package lb

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// pinProbe 描述一个 fast-path 缓存 selector 的 ABA pin 提取方式
type pinProbe struct {
	name   string
	newSel func() Selector
	pinOf  func(sel Selector) []Backend
}

// pinProbes 覆盖全部 10 个依赖 slicePtr+sliceLen fast path 的缓存型 selector，
// 其 pin 均在慢路径与 slicePtr 同步更新
func pinProbes() []pinProbe {
	return []pinProbe{
		{"weightedRR", NewWeightedRR, func(s Selector) []Backend { return s.(*weightedRR).backends }},
		{"edf", NewEDF, func(s Selector) []Backend { return s.(*edf).backends }},
		{"leastConn", NewLeastConn, func(s Selector) []Backend { return s.(*leastConn).backends }},
		{"activeRequestBias", NewActiveRequestBias, func(s Selector) []Backend { return s.(*activeRequestBias).backends }},
		{"leastTime", NewLeastTime, func(s Selector) []Backend { return s.(*leastTime).backends }},
		{"p2c", NewP2C, func(s Selector) []Backend { return s.(*p2c).data.Load().backends }},
		{"maglev", func() Selector { return NewMaglev(nil) }, func(s Selector) []Backend { return s.(*maglev).backends }},
		{"ringHash", func() Selector { return NewRingHash(nil) }, func(s Selector) []Backend { return s.(*ringHash).backends }},
		{"smoothWeightedRR", NewSmoothWeightedRR, func(s Selector) []Backend { return s.(*smoothWeightedRR).backends }},
		{"rendezvous", func() Selector { return NewRendezvous() }, func(s Selector) []Backend { return s.(*rendezvous).backends }},
	}
}

// TestSelectorBackendsPin 验证首次 Select（慢路径构建）后，selector 内部
// 持有对调用方 slice 底层数组的引用（ABA pin）。
// 若无该 pin：调用方废弃切片 → GC 回收底层数组 → 同长度新切片复用同一地址
// → fast path（ptr+length 判等）永久命中陈旧内部状态。
func TestSelectorBackendsPin(t *testing.T) {
	for _, p := range pinProbes() {
		t.Run(p.name, func(t *testing.T) {
			backends := generateBackends(8)
			sel := p.newSel()
			sel.Select(backends)

			pinned := p.pinOf(sel)
			require.NotNil(t, pinned, "pin field must be populated after first Select")
			require.True(t, unsafe.SliceData(pinned) == unsafe.SliceData(backends),
				"pin must reference the caller slice backing array")
		})
	}
}

// TestSelectorBackendsPin_TracksSlicePtrWithoutRebuild 验证同内容新 slice
// （fp 相同、ptr 不同，不触发 rebuild）时 pin 随 slicePtr 同步更新；
// 否则被缓存 ptr 的底层数组失去引用保护，ABA 仍可能发生。
func TestSelectorBackendsPin_TracksSlicePtrWithoutRebuild(t *testing.T) {
	for _, p := range pinProbes() {
		t.Run(p.name, func(t *testing.T) {
			a := generateBackends(8)
			b := generateBackends(8) // 内容相同（指纹一致），底层数组不同

			sel := p.newSel()
			sel.Select(a)
			sel.Select(b)

			pinned := p.pinOf(sel)
			require.NotNil(t, pinned)
			require.True(t, unsafe.SliceData(pinned) == unsafe.SliceData(b),
				"pin must follow cached slicePtr even when fingerprint is unchanged")
		})
	}
}
