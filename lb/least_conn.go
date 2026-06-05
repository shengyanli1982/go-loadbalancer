package lb

import "sync"

// leastConn 实现最少连接数负载均衡算法（支持加权）
// 对标 nginx ngx_http_upstream_least_conn_module：
//   - 整数交叉乘法比较 score（避免浮点精度问题）
//   - 平局时通过 rrIndex 轮询公平选择（nginx 用 SWRR，此处用均匀轮询）
//   - 使用 connCounts/weightCache 切片替代 map + 类型断言，O(1) 访问
type leastConn struct {
	mu                  sync.Mutex
	connections         map[string]int // 按地址索引的连接计数（用于 Release 和跨变化持久化）
	connCounts          []int          // 按位置索引的连接计数（Select 快速路径）
	weightCache         []int          // 按位置缓存的权重（避免重复类型断言）
	addrCache           []string       // 缓存后端地址（避免重复 Address() 接口调用）
	addrIndex           map[string]int // 地址到位置的映射（Release O(1) 查找）
	rrIndex             int64          // 全局轮询计数器，用于平局公平选择
	backendsFingerprint uint64         // 后端列表指纹，变化时清理过期条目
	backendsSlicePtr    uintptr        // 后端 slice 底层数组地址，用于快速缓存检测
	backendsSliceLen    int            // 后端 slice 长度，配合指针做快速缓存检测
}

// LeastConnReleaser 接口用于在请求完成后释放连接计数
type LeastConnReleaser interface {
	Release(backend Backend)
}

// NewLeastConn 创建最少连接数选择器
func NewLeastConn() Selector {
	return &leastConn{
		connections: make(map[string]int),
		addrIndex:   make(map[string]int),
	}
}

// Select 使用最少连接数算法选择一个后端
// 算法（对标 nginx ngx_http_upstream_get_least_conn_peer）：
//
//	第一轮：遍历所有后端，用整数交叉乘法比较 score
//	第二轮：当 tiedCount > 1 时，按 rrIndex % tiedCount 选择第 N 个 tied 候选者
//	最后递增选中后端的连接数
func (l *leastConn) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	l.mu.Lock()

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

	// 第一轮：使用 connCounts/weightCache 切片做 O(1) 访问
	bestIdx := 0
	bestConn := l.connCounts[0]
	bestWeight := l.weightCache[0]
	tiedCount := 1
	n := len(backends)

	for i := 1; i < n; i++ {
		conn := l.connCounts[i]
		weight := l.weightCache[i]

		if conn*bestWeight < bestConn*weight {
			bestIdx = i
			bestConn = conn
			bestWeight = weight
			tiedCount = 1
		} else if conn*bestWeight == bestConn*weight {
			tiedCount++
		}
	}

	// 第二轮：在 tied 候选者中公平选择（早期退出，平均 n/2 迭代）
	if tiedCount > 1 {
		target := int(l.rrIndex % int64(tiedCount))
		seen := 0
		for i := 0; i < n; i++ {
			conn := l.connCounts[i]
			weight := l.weightCache[i]
			if conn*bestWeight == bestConn*weight {
				if seen == target {
					bestIdx = i
					break
				}
				seen++
			}
		}
	}

	l.rrIndex++
	l.connCounts[bestIdx]++
	l.mu.Unlock()
	return backends[bestIdx]
}

func (l *leastConn) rebuildIndex(backends []Backend) {
	n := len(backends)

	// 保存旧的位置索引（避免 rebuild 覆盖后丢失数据）
	oldAddrCache := l.addrCache
	oldCounts := make([]int, len(l.connCounts))
	copy(oldCounts, l.connCounts)

	// 重建 addrCache 和 weightCache
	if cap(l.addrCache) >= n {
		l.addrCache = l.addrCache[:n]
	} else {
		l.addrCache = make([]string, n)
	}
	if cap(l.weightCache) >= n {
		l.weightCache = l.weightCache[:n]
	} else {
		l.weightCache = make([]int, n)
	}
	for i, b := range backends {
		l.addrCache[i] = b.Address()
		l.weightCache[i] = getWeight(b)
	}

	// 重建 addrIndex
	l.addrIndex = make(map[string]int, n)
	for i, addr := range l.addrCache {
		l.addrIndex[addr] = i
	}

	// 将 oldCounts 中仍存在于新集合的后端计数同步到 connections（持久化跨 rebuild 状态）
	for i := 0; i < len(oldAddrCache); i++ {
		if _, exists := l.addrIndex[oldAddrCache[i]]; exists {
			l.connections[oldAddrCache[i]] = oldCounts[i]
		}
	}

	// 从 connections 重建 connCounts（确保新增后端也有正确的计数）
	if cap(l.connCounts) >= n {
		l.connCounts = l.connCounts[:n]
	} else {
		l.connCounts = make([]int, n)
	}
	for i, addr := range l.addrCache {
		l.connCounts[i] = l.connections[addr]
	}

	// 清理已移除后端的过期条目（对标 nginx upstream 动态配置清理）
	for addr := range l.connections {
		if _, exists := l.addrIndex[addr]; !exists {
			delete(l.connections, addr)
		}
	}
}

// Release 释放一个后端的连接计数
func (l *leastConn) Release(backend Backend) {
	if backend == nil {
		return
	}
	addr := backend.Address()
	l.mu.Lock()
	defer l.mu.Unlock()

	if idx, ok := l.addrIndex[addr]; ok && l.connCounts[idx] > 0 {
		l.connCounts[idx]--
	}
	if conn, ok := l.connections[addr]; ok && conn > 0 {
		l.connections[addr] = conn - 1
	}
}
