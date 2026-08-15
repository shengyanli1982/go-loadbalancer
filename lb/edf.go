package lb

import "sync"

// edfItem 最小堆中的元素：存储调度 deadline 和后端索引
type edfItem struct {
	deadline float64 // 调度 deadline（越小越优先）
	index    int     // backends 中的位置索引，用于 deadline 相等时的二级排序
}

// less 堆元素比较：deadline 小者优先；deadline 相等时 index 小者优先
func (a edfItem) less(b edfItem) bool {
	if a.deadline != b.deadline {
		return a.deadline < b.deadline
	}
	return a.index < b.index
}

// edf 实现 Earliest Deadline First 调度加权轮询算法
// 为每个后端维护一个 deadline 值，每次 Select 选 deadline 最小的后端
// 被选中的后端 deadline += 1.0/weight，权重越高 deadline 增长越慢 → 被选更频繁
// 效果：在完整周期内严格按权重比例分配流量，且分布均匀
// 使用 O(log n) 最小堆替代 O(n) 线性扫描，大幅提升大规模后端列表的性能
type edf struct {
	mu            sync.Mutex
	backends      []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	pq            []edfItem // 优先队列（最小堆），按 deadline 排序（手写 sift，零分配）
	cachedWeights []int     // 权重缓存，rebuild 时填充
	cacheSnapshot
}

// NewEDF 创建 EDF 调度选择器
func NewEDF() Selector {
	return &edf{}
}

// Select 使用 EDF 调度算法选择一个后端
// 通过最小堆取得最小 deadline 的后端，更新 deadline 后放回堆中
// 时间复杂度 O(log n)，优于线性扫描的 O(n)
// 使用指纹缓存模式避免每次重建内部数据结构
func (e *edf) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(backends) == 1 {
		return backends[0]
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 快速路径：同一个 slice → 跳过 fingerprint 计算
	ptr := backendsSlicePtr(backends)
	if !(ptr == e.slicePtr && len(backends) == e.sliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != e.fingerprint || len(e.pq) == 0 {
			e.rebuild(backends, fp)
		}
		e.slicePtr = ptr
		e.sliceLen = len(backends)
		e.backends = backends
	}

	// 堆顶即为最小 deadline 元素
	item := e.pq[0]

	// 更新 deadline 并下滤（无需 Push/Pop，原地修改根节点后 siftDown → 零分配）
	e.pq[0].deadline += 1.0 / float64(e.cachedWeights[item.index])
	e.siftDown(0)

	return backends[item.index]
}

// rebuild 重建内部数据结构
// 复用已有切片容量，避免不必要的堆分配；使用 O(n) 批量建堆
func (e *edf) rebuild(backends []Backend, fp uint64) {
	n := len(backends)
	e.cachedWeights = resizeSlice(e.cachedWeights, n)

	// 复用堆 slice 容量，避免重新分配
	if cap(e.pq) >= n {
		e.pq = e.pq[:n]
	} else {
		e.pq = make([]edfItem, n)
	}
	for i, b := range backends {
		e.cachedWeights[i] = getWeight(b)
		e.pq[i] = edfItem{deadline: 0, index: i}
	}
	e.heapify() // O(n) 一次性构建

	e.fingerprint = fp
}

// heapify O(n) Floyd 算法批量建堆（从最后一个非叶节点开始逐层 siftDown）
func (e *edf) heapify() {
	for i := len(e.pq)/2 - 1; i >= 0; i-- {
		e.siftDown(i)
	}
}

// siftDown 将位置 i 处的元素向下移动到正确位置（O(log n)）
func (e *edf) siftDown(i int) {
	n := len(e.pq)
	for {
		left := 2*i + 1
		if left >= n {
			break // 已到叶节点
		}
		// 选较小的子节点
		child := left
		if right := left + 1; right < n && e.pq[right].less(e.pq[left]) {
			child = right
		}
		// 如果父节点已经 ≤ 最小子节点，停止
		if !e.pq[child].less(e.pq[i]) {
			break
		}
		e.pq[i], e.pq[child] = e.pq[child], e.pq[i]
		i = child
	}
}
