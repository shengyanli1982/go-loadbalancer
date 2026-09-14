package lb

type leastConn struct {
	connectionTracker

	backends []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	heap     idxHeap   // 索引堆：堆顶为最优后端（最小 conn/平局最小 index）
	cacheSnapshot
}

// NewLeastConn creates a least-connections selector.
//
// # NewLeastConn 创建 Least Connections（最少连接）选择器
//
// 算法语义：
//   - 选择在途连接数最少的后端；所有后端权重相等时直接比较 conn，权重不全相等时
//     改为比较 conn/weight（交叉乘法，避免浮点），即加权最少连接
//   - 平局处理：N < TreeThresholdLeastConn 的线性扫描路径在并列索引间用 rrIndex 轮转；
//     N >= TreeThresholdLeastConn 走索引堆 O(log n) 路径，平局固定取最小索引
//     （跨阈值时轮转相位不保证一致）
//   - Select 递增所选后端的在途连接计数
//
// 返回 TrackedSelector，因此 Release 是接口上的静态方法，无需类型断言；
// 请求完成后必须对同一个 backend 配对调用一次 Release 递减计数，
// 漏掉会让计数只增不减，被抬高计数的后端遭系统性饿死。
func NewLeastConn() TrackedSelector {
	return &leastConn{
		connectionTracker: newConnectionTracker(),
	}
}

// Select 选择在途连接数最少的后端。
//
// 算法：
//   - 比较语义分等权/加权两条路径：所有权重相等（hasUniformWeights）时直接比较 conn；
//     否则用交叉乘法比较 conn/weight（整数运算，避免浮点），即加权最少连接。
//   - 平局裁决随规模切换路径而不同：n < TreeThresholdLeastConn 走 selectLinear 线性扫描，
//     在 tiedIndices 记录的并列索引间用 rrIndex 轮转（公平分配）；n >= TreeThresholdLeastConn
//     走 selectHeap 取索引堆堆顶 O(log n)，平局固定取最小索引（跨阈值时轮转相位不保证一致，已审计接受）。
//   - 返回前递增所选后端的在途连接计数（connByIndex[bestIdx]++），须由配对的 Release 递减。
//
// 空列表返回 nil；所有可变状态由 mutex 保护。
func (l *leastConn) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	ptr := backendsSlicePtr(backends)
	if !(ptr == l.slicePtr && len(backends) == l.sliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != l.fingerprint || len(l.heap.heap) == 0 {
			l.rebuildIndex(backends)
			l.fingerprint = fp
		}
		l.slicePtr = ptr
		l.sliceLen = len(backends)
		l.backends = backends
	}

	n := len(backends)

	// 平局处理：n < 32 线性路径用 rrIndex 轮转，n >= 32 堆路径固定最小 index；跨阈值轮转相位不保证一致（已审计接受）
	if n >= TreeThresholdLeastConn {
		return l.selectHeap(backends)
	}

	return l.selectLinear(backends, n)
}

func (l *leastConn) selectLinear(backends []Backend, n int) Backend {
	// resizeSlice 返回 s[:n]，把 len 一并拉满，消除「按 cap 判断却按绝对下标写入」的越界隐患
	l.tiedIndices = resizeSlice(l.tiedIndices, n)

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

// selectHeap 堆路径选择，O(logn)：取堆顶（最小 conn，平局最小 index），
// conn++ 后 key 增大，单次 siftDown 修复堆序（等价于旧实现的 delete+reinsert 语义）
func (l *leastConn) selectHeap(backends []Backend) Backend {
	if len(l.heap.heap) == 0 {
		return backends[0]
	}

	bestIdx := l.heap.heap[0]
	l.connByIndex[bestIdx]++
	l.siftDown(0)

	l.rrIndex++
	return backends[bestIdx]
}

// Release 递减 backend 的在途连接计数，与 Select 的递增配对使用。
//
// 语义：
//   - backend 为 nil、或其地址不在索引中（addrIndex 未命中）、或计数已 <= 0 时直接返回（幂等，不会出现负计数）。
//   - 先减按位置计数 connByIndex，再同步减按地址计数 connByAddr（后者跨 rebuild 持久化）。
//   - 堆在 rebuildIndex 时经 rebuildHeap 恒构建：addrIndex 命中即已发生过 rebuild，
//     heap.pos 恒非 nil，下方守卫恒真。conn-- 使 key 减小，单次 siftUp 修复堆序；
//     n < TreeThresholdLeastConn 时选择走线性扫描、堆不参与，此处 siftUp 为无害冗余（≤5 次交换）。
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
	l.rebuildHeap()
}

func (l *leastConn) rebuildHeap() {
	l.heap.reset(len(l.connByIndex), l.heapLess)
}
