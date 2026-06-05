package lb

// ipHash 实现 IP 哈希负载均衡算法
// 相同客户端 IP 始终路由到同一后端，适用于需要基于 IP 的会话保持场景
// 对标 nginx ngx_http_upstream_ip_hash_module
type ipHash struct{}

// NewIPHash 创建 IP 哈希选择器
func NewIPHash() HashSelector {
	return &ipHash{}
}

// SelectByHash 根据客户端 IP 的哈希值选择后端
// key 应为客户端 IP 地址的字节表示（如 net.IP 的字节切片）
// 使用 xxhash 对 key 取模后端数量，保证相同 key 始终映射到同一后端
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
