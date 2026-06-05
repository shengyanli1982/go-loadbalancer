package lb

import (
	"math/rand/v2"
	"sync"
)

// lockedRNG 是带互斥锁的随机数生成器
// 保证在多 goroutine 环境下安全使用 *rand.Rand 实例
// （math/rand/v2 的 *rand.Rand 非并发安全，需要外部锁保护）
type lockedRNG struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// globalRNGInstance 全局共享的随机数生成器实例
// 使用 PCG 算法（rand/v2 默认），种子来自系统熵源
var globalRNGInstance = &lockedRNG{
	rng: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
}

// globalRNG 返回全局共享的随机数生成器实例
// 供 RingHash.Select 和 Maglev.Select 等需要随机 key 的算法使用
func globalRNG() *lockedRNG {
	return globalRNGInstance
}
