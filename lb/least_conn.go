package lb

type leastConn struct {
	connectionTracker

	tree                *rbTree
	posMap              []*rbNode
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
		tree:              newRBTree(),
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
		fp := computeBackendsFingerprint(backends)
		if fp != l.backendsFingerprint {
			l.rebuildIndex(backends)
			l.backendsFingerprint = fp
		}
		l.backendsSlicePtr = ptr
		l.backendsSliceLen = len(backends)
	}

	n := len(backends)

	if n >= TreeThresholdLeastConn {
		return l.selectTree(backends, ptr)
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

			if conn*bestWeight < bestConn*weight {
				bestConn = conn
				bestWeight = weight
				l.tiedIndices[0] = i
				tieLen = 1
			} else if conn*bestWeight == bestConn*weight {
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

func (l *leastConn) selectTree(backends []Backend, ptr uintptr) Backend {
	minNode := l.tree.min()
	if minNode == nil {
		return backends[0]
	}

	bestIdx := minNode.index
	l.tree.delete(minNode)
	l.connByIndex[bestIdx]++
	minNode.color = red
	minNode.left = l.tree.sentinel
	minNode.right = l.tree.sentinel
	minNode.parent = l.tree.sentinel
	l.tree.insertNode(minNode, l.less)
	l.posMap[bestIdx] = minNode

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

	var node *rbNode
	if l.posMap != nil && l.posMap[idx] != nil {
		node = l.posMap[idx]
		l.tree.delete(node)
	}

	l.connByIndex[idx]--
	if conn, ok := l.connByAddr[addr]; ok && conn > 0 {
		l.connByAddr[addr] = conn - 1
	}

	if node != nil {
		node.color = red
		node.left = l.tree.sentinel
		node.right = l.tree.sentinel
		node.parent = l.tree.sentinel
		l.tree.insertNode(node, l.less)
		l.posMap[idx] = node
	}
}

func (l *leastConn) less(a, b *rbNode) bool {
	if l.hasUniformWeights {
		if l.connByIndex[a.index] != l.connByIndex[b.index] {
			return l.connByIndex[a.index] < l.connByIndex[b.index]
		}
		return a.index < b.index
	}

	scoreA := int64(l.connByIndex[a.index]) * int64(l.weightCache[b.index])
	scoreB := int64(l.connByIndex[b.index]) * int64(l.weightCache[a.index])
	if scoreA != scoreB {
		return scoreA < scoreB
	}
	return a.index < b.index
}

func (l *leastConn) rebuildIndex(backends []Backend) {
	l.connectionTracker.rebuildIndex(backends)
	l.rebuildTree()
}

func (l *leastConn) rebuildTree() {
	rebuildRBTree(&l.tree, &l.posMap, len(l.connByIndex), l.less)
}
