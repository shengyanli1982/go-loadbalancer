package lb

import (
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
	_ "unsafe" // for go:linkname
)

// nanotime 直接链接 runtime.nanotime 读取单调时钟（纳秒），用于摊销式衰减计时，避免 time.Now() 开销。
// 该 //go:linkname 反向引用受运行时官方承诺保护：GOROOT/src/runtime/time_nofake.go 明写
// “Do not remove or change the type signature”（golang/go issue 67401），故不会因运行时演进而被单方面破坏。
// //go:noescape 对无指针参数的函数（nanotime 无参、返回 int64，不存在指针逃逸）是空操作，
// 此处保留仅为与运行时侧声明约定一致。
//
//go:noescape
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// cacheSnapshot 封装后端列表的缓存检测元数据。
// 通过比较 fingerprint / slicePtr / sliceLen 判断是否需要重建。
type cacheSnapshot struct {
	fingerprint uint64  // 后端列表指纹
	slicePtr    uintptr // 后端 slice 底层数组地址
	sliceLen    int     // 后端 slice 长度
}

// p2cData 包含 P2C 选择器的所有可变状态。
// 通过 atomic.Pointer 实现无锁读取，仅在重建时加锁。
type p2cData struct {
	backends   []Backend      // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（随快照原子持有）
	loadCounts []atomic.Int64 // 按位置索引的负载计数（Select 快速路径）
	addrs      []string       // 按位置缓存的后端地址
	addrIndex  map[string]int // 地址到位置的映射（Release O(1) 查找）
	cacheSnapshot
}

// p2c 实现 Power of Two Choices (P2C) 负载均衡算法
// 特点：随机选择两个后端，选择负载较低的一个
//
// 性能优化：
//   - 使用 atomic.Pointer[p2cData] 实现 Select 快速路径无锁读取
//   - 使用 loads []atomic.Int64 slice 按索引 O(1) 访问，替代 sync.Map
//   - 使用 nextDecay + CAS 减少原子写操作频率（快速路径仅 Load，无 Swap）
type p2c struct {
	data      atomic.Pointer[p2cData] // 原子指针，支持无锁读取
	decay     float64                 // 负载衰减因子（创建后不变）
	nextDecay atomic.Int64            // 下次衰减时间点（纳秒）
	mu        sync.Mutex              // 仅用于 rebuild（后端列表变化时）
}

// P2COptions configures the Power of Two Choices selector.
//
// P2COptions 配置选项
type P2COptions struct {
	// Decay is the load decay factor; a smaller value decays faster.
	Decay float64 // 负载衰减因子，值越小衰减越快
}

// NewP2C creates a Power of Two Choices selector with default options.
//
// NewP2C 创建 P2C 选择器（使用默认配置）
func NewP2C() TrackedSelector {
	return NewP2CWithOptions(nil)
}

// defaultP2CDecay 是 P2C 的默认负载衰减因子（每秒衰减 10%）。
// 作为算法内部默认值置于本文件，而非 const.go（后者只放导出常量）。
const defaultP2CDecay = 0.9

// NewP2CWithOptions creates a Power of Two Choices selector with the given options.
//
// NewP2CWithOptions 创建 P2C 选择器（可自定义配置）
func NewP2CWithOptions(opts *P2COptions) TrackedSelector {
	decay := defaultP2CDecay
	if opts != nil && opts.Decay > 0 && opts.Decay < 1 {
		decay = opts.Decay
	}
	p := &p2c{
		decay: decay,
	}
	p.data.Store(&p2cData{})
	p.nextDecay.Store(nanotime() + int64(time.Second))
	return p
}

// Select 使用 P2C 算法选择一个后端
// 算法：
// 1. 随机选择两个不同的后端
// 2. 比较两个后端的负载
// 3. 选择负载较低的后端
// 4. 如果超过1秒没有衰减，对负载进行指数衰减
func (p *p2c) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	// 单后端直接返回
	if len(backends) == 1 {
		data := p.getData(backends)
		p.applyDecay(data)
		data.loadCounts[0].Add(1)
		return backends[0]
	}

	data := p.getData(backends)

	// 随机选择两个不同的后端（O(1) 无循环）
	// 单次 rand.Uint64 派生两个索引，替代两次 rand.IntN 的完整抽样开销；
	// 低/高 32 位近似独立，取模偏差量级 n/2^32，对负载均衡可忽略
	n := len(backends)
	u := rand.Uint64()

	// 摊销时钟读取：复用随机数低位，约 1/16 概率才检查衰减
	// 衰减触发平均延迟约 16 次选择（秒级衰减粒度下漂移可忽略）
	if u&0xf == 0 {
		p.applyDecay(data)
	}
	idx1 := int(u % uint64(n))
	idx2 := int((u >> 32) % uint64(n-1))
	if idx2 >= idx1 {
		idx2++
	}

	// 使用 slice 按索引 O(1) 访问，无锁
	load1 := data.loadCounts[idx1].Load()
	load2 := data.loadCounts[idx2].Load()

	// 选择负载较低的后端
	if load1 <= load2 {
		data.loadCounts[idx1].Add(1)
		return backends[idx1]
	}
	data.loadCounts[idx2].Add(1)
	return backends[idx2]
}

// getData 获取当前状态，如后端列表变化则触发重建
func (p *p2c) getData(backends []Backend) *p2cData {
	data := p.data.Load()
	ptr := backendsSlicePtr(backends)
	n := len(backends)

	// 快速路径：同一个 slice 指针 + 长度，直接返回
	if ptr == data.slicePtr && n == data.sliceLen {
		return data
	}

	return p.getDataSlow(backends, data, ptr, n)
}

// getDataSlow 慢路径：检查 fingerprint，必要时重建
func (p *p2c) getDataSlow(backends []Backend, data *p2cData, ptr uintptr, n int) *p2cData {
	fp := computeBackendsFingerprint(backends)

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check: 重新加载最新状态
	data = p.data.Load()

	// fingerprint 匹配且结构已初始化，仅更新缓存的 ptr/len（避免重复计算 fingerprint）
	if fp == data.fingerprint && data.loadCounts != nil {
		if ptr != data.slicePtr || n != data.sliceLen {
			newData := *data // 浅拷贝，共享 loads/addrs/addrIndex
			newData.slicePtr = ptr
			newData.sliceLen = n
			newData.backends = backends
			p.data.Store(&newData)
			return &newData
		}
		return data
	}

	// fingerprint 不匹配，完整重建
	newData := p.rebuildData(backends, fp, ptr, data)
	p.data.Store(newData)
	return newData
}

// rebuildData 重建状态数据，迁移已有负载
func (p *p2c) rebuildData(backends []Backend, fp uint64, ptr uintptr, oldData *p2cData) *p2cData {
	n := len(backends)
	newData := &p2cData{
		backends:   backends,
		loadCounts: make([]atomic.Int64, n),
		addrs:      make([]string, n),
		addrIndex:  make(map[string]int, n),
		cacheSnapshot: cacheSnapshot{
			fingerprint: fp,
			slicePtr:    ptr,
			sliceLen:    n,
		},
	}

	for i, b := range backends {
		addr := b.Address()
		newData.addrs[i] = addr
		newData.addrIndex[addr] = i

		// 迁移已有负载（如果后端存在于旧状态）
		if oldIdx, ok := oldData.addrIndex[addr]; ok {
			newData.loadCounts[i].Store(oldData.loadCounts[oldIdx].Load())
		}
	}

	return newData
}

// applyDecay 对所有后端的负载进行指数衰减
// 优化：快速路径仅 atomic.Load 检查时间，无原子写入
// 通过 CAS 确保同一周期内只有一个 goroutine 执行衰减
// 推进式语义：空闲多个周期后一次补足缺失周期数（上限截断），
// CAS 目标基于 nextDecay 推进而非 now，保证并发下不丢周期
func (p *p2c) applyDecay(data *p2cData) {
	const maxDecayPeriods = 60 // 周期数上限：防 math.Pow 指数过大与结果下溢失真
	period := int64(time.Second)

	now := nanotime()
	next := p.nextDecay.Load()

	// 快速路径：尚未到衰减时间，仅一次 atomic.Load
	if now < next {
		return
	}

	// CAS 确保只有一个 goroutine 对每个缺失周期段执行衰减
	var periods int64
	for {
		periods = (now-next)/period + 1
		if periods > maxDecayPeriods {
			periods = maxDecayPeriods
		}
		if p.nextDecay.CompareAndSwap(next, next+periods*period) {
			break
		}
		// CAS 失败：其他 goroutine 已推进，重新读取后再判定
		next = p.nextDecay.Load()
		if now < next {
			return
		}
	}

	// 对所有负载进行指数衰减（periods 个周期合并为一次乘法）
	factor := math.Pow(p.decay, float64(periods))
	for i := range data.loadCounts {
		for {
			current := data.loadCounts[i].Load()
			decayed := int64(float64(current) * factor)
			if data.loadCounts[i].CompareAndSwap(current, decayed) {
				break
			}
		}
	}
}

// Release 释放一个后端的负载
// 在请求完成后调用此方法，减少该后端的负载计数
func (p *p2c) Release(backend Backend) {
	if backend == nil {
		return
	}
	data := p.data.Load()
	addr := backend.Address()

	idx, ok := data.addrIndex[addr]
	if !ok {
		return
	}

	load := &data.loadCounts[idx]
	for {
		current := load.Load()
		if current <= 0 {
			return
		}
		if load.CompareAndSwap(current, current-1) {
			return
		}
	}
}
