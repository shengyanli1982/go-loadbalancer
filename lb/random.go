package lb

import (
	"math/rand"
)

// random 实现随机负载均衡算法
// 随机选择一个后端服务器处理请求
type random struct {
	rng *rand.Rand // 随机数生成器
}

// NewRandom 创建新的随机负载均衡选择器
// 使用固定的种子值0，保证可复现的随机序列
func NewRandom() Selector {
	return &random{
		rng: rand.New(rand.NewSource(0)),
	}
}

// Select 从后端列表中随机选择一个后端服务器
func (r *random) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	return backends[r.rng.Intn(len(backends))]
}
