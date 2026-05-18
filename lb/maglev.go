package lb

import (
	"encoding/binary"
	"sync"
)

const (
	DefaultMaglevTableSize = 65537 // Maglev 表默认大小（质数）
)

// maglev 实现 Maglev 一致性哈希算法
// 特点：Google 论文实现，O(1) 查找速度，空间效率高
// 原理：使用查找表（lookup table）实现快速路由
type maglev struct {
	mu        sync.RWMutex
	table     []int     // 查找表，大小为 tableSize
	backends  []Backend // 缓存后端列表
	tableSize int       // 表大小
	n         int       // 后端数量
}

// MaglevOptions 配置选项
type MaglevOptions struct {
	TableSize int // 查找表大小，应为质数以获得更好的分布
}

// MaglevSelector 接口，同时支持 Select 和 SelectByHash
type MaglevSelector interface {
	Selector
	HashSelector
}

// NewMaglev 创建 Maglev 选择器
func NewMaglev(opts *MaglevOptions) MaglevSelector {
	size := DefaultMaglevTableSize
	if opts != nil && opts.TableSize > 0 {
		size = opts.TableSize
	}
	return &maglev{
		tableSize: size,
	}
}

// Select 随机选择一个后端（使用 Maglev 算法）
// 使用随机 key 调用 SelectByHash
func (m *maglev) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	rng := globalRNG()
	rng.mu.Lock()
	randomKey := rng.rng.Int63()
	rng.mu.Unlock()
	var keyBuf [8]byte
	binary.BigEndian.PutUint64(keyBuf[:], uint64(randomKey))
	return m.SelectByHash(backends, keyBuf[:])
}

// SelectByHash 使用 Maglev 算法选择一个后端
// 算法：
// 1. 计算 key 的哈希值
// 2. 取模表大小得到索引
// 3. 从查找表中获取对应的后端索引
// 优化：使用字符串比较检测后端变化，仅在变化时重建查找表
func (m *maglev) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	// 快速路径：表已构建且后端未变化
	m.mu.RLock()
	if m.table != nil && !m.needsRebuild(backends) {
		h := hash64(key)
		idx := h % uint64(m.tableSize)
		result := m.table[idx]
		m.mu.RUnlock()
		if result >= 0 && result < len(backends) {
			return backends[result]
		}
		return backends[0]
	}
	m.mu.RUnlock()

	// 慢速路径：需要重建查找表
	m.mu.Lock()
	if m.needsRebuild(backends) {
		m.buildTable(backends)
	}
	h := hash64(key)
	idx := h % uint64(m.tableSize)
	result := m.table[idx]
	m.mu.Unlock()
	if result >= 0 && result < len(backends) {
		return backends[result]
	}
	return backends[0]
}

// needsRebuild 检测是否需要重建查找表
// 比较后端列表长度和每个后端的地址
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
// 算法：为每个后端计算 offset 和 skip，使用轮询填充算法
// 参考 Google 论文 "Maglev: A Fast and Reliable Software Network Load Balancer"
func (m *maglev) buildTable(backends []Backend) {
	m.backends = make([]Backend, len(backends))
	copy(m.backends, backends)
	m.n = len(backends)

	if m.n == 0 {
		m.table = nil
		return
	}

	m.table = make([]int, m.tableSize)
	for i := range m.table {
		m.table[i] = -1
	}

	// 为每个后端计算 offset 和 skip
	offsets := make([]int, m.n)
	skips := make([]int, m.n)
	for i, b := range backends {
		offsets[i] = int(hash64([]byte("offset:"+b.Address())) % uint64(m.tableSize))
		skips[i] = int(hash64([]byte("skip:"+b.Address()))%uint64(m.tableSize-1)) + 1
	}

	// 轮询填充算法
	next := make([]int, m.n)

	for filled := 0; filled < m.tableSize; {
		for i := 0; i < m.n; i++ {
			c := (offsets[i] + next[i]*skips[i]) % m.tableSize
			next[i]++
			if m.table[c] < 0 {
				m.table[c] = i
				filled++
				if filled >= m.tableSize {
					break
				}
			}
		}
	}
}
