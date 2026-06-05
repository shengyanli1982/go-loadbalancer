package lb

import "bytes"

// uriHash 实现 URI 哈希负载均衡算法
// 相同 URI 请求始终路由到同一后端，提高后端缓存命中率
// 可选择包含或排除查询参数进行哈希计算
type uriHash struct {
	includeQuery bool // 是否在哈希计算中包含查询参数（?key=value）
}

// URIHashOptions 配置 URI 哈希选择器的选项
type URIHashOptions struct {
	IncludeQuery bool // true: 包含查询参数；false: 仅对路径部分哈希
}

// NewURIHash 创建 URI 哈希选择器
// opts 为 nil 时默认包含查询参数
func NewURIHash(opts *URIHashOptions) HashSelector {
	h := &uriHash{}
	if opts != nil {
		h.includeQuery = opts.IncludeQuery
	} else {
		h.includeQuery = true // 默认包含查询参数
	}
	return h
}

// SelectByHash 根据 URI 的哈希值选择后端
// key 应为请求 URI 的字节表示（如 []byte("/api/users?page=1")）
// 当 includeQuery 为 false 时，自动截取 ? 前的路径部分进行哈希
func (h *uriHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	hashKey := key
	if !h.includeQuery {
		if idx := bytes.IndexByte(key, '?'); idx >= 0 {
			hashKey = key[:idx]
		}
	}

	h2 := hash64(hashKey)
	i := h2 % uint64(len(backends))
	return backends[i]
}
