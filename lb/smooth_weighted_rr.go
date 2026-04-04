package lb

import "sync"

// smoothWeightedRR 实现平滑加权轮询负载均衡算法
// 该算法在加权轮询的基础上增加了平滑机制，使权重分配更加均匀
type smoothWeightedRR struct {
	mu              sync.Mutex // 互斥锁，保证并发安全
	backends        []Backend  // 后端服务器列表
	currentWeight   []int      // 当前权重（动态变化）
	effectiveWeight []int      // 有效权重（固定值）
	totalWeight     int        // 总权重
	initialized     bool       // 是否已初始化
}

// NewSmoothWeightedRR 创建新的平滑加权轮询负载均衡选择器
func NewSmoothWeightedRR() Selector {
	return &smoothWeightedRR{}
}

// Select 从后端列表中选择一个后端服务器
// 使用平滑加权算法，每次选择当前权重最大的后端，然后降低其权重
func (s *smoothWeightedRR) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	// 延迟初始化：如果未初始化或后端列表变化，重新初始化
	if !s.initialized || len(s.effectiveWeight) != len(backends) {
		s.mu.Lock()
		// 双重检查锁定
		if !s.initialized || len(s.effectiveWeight) != len(backends) {
			s.backends = make([]Backend, len(backends))
			s.currentWeight = make([]int, len(backends))
			s.effectiveWeight = make([]int, len(backends))
			s.totalWeight = 0
			for i, b := range backends {
				s.backends[i] = b
				wb, ok := b.(WeightedBackend)
				var w int
				if !ok {
					// 默认权重为1
					w = 1
				} else {
					w = wb.Weight()
				}
				s.effectiveWeight[i] = w
				s.currentWeight[i] = w
				s.totalWeight += w
			}
			s.initialized = true
		}
		s.mu.Unlock()
		return s.backends[0]
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果总权重为0，返回第一个后端
	if s.totalWeight == 0 {
		return s.backends[0]
	}

	// 找出当前权重最大的后端
	maxWeight := 0
	selected := 0
	for i := range s.currentWeight {
		s.currentWeight[i] += s.effectiveWeight[i]
		if s.currentWeight[i] > maxWeight {
			maxWeight = s.currentWeight[i]
			selected = i
		}
	}

	// 选中后降低其当前权重
	s.currentWeight[selected] -= s.totalWeight

	return s.backends[selected]
}
