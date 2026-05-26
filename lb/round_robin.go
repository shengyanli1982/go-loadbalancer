package lb

import "sync/atomic"

// roundRobin 实现轮询负载均衡算法
// 特点：简单高效，请求均匀分配
type roundRobin struct {
	index atomic.Uint64
}

// NewRoundRobin 创建轮询选择器
func NewRoundRobin() Selector {
	return &roundRobin{}
}

// Select 使用轮询算法选择一个后端
// 算法：顺序遍历后端列表，周而复始
func (r *roundRobin) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	n := r.index.Add(1)
	return backends[(n-1)%uint64(len(backends))]
}
