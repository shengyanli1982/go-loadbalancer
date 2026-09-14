package lb

import "sync"

// connectionTracker 封装连接跟踪类负载均衡算法的公共状态和操作。
// leastConn、activeRequestBias、leastTime 通过嵌入复用此结构，
// 消除 rebuildIndex/hasUniformWeights 检测的重复实现。
// Release 不在此共享：leastConn/activeRequestBias 递减计数后还须修复自身堆序，
// 共享版本覆盖不了这一步，故由各自内联实现。
//
// Select 方法使用 tiedIndices 做平局缓冲，rrIndex 做平局公平轮询。
// 嵌入方可直接通过 Go 字段 promotion 访问所有字段（如 l.connByIndex[0]）。
type connectionTracker struct {
	mu                sync.Mutex
	connByAddr        map[string]int // 按地址索引的连接计数（Release 使用，跨 rebuild 持久化）
	connByIndex       []int          // 按位置索引的连接计数（Select 快速路径，O(1)）
	weightCache       []int          // 按位置缓存的后端权重（避免重复 getWeight 类型断言）
	addrCache         []string       // 缓存后端地址（避免重复 Address() 调用）
	addrIndex         map[string]int // 地址到位置的映射（Release O(1) 查找）
	tiedIndices       []int          // 平局索引缓冲区（Select 复用，无热路径分配）
	rrIndex           uint64         // 全局轮询计数器，用于平局公平选择
	hasUniformWeights bool           // 所有权重是否相等（Select 快速路径依据）
}

// newConnectionTracker 创建一个内部 map 已初始化的 connectionTracker。
// 返回值类型而非指针：嵌入方写 connectionTracker: newConnectionTracker()，
// 避免「对返回值解引用即复制含 sync.Mutex 的结构体」这一 copylocks 反模式
// （旧的解引用写法虽未被 vet 报错，但任何后续再复制都会静默产生锁副本）。
// 嵌入的是值字段，在指针接收者方法内 t.connectionTracker 可寻址，
// rebuildIndex 等指针接收者方法仍能自动取址调用，调用点无需改动。
func newConnectionTracker() connectionTracker {
	return connectionTracker{
		connByAddr: make(map[string]int),
		addrIndex:  make(map[string]int),
	}
}

// rebuildIndex 重建索引（后端列表变化时调用，已持有锁）。
//
// 步骤顺序：旧计数按地址落盘 connByAddr → 重建 addrCache/weightCache →
// 检测 hasUniformWeights → clear 后重填 addrIndex → 从 connByAddr 回填 connByIndex →
// 清理过期条目。
//
// 顺序为什么是关键（改动前必读）：第一步的落盘必须发生在 addrCache 被 resize/覆写**之前**。
// resizeSlice 在 cap 足够时返回同一底层数组的别名，若先保存旧 header 再重建 addrCache，
// 新地址写入会同步写穿旧 header 的前 min(n, oldLen) 个元素，落盘退化为
// connByAddr[新地址i] = 旧计数[i] —— 按**位置**继承计数而非按**地址**迁移。
// 触发条件是 cap(addrCache) >= 新 n，即所有"新集合长度 <= 历史最大长度"的成员变更
// （头部/中部移除、同长度整体替换、同集合换序、缩容后再扩容）；唯一侥幸安全的路径是
// n 超过历史最大 cap（resizeSlice 走 make 新分配）。
// 污染写入 connByAddr 这一跨 rebuild 持久层后会被后续重建继续搬运、不自愈，
// 使被抬高计数的后端遭系统性饿死（幽灵计数没有对应的 Release 能消除）。
// 由 TestConnectionTrackerRebuild_MigratesCountsByAddressNotPosition 回归钉住。
func (t *connectionTracker) rebuildIndex(backends []Backend) {
	n := len(backends)

	// 步骤1：把旧的 (地址 → 计数) 无条件落盘到 connByAddr（跨 rebuild 持久层）。
	// 必须排在步骤2 之前：resizeSlice 在 cap 足够时返回同一底层数组的别名，
	// 先存旧 header 再覆写 addrCache 会被写穿，落盘退化为按位置继承（详见函数注释）。
	//
	// 此处不过滤"地址是否仍在新集合中"：步骤6 会统一删除所有不在新 addrIndex 中的
	// 条目，过滤只是多花一次 map 查找。
	//
	// 依赖的入口不变式：len(t.addrCache) == len(t.connByIndex)。两者只在本函数内被
	// resizeSlice 到同一个 n，其余路径（各嵌入方的 Release/Select）只做原地增减不改
	// 长度；首次调用时两者均为 nil（len 0 == len 0）。
	for i, addr := range t.addrCache {
		t.connByAddr[addr] = t.connByIndex[i]
	}

	// 步骤2：重建 addrCache 和 weightCache
	t.addrCache = resizeSlice(t.addrCache, n)
	t.weightCache = resizeSlice(t.weightCache, n)
	for i, b := range backends {
		t.addrCache[i] = b.Address()
		t.weightCache[i] = getWeight(b)
	}

	// 步骤3：检测是否所有权重相等（Select 快速路径依据）
	t.hasUniformWeights = allWeightsEqual(t.weightCache)

	// 步骤4：重建 addrIndex（clear 后重填，复用已有 map 的桶存储，避免每次 rebuild 重新分配）
	clear(t.addrIndex)
	for i, addr := range t.addrCache {
		t.addrIndex[addr] = i
	}

	// 步骤5：从 connByAddr 回填 connByIndex（确保新增后端也有正确的计数）
	t.connByIndex = resizeSlice(t.connByIndex, n)
	for i, addr := range t.addrCache {
		t.connByIndex[i] = t.connByAddr[addr]
	}

	// 步骤6：清理已移除后端的过期条目（对标 nginx upstream 动态配置清理）
	for addr := range t.connByAddr {
		if _, exists := t.addrIndex[addr]; !exists {
			delete(t.connByAddr, addr)
		}
	}
}
