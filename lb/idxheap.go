package lb

// idxHeap 索引最小堆：heap 存后端索引，pos 记录各索引的堆中位置，
// 堆顶恒为 lessFunc 意义下的最优索引。
// idxHeap 只提供 reset / siftDown / swap 三个方法，不提供 siftUp：
//   - reset 用于堆重建，走 O(n) Floyd 堆化，内部复用自身 siftDown（经 h.lessFunc 闭包比较）；
//   - 热路径的 Select（key 变差后 siftDown）与 Release（key 变优后 siftUp）不经 idxHeap，
//     而由使用方（least_conn / arb）各自内联实现——把等权/加权比较逻辑直接写进循环，
//     避免 h.lessFunc 闭包的间接调用开销（合理的性能取舍，故不合并回 idxHeap）。
//
// 单元素 key 变化只需一次 O(logn) 数组内 sift（零分配、缓存友好）。
type idxHeap struct {
	heap     []int               // heap[位置]=后端索引
	pos      []int               // pos[后端索引]=堆中位置
	lessFunc func(i, j int) bool // 索引比较：true 表示 i 比 j 更优（更靠近堆顶）
}

// reset 重建堆（复用容量）：初始化为恒等排列后自底向上堆化，O(n)
func (h *idxHeap) reset(n int, less func(i, j int) bool) {
	h.lessFunc = less
	if cap(h.heap) >= n {
		h.heap = h.heap[:n]
	} else {
		h.heap = make([]int, n)
	}
	if cap(h.pos) >= n {
		h.pos = h.pos[:n]
	} else {
		h.pos = make([]int, n)
	}
	for i := 0; i < n; i++ {
		h.heap[i] = i
		h.pos[i] = i
	}
	for i := n/2 - 1; i >= 0; i-- {
		h.siftDown(i)
	}
}

// siftDown key 增大后向下修复堆序
func (h *idxHeap) siftDown(i int) {
	n := len(h.heap)
	for {
		l := 2*i + 1
		if l >= n {
			return
		}
		m := l
		if r := l + 1; r < n && h.lessFunc(h.heap[r], h.heap[l]) {
			m = r
		}
		if !h.lessFunc(h.heap[m], h.heap[i]) {
			return
		}
		h.swap(i, m)
		i = m
	}
}

func (h *idxHeap) swap(a, b int) {
	ia, ib := h.heap[a], h.heap[b]
	h.heap[a], h.heap[b] = ib, ia
	h.pos[ia], h.pos[ib] = b, a
}
