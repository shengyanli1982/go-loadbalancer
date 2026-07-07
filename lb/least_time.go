package lb

// leastTime 实现延迟感知（Least Time）负载均衡算法（对标 Traefik v3.6）
//
// Score 公式: score = avgLatency * (1 + activeConns) / weight
// 选中 score 最小的后端；等权重时退化为比较 avgLatency * (1 + activeConns)
//
// 零分配快速路径设计（与 leastConn 相同模式）:
//   - Select() 不在快速路径上分配堆内存（connByIndex/weightCache/tiedIndices 均预分配复用）
//   - 单轮扫描 + tiedIndices 记录平局索引，用 rrIndex 公平选择
//   - 未实现 LatencyBackend 的后端 score=0（explore-exploit：优先探测未知）
//
// 性能优化（对标 P2C 预缓存模式）:
//   - 在 rebuildIndex 时缓存 LatencyBackend 类型断言结果（latencyBackends []LatencyBackend）
//   - Select 热路径直接使用缓存的接口指针，零类型断言开销
//   - 此优化将 per-backend per-Select 的接口满足检查降至零（原每次 Select 需 N 次完整 itab 查找）
//
// 线程安全：使用 sync.Mutex 保护所有可变状态
//
// LeastConnReleaser 接口复用：Release 行为与 leastConn 完全相同（连接数递减）
type leastTime struct {
	connectionTracker
	latencyBackends    []LatencyBackend // 缓存的 LatencyBackend 接口（rebuildIndex 预断言，Select 零类型检查）
	allLatencyBackends bool             // 所有后端都实现了 LatencyBackend（Select 快速路径依据）
	backendsFingerprint uint64          // 后端列表指纹，变化时清理过期条目
	backendsSlicePtr    uintptr         // 后端 slice 底层数组地址，快速缓存检测
	backendsSliceLen    int             // 后端 slice 长度，配合指针做快速缓存检测
}

// NewLeastTime 创建延迟感知选择器（对标 Traefik v3.6 Least Time）
func NewLeastTime() Selector {
	return &leastTime{
		connectionTracker: *newConnectionTracker(),
	}
}

// Select 使用 Least Time 算法选择一个后端
//
// 算法:
//
//	单轮扫描：遍历所有后端，计算 score
//	score = avgLatency * (1 + activeConns) / weight （LatencyBackend 时）
//	score = 0 （非LatencyBackend：视为"未探索"，优先选中以采集数据）
//	等权重时退化为比较 avgLatency * (1 + activeConns)（省去一次除法）
//	遇到平局时记录到 tiedIndices，扫描结束后用 rrIndex % tieLen 选取
//	最后递增选中后端的内部连接计数
func (l *leastTime) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 快速路径：同一个 slice → 跳过 fingerprint 计算和索引重建
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

	// 确保 tiedIndices 容量足够（复用，无热路径分配）
	if cap(l.tiedIndices) < n {
		l.tiedIndices = make([]int, n)
	}

	var bestIdx int

	if l.hasUniformWeights {
		// 快速路径：所有权重相等 → 比较 avgLatency * (1+conns)，省去一次除法
		if l.allLatencyBackends {
			// 极速路径：所有后端均实现 LatencyBackend，无需 nil check
			// 内联接口调用，消除 latencyConns 函数调用开销
			lb0 := l.latencyBackends[0]
			bestScore := lb0.AverageLatency() * float64(1+lb0.ActiveConnections())
			l.tiedIndices[0] = 0
			tieLen := 1

			for i := 1; i < n; i++ {
				lb := l.latencyBackends[i]
				score := lb.AverageLatency() * float64(1+lb.ActiveConnections())
				if score < bestScore {
					bestScore = score
					l.tiedIndices[0] = i
					tieLen = 1
				} else if score == bestScore {
					l.tiedIndices[tieLen] = i
					tieLen++
				}
			}
			bestIdx = l.tiedIndices[int(l.rrIndex%uint64(tieLen))]
		} else {
			lt0, lc0 := l.latencyConns(0)
			bestScore := lt0 * float64(1+lc0)
			l.tiedIndices[0] = 0
			tieLen := 1

			for i := 1; i < n; i++ {
				lt, lc := l.latencyConns(i)
				score := lt * float64(1+lc)
				if score < bestScore {
					bestScore = score
					l.tiedIndices[0] = i
					tieLen = 1
				} else if score == bestScore {
					l.tiedIndices[tieLen] = i
					tieLen++
				}
			}
			bestIdx = l.tiedIndices[int(l.rrIndex%uint64(tieLen))]
		}
	} else {
		// 加权路径：score = latency * (1+conns) / weight，浮点比较
		if l.allLatencyBackends {
			// 极速路径：所有后端均实现 LatencyBackend，无需 nil check
			lb0 := l.latencyBackends[0]
			bestScore := lb0.AverageLatency() * float64(1+lb0.ActiveConnections()) / float64(l.weightCache[0])
			l.tiedIndices[0] = 0
			tieLen := 1

			for i := 1; i < n; i++ {
				lb := l.latencyBackends[i]
				score := lb.AverageLatency() * float64(1+lb.ActiveConnections()) / float64(l.weightCache[i])
				if score < bestScore {
					bestScore = score
					l.tiedIndices[0] = i
					tieLen = 1
				} else if score == bestScore {
					l.tiedIndices[tieLen] = i
					tieLen++
				}
			}
			bestIdx = l.tiedIndices[int(l.rrIndex%uint64(tieLen))]
		} else {
			lt0, lc0 := l.latencyConns(0)
			bestScore := lt0 * float64(1+lc0) / float64(l.weightCache[0])
			l.tiedIndices[0] = 0
			tieLen := 1

			for i := 1; i < n; i++ {
				lt, lc := l.latencyConns(i)
				score := lt * float64(1+lc) / float64(l.weightCache[i])
				if score < bestScore {
					bestScore = score
					l.tiedIndices[0] = i
					tieLen = 1
				} else if score == bestScore {
					l.tiedIndices[tieLen] = i
					tieLen++
				}
			}
			bestIdx = l.tiedIndices[int(l.rrIndex%uint64(tieLen))]
		}
	}

	l.rrIndex++
	l.connByIndex[bestIdx]++
	return backends[bestIdx]
}

// latencyConns 从缓存的 LatencyBackend 接口提取延迟和活跃连接数
// 优化：使用 rebuildIndex 预断言的缓存接口，避免 Select 热路径类型断言
// nil 表示该后端未实现 LatencyBackend → explore 策略：score=0 优先被选中
func (l *leastTime) latencyConns(i int) (float64, int) {
	if lb := l.latencyBackends[i]; lb != nil {
		return lb.AverageLatency(), lb.ActiveConnections()
	}
	return 0, 0
}

// rebuildIndex 重建内部索引（后端列表变化时调用，已持有锁）
// 先委托给 connectionTracker 完成公共逻辑，再处理 LatencyBackend 缓存
func (l *leastTime) rebuildIndex(backends []Backend) {
	l.connectionTracker.rebuildIndex(backends)

	// 额外：检测哪些后端实现了 LatencyBackend
	n := len(backends)
	l.latencyBackends = resizeSlice(l.latencyBackends, n)
	l.allLatencyBackends = n > 0
	for i, b := range backends {
		if lb, ok := b.(LatencyBackend); ok {
			l.latencyBackends[i] = lb
		} else {
			l.latencyBackends[i] = nil
			l.allLatencyBackends = false
		}
	}
}

// Release 释放一个后端的连接计数（实现 LeastConnReleaser 接口）
// 与 leastConn.Release 逻辑完全相同
func (l *leastTime) Release(backend Backend) {
	l.connectionTracker.release(backend)
}
