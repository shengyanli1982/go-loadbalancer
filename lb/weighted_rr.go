package lb

// weightedRR 实现加权轮询负载均衡算法
// 根据后端的权重比例分配请求
type weightedRR struct {
	index       int   // 当前选择的索引
	weights     []int // 每个后端的权重值
	totalWeight int   // 所有后端的总权重
}

// NewWeightedRR 创建新的加权轮询负载均衡选择器
func NewWeightedRR() Selector {
	return &weightedRR{}
}

// Select 从后端列表中选择一个后端服务器
// 根据后端的权重比例计算累计权重，然后选择对应的后端
func (w *weightedRR) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	// 如果权重数组长度不匹配后端列表，重新计算权重
	if len(w.weights) != len(backends) {
		w.weights = make([]int, len(backends))
		w.totalWeight = 0
		for i, b := range backends {
			wb, ok := b.(WeightedBackend)
			if !ok {
				// 如果后端不支持权重接口，默认权重为1
				w.weights[i] = 1
			} else {
				w.weights[i] = wb.Weight()
			}
			w.totalWeight += w.weights[i]
		}
	}

	// 如果总权重为0，返回第一个后端
	if w.totalWeight == 0 {
		return backends[0]
	}

	// 计算当前位置
	pos := w.index % w.totalWeight
	w.index++

	// 遍历权重，找到对应的后端
	cumulative := 0
	for i, weight := range w.weights {
		cumulative += weight
		if pos < cumulative {
			return backends[i]
		}
	}

	// 兜底返回最后一个后端
	return backends[len(backends)-1]
}

// simpleWeightedBackend 实现简单的带权重后端
type simpleWeightedBackend struct {
	addr   string // 后端地址
	weight int    // 权重值
}

// Address 返回后端地址
func (b *simpleWeightedBackend) Address() string { return b.addr }

// Weight 返回后端权重
func (b *simpleWeightedBackend) Weight() int { return b.weight }
