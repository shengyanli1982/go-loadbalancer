package lb

// roundRobin 实现轮询负载均衡算法
// 轮询算法按顺序依次选择每个后端服务器
type roundRobin struct {
	index int // 当前选择的索引位置
}

// NewRoundRobin 创建新的轮询负载均衡选择器
func NewRoundRobin() Selector {
	return &roundRobin{index: 0}
}

// Select 从后端列表中选择一个后端服务器
// 采用轮询策略，每次调用选择下一个后端
func (r *roundRobin) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	// 如果索引超出范围，重置为0
	if r.index >= len(backends) {
		r.index = 0
	}
	backend := backends[r.index]
	r.index++
	return backend
}
