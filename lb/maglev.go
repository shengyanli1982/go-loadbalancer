package lb

import (
	"sync"
)

// Maglev 算法默认表大小
const (
	DefaultMaglevTableSize = 65537
)

// maglev 实现 Maglev 负载均衡算法
// 一种基于查找表的一致性哈希算法，具有更好的分布均匀性
type maglev struct {
	mu        sync.RWMutex // 读写锁，保证并发安全
	table     []int        // 查找表，存储后端索引
	backends  []Backend    // 后端服务器列表
	tableSize int          // 表大小
	n         int          // 后端数量
}

// MaglevOptions 配置 Maglev 算法的选项
type MaglevOptions struct {
	TableSize int // 查找表大小
}

// MaglevSelector 定义 Maglev 选择器的接口
// 继承自 Selector 和 HashSelector
type MaglevSelector interface {
	Selector
	HashSelector
}

// NewMaglev 创建新的 Maglev 负载均衡选择器
// 如果未指定选项或表大小无效，使用默认值
func NewMaglev(opts *MaglevOptions) MaglevSelector {
	size := DefaultMaglevTableSize
	if opts != nil && opts.TableSize > 0 {
		size = opts.TableSize
	}
	return &maglev{
		tableSize: size,
	}
}

// Select 选择第一个后端（未实现真正的选择逻辑）
func (m *maglev) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	return backends[0]
}

// SelectByHash 根据哈希键选择后端服务器
// 使用 Maglev 算法通过查找表快速定位后端
func (m *maglev) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	// 尝试获取读锁检查表是否有效
	m.mu.RLock()
	tableValid := !m.needsRebuild(backends) && m.table != nil
	m.mu.RUnlock()

	// 如果表无效，需要重建
	if !tableValid {
		m.mu.Lock()
		if m.needsRebuild(backends) {
			m.buildTable(backends)
		}
		m.mu.Unlock()
	}

	// 计算哈希值并查表
	hash := hash64(key)
	idx := int(hash % uint64(m.tableSize))
	if idx < 0 {
		idx = -idx % m.tableSize
	}
	if idx >= len(m.table) {
		idx = idx % m.tableSize
	}
	if idx < 0 {
		idx = 0
	}

	// 从表中获取后端索引
	result := m.table[idx]
	if result >= 0 && result < len(backends) {
		return backends[result]
	}
	return backends[0]
}

// needsRebuild 检查是否需要重建查找表
// 当后端列表发生变化时需要重建
func (m *maglev) needsRebuild(backends []Backend) bool {
	if m.table == nil || len(m.backends) != len(backends) {
		return true
	}
	for i, b := range backends {
		if m.backends[i].Address() != b.Address() {
			return true
		}
	}
	return false
}

// buildTable 构建 Maglev 查找表
// 使用轮询和偏移算法为每个后端分配表中的位置
func (m *maglev) buildTable(backends []Backend) {
	m.backends = make([]Backend, len(backends))
	copy(m.backends, backends)
	m.n = len(backends)

	// 如果没有后端，清空表
	if m.n == 0 {
		m.table = nil
		return
	}

	// 初始化查找表，所有位置初始化为-1
	m.table = make([]int, m.tableSize)
	for i := range m.table {
		m.table[i] = -1
	}

	offset := make([]int, m.n) // 每个后端的偏移量
	skip := make([]bool, m.n)  // 标记后端是否已完成分配
	pos := 0

	// 填充查找表
	for filled := 0; filled < m.tableSize; {
		for i := 0; i < m.n; i++ {
			if skip[i] {
				continue
			}
			// 计算候选位置
			candidate := (offset[i] + pos) % m.tableSize
			if candidate < 0 {
				candidate = -candidate % m.tableSize
			}
			// 如果位置空闲，分配给当前后端
			if m.table[candidate] == -1 {
				m.table[candidate] = i
				filled++
				offset[i] = candidate
				if offset[i] >= m.tableSize {
					skip[i] = true
				}
			} else {
				// 位置已被占用，尝试下一个偏移
				offset[i]++
				if offset[i] >= m.tableSize {
					skip[i] = true
				}
			}
		}
		pos++
		// 防止无限循环
		if pos > m.tableSize*m.n {
			break
		}
	}
}
