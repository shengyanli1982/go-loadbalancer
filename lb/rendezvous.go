package lb

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
)

const maxUint64Plus1 float64 = 1 << 64 // float64 精确表示 2^64

// rendezvousLogTableSize 查找表条目数（256 条目覆盖 mantissa 的 [1, 2) 区间）
const rendezvousLogTableSize = 256

// negLog2Table 预计算的 -log2(x) 表，x 在 [1, 2) 均匀分布
// 由包级 var 初始化表达式填充（不使用 init()：可测试、初始化依赖显式化），
// 运行时只读，cache-friendly (257 * 8B ≈ 2KB)
var negLog2Table = buildNegLog2Table()

// buildNegLog2Table 构建 -log2(x) 预计算表：第 i 个条目对应 x = 1 + i/256，
// 末条目（i = 256，x = 2）作为最后一个插值区间的右端点
func buildNegLog2Table() [rendezvousLogTableSize + 1]float64 {
	var table [rendezvousLogTableSize + 1]float64
	for i := 0; i <= rendezvousLogTableSize; i++ {
		m := 1.0 + float64(i)/rendezvousLogTableSize
		table[i] = -math.Log2(m)
	}
	return table
}

// rendezvousData 是 rendezvous 的不可变快照，持有全部依赖后端拓扑的状态。
// 由 atomic.Pointer 原子发布，读者 Load 之后无需加锁；旧快照交给 GC 回收。
//
// 快照不可变约束：cachedAddrHashes / cachedWeights 每次重建都必须全新分配，
// 禁止用 resizeSlice 复用旧底层数组——复用会写穿仍被在途读者持有的旧快照，
// 由 TestRCU_Rendezvous_SnapshotImmutableAcrossRebuild 钉住。
type rendezvousData struct {
	backends          []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（随快照原子持有）
	cachedAddrHashes  []uint64  // 预计算的 hash(address) 缓存，避免每次 Select 重复计算
	cachedWeights     []int     // 权重缓存，rebuild 时填充
	hasUniformWeights bool
	cacheSnapshot
}

// rendezvous 实现 Rendezvous Hashing（Highest Random Weight）负载均衡算法
//
// 性能优化（与同包 p2c 同一 RCU 范式）：
//   - 读路径经 atomic.Pointer[rendezvousData] 无锁取快照，消除 RWMutex.RLock
//     在热路径上的跨核 atomic 写争用（Select 每请求触发，是主要矛盾）
//   - 预计算 hash(address) 和权重缓存，SelectByHash 热路径仅需 hash(key) + 整数混合
//   - fingerprint + slicePtr/sliceLen 双重缓存检测，避免冗余重建
//   - Select() 的随机 key 由 randomKey8() 生成，底层是 math/rand/v2 全局无锁 PRNG，无需自建带 sync.Mutex 的全局 RNG
//
// 算法保证：
//   - 相同 key 始终映射到同一后端（亲和性）
//   - 后端变化时最小化重映射（~1/N 的 key 需要迁移）
//   - 加权公式 score = weight / (-log(normalizedHash)) 保留 Gumbel 分布特性
type rendezvous struct {
	data     atomic.Pointer[rendezvousData] // 原子指针，支持无锁读取
	mu       sync.Mutex                     // 仅用于 rebuild（后端列表变化时）
	rebuilds int                            // rebuildCache 实际执行次数（仅测试观测用，始终在 mu 内写入）
}

// NewRendezvous creates a rendezvous-hashing (highest-random-weight) selector.
//
// NewRendezvous 创建 Rendezvous Hashing 选择器
// 地址哈希与权重缓存延迟到首次 rebuildCache 时分配
func NewRendezvous() ConsistentHashSelector {
	r := &rendezvous{}
	r.data.Store(&rendezvousData{})
	return r
}

// Select 使用随机 key 调用 SelectByHash
// 优化：随机 key 由 randomKey8() 生成，其底层使用 math/rand/v2 的全局无锁 PRNG（ChaCha8），无需自建带 sync.Mutex 的全局 RNG
func (r *rendezvous) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	key := randomKey8()
	return r.SelectByHash(backends, key[:])
}

// SelectByHash 使用 Rendezvous Hashing 算法根据 key 选择后端
//
// 快速路径：一次 atomic.Pointer.Load 后直接查表，零锁、零分配、零原子写
// 慢速路径：检测 fingerprint 变化并重建缓存（仅此处加 mu）
//
// 算法：对每个后端计算 score = weight / (-log(normalizedHash))，选最大 score
// 哈希混合使用 splitmix64 finalizer 替代原始 Digest 流式写入，避免堆分配
func (r *rendezvous) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(backends) == 1 {
		return backends[0]
	}
	if len(key) == 0 {
		return backends[0]
	}

	return r.getData(backends).lookup(backends, key)
}

// getData 获取当前快照，如后端列表变化则触发重建
func (r *rendezvous) getData(backends []Backend) *rendezvousData {
	data := r.data.Load()
	ptr := backendsSlicePtr(backends)
	n := len(backends)

	// 快速路径：同一个 slice 指针 + 长度，直接返回
	if ptr == data.slicePtr && n == data.sliceLen {
		return data
	}

	return r.getDataSlow(backends, ptr, n)
}

// getDataSlow 慢路径：检查 fingerprint，必要时重建
//
// 重建与否只由 fingerprint（内容）决定；slicePtr/sliceLen 每次慢路径无条件更新，
// 避免"同内容新 slice"反复触发重建、或 fp 匹配但 ptr 失配时永久停留在慢路径
func (r *rendezvous) getDataSlow(backends []Backend, ptr uintptr, n int) *rendezvousData {
	fp := computeWeightedFingerprint(backends)

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check: 重新加载最新状态，避免多个 goroutine 重复重建并互相覆盖
	data := r.data.Load()

	// fingerprint 匹配且缓存已构建，仅更新缓存的 ptr/len（避免重复计算 fingerprint）
	if fp == data.fingerprint && data.cachedAddrHashes != nil {
		if ptr != data.slicePtr || n != data.sliceLen {
			newData := *data // 浅拷贝，共享 cachedAddrHashes/cachedWeights（发布后不再写入）
			newData.slicePtr = ptr
			newData.sliceLen = n
			newData.backends = backends
			r.data.Store(&newData)
			return &newData
		}
		return data
	}

	// fingerprint 不匹配，完整重建
	newData := r.rebuildCache(backends)
	newData.fingerprint = fp
	newData.slicePtr = ptr
	newData.sliceLen = n
	r.data.Store(newData)
	r.rebuilds++
	return newData
}

// rebuildCache 重建预计算缓存，返回尚未发布的新快照（由调用方原子 Store）
// 预计算每个后端的 hash(address) 和权重，存入全新分配的切片供 lookup 使用
func (r *rendezvous) rebuildCache(backends []Backend) *rendezvousData {
	n := len(backends)

	addrHashes := make([]uint64, n)
	weights := make([]int, n)
	for i, b := range backends {
		// 预计算 hash(address)，使用 Sum64String 避免 []byte 转换
		addrHashes[i] = xxhash.Sum64String(b.Address())
		weights[i] = getWeight(b)
	}

	return &rendezvousData{
		backends:          backends,
		cachedAddrHashes:  addrHashes,
		cachedWeights:     weights,
		hasUniformWeights: allWeightsEqual(weights),
	}
}

// lookup 热路径：使用预计算的 addrHashes 和 weights，零分配、零锁
// 每次仅需一次 xxhash.Sum64(key) + N 次整数混合 + N 次 fastNegLog2 查找表查询
//
// 接收者是快照本身：调用方 Load 到的快照在整个函数执行期间不会被修改，
// 因此无需持锁。backends 长度与 cachedAddrHashes 长度由 getData 的
// sliceLen 判等保证一致。
func (d *rendezvousData) lookup(backends []Backend, key []byte) Backend {
	keyHash := xxhash.Sum64(key)

	if d.hasUniformWeights {
		bestIdx := 0
		bestHash := uint64(0)
		for i, ah := range d.cachedAddrHashes {
			combined := hashCombine(keyHash, ah)
			if combined > bestHash {
				bestHash = combined
				bestIdx = i
			}
		}
		return backends[bestIdx]
	}

	bestIdx := 0
	bestScore := -1.0

	for i, ah := range d.cachedAddrHashes {
		combined := hashCombine(keyHash, ah)
		// fastNegLog2: -log2((combined+1)/2^64) via IEEE 754 Float64bits
		// score = weight / (-log(combined+1/2^64)) = weight / (fastNegLog2 * math.Ln2)
		negLog := fastNegLog2((float64(combined)+1.0)/maxUint64Plus1) * math.Ln2
		score := float64(d.cachedWeights[i]) / negLog
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return backends[bestIdx]
}

// hashCombine 将两个 64 位哈希值混合为一个，使用 splitmix64 finalizer
// 特性：每个输入位以 ~50% 概率影响每个输出位（优秀雪崩效应）
// 非对称：hashCombine(a, b) ≠ hashCombine(b, a)，保留 key 与 address 的角色区分
func hashCombine(h1, h2 uint64) uint64 {
	h := h1 + h2*0x9E3779B97F4A7C15 // 黄金比例乘法打破对称性
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

// fastNegLog2 计算 -log2(x) 的快速近似值 (x ∈ (0, 1))
// 使用 IEEE 754 bit 操作提取指数 + 预计算 mantissa 查找表 + 线性插值
//
// x >= 1.0 时返回极小正值（1e-15）防止 score = weight/0 = +Inf 流量垄断；
// x <= 0 时返回 64（最大有效值）。
//
// 精度: ~13 bits，足够负载均衡后端选择（score 差异通常 <1e-4）
func fastNegLog2(x float64) float64 {
	if x <= 0 {
		return 64 // 最大有效值
	}
	if x >= 1.0 {
		// combined hash 达到/舍入到 uint64 上界 → x = 1.0 → -log2(1.0) = 0
		// → score = weight/0 = +Inf → 流量垄断（确定性的，对固定 key 永久发生）
		// 返回极小正值：score ≈ weight * 1.44e15（大但有限，权重可区分，平局可 RR）
		return 1e-15
	}
	v := math.Float64bits(x)
	e := int((v>>52)&0x7FF) - 1023 // IEEE 754 指数
	m := v & 0x000FFFFFFFFFFFFF    // 52-bit mantissa

	// 查找表索引: 取 mantissa top 8 bits
	idx := (m >> 44) & 0xFF
	frac := float64(m&0xFFFFFFFFFFF) / float64(1<<44) // 线性插值因子 [0, 1)

	// 线性插值: -log2(mantissa) ≈ table[idx] + frac * (table[idx+1] - table[idx])
	negLog2M := negLog2Table[idx] + frac*(negLog2Table[idx+1]-negLog2Table[idx])

	return float64(-e) + negLog2M
}
