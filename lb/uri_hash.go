package lb

import "bytes"

// uriHash 实现 URI 哈希负载均衡算法
// 特点：相同 URI 请求路由到同一后端，提高缓存命中率
// 适用：需要按请求 URI 进行一致性哈希的场景
type uriHash struct {
	includeQuery bool // 是否包含查询参数
}

// URIHashOptions 配置选项
type URIHashOptions struct {
	IncludeQuery bool // 是否在哈希计算中包含查询参数
}

// NewURIHash 创建 URI 哈希选择器
func NewURIHash(opts *URIHashOptions) HashSelector {
	h := &uriHash{}
	if opts != nil {
		h.includeQuery = opts.IncludeQuery
	} else {
		h.includeQuery = true // 默认包含查询参数
	}
	return h
}

// SelectByHash 使用 URI 哈希算法选择一个后端
// 算法：对 URI（可选择包含或排除查询参数）进行哈希，取模后端数量
func (h *uriHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	hashKey := key
	// 如果不包含查询参数，则只取 ? 前面的路径部分
	if !h.includeQuery {
		idx := bytes.IndexByte(key, '?')
		if idx >= 0 {
			hashKey = key[:idx]
		}
	}

	h2 := hash64(hashKey)
	i := h2 % uint64(len(backends))
	return backends[i]
}
