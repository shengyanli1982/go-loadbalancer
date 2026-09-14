package lb

import (
	"encoding/binary"
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
// 仅编码每个后端的 Address（权重变化不会触发重建），
// 用于选择不依赖权重的选择器（P2C、RingHash、Maglev）。
// 注意：LeastConn/ARB/LeastTime 存在加权评分路径，使用 computeWeightedFingerprint。
// 快速检测后端列表是否发生变化，避免每次都重新构建内部数据结构
//
// 编码方式：length-prefix——每个地址先写入 8 字节大端长度，再写入地址内容。
// 该编码是单射的：不同列表必然产生不同的字节流；而分隔符方案存在碰撞，
// 例如 "|" 分隔下 fp(["a|b","c"]) == fp(["a","b|c"])，会把含 "|" 的地址
// 列表迁移误判为"未变化"，导致选择器跳过内部数据结构重建。
//
// 历史教训（切勿再为性能移除 length-prefix）：
//   - 012e12a 用 length-prefix 修复了该碰撞
//   - 3b3c726 回归为 "|" 分隔符，碰撞重现
//   - 本次恢复 length-prefix，并由 TestFingerprint_Injectivity 回归测试固化
func computeBackendsFingerprint(backends []Backend) uint64 {
	h := xxhash.New()
	var buf [8]byte
	for _, b := range backends {
		addr := b.Address()
		binary.BigEndian.PutUint64(buf[:], uint64(len(addr)))
		h.Write(buf[:])
		h.WriteString(addr)
	}
	return h.Sum64()
}

// computeWeightedFingerprint 计算加权后端列表的指纹
// 编码每个后端的 Address 和 Weight，任一变化都会触发指纹变更
// 用于 WeightedRR、SmoothWeightedRR、EDF、Rendezvous、LeastConn、ARB、LeastTime
// 检测后端列表或权重变化
//
// 编码方式：与 computeBackendsFingerprint 相同的 length-prefix
// （先写 8 字节大端地址长度，再写地址内容），随后追加 8 字节大端权重，
// 保证地址边界无歧义且大权重不截断，整体编码单射。
//
// 权重语义保持不变：非 WeightedBackend 或权重 <= 0 记为 1。
//
// 不变式：内部缓存必须是 addr+weight 的纯派生数据；缓存「实例行为」（如接口值）
// 的算法必须在 fp 匹配但 slice 更换时重绑（见 leastTime.refreshLatencyCache）。
//
// 历史教训（切勿再为性能移除 length-prefix）：
// 012e12a 修复 → 3b3c726 回归 → 本次恢复；
// 行为由 TestFingerprint_Injectivity 回归测试固化。
func computeWeightedFingerprint(backends []Backend) uint64 {
	h := xxhash.New()
	var buf [8]byte
	for _, b := range backends {
		addr := b.Address()
		binary.BigEndian.PutUint64(buf[:], uint64(len(addr)))
		h.Write(buf[:])
		h.WriteString(addr)
		w := 1
		if wb, ok := b.(WeightedBackend); ok {
			if v := wb.Weight(); v > 0 {
				w = v
			}
		}
		binary.BigEndian.PutUint64(buf[:], uint64(w))
		h.Write(buf[:])
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
