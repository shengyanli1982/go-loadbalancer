package lb

import (
	"math/rand"
	"sync"
	"time"
)

// lockedRNG 是线程安全的随机数生成器
// 用于需要随机选择的算法（如 Random、P2C）
type lockedRNG struct {
	mu  sync.Mutex
	rng *rand.Rand
}

// globalRNGInstance 是全局共享的随机数生成器实例
var globalRNGInstance = &lockedRNG{
	rng: rand.New(rand.NewSource(time.Now().UnixNano())),
}

// globalRNG 返回全局随机数生成器实例
func globalRNG() *lockedRNG {
	return globalRNGInstance
}
