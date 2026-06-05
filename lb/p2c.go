package lb

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// p2c 实现 Power of Two Choices (P2C) 负载均衡算法
// 特点：随机选择两个后端，选择负载较低的一个
// 优势：结合了随机性和负载均衡，在大规模分布式系统中表现优秀
type p2c struct {
	mu       sync.Mutex
	rng      *rand.Rand
	loads    sync.Map     // 使用 atomic Int64 记录每个后端的负载
	decay    float64      // 负载衰减因子
	lastTime atomic.Int64 // 上次衰减时间
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
		rng:   rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
		decay: decay,
	}
	p.lastTime.Store(time.Now().UnixNano())
	return p
}

// Select 使用 P2C 算法选择一个后端
// 算法：
// 1. 随机选择两个不同的后端
// 2. 比较两个后端的负载（连接数/权重）
// 3. 选择负载较低的后端
// 4. 如果超过1秒没有更新，对负载进行指数衰减
func (p *p2c) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	// 单后端直接返回
	if len(backends) == 1 {
		addr := backends[0].Address()
		p.applyDecay()
		load := p.getOrCreateLoad(addr)
		load.Add(1)
		return backends[0]
	}

	p.applyDecay()

	// 随机选择两个不同的后端
	p.mu.Lock()
	idx1 := p.rng.IntN(len(backends))
	idx2 := p.rng.IntN(len(backends))
	for idx2 == idx1 {
		idx2 = p.rng.IntN(len(backends))
	}
	p.mu.Unlock()

	b1, b2 := backends[idx1], backends[idx2]
	addr1, addr2 := b1.Address(), b2.Address()

	load1 := p.getOrCreateLoad(addr1)
	load2 := p.getOrCreateLoad(addr2)

	// 选择负载较低的后端
	if load1.Load() <= load2.Load() {
		load1.Add(1)
		return b1
	}
	load2.Add(1)
	return b2
}

// getOrCreateLoad 获取或创建后端的负载计数器
func (p *p2c) getOrCreateLoad(addr string) *atomic.Int64 {
	if v, ok := p.loads.Load(addr); ok {
		return v.(*atomic.Int64)
	}
	newLoad := &atomic.Int64{}
	v, loaded := p.loads.LoadOrStore(addr, newLoad)
	if loaded {
		return v.(*atomic.Int64)
	}
	return newLoad
}

// applyDecay 对所有后端的负载进行指数衰减
// 每秒衰减一次，衰减因子由 decay 决定
func (p *p2c) applyDecay() {
	now := time.Now().UnixNano()
	last := p.lastTime.Swap(now)
	elapsed := now - last
	if elapsed > int64(time.Second) {
		p.loads.Range(func(key, value any) bool {
			load := value.(*atomic.Int64)
			current := load.Load()
			decayed := int64(float64(current) * p.decay)
			load.Store(decayed)
			return true
		})
	}
}

// Release 释放一个后端的负载
// 在请求完成后调用此方法，减少该后端的负载计数
func (p *p2c) Release(backend Backend) {
	if backend == nil {
		return
	}
	addr := backend.Address()
	if v, ok := p.loads.Load(addr); ok {
		load := v.(*atomic.Int64)
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
}
