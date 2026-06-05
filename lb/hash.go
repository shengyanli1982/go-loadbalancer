package lb

import (
	"unsafe"

	"github.com/cespare/xxhash/v2"
)

// hash64 使用 xxhash 对字节切片进行哈希计算
// xxhash 是一种高性能的哈希算法，适合负载均衡场景
func hash64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// hash64String 使用 xxhash 对字符串进行哈希计算
func hash64String(data string) uint64 {
	return xxhash.Sum64String(data)
}

// backendsSlicePtr 返回 []Backend 底层数组的指针地址
// 用于快速检测后端列表是否为同一个 slice（未变化）
//
// 安全性说明：
//   - 仅用于地址比较，不通过 uintptr 访问数据，无 GC 安全问题
//   - 在负载均衡场景中，后端列表变化通常通过整体替换 slice 实现
//   - 如果调用者传入同一个 slice 变量（底层数组地址不变），此值恒定
func backendsSlicePtr(backends []Backend) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(backends)))
}

// computeBackendsFingerprint 计算后端列表的指纹
// 仅编码每个后端的 Address，用于非加权选择器（RoundRobin、Random、LeastConn）
// 快速检测后端列表是否发生变化，避免每次都重新构建内部数据结构
func computeBackendsFingerprint(backends []Backend) uint64 {
	h := xxhash.New()
	for _, b := range backends {
		h.WriteString(b.Address())
	}
	return h.Sum64()
}

// computeWeightedFingerprint 计算加权后端列表的指纹
// 编码每个后端的 Address 和 Weight，任一变化都会触发指纹变更
// 用于 SmoothWeightedRR 和 WeightedRR 检测后端列表或权重变化
func computeWeightedFingerprint(backends []Backend) uint64 {
	h := xxhash.New()
	for _, b := range backends {
		h.WriteString(b.Address())
		w := 1
		if wb, ok := b.(WeightedBackend); ok {
			if v := wb.Weight(); v > 0 {
				w = v
			}
		}
		// 写入分隔符 "|" 和权重（2字节大端），防止 Address 边界歧义
		h.Write([]byte{'|'})
		wb := [2]byte{byte(w >> 8), byte(w)}
		h.Write(wb[:])
	}
	return h.Sum64()
}

// getWeight 获取后端权重，非加权后端或无效权重（<=0）返回 1
// 包级共享函数，供 weighted_rr 和 smooth_weighted_rr 统一使用
func getWeight(b Backend) int {
	if wb, ok := b.(WeightedBackend); ok {
		if w := wb.Weight(); w > 0 {
			return w
		}
	}
	return 1
}
