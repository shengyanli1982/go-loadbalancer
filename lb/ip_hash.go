package lb

// ipHash 实现 IP 哈希负载均衡算法
// 相同客户端 IP 始终路由到同一后端，适用于需要基于 IP 的会话保持场景
// 对标 nginx ngx_http_upstream_ip_hash_module
type ipHash struct{}

// NewIPHash creates an IP-hash selector.
//
// NewIPHash 创建 IP 哈希选择器
func NewIPHash() HashSelector {
	return &ipHash{}
}

// SelectByHash 根据客户端 IP 的哈希值选择后端
// key 应为客户端 IP 地址的字节表示（如 net.IP 的字节切片）
// 使用 xxhash 对 key 取模后端数量，保证相同 key 始终映射到同一后端
//
// 注意 net.IP 的双重表示陷阱：同一 IPv4 地址存在 4 字节（To4() 结果）与
// 16 字节（ParseIP 等产生的 4-in-6 形式）两种合法字节表示，经 String()
// 转文本后再取字节又是第三种形态。哈希对字节内容敏感：混用不同来源的
// 表示会使同一客户端 IP 在不同调用点算出不同哈希，被路由到不同后端，
// 会话保持静默失效（无任何报错）。调用方必须在传入前统一规范化 key
// （如一律使用 ip.String() 的字节，或一律 To4() 失败再 To16()）；
// 本库不做该规范化，也不校验 key 的形态一致性。
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
