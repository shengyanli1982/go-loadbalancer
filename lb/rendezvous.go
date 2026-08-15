package lb

import (
	"math"
	"sync"

	"github.com/cespare/xxhash/v2"
)

const maxUint64Plus1 float64 = 1 << 64 // float64 精确表示 2^64

// rendezvousLogTableSize 查找表条目数（256 条目覆盖 mantissa 的 [1, 2) 区间）
const rendezvousLogTableSize = 256

// negLog2Table 预计算的 -log2(x) 表，x 在 [1, 2) 均匀分布
// init 时填充，运行时只读，cache-friendly (257 * 8B ≈ 2KB)
var negLog2Table [rendezvousLogTableSize + 1]float64

func init() {
	for i := 0; i <= rendezvousLogTableSize; i++ {
		m := 1.0 + float64(i)/rendezvousLogTableSize
		negLog2Table[i] = -math.Log2(m)
	}
}

// rendezvous 实现 Rendezvous Hashing（Highest Random Weight）负载均衡算法
//
// 性能优化（对标 Maglev/RingHash 缓存模式）：
//   - 预计算 hash(address) 和权重缓存，SelectByHash 热路径仅需 hash(key) + 整数混合
//   - RWMutex 读锁快速路径 + 写锁慢路径重建，与 Maglev/RingHash 一致
//   - fingerprint + slicePtr 双重缓存检测，避免冗余重建
//   - Select() 使用 math/rand/v2 全局无锁 PRNG，替代 globalRNG 的 sync.Mutex
//
// 算法保证：
//   - 相同 key 始终映射到同一后端（亲和性）
//   - 后端变化时最小化重映射（~1/N 的 key 需要迁移）
//   - 加权公式 score = weight / (-log(normalizedHash)) 保留 Gumbel 分布特性
type rendezvous struct {
	mu                sync.RWMutex
	backends          []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	cachedAddrHashes  []uint64  // 预计算的 hash(address) 缓存，避免每次 Select 重复计算
	cachedWeights     []int     // 权重缓存，rebuild 时填充
	hasUniformWeights bool
	rebuilds          int // rebuildCache 实际执行次数（仅测试观测用，始终在写锁内访问）
	cacheSnapshot
}

// RendezvousSelector 接口，同时支持 Select 和 SelectByHash
type RendezvousSelector interface {
	Selector
	HashSelector
}

// NewRendezvous 创建 Rendezvous Hashing 选择器
func NewRendezvous() RendezvousSelector {
	return &rendezvous{}
}

// Select 使用随机 key 调用 SelectByHash
// 优化：使用 math/rand/v2 全局无锁 PRNG（ChaCha8），替代 globalRNG 的 sync.Mutex
func (r *rendezvous) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	key := randomKey8()
	return r.SelectByHash(backends, key[:])
}

// SelectByHash 使用 Rendezvous Hashing 算法根据 key 选择后端
//
// 快速路径（RLock）：使用预计算的 addrHashes + 缓存权重，仅需 hash(key) + 整数混合
// 慢速路径（Lock）：检测 fingerprint 变化并重建缓存
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

	ptr := backendsSlicePtr(backends)

	// 快速路径：读锁 + slicePtr 检测 → 直接查表
	r.mu.RLock()
	if r.cachedAddrHashes != nil && ptr == r.slicePtr && len(backends) == r.sliceLen {
		result := r.selectFast(backends, key)
		r.mu.RUnlock()
		return result
	}
	r.mu.RUnlock()

	// 慢速路径：fingerprint 检查 + 可能重建缓存
	// 重建与否只由 fingerprint（内容）决定；slicePtr/sliceLen 每次慢路径无条件更新，
	// 避免"同内容新 slice"反复触发重建、或 fp 匹配但 ptr 失配时永久停留在慢路径
	fp := computeWeightedFingerprint(backends)
	r.mu.Lock()
	if r.cachedAddrHashes == nil || fp != r.fingerprint {
		r.rebuildCache(backends, fp, ptr)
		r.rebuilds++
	}
	r.slicePtr = ptr
	r.sliceLen = len(backends)
	r.backends = backends
	result := r.selectFast(backends, key)
	r.mu.Unlock()
	return result
}

// selectFast 热路径：使用预计算的 addrHashes 和 weights，零分配
// 每次仅需一次 xxhash.Sum64(key) + N 次整数混合 + N 次 fastNegLog2 查找表查询
func (r *rendezvous) selectFast(backends []Backend, key []byte) Backend {
	keyHash := xxhash.Sum64(key)

	if r.hasUniformWeights {
		bestIdx := 0
		bestHash := uint64(0)
		for i, ah := range r.cachedAddrHashes {
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

	for i, ah := range r.cachedAddrHashes {
		combined := hashCombine(keyHash, ah)
		// fastNegLog2: -log2((combined+1)/2^64) via IEEE 754 Float64bits
		// score = weight / (-log(combined+1/2^64)) = weight / (fastNegLog2 * math.Ln2)
		negLog := fastNegLog2((float64(combined)+1.0)/maxUint64Plus1) * math.Ln2
		score := float64(r.cachedWeights[i]) / negLog
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

// rebuildCache 重建预计算缓存（已持有写锁）
// 预计算每个后端的 hash(address) 和权重，存入切片供 selectFast 使用
func (r *rendezvous) rebuildCache(backends []Backend, fp uint64, ptr uintptr) {
	n := len(backends)

	// 复用已有切片容量，避免堆分配（同 EDF/SWRR 模式）
	r.cachedAddrHashes = resizeSlice(r.cachedAddrHashes, n)
	r.cachedWeights = resizeSlice(r.cachedWeights, n)

	for i, b := range backends {
		// 预计算 hash(address)，使用 Sum64String 避免 []byte 转换
		r.cachedAddrHashes[i] = xxhash.Sum64String(b.Address())
		r.cachedWeights[i] = getWeight(b)
	}

	r.hasUniformWeights = allWeightsEqual(r.cachedWeights)
	r.fingerprint = fp
	r.slicePtr = ptr
	r.sliceLen = n
}

// fastNegLog2 计算 -log2(x) 的快速近似值 (x ∈ (0, 1])
// 使用 IEEE 754 bit 操作提取指数 + 预计算 mantissa 查找表 + 线性插值
//
// 精度: ~13 bits，足够负载均衡后端选择（score 差异通常 <1e-4）
func fastNegLog2(x float64) float64 {
	if x <= 0 {
		return 64 // 最大有效值
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
