package lb

type leastConn struct {
	connectionTracker

	backends            []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	heap                idxHeap   // 索引堆：堆顶为最优后端（最小 conn/平局最小 index）
	backendsFingerprint uint64
	backendsSlicePtr    uintptr
	backendsSliceLen    int
}

type LeastConnReleaser interface {
	Release(backend Backend)
}

func NewLeastConn() Selector {
	return &leastConn{
		connectionTracker: *newConnectionTracker(),
	}
}

func (l *leastConn) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	ptr := backendsSlicePtr(backends)
	if !(ptr == l.backendsSlicePtr && len(backends) == l.backendsSliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != l.backendsFingerprint || len(l.heap.heap) == 0 {
			l.rebuildIndex(backends)
			l.backendsFingerprint = fp
		}
		l.backendsSlicePtr = ptr
		l.backendsSliceLen = len(backends)
		l.backends = backends
	}

	n := len(backends)

	// 平局处理：n < 32 线性路径用 rrIndex 轮转，n >= 32 堆路径固定最小 index；跨阈值轮转相位不保证一致（已审计接受）
	if n >= TreeThresholdLeastConn {
		return l.selectTree(backends)
	}

	return l.selectLinear(backends, n)
}

func (l *leastConn) selectLinear(backends []Backend, n int) Backend {
	if cap(l.tiedIndices) < n {
		l.tiedIndices = make([]int, n)
	}

	var bestIdx int

	if l.hasUniformWeights {
		bestConn := l.connByIndex[0]
		l.tiedIndices[0] = 0
		tieLen := 1

		for i := 1; i < n; i++ {
			conn := l.connByIndex[i]
			if conn < bestConn {
				bestConn = conn
				l.tiedIndices[0] = i
				tieLen = 1
			} else if conn == bestConn {
				l.tiedIndices[tieLen] = i
				tieLen++
			}
		}

		bestIdx = l.tiedIndices[int(l.rrIndex%uint64(tieLen))]
	} else {
		bestConn := l.connByIndex[0]
		bestWeight := l.weightCache[0]
		l.tiedIndices[0] = 0
		tieLen := 1

		for i := 1; i < n; i++ {
			conn := l.connByIndex[i]
			weight := l.weightCache[i]

			if int64(conn)*int64(bestWeight) < int64(bestConn)*int64(weight) {
				bestConn = conn
				bestWeight = weight
				l.tiedIndices[0] = i
				tieLen = 1
			} else if int64(conn)*int64(bestWeight) == int64(bestConn)*int64(weight) {
				l.tiedIndices[tieLen] = i
				tieLen++
			}
		}

		bestIdx = l.tiedIndices[int(l.rrIndex%uint64(tieLen))]
	}

	l.rrIndex++
	l.connByIndex[bestIdx]++
	return backends[bestIdx]
}

// selectTree 堆路径选择，O(logn)：取堆顶（最小 conn，平局最小 index），
// conn++ 后 key 增大，单次 siftDown 修复堆序（等价于旧实现的 delete+reinsert 语义）
func (l *leastConn) selectTree(backends []Backend) Backend {
	if len(l.heap.heap) == 0 {
		return backends[0]
	}

	bestIdx := l.heap.heap[0]
	l.connByIndex[bestIdx]++
	l.siftDown(0)

	l.rrIndex++
	return backends[bestIdx]
}

func (l *leastConn) Release(backend Backend) {
	if backend == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	addr := backend.Address()
	idx, ok := l.addrIndex[addr]
	if !ok || l.connByIndex[idx] <= 0 {
		return
	}

	// conn-- 使 key 减小，单次 siftUp 修复堆序（等价于旧实现的 delete+reinsert 语义）
	l.connByIndex[idx]--
	if conn, ok := l.connByAddr[addr]; ok && conn > 0 {
		l.connByAddr[addr] = conn - 1
	}

	if l.heap.pos != nil {
		l.siftUp(l.heap.pos[idx])
	}
}

// siftDown key 增大后下沉修复堆序（Select 热路径）。
// 与 selectLinear 一致按等权/加权分支，比较逻辑内联于循环，避免闭包间接调用开销。
// 语义与 heapLess 完全一致：最小 conn 优先，平局取小 index。
func (l *leastConn) siftDown(i int) {
	heap := l.heap.heap
	pos := l.heap.pos
	n := len(heap)

	if l.hasUniformWeights {
		conn := l.connByIndex
		for {
			lc := 2*i + 1
			if lc >= n {
				return
			}
			m := lc
			if rc := lc + 1; rc < n && lessConn(conn[heap[rc]], heap[rc], conn[heap[lc]], heap[lc]) {
				m = rc
			}
			if !lessConn(conn[heap[m]], heap[m], conn[heap[i]], heap[i]) {
				return
			}
			ia, ib := heap[i], heap[m]
			heap[i], heap[m] = ib, ia
			pos[ia], pos[ib] = m, i
			i = m
		}
	}

	conn := l.connByIndex
	weight := l.weightCache
	for {
		lc := 2*i + 1
		if lc >= n {
			return
		}
		m := lc
		if rc := lc + 1; rc < n && lessWeighted(conn[heap[rc]], weight[heap[rc]], heap[rc], conn[heap[lc]], weight[heap[lc]], heap[lc]) {
			m = rc
		}
		if !lessWeighted(conn[heap[m]], weight[heap[m]], heap[m], conn[heap[i]], weight[heap[i]], heap[i]) {
			return
		}
		ia, ib := heap[i], heap[m]
		heap[i], heap[m] = ib, ia
		pos[ia], pos[ib] = m, i
		i = m
	}
}

// siftUp key 减小后上浮修复堆序（Release 热路径），比较语义同 siftDown
func (l *leastConn) siftUp(i int) {
	heap := l.heap.heap
	pos := l.heap.pos

	if l.hasUniformWeights {
		conn := l.connByIndex
		for i > 0 {
			p := (i - 1) / 2
			if !lessConn(conn[heap[i]], heap[i], conn[heap[p]], heap[p]) {
				return
			}
			ia, ib := heap[i], heap[p]
			heap[i], heap[p] = ib, ia
			pos[ia], pos[ib] = p, i
			i = p
		}
		return
	}

	conn := l.connByIndex
	weight := l.weightCache
	for i > 0 {
		p := (i - 1) / 2
		if !lessWeighted(conn[heap[i]], weight[heap[i]], heap[i], conn[heap[p]], weight[heap[p]], heap[p]) {
			return
		}
		ia, ib := heap[i], heap[p]
		heap[i], heap[p] = ib, ia
		pos[ia], pos[ib] = p, i
		i = p
	}
}

// lessConn 等权比较：conn 小者优先，平局索引小者优先（可内联）
func lessConn(ci, i, cj, j int) bool {
	if ci != cj {
		return ci < cj
	}
	return i < j
}

// lessWeighted 加权比较：conn/weight 交叉乘法，平局索引小者优先（可内联）
func lessWeighted(ci, wi, i, cj, wj, j int) bool {
	si := int64(ci) * int64(wj)
	sj := int64(cj) * int64(wi)
	if si != sj {
		return si < sj
	}
	return i < j
}

// heapLess 堆重建用比较函数（慢路径），选择语义与 siftDown/siftUp 一致
func (l *leastConn) heapLess(i, j int) bool {
	if l.hasUniformWeights {
		return lessConn(l.connByIndex[i], i, l.connByIndex[j], j)
	}
	return lessWeighted(l.connByIndex[i], l.weightCache[i], i, l.connByIndex[j], l.weightCache[j], j)
}

func (l *leastConn) rebuildIndex(backends []Backend) {
	l.connectionTracker.rebuildIndex(backends)
	l.rebuildTree()
}

func (l *leastConn) rebuildTree() {
	l.heap.reset(len(l.connByIndex), l.heapLess)
}
