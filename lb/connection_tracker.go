package lb

import "sync"

// connectionTracker 封装连接跟踪类负载均衡算法的公共状态和操作。
// leastConn、activeRequestBias、leastTime 通过嵌入复用此结构，
// 消除 rebuildIndex/Release/hasUniformWeights 检测的重复实现。
//
// Select 方法使用 tiedIndices 做平局缓冲，rrIndex 做平局公平轮询。
// 嵌入方可直接通过 Go 字段 promotion 访问所有字段（如 l.connByIndex[0]）。
type connectionTracker struct {
	mu               sync.Mutex
	connByAddr       map[string]int // 按地址索引的连接计数（Release 使用，跨 rebuild 持久化）
	connByIndex      []int          // 按位置索引的连接计数（Select 快速路径，O(1)）
	weightCache      []int          // 按位置缓存的后端权重（避免重复 getWeight 类型断言）
	addrCache        []string       // 缓存后端地址（避免重复 Address() 调用）
	addrIndex        map[string]int // 地址到位置的映射（Release O(1) 查找）
	tiedIndices      []int          // 平局索引缓冲区（Select 复用，无热路径分配）
	rrIndex          uint64         // 全局轮询计数器，用于平局公平选择
	hasUniformWeights bool           // 所有权重是否相等（Select 快速路径依据）
}

// newConnectionTracker 创建 initialized connectionTracker
func newConnectionTracker() *connectionTracker {
	return &connectionTracker{
		connByAddr: make(map[string]int),
		addrIndex:  make(map[string]int),
	}
}

// rebuildIndex 重建索引（后端列表变化时调用，已持有锁）。
// 保存旧计数 → 重建缓存 → 检测 hasUniformWeights → 同步连接计数 → 清理过期条目。
func (t *connectionTracker) rebuildIndex(backends []Backend) {
	n := len(backends)

	// 保存旧的位置索引（避免 rebuild 覆盖后丢失数据）
	oldAddrCache := t.addrCache
	oldCounts := make([]int, len(t.connByIndex))
	copy(oldCounts, t.connByIndex)

	// 重建 addrCache 和 weightCache
	t.addrCache = resizeSlice(t.addrCache, n)
	t.weightCache = resizeSlice(t.weightCache, n)
	for i, b := range backends {
		t.addrCache[i] = b.Address()
		t.weightCache[i] = getWeight(b)
	}

	// 检测是否所有权重相等（Select 快速路径依据）
	t.hasUniformWeights = allWeightsEqual(t.weightCache)

	// 重建 addrIndex
	t.addrIndex = make(map[string]int, n)
	for i, addr := range t.addrCache {
		t.addrIndex[addr] = i
	}

	// 将 oldCounts 中仍存在于新集合的后端计数同步到 connByAddr（持久化跨 rebuild 状态）
	for i := 0; i < len(oldAddrCache); i++ {
		if _, exists := t.addrIndex[oldAddrCache[i]]; exists {
			t.connByAddr[oldAddrCache[i]] = oldCounts[i]
		}
	}

	// 从 connByAddr 重建 connByIndex（确保新增后端也有正确的计数）
	t.connByIndex = resizeSlice(t.connByIndex, n)
	for i, addr := range t.addrCache {
		t.connByIndex[i] = t.connByAddr[addr]
	}

	// 清理已移除后端的过期条目（对标 nginx upstream 动态配置清理）
	for addr := range t.connByAddr {
		if _, exists := t.addrIndex[addr]; !exists {
			delete(t.connByAddr, addr)
		}
	}
}

// release 释放指定后端的连接计数（leastConn/activeRequestBias/leastTime 公共行为）
func (t *connectionTracker) release(backend Backend) {
	if backend == nil {
		return
	}
	addr := backend.Address()
	t.mu.Lock()
	defer t.mu.Unlock()

	if idx, ok := t.addrIndex[addr]; ok && t.connByIndex[idx] > 0 {
		t.connByIndex[idx]--
	}
	if conn, ok := t.connByAddr[addr]; ok && conn > 0 {
		t.connByAddr[addr] = conn - 1
	}
}
