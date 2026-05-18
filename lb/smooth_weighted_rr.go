package lb

import "sync"

// smoothWeightedRR 实现平滑加权轮询算法（Nginx 风格）
// 特点：权重越高被选中的概率越大，但不会连续选中同一后端
// 原理：每次选择后，当前权重会减去总权重，实现平滑分布
type smoothWeightedRR struct {
	mu              sync.Mutex
	backends        []Backend
	currentWeight   []int // 当前权重，随选择动态变化
	effectiveWeight []int // 有效权重（固定）
	totalWeight     int   // 总权重
	backendsLen     int   // 后端列表长度
}

// NewSmoothWeightedRR 创建平滑加权轮询选择器
func NewSmoothWeightedRR() Selector {
	return &smoothWeightedRR{}
}

// Select 使用平滑加权轮询算法选择一个后端
// 算法：每个后端的当前权重等于有效权重加上一次选择后的值，
//
//	每次选择当前权重最大的后端，然后减去总权重
func (s *smoothWeightedRR) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 检测后端列表是否变化，如果是则重新初始化
	if s.backendsLen != len(backends) {
		s.backends = make([]Backend, len(backends))
		s.currentWeight = make([]int, len(backends))
		s.effectiveWeight = make([]int, len(backends))
		s.totalWeight = 0
		for i, b := range backends {
			s.backends[i] = b
			wb, ok := b.(WeightedBackend)
			w := 1
			if ok {
				wt := wb.Weight()
				if wt > 0 {
					w = wt
				}
			}
			s.effectiveWeight[i] = w
			s.currentWeight[i] = 0
			s.totalWeight += w
		}
		s.backendsLen = len(backends)
	}

	// 找到当前权重最大的后端
	maxWeight := 0
	selected := 0
	for i := range s.currentWeight {
		s.currentWeight[i] += s.effectiveWeight[i]
		if s.currentWeight[i] > maxWeight {
			maxWeight = s.currentWeight[i]
			selected = i
		}
	}

	// 选中后减去总权重，实现平滑
	s.currentWeight[selected] -= s.totalWeight

	return s.backends[selected]
}
