package lb

// ipHash 实现 IP 哈希负载均衡算法
// 根据客户端 IP 地址计算哈希值，选择对应的后端服务器
type ipHash struct{}

// NewIPHash 创建新的 IP 哈希负载均衡选择器
func NewIPHash() HashSelector {
	return &ipHash{}
}

// SelectByHash 根据客户端 IP 的哈希值选择后端服务器
// 相同的客户端 IP 会始终路由到相同的后端服务器
func (h *ipHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}
	// 使用哈希值对后端数量取模，获取后端索引
	h2 := hash64(key)
	idx := h2 % uint64(len(backends))
	return backends[idx]
}
