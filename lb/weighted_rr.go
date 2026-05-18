package lb

import "sync"

// weightedRR 实现加权轮询负载均衡算法
// 特点：按权重比例分配流量，可能连续选择高权重后端
type weightedRR struct {
	mu    sync.Mutex
	index int64
}

// NewWeightedRR 创建加权轮询选择器
func NewWeightedRR() Selector {
	return &weightedRR{}
}

// Select 使用加权轮询算法选择一个后端
// 算法：根据权重计算当前位置，顺序遍历找到对应的后端
func (w *weightedRR) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	weights := make([]int, len(backends))
	totalWeight := 0
	for i, b := range backends {
		wb, ok := b.(WeightedBackend)
		if !ok {
			weights[i] = 1
		} else {
			wt := wb.Weight()
			if wt <= 0 {
				wt = 1
			}
			weights[i] = wt
		}
		totalWeight += weights[i]
	}

	if totalWeight == 0 {
		return backends[0]
	}

	w.mu.Lock()
	pos := w.index % int64(totalWeight)
	w.index++
	w.mu.Unlock()

	cumulative := int64(0)
	selected := len(backends) - 1
	for i, weight := range weights {
		cumulative += int64(weight)
		if pos < cumulative {
			selected = i
			break
		}
	}

	return backends[selected]
}

// simpleWeightedBackend 是实现 WeightedBackend 接口的简单示例
type simpleWeightedBackend struct {
	addr   string
	weight int
}

func (b *simpleWeightedBackend) Address() string { return b.addr }
func (b *simpleWeightedBackend) Weight() int     { return b.weight }
