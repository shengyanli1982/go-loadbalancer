package lb

import (
	"slices"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
)

// ringHashData 是 ringHash 的不可变快照，持有全部依赖后端拓扑的状态。
// 由 atomic.Pointer 原子发布，读者 Load 之后无需加锁；旧快照交给 GC 回收。
//
// 快照不可变约束：ring 与 hashToIdx 每次重建都必须全新分配，禁止 ring[:0] 的
// 容量复用与 clear(hashToIdx) 的桶复用——两者都会就地改写仍被在途读者持有的旧
// 快照（读者会在"新环 + 旧映射"的混合状态上做二分查找），
// 由 TestRCU_RingHash_SnapshotImmutableAcrossRebuild 钉住。
type ringHashData struct {
	backends  []Backend      // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（随快照原子持有）
	ring      []uint64       // 哈希环，存储虚拟节点的哈希值（有序）
	hashToIdx map[uint64]int // 哈希值到后端在 backends 中的索引
	cacheSnapshot
}

// ringHash 实现一致性哈希（Ring Hash/Consistent Hash）算法
// 特点：后端节点变化时，只影响少量请求的路由，最小化迁移
// 原理：将后端映射到哈希环上，使用虚拟节点提高分布均匀性
//
// 性能优化（与同包 p2c 同一 RCU 范式）：读路径经 atomic.Pointer[ringHashData]
// 无锁取快照，消除 RWMutex.RLock 在热路径上的跨核 atomic 写争用
// （Select 每请求触发，是主要矛盾）；rebuild 是冷路径，仍由 mu 串行化。
type ringHash struct {
	data         atomic.Pointer[ringHashData] // 原子指针，支持无锁读取
	mu           sync.Mutex                   // 仅用于 rebuild（后端列表变化时）
	virtualNodes int                          // 虚拟节点数量（创建后不变）
	rebuilds     int                          // buildRing 实际执行次数（仅测试观测用，始终在 mu 内写入）
}

// RingHashOptions configures the ring-hash selector; its VirtualNodes field sets the
// number of ring nodes per backend.
//
// RingHashOptions 配置选项
type RingHashOptions struct {
	// RingSize is ignored and kept only for backward compatibility.
	//
	// Deprecated: RingSize 已废弃且被忽略，保留仅为 API 兼容；请改用 VirtualNodes 控制环规模。
	RingSize     int
	VirtualNodes int // 虚拟节点数量，越多分布越均匀，但占用更多内存
}

// maxVirtualNodes 是 NewRingHash 采纳 VirtualNodes 的上界：1M 虚拟节点/后端使
// buildRing 的 total = len(backends)×virtualNodes 远离 int 溢出域（溢出会使 make
// 负容量在首次 SelectByHash 时 panic，崩溃点与配置点分离），越界输入按包约定
// 回落 DefaultVirtualNodes（由 TestRingHashOptions_VirtualNodesGuard 钉住）。
// 平台前提：「远离溢出域」按 64-bit int 成立（溢出需 len(backends) > 2^43）；
// 32-bit 平台上 len(backends) > 2^11 时 total 仍可能溢出，目标部署面为 64-bit 服务器。
const maxVirtualNodes = 1 << 20

// NewRingHash creates a consistent-hashing (ring hash) selector.
//
// NewRingHash 创建一致性哈希选择器
// ring 与 hashToIdx 延迟到首次 buildRing 时按实际规模分配（见 buildRing 说明）
// VirtualNodes 越出 (0, maxVirtualNodes] 时回落 DefaultVirtualNodes；RingSize 已废弃、恒被忽略
func NewRingHash(opts *RingHashOptions) ConsistentHashSelector {
	r := &ringHash{
		virtualNodes: DefaultVirtualNodes,
	}
	if opts != nil {
		if opts.VirtualNodes > 0 && opts.VirtualNodes <= maxVirtualNodes {
			r.virtualNodes = opts.VirtualNodes
		}
	}
	r.data.Store(&ringHashData{})
	return r
}

// Select 随机选择一个后端（使用一致性哈希）
// 使用随机 key 调用 SelectByHash
// 优化：随机 key 由 randomKey8() 生成，其底层使用 math/rand/v2 的全局无锁 PRNG（ChaCha8），无需自建带 sync.Mutex 的全局 RNG
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
//
// 快速路径：一次 atomic.Pointer.Load 后直接查环，零锁、零分配、零原子写
// 慢速路径：使用后端指纹缓存，仅在后端列表变化时重建哈希环（仅此处加 mu）
func (r *ringHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	return r.getData(backends).lookup(backends, key)
}

// getData 获取当前快照，如后端列表变化则触发重建
func (r *ringHash) getData(backends []Backend) *ringHashData {
	data := r.data.Load()
	ptr := backendsSlicePtr(backends)
	n := len(backends)

	// 快速路径：同一个 slice 指针 + 长度，直接返回
	if ptr == data.slicePtr && n == data.sliceLen {
		return data
	}

	return r.getDataSlow(backends, ptr, n)
}

// getDataSlow 慢路径：检查 fingerprint，必要时重建哈希环
//
// 重建与否只由 fingerprint（内容）决定；slicePtr/sliceLen 每次慢路径无条件更新，
// 避免 fp 匹配但 ptr 失配时永久停留在慢路径
func (r *ringHash) getDataSlow(backends []Backend, ptr uintptr, n int) *ringHashData {
	fp := computeBackendsFingerprint(backends)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check: 重新加载最新状态，避免多个 goroutine 重复重建并互相覆盖
	data := r.data.Load()

	// fingerprint 匹配且环已构建，仅更新缓存的 ptr/len（避免重复建环）
	if fp == data.fingerprint && len(data.ring) > 0 {
		if ptr != data.slicePtr || n != data.sliceLen {
			newData := *data // 浅拷贝，共享 ring/hashToIdx（发布后不再写入）
			newData.slicePtr = ptr
			newData.sliceLen = n
			newData.backends = backends
			r.data.Store(&newData)
			return &newData
		}
		return data
	}

	// fingerprint 不匹配，完整重建
	newData := r.buildRing(backends)
	newData.fingerprint = fp
	newData.slicePtr = ptr
	newData.sliceLen = n
	r.data.Store(newData)
	r.rebuilds++
	return newData
}

// lookup 在快照的哈希环上二分查找 key 对应的后端（热路径，零锁零分配）
//
// 接收者是快照本身：调用方 Load 到的快照在整个函数执行期间不会被修改，
// 因此无需持锁。backends 长度与建环时的后端数由 getData 的 sliceLen 判等保证一致。
func (d *ringHashData) lookup(backends []Backend, key []byte) Backend {
	h := hash64(key)
	idx := sort.Search(len(d.ring), func(i int) bool {
		return d.ring[i] >= h
	})
	if idx >= len(d.ring) {
		idx = 0 // 环形回绕：大于环上最大哈希值的 key 落到首个虚拟节点
	}
	return backends[d.hashToIdx[d.ring[idx]]]
}

// buildRing 构建哈希环，返回尚未发布的新快照（由调用方原子 Store）
// 为每个后端创建 virtualNodes 个虚拟节点，将它们添加到环上
//
// 冷路径去分配优化（与 RCU 快照不可变约束相容的部分）：
//   - ring 与 hashToIdx 按 n×virtualNodes 精确预分配，杜绝增长再分配；
//     但**必须每次全新分配**，不得跨重建复用（见 ringHashData 注释）
//   - 虚拟节点 key 在单次分配的 scratch buffer 中按字节拼装（等价于原 fmt 格式 "%s#%d"），
//     杜绝每 vnode 的 fmt.Sprintf 与字符串拼接分配
func (r *ringHash) buildRing(backends []Backend) *ringHashData {
	total := len(backends) * r.virtualNodes

	ring := make([]uint64, 0, total)
	hashToIdx := make(map[uint64]int, total)

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
			h := probeFreeSlot(hashToIdx, h1, h2)
			ring = append(ring, h)
			hashToIdx[h] = j
		}
	}

	// 对环进行排序，便于二分查找
	slices.Sort(ring)

	return &ringHashData{
		backends:  backends,
		ring:      ring,
		hashToIdx: hashToIdx,
	}
}

// probeFreeSlot 以 double-hashing 探测在 nodeMap 中寻找空闲槽位，返回首个未占用的哈希值
// 参考 Envoy Ring Hash 实现：探测序列为 h1 + k*h2（k = 0, 1, 2, ...，
// uint64 自然溢出即 mod 2^64）
//
// h2 == 0 退化加固：此时探测序列恒等于 h1，若 h1 已被占用则永不终止；
// 该循环处于 getDataSlow 的持写锁路径，死循环会挂起整个 selector 实例。
// xxhash 种子公开且后端地址可来自外部输入（服务发现投毒场景），退化可被构造，
// 故按 Maglev 原论文对 skip 的标准约束将步长退化为 1——仅影响原本会死循环的
// 输入，h2 != 0 的正常探测序列与环产出逐元素不变
// （由 TestRingHash_BuildRingMatchesReference 对照参考实现钉住）。
func probeFreeSlot(nodeMap map[uint64]int, h1, h2 uint64) uint64 {
	if h2 == 0 {
		h2 = 1
	}
	pos := h1
	for {
		if _, exists := nodeMap[pos]; !exists {
			return pos
		}
		// Double-hashing 线性探测：pos += h2（uint64 自然溢出即 mod 2^64）
		pos += h2
	}
}
