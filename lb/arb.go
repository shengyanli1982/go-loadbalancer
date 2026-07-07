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
	tree                *rbTree
	posMap              []*rbNode
	backendsFingerprint uint64  // 后端列表指纹，变化时清理过期条目
	backendsSlicePtr    uintptr // 后端 slice 底层数组地址，用于快速缓存检测
	backendsSliceLen    int     // 后端 slice 长度，配合指针做快速缓存检测
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
		tree:              newRBTree(),
	}
}

// Select 使用 Active Request Bias 算法选择一个后端
// 算法（对标 Envoy Weighted Least Request）：
//
//	单轮扫描：遍历所有后端，计算 score = weight / (conns+1)^bias
//	bias=0 时退化为 RR（score=weight 是常量）
//	等权重时 score = 1/(conns+1)^bias，bias=1 时进一步简化为 1/(conns+1)
//	遇到平局时将索引记录到 tiedIndices，扫描结束后用 rrIndex % tieLen 选取代
//	N >= arbTreeThreshold 时走 rbTree O(log n) 路径
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
		fp := computeBackendsFingerprint(backends)
		if fp != a.backendsFingerprint {
			a.connectionTracker.rebuildIndex(backends)
			a.rebuildTree()
			a.backendsFingerprint = fp
		}
		a.backendsSlicePtr = ptr
		a.backendsSliceLen = len(backends)
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

	// 大规模后端：rbTree O(log n) 路径
	if n >= TreeThresholdARB {
		return a.selectTree(backends)
	}

	// 小规模后端：线性扫描路径
	return a.selectLinear(backends, n)
}

// selectTree 基于 rbTree 的选择路径，O(log n)
// 取树中最右侧节点（max score），更新计数后重新插入
func (a *activeRequestBias) selectTree(backends []Backend) Backend {
	maxNode := a.tree.max()
	if maxNode == nil {
		return backends[0]
	}

	bestIdx := maxNode.index
	a.tree.delete(maxNode)
	a.connByIndex[bestIdx]++
	maxNode.color = red
	maxNode.left = a.tree.sentinel
	maxNode.right = a.tree.sentinel
	maxNode.parent = a.tree.sentinel
	a.tree.insertNode(maxNode, a.less)
	a.posMap[bestIdx] = maxNode

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
// 当存在 rbTree 时，同步更新树节点位置
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

	// 若树路径已启用，先从树中移除节点
	var node *rbNode
	if a.posMap != nil && a.posMap[idx] != nil {
		node = a.posMap[idx]
		a.tree.delete(node)
	}

	// 更新计数
	a.connByIndex[idx]--
	if conn, ok := a.connByAddr[addr]; ok && conn > 0 {
		a.connByAddr[addr] = conn - 1
	}

	// 重新插入树中（新位置反映更新后的计数）
	if node != nil {
		node.color = red
		node.left = a.tree.sentinel
		node.right = a.tree.sentinel
		node.parent = a.tree.sentinel
		a.tree.insertNode(node, a.less)
		a.posMap[idx] = node
	}
}

// less 定义 rbTree 的排序规则（标准 BST 语义，less(a,b)=true 表示 a 排在 b 左侧）
// ARB 选择 score 最大的后端，因此将 max score 置于树最右侧：
//   - score1 < score2 → true（低分在左）
//   - 平局时，小 index 置于右侧（max() 优先选小 index，与 LeastConn 行为一致）
func (a *activeRequestBias) less(n1, n2 *rbNode) bool {
	if a.bias == 1.0 {
		if a.hasUniformWeights {
			// score = 1/(c+1)：score1 < score2 ↔ c1 > c2
			c1, c2 := a.connByIndex[n1.index], a.connByIndex[n2.index]
			if c1 != c2 {
				return c1 > c2
			}
			return n1.index > n2.index // 平局：小 index 在右侧（"更大"）
		}
		// 加权 bias=1：score = w/(c+1)
		// score1 < score2 ↔ w1*(c2+1) < w2*(c1+1)
		lhs := int64(a.weightCache[n1.index]) * int64(a.connByIndex[n2.index]+1)
		rhs := int64(a.weightCache[n2.index]) * int64(a.connByIndex[n1.index]+1)
		if lhs != rhs {
			return lhs < rhs
		}
		return n1.index > n2.index
	}
	// 通用路径（0 < bias < 1）：浮点比较
	score1 := float64(a.weightCache[n1.index]) / math.Pow(float64(a.connByIndex[n1.index]+1), a.bias)
	score2 := float64(a.weightCache[n2.index]) / math.Pow(float64(a.connByIndex[n2.index]+1), a.bias)
	if score1 != score2 {
		return score1 < score2
	}
	return n1.index > n2.index
}

// rebuildTree 在 rebuildIndex 后重建 rbTree（所有节点零分配复用）
func (a *activeRequestBias) rebuildTree() {
	rebuildRBTree(&a.tree, &a.posMap, len(a.connByIndex), a.less)
}
