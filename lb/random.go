package lb

import (
	"math/rand/v2"
	"sync"
)

// random 实现随机负载均衡算法
// 特点：完全随机，无状态，无额外开销
type random struct {
	mu      sync.Mutex
	rng     *rand.Rand
	hasSeed bool
}

// NewRandom creates a random selector backed by the global lock-free math/rand/v2 PRNG.
//
// NewRandom 创建随机选择器。
// 使用 math/rand/v2 的全局无锁 PRNG：该全局源由运行时熵播种，结果不可复现
// （rand/v2 自 Go 1.20 起已弃用「时间种子」语义，故并非「使用当前时间作为随机种子」）。
// 需要可复现的随机序列（如测试）请改用 NewRandomWithSeed。
func NewRandom() Selector {
	return &random{}
}

// NewRandomWithSeed creates a random selector that reproduces the same sequence for a given seed.
//
// NewRandomWithSeed 创建随机选择器（使用指定种子）
// 可用于测试场景，确保随机结果可复现
func NewRandomWithSeed(seed int64) Selector {
	seed1 := uint64(seed)
	seed2 := seed1 ^ 0x9E3779B97F4A7C15
	return &random{
		rng:     rand.New(rand.NewPCG(seed1, seed2)),
		hasSeed: true,
	}
}

// Select 随机选择一个后端
func (r *random) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	if r.hasSeed {
		r.mu.Lock()
		idx := r.rng.IntN(len(backends))
		r.mu.Unlock()
		return backends[idx]
	}
	return backends[rand.IntN(len(backends))]
}
