package lb

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
	_ "unsafe" // for go:linkname
)

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
	loads     []atomic.Int64 // 按位置索引的负载计数（Select 快速路径）
	addrs     []string       // 按位置缓存的后端地址
	addrIndex map[string]int // 地址到位置的映射（Release O(1) 查找）
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

// P2CReleaser 接口，用于在请求完成后释放负载
type P2CReleaser interface {
	Release(backend Backend)
}

// P2COptions 配置选项
type P2COptions struct {
	Decay float64 // 负载衰减因子，值越小衰减越快
}

// NewP2C 创建 P2C 选择器（使用默认配置）
func NewP2C() Selector {
	return NewP2CWithOptions(nil)
}

// NewP2CWithOptions 创建 P2C 选择器（可自定义配置）
func NewP2CWithOptions(opts *P2COptions) Selector {
	decay := 0.9 // 默认衰减因子，每秒衰减10%
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
		data.loads[0].Add(1)
		return backends[0]
	}

	data := p.getData(backends)
	p.applyDecay(data)

	// 随机选择两个不同的后端（O(1) 无循环）
	n := len(backends)
	idx1 := rand.IntN(n)
	idx2 := rand.IntN(n - 1)
	if idx2 >= idx1 {
		idx2++
	}

	// 使用 slice 按索引 O(1) 访问，无锁
	load1 := data.loads[idx1].Load()
	load2 := data.loads[idx2].Load()

	// 选择负载较低的后端
	if load1 <= load2 {
		data.loads[idx1].Add(1)
		return backends[idx1]
	}
	data.loads[idx2].Add(1)
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

	// fingerprint 匹配，仅更新缓存的 ptr/len（避免重复计算 fingerprint）
	if fp == data.fingerprint {
		if ptr != data.slicePtr || n != data.sliceLen {
			newData := *data // 浅拷贝，共享 loads/addrs/addrIndex
			newData.slicePtr = ptr
			newData.sliceLen = n
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
		loads:     make([]atomic.Int64, n),
		addrs:     make([]string, n),
		addrIndex: make(map[string]int, n),
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
			newData.loads[i].Store(oldData.loads[oldIdx].Load())
		}
	}

	return newData
}

// applyDecay 对所有后端的负载进行指数衰减
// 优化：快速路径仅 atomic.Load 检查时间，无原子写入
// 通过 CAS 确保同一秒内只有一个 goroutine 执行衰减
func (p *p2c) applyDecay(data *p2cData) {
	now := nanotime()
	next := p.nextDecay.Load()

	// 快速路径：尚未到衰减时间，仅一次 atomic.Load
	if now < next {
		return
	}

	// CAS 确保只有一个 goroutine 执行衰减
	newNext := now + int64(time.Second)
	if !p.nextDecay.CompareAndSwap(next, newNext) {
		return // 其他 goroutine 已处理
	}

	// 对所有负载进行指数衰减
	for i := range data.loads {
		for {
			current := data.loads[i].Load()
			decayed := int64(float64(current) * p.decay)
			if data.loads[i].CompareAndSwap(current, decayed) {
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

	load := &data.loads[idx]
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
