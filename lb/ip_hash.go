package lb

// ipHash 实现 IP 哈希负载均衡算法
// 特点：相同客户端 IP 始终路由到同一后端，实现会话保持
// 适用：需要基于客户端 IP 进行会话保持的场景
type ipHash struct{}

// NewIPHash 创建 IP 哈希选择器
func NewIPHash() HashSelector {
	return &ipHash{}
}

// SelectByHash 使用 IP 哈希算法选择一个后端
// 算法：对客户端 IP 进行哈希，取模后端数量
func (h *ipHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}
	h2 := hash64(key)
	idx := h2 % uint64(len(backends))
	return backends[idx]
}
