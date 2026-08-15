package lb

import (
	"sort"
	"sync"
)

// weightedRR 实现加权轮询负载均衡算法
// 按权重比例分配流量，权重为 W 的后端在 W/totalWeight 的周期内被选中 W 次
// 使用累积权重数组实现 O(log n) 二分查找选择，指纹缓存避免每次重建
type weightedRR struct {
	mu                  sync.Mutex
	backends            []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	cachedCumulativeWts []int64   // 累积权重数组，用于二分查找 pos 落入的区间
	totalWeight         int64     // 所有后端的权重之和
	currentIndex        int64     // 轮询计数器（int64，取模时转为 uint64）
	cacheSnapshot
}

// NewWeightedRR 创建加权轮询选择器
func NewWeightedRR() Selector {
	return &weightedRR{}
}

// Select 使用加权轮询算法选择一个后端
// 算法：计算 pos = index % totalWeight，在累积权重数组中二分查找 pos 落入的区间，复杂度 O(log n)
// 注意：index 使用 uint64 取模，防止 int64 溢出后 pos 变为负数
func (w *weightedRR) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 快速路径：同一个 slice → 跳过 fingerprint 计算
	ptr := backendsSlicePtr(backends)
	if !(ptr == w.slicePtr && len(backends) == w.sliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != w.fingerprint || len(w.cachedCumulativeWts) == 0 {
			w.rebuild(backends, fp)
		}
		w.slicePtr = ptr
		w.sliceLen = len(backends)
		w.backends = backends
	}

	if w.totalWeight == 0 {
		return backends[0]
	}

	// uint64 取模防止 int64 溢出后 pos 变负
	pos := int64(uint64(w.currentIndex) % uint64(w.totalWeight))
	w.currentIndex++

	// 二分查找：找到第一个 cachedCumulativeWts[i] > pos 的索引
	// 累积权重严格递增（每个权重 >= 1），二分查找安全
	idx := sort.Search(len(w.cachedCumulativeWts), func(i int) bool {
		return w.cachedCumulativeWts[i] > pos
	})
	if idx >= len(w.cachedCumulativeWts) {
		idx = len(w.cachedCumulativeWts) - 1
	}
	return backends[idx]
}

// rebuild 重建累积权重数组和指纹
// 复用已有切片容量，避免不必要的堆分配
func (w *weightedRR) rebuild(backends []Backend, fp uint64) {
	n := len(backends)
	w.cachedCumulativeWts = resizeSlice(w.cachedCumulativeWts, n)
	w.totalWeight = 0
	for i, b := range backends {
		w.totalWeight += int64(getWeight(b))
		w.cachedCumulativeWts[i] = w.totalWeight
	}
	w.fingerprint = fp
}
