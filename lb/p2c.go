package lb

import (
	"math/rand"
	"sync/atomic"
)

// p2c 实现 Power of Two Choices (P2C) 负载均衡算法
// 随机选择两个后端，然后选择负载较低的哪个
type p2c struct {
	rng   *rand.Rand               // 随机数生成器
	loads map[string]*atomic.Int64 // 记录每个后端的当前负载
}

// NewP2C 创建新的 P2C 负载均衡选择器
// 使用固定的种子值0，保证可复现的随机序列
func NewP2C() Selector {
	return &p2c{
		rng:   rand.New(rand.NewSource(0)),
		loads: make(map[string]*atomic.Int64),
	}
}

// Select 使用 P2C 算法选择一个后端服务器
// 1. 如果只有一个后端，直接返回
// 2. 随机选择两个不同的后端
// 3. 比较两个后端的负载，选择负载较低的那个
func (p *p2c) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	// 只有一个后端，直接返回并增加其负载计数
	if len(backends) == 1 {
		addr := backends[0].Address()
		load, ok := p.loads[addr]
		if !ok {
			var newLoad atomic.Int64
			newLoad.Store(1)
			p.loads[addr] = &newLoad
			return backends[0]
		}
		load.Add(1)
		return backends[0]
	}

	// 随机选择两个不同的后端
	idx1 := p.rng.Intn(len(backends))
	idx2 := (idx1 + 1 + p.rng.Intn(len(backends)-1)) % len(backends)

	b1, b2 := backends[idx1], backends[idx2]
	addr1, addr2 := b1.Address(), b2.Address()

	// 获取或创建第一个后端的负载计数器
	load1, ok1 := p.loads[addr1]
	if !ok1 {
		var newLoad atomic.Int64
		newLoad.Store(1)
		p.loads[addr1] = &newLoad
		load1 = p.loads[addr1]
	}
	// 获取或创建第二个后端的负载计数器
	load2, ok2 := p.loads[addr2]
	if !ok2 {
		var newLoad atomic.Int64
		newLoad.Store(1)
		p.loads[addr2] = &newLoad
		load2 = p.loads[addr2]
	}

	// 选择负载较低的后端，并增加其负载计数
	if load1.Load() <= load2.Load() {
		load1.Add(1)
		return b1
	}
	load2.Add(1)
	return b2
}
