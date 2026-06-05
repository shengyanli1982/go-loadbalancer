package lb

import "sync"

// smoothWeightedRR 实现平滑加权轮询算法（nginx 风格）
// 算法原理（对标 nginx ngx_http_upstream_get_peer）：
//   - 每个后端维护 currentWeight 和 effectiveWeight
//   - 每轮：currentWeight += effectiveWeight，选 currentWeight 最大的后端
//   - 选中后：currentWeight -= totalWeight
//
// 效果：权重高的后端更频繁被选中，但不会连续选中同一后端
type smoothWeightedRR struct {
	mu                  sync.Mutex
	backends            []Backend // 缓存后端列表
	currentWeight       []int     // 当前权重，每轮动态变化
	effectiveWeight     []int     // 有效权重（初始化时固定）
	totalWeight         int       // 所有 effectiveWeight 之和
	backendsFingerprint uint64    // 后端列表指纹（含权重），变化时触发重建
	backendsSlicePtr    uintptr   // 后端 slice 底层数组地址，用于快速缓存检测
	backendsSliceLen    int       // 后端 slice 长度，配合指针做快速缓存检测
}

// NewSmoothWeightedRR 创建平滑加权轮询选择器
func NewSmoothWeightedRR() Selector {
	return &smoothWeightedRR{}
}

// Select 使用平滑加权轮询算法选择一个后端
// 对标 nginx ngx_http_upstream_get_peer 中的 SWRR 核心循环
func (s *smoothWeightedRR) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 快速路径：同一个 slice → 跳过 fingerprint 计算
	ptr := backendsSlicePtr(backends)
	if !(ptr == s.backendsSlicePtr && len(backends) == s.backendsSliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != s.backendsFingerprint {
			s.rebuild(backends, fp)
		}
		s.backendsSlicePtr = ptr
		s.backendsSliceLen = len(backends)
	}

	// SWRR 核心：currentWeight += effectiveWeight，选最大，减 totalWeight
	maxWeight := 0
	selected := 0
	for i := range s.currentWeight {
		s.currentWeight[i] += s.effectiveWeight[i]
		if s.currentWeight[i] > maxWeight {
			maxWeight = s.currentWeight[i]
			selected = i
		}
	}
	s.currentWeight[selected] -= s.totalWeight

	return s.backends[selected]
}

// rebuild 重新初始化后端权重数据
// 复用已有切片容量，避免不必要的堆分配
func (s *smoothWeightedRR) rebuild(backends []Backend, fp uint64) {
	n := len(backends)
	if cap(s.backends) >= n {
		s.backends = s.backends[:n]
	} else {
		s.backends = make([]Backend, n)
	}
	if cap(s.currentWeight) >= n {
		s.currentWeight = s.currentWeight[:n]
	} else {
		s.currentWeight = make([]int, n)
	}
	if cap(s.effectiveWeight) >= n {
		s.effectiveWeight = s.effectiveWeight[:n]
	} else {
		s.effectiveWeight = make([]int, n)
	}
	s.totalWeight = 0
	for i, b := range backends {
		w := getWeight(b)
		s.backends[i] = b
		s.effectiveWeight[i] = w
		s.currentWeight[i] = 0
		s.totalWeight += w
	}
	s.backendsFingerprint = fp
}
