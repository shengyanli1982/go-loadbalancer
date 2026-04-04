package lb

// uriHash 实现 URI 哈希负载均衡算法
// 根据请求 URI 计算哈希值，选择对应的后端服务器
type uriHash struct {
	includeQuery bool // 是否在哈希计算中包含查询参数
}

// URIHashOptions 配置 URI 哈希算法的选项
type URIHashOptions struct {
	IncludeQuery bool // 是否包含查询参数
}

// NewURIHash 创建新的 URI 哈希负载均衡选择器
func NewURIHash(opts *URIHashOptions) HashSelector {
	h := &uriHash{}
	if opts != nil {
		h.includeQuery = opts.IncludeQuery
	}
	return h
}

// SelectByHash 根据请求 URI 的哈希值选择后端服务器
// 相同的 URI 会始终路由到相同的后端服务器
func (h *uriHash) SelectByHash(backends []Backend, key []byte) Backend {
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
