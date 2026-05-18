package lb

import "sync"

// leastConn 实现最少连接数负载均衡算法
// 特点：动态适应后端负载，选择当前连接数最少的后端
// 适用：后端处理时间差异较大的场景
type leastConn struct {
	mu                  sync.Mutex
	connections         map[string]int   // 记录每个后端的当前连接数
	rrCounter           map[string]int64 // 轮询计数器，用于连接数相同时的决策
	backendsFingerprint uint64           // 后端列表指纹，用于快速检测变化
	hasWeighted         bool             // 是否有加权后端
}

// LeastConnReleaser 接口，用于在请求完成后释放连接
// 使用示例：
//
//	be := selector.Select(backends)
//	if releaser, ok := selector.(lb.LeastConnReleaser); ok {
//	    releaser.Release(be)
//	}
type LeastConnReleaser interface {
	Release(backend Backend)
}

// NewLeastConn 创建最少连接数选择器
func NewLeastConn() Selector {
	return &leastConn{
		connections: make(map[string]int),
		rrCounter:   make(map[string]int64),
	}
}

// Select 使用最少连接数算法选择一个后端
// 算法：选择当前连接数最少的后端，连接数相同时使用轮询决策
// 优化：使用后端指纹缓存，仅在后端列表变化时清理过期条目
func (l *leastConn) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 使用指纹快速检测后端列表是否变化
	fp := computeBackendsFingerprint(backends)
	if fp != l.backendsFingerprint {
		l.cleanupStaleEntries(backends)
		l.backendsFingerprint = fp
	}

	selected := backends[0]
	selectedAddr := selected.Address()

	minScore := l.computeScore(selectedAddr, selected)

	// 遍历找到连接数最少的后端
	for i := 1; i < len(backends); i++ {
		b := backends[i]
		addr := b.Address()
		score := l.computeScore(addr, b)
		if score < minScore {
			minScore = score
			selected = b
			selectedAddr = addr
		} else if score == minScore {
			// 连接数相同时，使用轮询计数器决定
			l.rrCounter[addr]++
			l.rrCounter[selectedAddr]++
			if l.rrCounter[addr] < l.rrCounter[selectedAddr] {
				minScore = score
				selected = b
				selectedAddr = addr
			}
		}
	}

	l.connections[selectedAddr]++
	return selected
}

// computeScore 计算后端的评分
// 评分 = 连接数 / 权重（如果有权重）
func (l *leastConn) computeScore(addr string, b Backend) float64 {
	conn := l.connections[addr]
	wb, ok := b.(WeightedBackend)
	if !ok {
		return float64(conn)
	}
	w := wb.Weight()
	if w <= 0 {
		w = 1
	}
	return float64(conn) / float64(w)
}

// cleanupStaleEntries 清理不再存在后端的连接记录
func (l *leastConn) cleanupStaleEntries(backends []Backend) {
	active := make(map[string]struct{}, len(backends))
	for _, b := range backends {
		active[b.Address()] = struct{}{}
	}
	for addr := range l.connections {
		if _, ok := active[addr]; !ok {
			delete(l.connections, addr)
		}
	}
	for addr := range l.rrCounter {
		if _, ok := active[addr]; !ok {
			delete(l.rrCounter, addr)
		}
	}
}

// Release 释放一个后端的连接
// 在请求完成后调用此方法，减小该后端的连接计数
func (l *leastConn) Release(backend Backend) {
	if backend == nil {
		return
	}
	addr := backend.Address()
	l.mu.Lock()
	defer l.mu.Unlock()
	if conn, ok := l.connections[addr]; ok && conn > 0 {
		l.connections[addr] = conn - 1
	}
}
