package lb

import (
	"slices"
	"sort"
	"strconv"
	"sync"
)

// ringHash 实现一致性哈希（Ring Hash/Consistent Hash）算法
// 特点：后端节点变化时，只影响少量请求的路由，最小化迁移
// 原理：将后端映射到哈希环上，使用虚拟节点提高分布均匀性
type ringHash struct {
	mu           sync.RWMutex
	ring         []uint64       // 哈希环，存储虚拟节点的哈希值（有序）
	backends     []Backend      // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	nodeMap      map[uint64]int // 哈希值到后端在 backends 中的索引
	virtualNodes int            // 虚拟节点数量
	rebuilds     int            // buildRing 实际执行次数（仅测试观测用，始终在写锁内访问）
	cacheSnapshot
}

// RingHashOptions 配置选项
type RingHashOptions struct {
	RingSize     int // 已废弃且被忽略，保留仅为 API 兼容
	VirtualNodes int // 虚拟节点数量，越多分布越均匀，但占用更多内存
}

// RingHashSelector 接口，同时支持 Select 和 SelectByHash
type RingHashSelector interface {
	Selector
	HashSelector
}

// NewRingHash 创建一致性哈希选择器
// nodeMap 延迟到首次 buildRing 时按实际规模预分配（见 buildRing 说明）
func NewRingHash(opts *RingHashOptions) RingHashSelector {
	r := &ringHash{
		virtualNodes: DefaultVirtualNodes,
	}
	if opts != nil {
		if opts.VirtualNodes > 0 {
			r.virtualNodes = opts.VirtualNodes
		}
	}
	return r
}

// Select 随机选择一个后端（使用一致性哈希）
// 使用随机 key 调用 SelectByHash
// 优化：使用 math/rand/v2 全局无锁 PRNG（ChaCha8），替代 globalRNG 的 sync.Mutex
func (r *ringHash) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	key := randomKey8()
	return r.SelectByHash(backends, key[:])
}

// SelectByHash 使用一致性哈希选择一个后端
// 算法：
// 1. 计算 key 的哈希值
// 2. 在哈希环上二分查找第一个大于等于该哈希值的位置
// 3. 返回该位置对应的后端
// 优化：使用后端指纹缓存，仅在后端列表变化时重建哈希环
func (r *ringHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	// 快速路径：slice 指针匹配且环已构建 → 直接查找
	ptr := backendsSlicePtr(backends)
	r.mu.RLock()
	if ptr == r.slicePtr && len(backends) == r.sliceLen && len(r.ring) > 0 {
		h := hash64(key)
		idx := sort.Search(len(r.ring), func(i int) bool {
			return r.ring[i] >= h
		})
		if idx >= len(r.ring) {
			idx = 0
		}
		bIdx := r.nodeMap[r.ring[idx]]
		r.mu.RUnlock()
		return backends[bIdx]
	}
	r.mu.RUnlock()

	// 慢速路径：需要计算 fingerprint 并可能重建哈希环
	// 重建与否只由 fingerprint（内容）决定；slicePtr/sliceLen 每次慢路径无条件更新，
	// 避免 fp 匹配但 ptr 失配时永久停留在慢路径
	fp := computeBackendsFingerprint(backends)
	r.mu.Lock()
	if fp != r.fingerprint || len(r.ring) == 0 {
		r.buildRing(backends)
		r.fingerprint = fp
		r.rebuilds++
	}
	r.slicePtr = ptr
	r.sliceLen = len(backends)
	r.backends = backends
	h := hash64(key)
	idx := sort.Search(len(r.ring), func(i int) bool {
		return r.ring[i] >= h
	})
	if idx >= len(r.ring) {
		idx = 0
	}
	bIdx := r.nodeMap[r.ring[idx]]
	r.mu.Unlock()
	return backends[bIdx]
}

// buildRing 构建哈希环
// 为每个后端创建 virtualNodes 个虚拟节点，将它们添加到环上
// 慢路径去分配优化：
//   - ring 容量在重建间复用；不足时按 n×virtualNodes 精确预分配，杜绝增长再分配
//   - nodeMap 跨重建复用：clear() 保留桶存储，后续重建零分配；
//     首次建环时按 n×virtualNodes 预分配（map 按提示规模创建本身有多次分配，
//     须靠重建间复用摊销）
//   - 虚拟节点 key 在单次分配的 scratch buffer 中按字节拼装（等价于原 fmt 格式 "%s#%d"），
//     杜绝每 vnode 的 fmt.Sprintf 与字符串拼接分配
func (r *ringHash) buildRing(backends []Backend) {
	r.backends = backends

	total := len(backends) * r.virtualNodes

	if cap(r.ring) >= total {
		r.ring = r.ring[:0]
	} else {
		r.ring = make([]uint64, 0, total)
	}
	if r.nodeMap == nil {
		r.nodeMap = make(map[uint64]int, total)
	} else {
		clear(r.nodeMap)
	}

	// Scratch buffer：地址 + '#' + vnode 序号（+#skip 后缀）足够长，每次构建只分配一次
	capHint := 32
	if len(backends) > 0 {
		capHint = len(backends[0].Address()) + 24
	}
	scratch := make([]byte, 0, capHint)

	// 为每个后端创建虚拟节点（使用 double-hashing 策略处理碰撞）
	// 参考 Envoy Ring Hash 实现：h = h1 + j * h2（mod 2^64，uint64 自然溢出）
	// h1 = hash(address#i), h2 = hash(address#i#skip)
	// 由于 xxhash 64bit 碰撞概率极低（~2^-64），double-hashing 确保即使碰撞也能分散
	for j, b := range backends {
		addr := b.Address()
		for i := 0; i < r.virtualNodes; i++ {
			// 字节拼装：addr + '#' + 序号，与原 "%s#%d" 格式逐字节一致，哈希值不变
			scratch = append(scratch[:0], addr...)
			scratch = append(scratch, '#')
			scratch = strconv.AppendInt(scratch, int64(i), 10)
			h1 := hash64(scratch)
			scratch = append(scratch, "#skip"...)
			h2 := hash64(scratch)
			h := h1
			for {
				if _, exists := r.nodeMap[h]; !exists {
					break
				}
				// Double-hashing 线性探测：h = h1 + h2（uint64 自然溢出即 mod 2^64）
				h = h1 + h2
				h1 = h
			}
			r.ring = append(r.ring, h)
			r.nodeMap[h] = j
		}
	}

	// 对环进行排序，便于二分查找
	slices.Sort(r.ring)
}
