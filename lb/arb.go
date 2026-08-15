package lb

import "math"

// ARBOptions 配置选项
type ARBOptions struct {
	Bias float64 // 0: 忽略连接数(纯WRR), 1: 标准LeastConn. 默认 1.0
}

// activeRequestBias 实现 Active Request Bias (ARB) 算法
// 对标 Envoy Weighted Least Request：
//   - score = weight / (active_conns + 1) ^ bias
//   - bias=0: 退化为纯 WRR（score=weight，所有后端 score 相同）
//   - bias=1: 标准 LeastConn（交叉乘法形式）
//   - 0<bias<1: 平滑过渡
//   - 选 score 最大的后端
type activeRequestBias struct {
	connectionTracker
	bias                float64
	backends            []Backend // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	heap                idxHeap   // 索引堆：堆顶为最优后端（max score/平局最小 index）
	backendsFingerprint uint64    // 后端列表指纹，变化时清理过期条目
	backendsSlicePtr    uintptr   // 后端 slice 底层数组地址，用于快速缓存检测
	backendsSliceLen    int       // 后端 slice 长度，配合指针做快速缓存检测
}

// NewActiveRequestBias 创建 Active Request Bias 选择器，bias=1.0
func NewActiveRequestBias() Selector {
	return NewActiveRequestBiasWithOptions(&ARBOptions{Bias: 1.0})
}

// NewActiveRequestBiasWithOptions 创建 Active Request Bias 选择器，支持配置 bias
func NewActiveRequestBiasWithOptions(opts *ARBOptions) Selector {
	bias := 1.0
	if opts != nil && opts.Bias >= 0 {
		bias = opts.Bias
	}
	return &activeRequestBias{
		connectionTracker: *newConnectionTracker(),
		bias:              bias,
	}
}

// Select 使用 Active Request Bias 算法选择一个后端
// 算法（对标 Envoy Weighted Least Request）：
//
//	单轮扫描：遍历所有后端，计算 score = weight / (conns+1)^bias
//	bias=0 时退化为 RR（score=weight 是常量）
//	等权重时 score = 1/(conns+1)^bias，bias=1 时进一步简化为 1/(conns+1)
//	遇到平局时将索引记录到 tiedIndices，扫描结束后用 rrIndex % tieLen 选取代
//	N >= arbTreeThreshold 时走索引堆 O(log n) 路径
//	最后递增选中后端的连接数
func (a *activeRequestBias) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 快速路径：同一个 slice → 跳过 fingerprint 计算和索引重建
	ptr := backendsSlicePtr(backends)
	if !(ptr == a.backendsSlicePtr && len(backends) == a.backendsSliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != a.backendsFingerprint || len(a.heap.heap) == 0 {
			a.connectionTracker.rebuildIndex(backends)
			a.rebuildTree()
			a.backendsFingerprint = fp
		}
		a.backendsSlicePtr = ptr
		a.backendsSliceLen = len(backends)
		a.backends = backends
	}

	n := len(backends)

	// 确保 tiedIndices 容量足够（复用，无热路径分配）
	if cap(a.tiedIndices) < n {
		a.tiedIndices = make([]int, n)
	}

	// bias=0 快速路径：退化为 RR
	if a.bias == 0 {
		bestIdx := int(a.rrIndex % uint64(n))
		a.rrIndex++
		a.connByIndex[bestIdx]++
		return backends[bestIdx]
	}

	// 大规模后端：索引堆 O(log n) 路径
	// 平局处理：n < 32 线性路径用 rrIndex 轮转，n >= 32 堆路径固定最小 index；跨阈值轮转相位不保证一致（已审计接受）
	if n >= TreeThresholdARB {
		return a.selectTree(backends)
	}

	// 小规模后端：线性扫描路径
	return a.selectLinear(backends, n)
}

// selectTree 堆路径选择，O(logn)：取堆顶（max score，平局最小 index），
// conn++ 使 score 降低，单次 siftDown 修复堆序（等价于旧实现的 delete+reinsert 语义）
func (a *activeRequestBias) selectTree(backends []Backend) Backend {
	if len(a.heap.heap) == 0 {
		return backends[0]
	}

	bestIdx := a.heap.heap[0]
	a.connByIndex[bestIdx]++
	a.siftDown(0)

	a.rrIndex++
	return backends[bestIdx]
}

// selectLinear 线性扫描路径，小规模后端（N < arbTreeThreshold）
func (a *activeRequestBias) selectLinear(backends []Backend, n int) Backend {
	var bestIdx int

	// 等权重 + bias=1 快速路径：简化为 LeastConn
	if a.hasUniformWeights && a.bias == 1.0 {
		bestConn := a.connByIndex[0]
		a.tiedIndices[0] = 0
		tieLen := 1

		for i := 1; i < n; i++ {
			conn := a.connByIndex[i]
			if conn < bestConn {
				bestConn = conn
				a.tiedIndices[0] = i
				tieLen = 1
			} else if conn == bestConn {
				a.tiedIndices[tieLen] = i
				tieLen++
			}
		}

		bestIdx = a.tiedIndices[int(a.rrIndex%uint64(tieLen))]
		a.rrIndex++
		a.connByIndex[bestIdx]++
		return backends[bestIdx]
	}

	// biased=1 快速路径（非等权重）：score = weight / (conns+1)，math.Pow(x,1.0) == x
	if a.bias == 1.0 {
		bestScore := float64(a.weightCache[0]) / float64(a.connByIndex[0]+1)
		a.tiedIndices[0] = 0
		tieLen := 1
		for i := 1; i < n; i++ {
			score := float64(a.weightCache[i]) / float64(a.connByIndex[i]+1)
			if score > bestScore {
				bestScore = score
				a.tiedIndices[0] = i
				tieLen = 1
			} else if score == bestScore {
				a.tiedIndices[tieLen] = i
				tieLen++
			}
		}
		bestIdx = a.tiedIndices[int(a.rrIndex%uint64(tieLen))]
		a.rrIndex++
		a.connByIndex[bestIdx]++
		return backends[bestIdx]
	}

	// 通用路径：计算 score = weight / (conns+1)^bias（仅 0<bias<1 时命中）
	bestScore := float64(a.weightCache[0]) / math.Pow(float64(a.connByIndex[0]+1), a.bias)
	a.tiedIndices[0] = 0
	tieLen := 1

	for i := 1; i < n; i++ {
		score := float64(a.weightCache[i]) / math.Pow(float64(a.connByIndex[i]+1), a.bias)
		if score > bestScore {
			bestScore = score
			a.tiedIndices[0] = i
			tieLen = 1
		} else if score == bestScore {
			a.tiedIndices[tieLen] = i
			tieLen++
		}
	}

	bestIdx = a.tiedIndices[int(a.rrIndex%uint64(tieLen))]
	a.rrIndex++
	a.connByIndex[bestIdx]++
	return backends[bestIdx]
}

// Release 释放一个后端的连接计数
// 堆路径下 conn-- 使 score 升高，单次 siftUp 修复堆序
func (a *activeRequestBias) Release(backend Backend) {
	if backend == nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	addr := backend.Address()
	idx, ok := a.addrIndex[addr]
	if !ok || a.connByIndex[idx] <= 0 {
		return
	}

	// 更新计数
	a.connByIndex[idx]--
	if conn, ok := a.connByAddr[addr]; ok && conn > 0 {
		a.connByAddr[addr] = conn - 1
	}

	// 堆路径已启用时修复堆序（等价于旧实现的 delete+reinsert 语义）
	if a.heap.pos != nil {
		a.siftUp(a.heap.pos[idx])
	}
}

// siftDown key 变差后下沉修复堆序（Select 热路径：conn++ 使 score 降低）。
// 语义：max score 优先，平局取小 index。
// bias=1 等权/加权分支内联比较，避免闭包间接调用开销。
func (a *activeRequestBias) siftDown(i int) {
	heap := a.heap.heap
	pos := a.heap.pos
	n := len(heap)

	if a.bias == 1.0 {
		conn := a.connByIndex
		if a.hasUniformWeights {
			// score = 1/(c+1)：conn 小者 score 高，平局索引小者优先（同 leastConn）
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
		weight := a.weightCache
		// score = w/(c+1)：i 更优 ↔ w_i*(c_j+1) > w_j*(c_i+1)
		for {
			lc := 2*i + 1
			if lc >= n {
				return
			}
			m := lc
			if rc := lc + 1; rc < n && betterARBWeighted(conn[heap[rc]], weight[heap[rc]], heap[rc], conn[heap[lc]], weight[heap[lc]], heap[lc]) {
				m = rc
			}
			if !betterARBWeighted(conn[heap[m]], weight[heap[m]], heap[m], conn[heap[i]], weight[heap[i]], heap[i]) {
				return
			}
			ia, ib := heap[i], heap[m]
			heap[i], heap[m] = ib, ia
			pos[ia], pos[ib] = m, i
			i = m
		}
	}

	// 通用 0<bias<1 浮点路径（低频）
	for {
		lc := 2*i + 1
		if lc >= n {
			return
		}
		m := lc
		if rc := lc + 1; rc < n && a.betterScore(heap[rc], heap[lc]) {
			m = rc
		}
		if !a.betterScore(heap[m], heap[i]) {
			return
		}
		ia, ib := heap[i], heap[m]
		heap[i], heap[m] = ib, ia
		pos[ia], pos[ib] = m, i
		i = m
	}
}

// siftUp key 变优后上浮修复堆序（Release 热路径：conn-- 使 score 升高）
func (a *activeRequestBias) siftUp(i int) {
	heap := a.heap.heap
	pos := a.heap.pos

	if a.bias == 1.0 {
		conn := a.connByIndex
		if a.hasUniformWeights {
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
		weight := a.weightCache
		for i > 0 {
			p := (i - 1) / 2
			if !betterARBWeighted(conn[heap[i]], weight[heap[i]], heap[i], conn[heap[p]], weight[heap[p]], heap[p]) {
				return
			}
			ia, ib := heap[i], heap[p]
			heap[i], heap[p] = ib, ia
			pos[ia], pos[ib] = p, i
			i = p
		}
		return
	}

	for i > 0 {
		p := (i - 1) / 2
		if !a.betterScore(heap[i], heap[p]) {
			return
		}
		ia, ib := heap[i], heap[p]
		heap[i], heap[p] = ib, ia
		pos[ia], pos[ib] = p, i
		i = p
	}
}

// betterARBWeighted 加权 bias=1 比较：score=w/(c+1)，score 高者优先，平局索引小者优先（可内联）
func betterARBWeighted(ci, wi, i, cj, wj, j int) bool {
	lhs := int64(wi) * int64(cj+1)
	rhs := int64(wj) * int64(ci+1)
	if lhs != rhs {
		return lhs > rhs
	}
	return i < j
}

// betterScore 通用 score 比较（0<bias<1 浮点路径）：score 高者优先，平局索引小者优先
func (a *activeRequestBias) betterScore(i, j int) bool {
	si := float64(a.weightCache[i]) / math.Pow(float64(a.connByIndex[i]+1), a.bias)
	sj := float64(a.weightCache[j]) / math.Pow(float64(a.connByIndex[j]+1), a.bias)
	if si != sj {
		return si > sj
	}
	return i < j
}

// heapLess 堆重建用比较函数（慢路径），语义与 siftDown/siftUp 一致
func (a *activeRequestBias) heapLess(i, j int) bool {
	if a.bias == 1.0 {
		if a.hasUniformWeights {
			return lessConn(a.connByIndex[i], i, a.connByIndex[j], j)
		}
		return betterARBWeighted(a.connByIndex[i], a.weightCache[i], i, a.connByIndex[j], a.weightCache[j], j)
	}
	return a.betterScore(i, j)
}

// rebuildTree 在 rebuildIndex 后重建索引堆（复用容量，O(n) 堆化）
func (a *activeRequestBias) rebuildTree() {
	a.heap.reset(len(a.connByIndex), a.heapLess)
}
