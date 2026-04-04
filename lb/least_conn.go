package lb

import "sync"

// leastConn 实现最少连接数负载均衡算法
// 选择当前连接数最少的后端服务器来处理新请求
type leastConn struct {
	mu          sync.Mutex     // 互斥锁，保证并发安全
	connections map[string]int // 记录每个后端的当前连接数
}

// LeastConnReleaser 定义连接释放的接口
// 后端处理完请求后需要调用此接口释放连接
type LeastConnReleaser interface {
	// Release 释放指定后端的一个连接
	Release(backend Backend)
}

// NewLeastConn 创建新的最少连接数负载均衡选择器
func NewLeastConn() Selector {
	return &leastConn{
		connections: make(map[string]int),
	}
}

// Select 选择连接数最少的后端服务器
// 遍历所有后端，找出当前连接数最少的后端
func (l *leastConn) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	selected := backends[0]
	selectedAddr := selected.Address()
	minConn := l.connections[selectedAddr]

	// 遍历所有后端，找到连接数最少的
	for i := 1; i < len(backends); i++ {
		b := backends[i]
		addr := b.Address()
		conn := l.connections[addr]
		if conn < minConn {
			minConn = conn
			selected = b
			selectedAddr = addr
		}
	}

	// 选中的后端连接数加1
	l.connections[selectedAddr] = minConn + 1
	return selected
}

// Release 释放指定后端的一个连接
// 在后端处理完请求后调用此方法
func (l *leastConn) Release(backend Backend) {
	if backend == nil {
		return
	}
	addr := backend.Address()
	l.mu.Lock()
	defer l.mu.Unlock()
	// 减少连接数，但不低于0
	if conn, ok := l.connections[addr]; ok && conn > 0 {
		l.connections[addr] = conn - 1
	}
}
