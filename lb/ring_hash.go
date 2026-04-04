package lb

import (
	"sort"
)

// 虚拟节点数量，用于增加哈希分布的均匀性
const (
	VirtualNodes = 100
)

// ringHash 实现一致性哈希负载均衡算法（也称为环形哈希）
// 通过将后端映射到哈希环上的位置，实现负载的分发
type ringHash struct {
	ring     []uint64           // 哈希环，存储排序后的哈希值
	backends []Backend          // 后端服务器列表
	nodeMap  map[uint64]Backend // 哈希值到后端的映射
	ringSize int                // 哈希环大小
}

// RingHashOptions 配置一致性哈希算法的选项
type RingHashOptions struct {
	RingSize     int // 哈希环大小
	VirtualNodes int // 每个后端的虚拟节点数量
}

// RingHashSelector 定义一致性哈希选择器的接口
// 继承自 Selector 和 HashSelector
type RingHashSelector interface {
	Selector
	HashSelector
}

// NewRingHash 创建新的一致性哈希负载均衡选择器
// 如果未指定选项或选项值无效，使用默认值
func NewRingHash(opts *RingHashOptions) RingHashSelector {
	r := &ringHash{
		ringSize: DefaultRingSize,
		nodeMap:  make(map[uint64]Backend),
	}
	// 验证并设置自定义环大小
	if opts != nil && opts.RingSize >= MinRingSize && opts.RingSize <= MaxRingSize {
		r.ringSize = opts.RingSize
	}
	return r
}

// Select 选择第一个后端（未实现真正的选择逻辑）
func (r *ringHash) Select(backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	return backends[0]
}

// SelectByHash 根据哈希键选择后端服务器
// 如果后端列表发生变化或哈希环为空，会重新构建哈希环
func (r *ringHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	// 如果需要，重新构建哈希环
	if len(r.ring) == 0 || !r.backendsEqual(backends) {
		r.buildRing(backends)
	}

	// 计算哈希值并查找对应位置
	h := hash64(key)
	idx := sort.Search(len(r.ring), func(i int) bool {
		return r.ring[i] >= h
	})

	// 环形结构，如果超出范围则从0开始
	if idx >= len(r.ring) {
		idx = 0
	}

	return r.nodeMap[r.ring[idx]]
}

// buildRing 构建一致性哈希环
// 为每个后端创建多个虚拟节点，使哈希分布更加均匀
func (r *ringHash) buildRing(backends []Backend) {
	r.backends = make([]Backend, len(backends))
	copy(r.backends, backends)

	r.ring = make([]uint64, 0, len(backends)*VirtualNodes)
	r.nodeMap = make(map[uint64]Backend)

	// 为每个后端创建虚拟节点
	for _, b := range backends {
		for i := 0; i < VirtualNodes; i++ {
			// 使用后端地址和虚拟节点索引生成唯一哈希键
			nodeKey := hash64([]byte(b.Address() + ":" + string(rune(i))))
			r.ring = append(r.ring, nodeKey)
			r.nodeMap[nodeKey] = b
		}
	}

	// 对哈希环进行排序
	sort.Slice(r.ring, func(i, j int) bool {
		return r.ring[i] < r.ring[j]
	})
}

// backendsEqual 检查后端列表是否与之前相同
func (r *ringHash) backendsEqual(backends []Backend) bool {
	if len(backends) != len(r.backends) {
		return false
	}
	for i, b := range backends {
		if b.Address() != r.backends[i].Address() {
			return false
		}
	}
	return true
}
