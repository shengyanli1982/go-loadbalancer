package lb

import (
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// ringHash 实现一致性哈希（Ring Hash/Consistent Hash）算法
// 特点：后端节点变化时，只影响少量请求的路由，最小化迁移
// 原理：将后端映射到哈希环上，使用虚拟节点提高分布均匀性
type ringHash struct {
	mu                  sync.RWMutex
	ring                []uint64           // 哈希环，存储虚拟节点的哈希值（有序）
	backends            []Backend          // 缓存后端列表
	nodeMap             map[uint64]Backend // 哈希值到后端的映射
	ringSize            int                // 哈希环大小
	virtualNodes        int                // 虚拟节点数量
	backendsFingerprint uint64             // 后端列表指纹，用于快速检测变化
}

// RingHashOptions 配置选项
type RingHashOptions struct {
	RingSize     int // 哈希环大小（已废弃，使用默认值）
	VirtualNodes int // 虚拟节点数量，越多分布越均匀，但占用更多内存
}

// RingHashSelector 接口，同时支持 Select 和 SelectByHash
type RingHashSelector interface {
	Selector
	HashSelector
}

// NewRingHash 创建一致性哈希选择器
func NewRingHash(opts *RingHashOptions) RingHashSelector {
	r := &ringHash{
		ringSize:     DefaultRingSize,
		virtualNodes: DefaultVirtualNodes,
		nodeMap:      make(map[uint64]Backend),
	}
	if opts != nil {
		if opts.RingSize >= MinRingSize && opts.RingSize <= MaxRingSize {
			r.ringSize = opts.RingSize
		}
		if opts.VirtualNodes > 0 {
			r.virtualNodes = opts.VirtualNodes
		}
	}
	return r
}

// Select 随机选择一个后端（使用一致性哈希）
// 使用随机 key 调用 SelectByHash
func (r *ringHash) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	rng := globalRNG()
	rng.mu.Lock()
	randomKey := rng.rng.Int63()
	rng.mu.Unlock()
	var keyBuf [8]byte
	binary.BigEndian.PutUint64(keyBuf[:], uint64(randomKey))
	return r.SelectByHash(backends, keyBuf[:])
}

// SelectByHash 使用一致性哈希选择一个后端
// 算法：
// 1. 计算 key 的哈希值
// 2. 在哈希环上二分查找第一个大于等于该哈希值的位置
// 3. 返回该位置对应的后端
// 优化：使用后端指纹缓存，仅在后端列表变化时重建哈希环
func (r *ringHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	fp := computeBackendsFingerprint(backends)

	// 快速路径：指纹匹配且环已构建，直接查找
	r.mu.RLock()
	if fp == r.backendsFingerprint && len(r.ring) > 0 {
		h := hash64(key)
		idx := sort.Search(len(r.ring), func(i int) bool {
			return r.ring[i] >= h
		})
		if idx >= len(r.ring) {
			idx = 0
		}
		result := r.nodeMap[r.ring[idx]]
		r.mu.RUnlock()
		return result
	}
	r.mu.RUnlock()

	// 慢速路径：需要重建哈希环
	r.mu.Lock()
	if fp != r.backendsFingerprint || len(r.ring) == 0 {
		r.buildRing(backends)
		r.backendsFingerprint = fp
	}
	h := hash64(key)
	idx := sort.Search(len(r.ring), func(i int) bool {
		return r.ring[i] >= h
	})
	if idx >= len(r.ring) {
		idx = 0
	}
	result := r.nodeMap[r.ring[idx]]
	r.mu.Unlock()
	return result
}

// buildRing 构建哈希环
// 为每个后端创建 virtualNodes 个虚拟节点，将它们添加到环上
func (r *ringHash) buildRing(backends []Backend) {
	r.backends = make([]Backend, len(backends))
	copy(r.backends, backends)

	r.ring = make([]uint64, 0, len(backends)*r.virtualNodes)
	r.nodeMap = make(map[uint64]Backend)

	// 为每个后端创建虚拟节点
	for _, b := range backends {
		for i := 0; i < r.virtualNodes; i++ {
			nodeKey := fmt.Sprintf("%s:%d", b.Address(), i)
			h := hash64String(nodeKey)
			// 避免哈希冲突
			if _, exists := r.nodeMap[h]; !exists {
				r.ring = append(r.ring, h)
				r.nodeMap[h] = b
			}
		}
	}

	// 对环进行排序，便于二分查找
	sort.Slice(r.ring, func(i, j int) bool {
		return r.ring[i] < r.ring[j]
	})
}
