package lb

import "sync"

// weightedRR 实现加权轮询负载均衡算法
// 按权重比例分配流量，权重为 W 的后端在 W/totalWeight 的周期内被选中 W 次
// 使用累积权重数组实现 O(n) 选择，指纹缓存避免每次重建
type weightedRR struct {
	mu                  sync.Mutex
	cumulativeWeights   []int64 // 累积权重数组，用于查找 pos 落入的区间
	totalWeight         int64   // 所有后端的权重之和
	index               int64   // 轮询计数器（int64，取模时转为 uint64）
	backendsFingerprint uint64  // 后端列表指纹（含权重），变化时触发重建
	backendsSlicePtr    uintptr // 后端 slice 底层数组地址，用于快速缓存检测
	backendsSliceLen    int     // 后端 slice 长度，配合指针做快速缓存检测
}

// NewWeightedRR 创建加权轮询选择器
func NewWeightedRR() Selector {
	return &weightedRR{}
}

// Select 使用加权轮询算法选择一个后端
// 算法：计算 pos = index % totalWeight，在累积权重数组中找到 pos 落入的区间
// 注意：index 使用 uint64 取模，防止 int64 溢出后 pos 变为负数
func (w *weightedRR) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// 快速路径：同一个 slice → 跳过 fingerprint 计算
	ptr := backendsSlicePtr(backends)
	if !(ptr == w.backendsSlicePtr && len(backends) == w.backendsSliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != w.backendsFingerprint {
			w.rebuild(backends, fp)
		}
		w.backendsSlicePtr = ptr
		w.backendsSliceLen = len(backends)
	}

	if w.totalWeight == 0 {
		return backends[0]
	}

	// uint64 取模防止 int64 溢出后 pos 变负
	pos := uint64(w.index) % uint64(w.totalWeight)
	w.index++

	for i, cumulative := range w.cumulativeWeights {
		if int64(pos) < cumulative {
			return backends[i]
		}
	}

	return backends[len(backends)-1]
}

// rebuild 重建累积权重数组和指纹
// 复用已有切片容量，避免不必要的堆分配
func (w *weightedRR) rebuild(backends []Backend, fp uint64) {
	n := len(backends)
	if cap(w.cumulativeWeights) >= n {
		w.cumulativeWeights = w.cumulativeWeights[:n]
	} else {
		w.cumulativeWeights = make([]int64, n)
	}
	w.totalWeight = 0
	for i, b := range backends {
		w.totalWeight += int64(getWeight(b))
		w.cumulativeWeights[i] = w.totalWeight
	}
	w.backendsFingerprint = fp
}
