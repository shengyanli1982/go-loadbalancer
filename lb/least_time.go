package lb

import "math"

// leastTime 实现延迟感知（Least Time）负载均衡算法（对标 Traefik v3.6）
//
// Score 公式: score = avgLatency * (1 + activeConns) / weight
// 选中 score 最小的后端；等权重时退化为比较 avgLatency * (1 + activeConns)
//
// 零分配快速路径设计（与 leastConn 相同模式）:
//   - Select() 不在快速路径上分配堆内存（connByIndex/weightCache/tiedIndices 均预分配复用）
//   - 单轮扫描 + tiedIndices 记录平局索引，用 rrIndex 公平选择
//   - 未实现 LatencyBackend 的后端视为「延迟未知」，取惩罚哨兵 math.Inf(1)，
//     仅在集合中不存在任何可观测后端时才被选中（此时退化为 RR 轮转）
//
// 性能优化（对标 P2C 预缓存模式）:
//   - 在 rebuildIndex 时缓存 LatencyBackend 类型断言结果（latencyBackends []LatencyBackend）
//   - Select 热路径直接使用缓存的接口指针，零类型断言开销
//   - 此优化将 per-backend per-Select 的接口满足检查降至零（原每次 Select 需 N 次完整 itab 查找）
//
// 线程安全：使用 sync.Mutex 保护所有可变状态
type leastTime struct {
	connectionTracker
	backends           []Backend        // 钉住后端 slice 底层数组，防 GC 回收后地址复用导致 fast path ABA（仅慢路径更新）
	latencyBackends    []LatencyBackend // 缓存的 LatencyBackend 接口（rebuildIndex 预断言，Select 零类型检查）
	allLatencyBackends bool             // 所有后端都实现了 LatencyBackend（Select 快速路径依据）
	cacheSnapshot
}

// NewLeastTime creates a latency-aware selector that returns a plain Selector with no Release.
//
// NewLeastTime 创建延迟感知选择器（对标 Traefik v3.6 Least Time）
func NewLeastTime() Selector {
	return &leastTime{
		connectionTracker: newConnectionTracker(),
	}
}

// Select 使用 Least Time 算法选择一个后端
//
// 算法:
//
//	单轮扫描：遍历所有后端，计算 score
//	score = avgLatency * (1 + activeConns) / weight （LatencyBackend 时）
//	score = math.Inf(1) * (1 + 0) / weight （非LatencyBackend：延迟未知，惩罚哨兵使其恒败于已观测后端）
//	等权重时退化为比较 avgLatency * (1 + activeConns)（省去一次除法）
//	遇到平局时记录到 tiedIndices，扫描结束后用 rrIndex % tieLen 选取
func (l *leastTime) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 快速路径：同一个 slice → 跳过 fingerprint 计算和索引重建
	ptr := backendsSlicePtr(backends)
	if !(ptr == l.slicePtr && len(backends) == l.sliceLen) {
		fp := computeWeightedFingerprint(backends)
		if fp != l.fingerprint || len(l.latencyBackends) == 0 {
			l.rebuildIndex(backends)
			l.fingerprint = fp
		} else {
			// fp 相同但 slice 已更换：addr+weight 派生的缓存（权重/连接索引）仍有效，
			// 但 latencyBackends 缓存的是实例行为，必须重绑到新实例，
			// 否则选择决策由旧实例的冻结指标驱动（见 refreshLatencyCache）。
			l.refreshLatencyCache(backends)
		}
		l.slicePtr = ptr
		l.sliceLen = len(backends)
		l.backends = backends
	}

	n := len(backends)

	// 确保 tiedIndices 长度足够（复用底层数组，无热路径分配）；
	// resizeSlice 返回 s[:n]，把 len 一并拉满，消除「按 cap 判断却按绝对下标写入」的越界隐患
	l.tiedIndices = resizeSlice(l.tiedIndices, n)

	var bestIdx int

	if l.hasUniformWeights {
		// 快速路径：所有权重相等 → 比较 avgLatency * (1+conns)，省去一次除法
		if l.allLatencyBackends {
			// 极速路径：所有后端均实现 LatencyBackend，无需 nil check
			// 内联接口调用，消除 latencyConns 函数调用开销
			lb0 := l.latencyBackends[0]
			bestScore := sanitizeScore(lb0.AverageLatency() * float64(1+lb0.ActiveConnections()))
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
			bestScore := sanitizeScore(lt0 * float64(1+lc0))
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
			bestScore := sanitizeScore(lb0.AverageLatency() * float64(1+lb0.ActiveConnections()) / float64(l.weightCache[0]))
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
			bestScore := sanitizeScore(lt0 * float64(1+lc0) / float64(l.weightCache[0]))
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
	return backends[bestIdx]
}

// sanitizeScore 将 NaN 评分归一为 +Inf，防止 NaN 在「取最小者胜」的比较域中垄断流量。
//
// 背景：AverageLatency() 的用户实现若做「0 个样本求均值」（0/0）就会返回 NaN。
// Select 仅用 < 与 == 比较，而 NaN 与任何值的 < 和 == 均为 false；若 bestScore 初值
// 被 NaN 污染，则其后所有后端都无法胜出，bestIdx 永远停在 0 → backends[0] 100% 垄断
// 流量且永不自愈。
//
// NaN 归一到 +Inf 与 latencyConns 对「未实现 LatencyBackend」后端返回的 +Inf 哨兵一致：
// 「无效/未知的延迟数据」统一落到比较域的最劣端，其余有限 score 的后端可正常胜出。
// 只在每个分支的 bestScore 初始化处调用一次，不在逐元素循环内调用（零热路径成本，可内联）；
// 循环内 backends[i]（i>0）若为 NaN，则 NaN<bestScore 与 NaN==bestScore 均 false 而被跳过、
// 永不入选——这是既有的良性行为，无需改动。
func sanitizeScore(v float64) float64 {
	if math.IsNaN(v) {
		return math.Inf(1)
	}
	return v
}

// latencyConns 从缓存的 LatencyBackend 接口提取延迟和活跃连接数
// 优化：使用 rebuildIndex 预断言的缓存接口，避免 Select 热路径类型断言
// nil 表示该后端未实现 LatencyBackend → 视为「延迟未知」，返回惩罚哨兵 math.Inf(1)，
// 使其恒败于任何有观测数据的后端，仅在集合中不存在任何可观测后端时才被选中。
// 连接数返回 0，使 (1+conns) 恒为乘法单位元 1，score 不被连接数扰动。
// 选用 Inf 而非 MaxFloat64 的理由：加权路径 score = latency*(1+conns)/weight 会按权重缩放有限哨兵，
// 导致无数据后端之间 score 互不相同、不再平局，退化为「最高权重恒胜」；
// 而 Inf 除以任意有限正权重仍为 Inf，故等权与加权两条路径上所有无数据后端 score 恒为 +Inf → 平局 → RR 退化。
// Select 仅用 < / == 比较：Inf < Inf 为 false、Inf == Inf 为 true，平局判定正确且无 NaN。
func (l *leastTime) latencyConns(i int) (float64, int) {
	if lb := l.latencyBackends[i]; lb != nil {
		return lb.AverageLatency(), lb.ActiveConnections()
	}
	return math.Inf(1), 0
}

// rebuildIndex 重建内部索引（后端列表变化时调用，已持有锁）
// 先委托给 connectionTracker 完成公共逻辑，再刷新 LatencyBackend 指标源缓存
func (l *leastTime) rebuildIndex(backends []Backend) {
	l.connectionTracker.rebuildIndex(backends)
	l.refreshLatencyCache(backends)
}

// refreshLatencyCache 重做 LatencyBackend 类型断言，把指标源重绑到当前 slice 的实例（已持有锁）。
//
// 两个调用点：
//   - rebuildIndex：成员集合或权重变化时随索引一并重建；
//   - Select 慢路径的 fp 匹配分支：computeWeightedFingerprint 只编码 addr+weight，
//     而 latencyBackends 缓存的是接口值（实例身份+行为）。调用方以同 addr+weight
//     重建实例并传入新 slice（README Caller contract 允许的用法）时指纹不变，
//     若不重绑，选择决策将永久由已被丢弃的旧实例的冻结延迟/连接数驱动，
//     违反「三个评分输入全部来自 backend 对象自身」与 +Inf 哨兵语义。
//
// 成本：慢路径已付出 O(n) 指纹计算，追加 O(n) 类型断言不改变量级；
// latencyBackends 是 mutex 保护的普通字段（非 RCU 快照），
// 可用 resizeSlice 复用容量实现零分配（每个槽位在循环内必然被覆写，无脏读）。
func (l *leastTime) refreshLatencyCache(backends []Backend) {
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
