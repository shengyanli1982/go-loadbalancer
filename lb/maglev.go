package lb

import (
	"sync"
	"sync/atomic"
)

// maglevData 是 maglev 的不可变快照，持有全部依赖后端拓扑的状态。
// 由 atomic.Pointer 原子发布，读者 Load 之后无需加锁；旧快照交给 GC 回收。
//
// 快照不可变约束：table 每次重建都必须全新分配，禁止用 resizeSlice 复用旧底层
// 数组——复用会就地改写仍被在途读者持有的旧快照，读者会读到"新旧拓扑混合"的槽位
// （静默误路由），由 TestRCU_Maglev_SnapshotImmutableAcrossRebuild 钉住。
// 代价是每次重建重新分配 tableSize×8B（默认 524KB）：重建是冷路径（仅拓扑变更
// 触发，生产环境秒级/分钟级频率），而 Select 是每请求触发的热路径，
// 冷热权衡偏向热路径的无锁读取。
type maglevData struct {
	backends []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（随快照原子持有）
	table    []int     // 查找表，长度为 tableSize
	cacheSnapshot
}

// maglev 实现 Maglev 一致性哈希算法
// 特点：Google 论文实现，O(1) 查找速度，空间效率高
// 原理：使用查找表（lookup table）实现快速路由
//
// 性能优化（与同包 p2c 同一 RCU 范式）：读路径经 atomic.Pointer[maglevData]
// 无锁取快照，消除 RWMutex.RLock 在热路径上的跨核 atomic 写争用
// （Select 每请求触发，是主要矛盾）；rebuild 是冷路径，仍由 mu 串行化。
type maglev struct {
	data      atomic.Pointer[maglevData] // 原子指针，支持无锁读取
	mu        sync.Mutex                 // 仅用于 rebuild（后端列表变化时）
	tableSize int                        // 表大小（创建后不变）
	rebuilds  int                        // buildTable 实际执行次数（仅测试观测用，始终在 mu 内写入）
}

// MaglevOptions configures the Maglev selector.
//
// MaglevOptions 配置选项
type MaglevOptions struct {
	// TableSize is the size of the Maglev lookup table and should be prime.
	TableSize int // 查找表大小，应为质数以获得更好的分布
}

// maxMaglevTableSize 是 NewMaglev 采纳 TableSize 的上界：8M 槽 × 8B ≈ 64MB 内存上限
// （默认 65537 槽仅 ≈ 524KB），同时把 isPrime 试除规模钉死在 i*i 不溢出的安全域内，
// 越界输入按包约定回落 DefaultMaglevTableSize（由 TestMaglevOptions_TableSizeGuard 钉住）。
const maxMaglevTableSize = 1 << 23

// NewMaglev creates a Maglev consistent-hashing selector.
//
// NewMaglev 创建 Maglev 选择器
// table 延迟到首次 buildTable 时按 tableSize 分配
// TableSize 越出 [2, maxMaglevTableSize] 时回落 DefaultMaglevTableSize；区间内非质数上修至最近质数
func NewMaglev(opts *MaglevOptions) ConsistentHashSelector {
	size := DefaultMaglevTableSize
	if opts != nil && opts.TableSize >= 2 && opts.TableSize <= maxMaglevTableSize {
		size = opts.TableSize
		if !isPrime(size) {
			size = nextPrime(size)
		}
	}
	m := &maglev{
		tableSize: size,
	}
	m.data.Store(&maglevData{})
	return m
}

// Select 随机选择一个后端（使用 Maglev 算法）
// 使用随机 key 调用 SelectByHash
// 优化：随机 key 由 randomKey8() 生成，其底层使用 math/rand/v2 的全局无锁 PRNG（ChaCha8），无需自建带 sync.Mutex 的全局 RNG
func (m *maglev) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	key := randomKey8()
	return m.SelectByHash(backends, key[:])
}

// SelectByHash 使用 Maglev 算法选择一个后端
// 算法：
// 1. 计算 key 的哈希值
// 2. 取模表大小得到索引
// 3. 从查找表中获取对应的后端索引
//
// 快速路径：一次 atomic.Pointer.Load 后直接查表，零锁、零分配、零原子写
// 慢速路径：使用 slice 指针+指纹快速检测后端变化，仅在变化时重建查找表（仅此处加 mu）
func (m *maglev) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	return m.getData(backends).lookup(backends, key, m.tableSize)
}

// getData 获取当前快照，如后端列表变化则触发重建
func (m *maglev) getData(backends []Backend) *maglevData {
	data := m.data.Load()
	ptr := backendsSlicePtr(backends)
	n := len(backends)

	// 快速路径：同一个 slice 指针 + 长度，直接返回
	if ptr == data.slicePtr && n == data.sliceLen {
		return data
	}

	return m.getDataSlow(backends, ptr, n)
}

// getDataSlow 慢路径：检查 fingerprint，必要时重建查找表
//
// 重建与否只由 fingerprint（内容）决定；slicePtr/sliceLen 每次慢路径无条件更新，
// 避免"同内容新 slice"反复触发全量重建、或 fp 匹配但 ptr 失配时永久停留在慢路径
func (m *maglev) getDataSlow(backends []Backend, ptr uintptr, n int) *maglevData {
	fp := computeBackendsFingerprint(backends)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check: 重新加载最新状态，避免多个 goroutine 重复重建并互相覆盖
	data := m.data.Load()

	// fingerprint 匹配且表已构建，仅更新缓存的 ptr/len（避免重复建表）
	if fp == data.fingerprint && data.table != nil {
		if ptr != data.slicePtr || n != data.sliceLen {
			newData := *data // 浅拷贝，共享 table（发布后不再写入）
			newData.slicePtr = ptr
			newData.sliceLen = n
			newData.backends = backends
			m.data.Store(&newData)
			return &newData
		}
		return data
	}

	// fingerprint 不匹配，完整重建
	newData := m.buildTable(backends)
	newData.fingerprint = fp
	newData.slicePtr = ptr
	newData.sliceLen = n
	m.data.Store(newData)
	m.rebuilds++
	return newData
}

// lookup 从快照的查找表解析 key 对应的后端（热路径，零锁零分配）
//
// 接收者是快照本身：调用方 Load 到的快照在整个函数执行期间不会被修改，
// 因此无需持锁。backends 长度与建表时的后端数由 getData 的 sliceLen 判等保证一致，
// 越界兜底回退 backends[0] 与改造前语义相同。
func (d *maglevData) lookup(backends []Backend, key []byte, tableSize int) Backend {
	idx := hash64(key) % uint64(tableSize)
	if result := d.table[idx]; result >= 0 && result < len(backends) {
		return backends[result]
	}
	return backends[0]
}

// buildTable 构建 Maglev 查找表，返回尚未发布的新快照（由调用方原子 Store）
// 算法：为每个后端计算 offset 和 skip，使用轮询填充算法
// 参考 Google 论文 "Maglev: A Fast and Reliable Software Network Load Balancer"
//
// 冷路径去分配与降低取模开销（产出与未优化版逐元素一致，由
// TestMaglev_BuildTableMatchesReference 对照固化）：
//   - 探测游标与 skips 合并为单次 2n 分配
//   - 内层循环的 % tableSize 是「除数为运行期变量」的取模开销（~9 万次/重建），
//     改为增量模加：c_{k+1} = (c_k + skip) mod d 与 c_k = (offset + k·skip) mod d
//     归纳等价（模加结合律），c+skip < 2d 用一次加法+条件减实现，任意 d 精确。
//     不采用 Lemire fastmod：其精确域为 x·((−2^64) mod d) < 2^64，大表
//     （tableSize > 2^21）下 x 接近 tableSize² 时失效，且成本高于增量模加
//
// 已放弃的优化：table 容量跨重建复用（resizeSlice）。它与 RCU 快照不可变性
// 直接冲突——等长拓扑交替时 cap 恒足够，复用会让新一轮填表写穿在途读者仍持有的
// 旧快照。见 maglevData 注释的冷热权衡说明。
func (m *maglev) buildTable(backends []Backend) *maglevData {
	n := len(backends)

	if n == 0 {
		return &maglevData{backends: backends}
	}

	table := make([]int, m.tableSize)
	for i := range table {
		table[i] = -1
	}

	// 为每个后端计算 offset（即首个探测游标）和 skip（与游标共用单次 2n 分配）
	tmp := make([]int, 2*n)
	cursor := tmp[:n:n]
	skips := tmp[n:]
	for i, b := range backends {
		cursor[i] = int(hash64([]byte("offset:"+b.Address())) % uint64(m.tableSize))
		skips[i] = int(hash64([]byte("skip:"+b.Address()))%uint64(m.tableSize-1)) + 1
	}

	// 轮询填充算法：cursor[i] 沿 (offset + k·skip) mod tableSize 序列就地推进。
	// 增量模加与原式归纳等价（模加结合律）：因 c_k < tableSize 且 skip <= tableSize-1，
	// 故 c_k + skip < 2·tableSize，一次条件减即完成精确归约。相比原式每次迭代一次
	// 「除数为运行期变量」的取模，此处以加法+条件减替代除法（strength reduction：
	// 除法 → 加法）。
	for filled := 0; filled < m.tableSize; {
		for i := 0; i < n; i++ {
			c := cursor[i] // 本轮探测槽位 c_k
			nxt := c + skips[i]
			if nxt >= m.tableSize {
				nxt -= m.tableSize // c_{k+1} = (c_k + skip) mod tableSize（c+skip < 2d）
			}
			cursor[i] = nxt
			if table[c] < 0 {
				table[c] = i
				filled++
				if filled >= m.tableSize {
					break
				}
			}
		}
	}

	return &maglevData{
		backends: backends,
		table:    table,
	}
}

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	if n < 4 {
		return true
	}
	if n%2 == 0 || n%3 == 0 {
		return false
	}
	for i := 5; i*i <= n; i += 6 {
		if n%i == 0 || n%(i+2) == 0 {
			return false
		}
	}
	return true
}

func nextPrime(n int) int {
	if n <= 2 {
		return 2
	}
	if n%2 == 0 {
		n++
	}
	for !isPrime(n) {
		n += 2
	}
	return n
}
