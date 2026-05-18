package lb

import (
	"math/rand"
	"sync"
	"time"
)

// random 实现随机负载均衡算法
// 特点：完全随机，无状态，无额外开销
type random struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// NewRandom 创建随机选择器（使用当前时间作为随机种子）
func NewRandom() Selector {
	return &random{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// NewRandomWithSeed 创建随机选择器（使用指定种子）
// 可用于测试场景，确保随机结果可复现
func NewRandomWithSeed(seed int64) Selector {
	return &random{
		rng: rand.New(rand.NewSource(seed)),
	}
}

// Select 随机选择一个后端
func (r *random) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	r.mu.Lock()
	idx := r.rng.Intn(len(backends))
	r.mu.Unlock()
	return backends[idx]
}
